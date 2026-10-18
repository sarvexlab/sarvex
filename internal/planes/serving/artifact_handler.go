// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	servingpkg "github.com/sarvexlab/sarvex/internal/pkg/serving"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
)

// ArtifactRoute is the public path surface artifacts are served under. It is
// outside the signed, negotiated UCP route group: a surface's fetcher (Google
// reading a feed) is not a UCP agent, so it presents no signature and no
// store id. The only thing it holds is the artifact's unguessable token.
const ArtifactRoute = "/artifacts/:file"

// ArtifactEndpoint is the rate-limit key for ArtifactRoute.
const ArtifactEndpoint = "GET /artifacts/:file"

// ArtifactHandler serves surface artifacts straight from the serving cache.
//
// It reads only the cache, never a store or the control plane, so it keeps the
// serving plane's discovery/read invariant. It resolves no tenant: the token
// maps to bytes and nothing else, and the fetch is reported on the bus for the
// sync plane — which owns the artifact record — to attribute.
type ArtifactHandler struct {
	artifacts servingpkg.ArtifactReader
	bus       eventbus.EventBus
	limiter   RateLimiter
	log       *slog.Logger
}

// NewArtifactHandler constructs the handler. bus may be nil (no fetch events);
// limiter may be nil (no rate limit — tests only).
func NewArtifactHandler(artifacts servingpkg.ArtifactReader, bus eventbus.EventBus, limiter RateLimiter, log *slog.Logger) *ArtifactHandler {
	return &ArtifactHandler{artifacts: artifacts, bus: bus, limiter: limiter, log: log}
}

// allowed applies two buckets before any cache read: one per token, so a
// single feed cannot be hammered, and one per client address, so guessing
// tokens is throttled long before the 192-bit space matters. The limiter's
// first argument is a key space, not a tenant: this route resolves none.
func (h *ArtifactHandler) allowed(c echo.Context, token string) bool {
	if h.limiter == nil {
		return true
	}
	ctx := c.Request().Context()
	return h.limiter.Allowed(ctx, "artifact-token:"+token, ArtifactEndpoint) &&
		h.limiter.Allowed(ctx, "artifact-ip:"+c.RealIP(), ArtifactEndpoint)
}

// Get serves GET /artifacts/{token}[.ext].
func (h *ArtifactHandler) Get(c echo.Context) error {
	file := c.Param("file")
	token := file
	if i := strings.IndexByte(file, '.'); i >= 0 {
		token = file[:i]
	}
	if token == "" {
		return c.NoContent(http.StatusNotFound)
	}

	if !h.allowed(c, token) {
		c.Response().Header().Set("Retry-After", "1")
		return c.NoContent(http.StatusTooManyRequests)
	}

	a, err := h.artifacts.GetArtifact(c.Request().Context(), token)
	if errors.Is(err, servingpkg.ErrNotFound) {
		return c.NoContent(http.StatusNotFound)
	}
	if err != nil {
		h.log.Error("artifact read failed", "error", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	h.reportFetch(c.Request().Context(), token)
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.Blob(http.StatusOK, a.ContentType, a.Body)
}

// reportFetch is fire-and-forget: a lost event costs one verification signal
// that the next fetch re-sends.
func (h *ArtifactHandler) reportFetch(ctx context.Context, token string) {
	if h.bus == nil {
		return
	}
	evt := &artifactFetchedEvent{token: token, at: time.Now().UTC()}
	if err := h.bus.Publish(context.WithoutCancel(ctx), evt); err != nil {
		h.log.Warn("failed to publish artifact fetch", "error", err)
	}
}

// artifactFetchedEvent carries the token as its payload and no tenant ids —
// see ucp.EventSurfaceArtifactFetched.
type artifactFetchedEvent struct {
	token string
	at    time.Time
}

func (e *artifactFetchedEvent) EventType() string     { return ucp.EventSurfaceArtifactFetched }
func (e *artifactFetchedEvent) MerchantID() string    { return "" }
func (e *artifactFetchedEvent) StoreID() string       { return "" }
func (e *artifactFetchedEvent) OccurredAt() time.Time { return e.at }
func (e *artifactFetchedEvent) Payload() []byte       { return []byte(`{"token":"` + e.token + `"}`) }
