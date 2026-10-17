// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/connector"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/store"
	"github.com/sarvexlabs/sarvex.cp/internal/platform"
)

// Console endpoints (bearer auth)

func (h *handler) listConnectors(c echo.Context) error {
	p := principalFrom(c)
	cursor := c.QueryParam("cursor")
	limit := 20
	if l := c.QueryParam("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	result, err := h.deps.Connector.List(c.Request().Context(), p.MerchantID, cursor, limit)
	if err != nil {
		if errors.Is(err, connector.ErrNotImplemented) {
			return notImplemented(c)
		}
		h.log.Error("listConnectors failed", "error", err, "merchant_id", p.MerchantID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, result)
}

func (h *handler) getConnector(c echo.Context) error {
	p := principalFrom(c)
	connectorID := c.Param("connector_id")
	if connectorID == "" {
		return errValidation(c, "connector_id is required")
	}

	conn, err := h.deps.Connector.Get(c.Request().Context(), p.MerchantID, connectorID)
	if err != nil {
		if errors.Is(err, connector.ErrNotImplemented) {
			return notImplemented(c)
		}
		if errors.Is(err, connector.ErrNotFound) {
			return errNotFound(c)
		}
		h.log.Error("getConnector failed", "error", err, "merchant_id", p.MerchantID, "connector_id", connectorID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, conn)
}

func (h *handler) resync(c echo.Context) error {
	p := principalFrom(c)
	connectorID := c.Param("connector_id")
	if connectorID == "" {
		return errValidation(c, "connector_id is required")
	}

	result, err := h.deps.Connector.TriggerResync(c.Request().Context(), p.MerchantID, connectorID)
	if err != nil {
		if errors.Is(err, connector.ErrNotImplemented) {
			return notImplemented(c)
		}
		if errors.Is(err, connector.ErrNotFound) {
			return errNotFound(c)
		}
		h.log.Error("resync failed", "error", err, "merchant_id", p.MerchantID, "connector_id", connectorID)
		return errInternal(c)
	}
	return c.JSON(http.StatusAccepted, result)
}

// Pairing flow

func (h *handler) beginPairing(c echo.Context) error {
	p := principalFrom(c)
	storeID := c.Param("store_id")
	if storeID == "" {
		return errValidation(c, "store_id is required")
	}

	var body struct {
		Platform domain.Platform `json:"platform"`
	}
	if err := c.Bind(&body); err != nil {
		return errValidation(c, "invalid request body")
	}

	if !platform.Supported(body.Platform) {
		return errValidation(c, unsupportedPlatformMessage())
	}

	// The store has to exist and belong to the caller before a connector is
	// minted for it. Without this, pairing against any string returned 201 with
	// a real pairing code and wrote a connector for a store that does not
	// exist — the LinkConnector call below only warns when the store is
	// missing, so nothing surfaced it. Store.Get is fenced on merchant_id
	// (ADR-2), so this rejects another tenant's store id as not-found too.
	if _, err := h.deps.Store.Get(c.Request().Context(), p.MerchantID, domain.StoreID(storeID)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return errNotFound(c)
		}
		h.log.Error("beginPairing store lookup failed", "error", err, "merchant_id", p.MerchantID, "store_id", storeID)
		return errInternal(c)
	}

	pairing, err := h.deps.Connector.BeginPairing(c.Request().Context(), p.MerchantID, domain.StoreID(storeID), body.Platform)
	if err != nil {
		h.log.Error("beginPairing failed", "error", err, "merchant_id", p.MerchantID, "store_id", storeID)
		return errInternal(c)
	}

	// Link the store to the connector immediately upon pairing initialization.
	// This ensures the store object includes the connector_id for subsequent API calls.
	if err := h.deps.Store.LinkConnector(c.Request().Context(), p.MerchantID, domain.StoreID(storeID), pairing.Connector.ConnectorID); err != nil {
		h.log.Warn("failed to link store to connector on beginPairing", "error", err, "store_id", storeID, "connector_id", pairing.Connector.ConnectorID)
		// Don't fail the API response if the link fails; the pairing is initiated
	}

	return c.JSON(http.StatusCreated, map[string]any{
		"connector_id": pairing.Connector.ConnectorID,
		"pairing_code": pairing.PairingCode,
		"store_id":     pairing.StoreID,
	})
}

func (h *handler) completePairing(c echo.Context) error {
	var body struct {
		ConnectorID      string         `json:"connector_id"`
		PairingCode      string         `json:"pairing_code"`
		StoreCredentials map[string]any `json:"store_credentials,omitempty"`
	}
	if err := c.Bind(&body); err != nil {
		return errValidation(c, "invalid request body")
	}

	if body.ConnectorID == "" || body.PairingCode == "" {
		return errValidation(c, "connector_id and pairing_code are required")
	}

	conn, err := h.deps.Connector.CompletePairing(c.Request().Context(), body.ConnectorID, body.PairingCode, body.StoreCredentials)
	if err != nil {
		h.log.Warn("completePairing failed", "error", err, "connector_id", body.ConnectorID)
		return errValidation(c, "invalid or expired pairing code")
	}

	return c.JSON(http.StatusOK, map[string]any{
		"connector_id": conn.ConnectorID,
		"store_id":     conn.StoreID,
		"status":       conn.Status,
	})
}

func (h *handler) disconnect(c echo.Context) error {
	connectorID := c.Param("connector_id")
	if connectorID == "" {
		return errValidation(c, "connector_id is required")
	}

	err := h.deps.Connector.Disconnect(c.Request().Context(), connectorID)
	if err != nil {
		if errors.Is(err, connector.ErrNotImplemented) {
			return notImplemented(c)
		}
		h.log.Error("disconnect failed", "error", err, "connector_id", connectorID)
		return errInternal(c)
	}
	return c.NoContent(http.StatusNoContent)
}
