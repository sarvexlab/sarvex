// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/sync/connector"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/sync/domain"
)

func (h *handler) acceptSignal(c echo.Context) error {
	p := principalFrom(c)
	connectorID := p.ConnectorID

	var sig domain.ConnectorSignal
	if err := c.Bind(&sig); err != nil {
		return errValidation(c, "invalid request body")
	}

	if sig.EntityType == "" || sig.EntityID == "" {
		return errValidation(c, "entity_type and entity_id are required")
	}

	err := h.deps.Connector.AcceptSignal(c.Request().Context(), connectorID, sig)
	if err != nil {
		h.log.Error("acceptSignal failed", "error", err, "connector_id", connectorID)
		return errInternal(c)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *handler) recordHeartbeat(c echo.Context) error {
	p := principalFrom(c)
	connectorID := p.ConnectorID

	var hb domain.Heartbeat
	if err := c.Bind(&hb); err != nil {
		return errValidation(c, "invalid request body")
	}

	if hb.PluginVersion == "" || hb.SiteURL == "" {
		return errValidation(c, "plugin_version and site_url are required")
	}

	err := h.deps.Connector.RecordHeartbeat(c.Request().Context(), connectorID, hb)
	if err != nil {
		h.log.Error("recordHeartbeat failed", "error", err, "connector_id", connectorID)
		return errInternal(c)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *handler) fetchProfile(c echo.Context) error {
	p := principalFrom(c)
	connectorID := p.ConnectorID
	etag := c.Request().Header.Get("If-None-Match")

	body, newEtag, notModified, err := h.deps.Connector.FetchProfile(c.Request().Context(), connectorID, etag)
	if err != nil {
		if errors.Is(err, connector.ErrNotImplemented) {
			return notImplemented(c)
		}
		if errors.Is(err, connector.ErrProfileNotReady) {
			// A 200 here is served verbatim at /.well-known/ucp, so an
			// empty one would replace the plugin's last-known-good copy
			// with nothing (sarvex-v1-control-plane.yaml: 409).
			return errProfileNotReady(c)
		}
		h.log.Error("fetchProfile failed", "error", err, "connector_id", connectorID)
		return errInternal(c)
	}

	if newEtag != "" {
		c.Response().Header().Set("ETag", newEtag)
	}

	if notModified {
		return c.NoContent(http.StatusNotModified)
	}

	c.Response().Header().Set("Content-Type", "application/json")
	return c.Blob(http.StatusOK, "application/json", body)
}
