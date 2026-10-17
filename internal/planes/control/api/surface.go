// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/surface"
)

// Every handler in this file is surface-agnostic. There is no branch on a
// surface id anywhere here, and there must never be one: a new surface is a
// descriptor and its adapters, which these routes serve without knowing
// anything about it.

// GET /surfaces — the registry.
//
// Not tenant-scoped, because the registry isn't: which surfaces exist is a
// property of the deployment, not of a merchant. What a *store* has done
// about them is GET /stores/:store_id/surfaces.
func (h *handler) listSurfaceRegistry(c echo.Context) error {
	if h.deps.Surfaces == nil {
		return notImplemented(c)
	}
	descriptors := h.deps.Surfaces.Registry(c.Request().Context())
	return c.JSON(http.StatusOK, domain.ListOf[domain.SurfaceDescriptor]{
		Data: descriptors,
		Page: domain.Page{HasMore: false},
	})
}

// GET /stores/:store_id/surfaces
func (h *handler) listStoreSurfaces(c echo.Context) error {
	p := principalFrom(c)
	storeID := domain.StoreID(c.Param("store_id"))

	if h.deps.Surfaces == nil {
		return notImplemented(c)
	}

	list, err := h.deps.Surfaces.List(c.Request().Context(), p.MerchantID, storeID)
	if err != nil {
		h.log.Error("listStoreSurfaces failed", "error", err, "merchant_id", p.MerchantID, "store_id", storeID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, domain.ListOf[domain.SurfaceEnrollment]{
		Data: list,
		Page: domain.Page{HasMore: false},
	})
}

// POST /stores/:store_id/surfaces/:surface_id/enroll
func (h *handler) enrollSurface(c echo.Context) error {
	p := principalFrom(c)
	storeID := domain.StoreID(c.Param("store_id"))
	surfaceID := c.Param("surface_id")

	if h.deps.Surfaces == nil {
		return notImplemented(c)
	}

	e, err := h.deps.Surfaces.Enroll(c.Request().Context(), p.MerchantID, storeID, surfaceID)
	if err != nil {
		return h.surfaceError(c, err, "enrollSurface", p.MerchantID, storeID, surfaceID)
	}
	return c.JSON(http.StatusOK, e)
}

// POST /stores/:store_id/surfaces/:surface_id/prerequisites/evaluate
func (h *handler) evaluateSurfacePrerequisites(c echo.Context) error {
	p := principalFrom(c)
	storeID := domain.StoreID(c.Param("store_id"))
	surfaceID := c.Param("surface_id")

	if h.deps.Surfaces == nil {
		return notImplemented(c)
	}

	e, err := h.deps.Surfaces.EvaluatePrerequisites(c.Request().Context(), p.MerchantID, storeID, surfaceID)
	if err != nil {
		return h.surfaceError(c, err, "evaluateSurfacePrerequisites", p.MerchantID, storeID, surfaceID)
	}
	return c.JSON(http.StatusOK, e)
}

// PUT /stores/:store_id/surfaces/:surface_id/config
//
// The body is a flat string map, deliberately: per-surface configuration
// shapes are the adapters' business, and typing them here would put
// surface-specific knowledge in the generic layer.
func (h *handler) setSurfaceConfig(c echo.Context) error {
	p := principalFrom(c)
	storeID := domain.StoreID(c.Param("store_id"))
	surfaceID := c.Param("surface_id")

	if h.deps.Surfaces == nil {
		return notImplemented(c)
	}

	var body struct {
		Config map[string]string `json:"config"`
	}
	if err := c.Bind(&body); err != nil {
		return errValidation(c, map[string]string{"config": "must be an object of string values"})
	}

	e, err := h.deps.Surfaces.SetConfig(c.Request().Context(), p.MerchantID, storeID, surfaceID, body.Config)
	if err != nil {
		return h.surfaceError(c, err, "setSurfaceConfig", p.MerchantID, storeID, surfaceID)
	}
	return c.JSON(http.StatusOK, e)
}

// POST /stores/:store_id/surfaces/:surface_id/submit
//
// Records that the merchant completed the surface's external steps. Not in
// R16's list, but a surface with external_steps has no other way to leave
// `configured` — without it, such a surface would stall short of verifying and
// the descriptor's external_steps would be decorative.
func (h *handler) submitSurface(c echo.Context) error {
	p := principalFrom(c)
	storeID := domain.StoreID(c.Param("store_id"))
	surfaceID := c.Param("surface_id")

	if h.deps.Surfaces == nil {
		return notImplemented(c)
	}

	e, err := h.deps.Surfaces.Submit(c.Request().Context(), p.MerchantID, storeID, surfaceID)
	if err != nil {
		return h.surfaceError(c, err, "submitSurface", p.MerchantID, storeID, surfaceID)
	}
	return c.JSON(http.StatusOK, e)
}

// DELETE /stores/:store_id/surfaces/:surface_id
//
// Withdraws enrollment. Returns 204 and touches nothing else: not the
// store's activation state, not any other surface (R16).
func (h *handler) withdrawSurface(c echo.Context) error {
	p := principalFrom(c)
	storeID := domain.StoreID(c.Param("store_id"))
	surfaceID := c.Param("surface_id")

	if h.deps.Surfaces == nil {
		return notImplemented(c)
	}

	if err := h.deps.Surfaces.Withdraw(c.Request().Context(), p.MerchantID, storeID, surfaceID); err != nil {
		return h.surfaceError(c, err, "withdrawSurface", p.MerchantID, storeID, surfaceID)
	}
	return c.NoContent(http.StatusNoContent)
}

// surfaceError maps the service's errors onto the HTTP surface once, so
// every handler above answers the same way.
//
// ErrStoreNotActive is a 409, not a 422: the request is well-formed and the
// merchant may well be entitled to it — the store simply isn't there yet.
// That is a conflict with current state, and it is the one error a console
// needs to distinguish, because the remedy ("finish activating") is
// somewhere else entirely.
func (h *handler) surfaceError(c echo.Context, err error, op string, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) error {
	switch {
	case errors.Is(err, surface.ErrNotFound):
		return errNotFound(c)
	case errors.Is(err, surface.ErrInvalidConfig):
		return errValidation(c, map[string]string{"config": err.Error()})
	case isStoreNotActive(err):
		return errConflict(c, "store must be UCP active before enrolling in an agent surface")
	}

	var invalid surface.ErrInvalidTransition
	if errors.As(err, &invalid) {
		return errConflict(c, invalid.Error())
	}

	h.log.Error(op+" failed", "error", err,
		"merchant_id", merchantID, "store_id", storeID, "surface_id", surfaceID)
	return errInternal(c)
}

func isStoreNotActive(err error) bool {
	var notActive surface.ErrStoreNotActive
	return errors.As(err, &notActive)
}
