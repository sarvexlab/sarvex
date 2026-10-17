// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/activation"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// GET /stores/:store_id/activation
//
// merchant_id comes from the bearer claim (principalFrom), never from the
// path — so a store id belonging to another tenant reads as not-found
// rather than leaking its existence.
func (h *handler) getActivation(c echo.Context) error {
	p := principalFrom(c)
	storeID := domain.StoreID(c.Param("store_id"))

	if h.deps.Activation == nil {
		return notImplemented(c)
	}

	a, err := h.deps.Activation.Get(c.Request().Context(), p.MerchantID, storeID)
	if errors.Is(err, activation.ErrNotFound) {
		return errNotFound(c)
	}
	if err != nil {
		h.log.Error("getActivation failed", "error", err, "merchant_id", p.MerchantID, "store_id", storeID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, a)
}

// POST /stores/:store_id/activation/verify
//
// Runs the conformance suite against the store's own endpoints and applies
// the result. Returns the report whether it passed or failed — a failing
// verification is a successful request that found problems, not a 5xx.
func (h *handler) verifyActivation(c echo.Context) error {
	p := principalFrom(c)
	storeID := domain.StoreID(c.Param("store_id"))

	if h.deps.Activation == nil {
		return notImplemented(c)
	}

	report, err := h.deps.Activation.Verify(c.Request().Context(), p.MerchantID, storeID)
	if errors.Is(err, activation.ErrNotFound) {
		return errNotFound(c)
	}
	if err != nil {
		h.log.Error("verifyActivation failed", "error", err, "merchant_id", p.MerchantID, "store_id", storeID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, report)
}
