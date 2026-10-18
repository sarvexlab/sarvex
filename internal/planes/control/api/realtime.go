// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/realtime"
)

// GET /stores/:store_id/journey/stream — upgrades to a WebSocket and pushes
// a journey_stream_message frame on every control.journey.updated event for
// this store (see internal/planes/control/realtime). Ownership of store_id is
// verified against the resolved principal before the upgrade completes,
// exactly as on GET /stores/:store_id.
func (h *handler) streamJourney(c echo.Context) error {
	p := principalFrom(c)

	storeID := domain.StoreID(c.Param("store_id"))
	if storeID == "" {
		return errValidation(c, "store_id is required")
	}

	if h.deps.Realtime == nil {
		return notImplemented(c)
	}

	if _, err := h.deps.Store.Get(c.Request().Context(), p.MerchantID, storeID); err != nil {
		return errNotFound(c)
	}

	upgrader := websocket.Upgrader{
		Subprotocols: []string{"bearer"},
		CheckOrigin:  h.checkWSOrigin,
	}
	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		// Upgrade has already written a response (or the connection is gone);
		// nothing more to send.
		h.log.Warn("journey stream upgrade failed", "error", err, "store_id", storeID)
		return nil
	}
	defer conn.Close()

	client := realtime.NewClient(conn)
	h.deps.Realtime.Register(string(storeID), client)
	defer h.deps.Realtime.Unregister(string(storeID), client)

	// The client never sends application messages on this stream; the read
	// loop exists only to detect the connection closing (client navigating
	// away, network drop) so Unregister runs promptly.
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return nil
		}
	}
}

// checkWSOrigin is the WebSocket handshake's Origin check — defense in depth
// alongside bearer auth (see Dependencies.DashboardOrigin).
func (h *handler) checkWSOrigin(r *http.Request) bool {
	if h.deps.DashboardOrigin == "" {
		return true
	}
	origin := strings.TrimRight(r.Header.Get("Origin"), "/")
	return origin == strings.TrimRight(h.deps.DashboardOrigin, "/")
}
