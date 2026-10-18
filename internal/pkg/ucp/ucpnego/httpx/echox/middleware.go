// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package echox provides an Echo v4 adapter for the UCP business
// negotiator.
//
// Usage:
//
//	e := echo.New()
//	neg, _ := httpx.NewBusinessNegotiator(own, source,
//	    httpx.WithContinueURL(func(r *http.Request) string {
//	        return "https://merchant.example.com" + r.URL.Path
//	    }),
//	)
//
//	// Apply to a group:
//	ucp := e.Group("/ucp", echox.Middleware(neg))
//	ucp.POST("/checkout-sessions", createCheckout)
//
//	// Or to a single route:
//	e.POST("/checkout-sessions", createCheckout, echox.Middleware(neg))
//
//	// Inside the handler:
//	func createCheckout(c echo.Context) error {
//	    result := echox.MustResult(c)
//	    env := httpx.BuildSuccessEnvelope(result, negotiate.OpCheckout)
//	    return c.JSON(http.StatusOK, map[string]any{
//	        "ucp":      env,
//	        "checkout": map[string]any{"id": "chk_123"},
//	    })
//	}
//
// The adapter:
//
//   - Runs negotiation in a middleware (echo.MiddlewareFunc).
//   - On success, stores the negotiate.Result in echo.Context under
//     httpx.ContextKey and ALSO in the request context, so handlers can
//     read it via either echox.Result(c) or httpx.FromContext(c.Request().Context()).
//   - On failure, writes a UCP-conformant response (4xx with DiscoveryError
//     for discovery/version errors, 200 OK with NegotiationFailure for
//     capabilities_incompatible) using c.JSON.
//   - Returns nil after writing a failure response so Echo's error handler
//     does not double-write.
package echox

import (
	"fmt"
	"net/http"

	specall "github.com/sarvexlab/sarvex/internal/spec/all"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/types"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/ucpnego/httpx"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/ucpnego/negotiate"
)

// Middleware returns an Echo middleware that runs UCP negotiation on
// every request. Apply it to a group or per-route — applying it globally
// is rarely what you want because /.well-known/ucp itself must not be
// behind negotiation.
func Middleware(neg *httpx.BusinessNegotiator) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()

			result, failure := neg.Negotiate(req)
			if failure != nil {
				return writeFailure(c, failure)
			}

			// Stash the Result in both bags so handlers can pick whichever
			// API they prefer.
			c.Set(httpx.ContextKey, result)
			c.SetRequest(req.WithContext(httpx.WithResult(req.Context(), result)))

			return next(c)
		}
	}
}

// Result returns the negotiation result attached by Middleware. The bool
// is false when no result is present — typically because the middleware
// did not run on this route.
func Result(c echo.Context) (negotiate.Result, bool) {
	v := c.Get(httpx.ContextKey)
	if v == nil {
		// Fall back to the request context in case a downstream middleware
		// re-wrapped the request without copying echo.Context values.
		return httpx.FromContext(c.Request().Context())
	}
	r, ok := v.(negotiate.Result)
	return r, ok
}

// MustResult is Result but panics if no result is present. Use only in
// handlers guaranteed to run behind Middleware.
func MustResult(c echo.Context) negotiate.Result {
	r, ok := Result(c)
	if !ok {
		panic("echox: negotiation result missing — is Middleware applied to this route?")
	}
	return r
}

// ProfileHandler returns an Echo handler that serves the negotiator's
// own profile at /.well-known/ucp, written in the form of the generation it
// declares, with a Cache-Control header satisfying the spec's TTL floor.
//
// Mount it OUTSIDE the negotiation middleware:
//
//	e.GET("/.well-known/ucp", echox.ProfileHandler(neg))
//	ucp := e.Group("/ucp", echox.Middleware(neg))
func ProfileHandler(neg *httpx.BusinessNegotiator) echo.HandlerFunc {
	return func(c echo.Context) error {
		own := neg.OwnProfile()
		gen, ok := specall.Default().Generations().Get(types.Version(own.UCP.Version))
		if !ok {
			return fmt.Errorf("echox: no generation registered for own profile version %s", own.UCP.Version)
		}
		body, err := gen.EncodeProfile(own)
		if err != nil {
			return fmt.Errorf("echox: encode own profile: %w", err)
		}
		c.Response().Header().Set("Cache-Control", "public, max-age=300")
		return c.JSONBlob(http.StatusOK, body)
	}
}

// writeFailure translates a FailureOutcome to the right Echo response.
// Discovery/version errors → 4xx with DiscoveryError body.
// capabilities_incompatible → 200 OK with NegotiationFailure body.
func writeFailure(c echo.Context, f *httpx.FailureOutcome) error {
	if f.IsNegotiationFailure() {
		return c.JSON(http.StatusOK, f.NegotiationBody())
	}
	return c.JSON(f.HTTPStatus, f.DiscoveryBody())
}
