// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package api is the HTTP layer for the Sarvex V1 control plane.
//
// All routes follow the contract defined in sarvex-v1-control-plane.yaml.
// merchant_id is never a URL parameter — it is resolved from the authenticated
// principal injected by requireAuth.
package api

import (
	"log/slog"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/utils"
	"github.com/sarvexlab/sarvex/internal/planes/sync/connector"
)

// Handler is the control-plane HTTP handler interface.
// Create a concrete instance with New and register routes with SetupRoutes.
type Handler interface {
	utils.HandlerSetup
}

// Dependencies holds all service interfaces the handler layer depends on.
// Each field is an interface so tests can inject stubs without a running system.
type Dependencies struct {
	Logger         *slog.Logger
	Connector      connector.Service
	EventPublisher eventbus.EventBus
	// ResolveHMAC validates X-Sarvex-Signature and returns a Principal (real: pairing lookup + HMAC verify).
	ResolveHMAC HMACResolver
}

type handler struct {
	log  *slog.Logger
	deps Dependencies
}

var _ Handler = (*handler)(nil)

// New constructs a Handler from the provided dependencies.
func New(deps Dependencies) Handler {
	log := deps.Logger
	if log == nil {
		log = slog.Default()
	}
	return &handler{log: log, deps: deps}
}

// SetupRoutes wires every control-plane route onto the given Echo group.
// The group is typically mounted at /v1.
func (h *handler) SetupRoutes(_ string, g *echo.Group) error {
	hmac := requireHMAC(h.deps.ResolveHMAC)

	// ── Connector ingest (HMAC-signed plugin calls) ───────────────────────
	g.POST("/connector/signals", h.acceptSignal, hmac)
	g.POST("/connector/heartbeat", h.recordHeartbeat, hmac)
	g.GET("/connector/profile", h.fetchProfile, hmac)

	return nil
}
