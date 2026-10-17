// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/catalog"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

func (h *handler) getReadiness(c echo.Context) error {
	p := principalFrom(c)
	storeID := domain.StoreID(c.Param("store_id"))
	r, err := h.deps.Catalog.GetReadiness(c.Request().Context(), p.MerchantID, storeID)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			return errNotFound(c)
		}
		h.log.Error("getReadiness failed", "error", err, "merchant_id", p.MerchantID, "store_id", storeID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, r)
}

func (h *handler) listProducts(c echo.Context) error {
	p := principalFrom(c)
	storeID := domain.StoreID(c.Param("store_id"))

	cursor := c.QueryParam("cursor")
	limit := parseLimit(c, 20)

	var minScore *int
	if s := c.QueryParam("min_score"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			minScore = &n
		}
	}

	var hasBlockingIssue *bool
	if b := c.QueryParam("has_blocking_issue"); b != "" {
		v := b == "true"
		hasBlockingIssue = &v
	}

	result, err := h.deps.Catalog.ListProducts(c.Request().Context(), p.MerchantID, storeID, cursor, limit, minScore, hasBlockingIssue)
	if err != nil {
		h.log.Error("listProducts failed", "error", err, "merchant_id", p.MerchantID, "store_id", storeID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, result)
}

func (h *handler) getProduct(c echo.Context) error {
	p := principalFrom(c)
	storeID := domain.StoreID(c.Param("store_id"))
	sku := c.Param("sku")

	product, err := h.deps.Catalog.GetProduct(c.Request().Context(), p.MerchantID, storeID, sku)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			return errNotFound(c)
		}
		h.log.Error("getProduct failed", "error", err, "merchant_id", p.MerchantID, "store_id", storeID, "sku", sku)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, product)
}

func (h *handler) triggerAudit(c echo.Context) error {
	p := principalFrom(c)
	storeID := domain.StoreID(c.Param("store_id"))

	job, err := h.deps.Catalog.TriggerAudit(c.Request().Context(), p.MerchantID, storeID)
	if err != nil {
		h.log.Error("triggerAudit failed", "error", err, "merchant_id", p.MerchantID, "store_id", storeID)
		return errInternal(c)
	}
	return c.JSON(http.StatusAccepted, job)
}

func (h *handler) listRemediations(c echo.Context) error {
	p := principalFrom(c)
	storeID := domain.StoreID(c.Param("store_id"))

	cursor := c.QueryParam("cursor")
	limit := parseLimit(c, 20)

	var remType *string
	if t := c.QueryParam("type"); t != "" {
		remType = &t
	}

	result, err := h.deps.Catalog.ListRemediations(c.Request().Context(), p.MerchantID, storeID, cursor, limit, remType)
	if err != nil {
		h.log.Error("listRemediations failed", "error", err, "merchant_id", p.MerchantID, "store_id", storeID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, result)
}

func (h *handler) applyRemediations(c echo.Context) error {
	p := principalFrom(c)
	storeID := domain.StoreID(c.Param("store_id"))

	var body domain.ApplyRemediationsRequest
	if err := c.Bind(&body); err != nil {
		return errValidation(c, err.Error())
	}
	if len(body.RemediationIDs) == 0 {
		return errValidation(c, "remediation_ids must not be empty")
	}

	result, err := h.deps.Catalog.ApplyRemediations(c.Request().Context(), p.MerchantID, storeID, body)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			return errNotFound(c)
		}
		if errors.Is(err, catalog.ErrRemediationApplied) {
			return errConflict(c, "one or more remediations have already been applied")
		}
		h.log.Error("applyRemediations failed", "error", err, "merchant_id", p.MerchantID, "store_id", storeID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, result)
}

func (h *handler) revertWriteBack(c echo.Context) error {
	p := principalFrom(c)
	storeID := domain.StoreID(c.Param("store_id"))
	writeBackID := c.Param("write_back_id")

	result, err := h.deps.Catalog.RevertWriteBack(c.Request().Context(), p.MerchantID, storeID, writeBackID)
	if err != nil {
		if errors.Is(err, catalog.ErrWriteBackNotFound) {
			return errNotFound(c)
		}
		h.log.Error("revertWriteBack failed", "error", err, "merchant_id", p.MerchantID, "store_id", storeID, "write_back_id", writeBackID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, result)
}

// parseLimit extracts a ?limit query param, returning defaultVal if absent or invalid.
func parseLimit(c echo.Context, defaultVal int) int {
	if l := c.QueryParam("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			return n
		}
	}
	return defaultVal
}
