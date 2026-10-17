// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/operator"
)

// GET /stores/:store_id/validation/status — get current validation state for a store.
func (h *handler) getValidationStatus(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "principal not found")
	}

	storeID := c.Param("store_id")
	if storeID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "store_id required")
	}

	status, err := h.deps.Validation.GetStatus(c.Request().Context(), principal.MerchantID, domain.StoreID(storeID))
	if err != nil {
		h.log.Error("get validation status failed", "error", err, "merchant_id", principal.MerchantID, "store_id", storeID)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to retrieve validation status")
	}

	return c.JSON(http.StatusOK, status)
}

// GET /stores/:store_id/validation/runs — list conformance records for a store.
func (h *handler) listValidationRuns(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "principal not found")
	}

	storeID := c.Param("store_id")
	if storeID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "store_id required")
	}

	cursor := c.QueryParam("cursor")
	limit := 20
	limitStr := c.QueryParam("limit")
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	runs, err := h.deps.Validation.ListRuns(c.Request().Context(), principal.MerchantID, domain.StoreID(storeID), cursor, limit)
	if err != nil {
		h.log.Error("list validation runs failed", "error", err, "merchant_id", principal.MerchantID, "store_id", storeID)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to retrieve validation runs")
	}

	return c.JSON(http.StatusOK, runs)
}

// POST /stores/:store_id/validation/runs — trigger a new validation run for a store.
func (h *handler) triggerValidationRun(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "principal not found")
	}

	storeID := c.Param("store_id")
	if storeID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "store_id required")
	}

	job, err := h.deps.Validation.TriggerRun(c.Request().Context(), principal.MerchantID, domain.StoreID(storeID))
	if err != nil {
		h.log.Error("trigger validation run failed", "error", err, "merchant_id", principal.MerchantID, "store_id", storeID)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to start validation run")
	}

	return c.JSON(http.StatusAccepted, job)
}

// GET /stores/:store_id/presence — get a store's serving layer presence and metadata.
func (h *handler) getPresence(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "principal not found")
	}

	storeID := c.Param("store_id")
	if storeID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "store_id required")
	}

	presence, err := h.deps.Operator.GetPresence(c.Request().Context(), principal.MerchantID, domain.StoreID(storeID))
	if err != nil {
		h.log.Error("get presence failed", "error", err, "merchant_id", principal.MerchantID, "store_id", storeID)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to retrieve presence")
	}

	return c.JSON(http.StatusOK, presence)
}

// GET /stores/:store_id/presence/manifest — get a store's deployed manifest and cache state.
func (h *handler) getManifest(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "principal not found")
	}

	storeID := c.Param("store_id")
	if storeID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "store_id required")
	}

	manifest, err := h.deps.Operator.GetManifest(c.Request().Context(), principal.MerchantID, domain.StoreID(storeID))
	if errors.Is(err, operator.ErrNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "store not found")
	}
	if err != nil {
		h.log.Error("get manifest failed", "error", err, "merchant_id", principal.MerchantID, "store_id", storeID)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to retrieve manifest")
	}

	return c.JSON(http.StatusOK, manifest)
}

// POST /stores/:store_id/presence/rebuild — trigger a cache rebuild for a store.
func (h *handler) rebuildCache(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "principal not found")
	}

	storeID := c.Param("store_id")
	if storeID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "store_id required")
	}

	job, err := h.deps.Operator.RebuildCache(c.Request().Context(), principal.MerchantID, domain.StoreID(storeID))
	if err != nil {
		h.log.Error("rebuild cache failed", "error", err, "merchant_id", principal.MerchantID, "store_id", storeID)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to trigger cache rebuild")
	}

	return c.JSON(http.StatusAccepted, job)
}

// GET /stores/:store_id/orders — list orders for a store.
func (h *handler) listOrders(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "principal not found")
	}

	storeID := c.Param("store_id")
	if storeID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "store_id required")
	}

	cursor := c.QueryParam("cursor")
	limit := 20
	limitStr := c.QueryParam("limit")
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	statusStr := c.QueryParam("status")
	var status *domain.OrderStatus
	if statusStr != "" {
		s := domain.OrderStatus(statusStr)
		status = &s
	}

	orders, err := h.deps.Operator.ListOrders(c.Request().Context(), principal.MerchantID, domain.StoreID(storeID), cursor, limit, status)
	if err != nil {
		h.log.Error("list orders failed", "error", err, "merchant_id", principal.MerchantID, "store_id", storeID)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to retrieve orders")
	}

	return c.JSON(http.StatusOK, orders)
}

// GET /stores/:store_id/orders/:order_id — get a specific order from a store.
func (h *handler) getOrder(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "principal not found")
	}

	storeID := c.Param("store_id")
	if storeID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "store_id required")
	}

	orderID := c.Param("order_id")
	if orderID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "order_id required")
	}

	order, err := h.deps.Operator.GetOrder(c.Request().Context(), principal.MerchantID, domain.StoreID(storeID), orderID)
	if errors.Is(err, operator.ErrOrderNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "order not found")
	}
	if err != nil {
		h.log.Error("get order failed", "error", err, "merchant_id", principal.MerchantID, "store_id", storeID, "order_id", orderID)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to retrieve order")
	}

	return c.JSON(http.StatusOK, order)
}

// GET /stores/:store_id/health/consistency — list consistency findings for a store.
func (h *handler) listConsistencyFindings(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "principal not found")
	}

	storeID := c.Param("store_id")
	if storeID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "store_id required")
	}

	cursor := c.QueryParam("cursor")
	limit := 20
	limitStr := c.QueryParam("limit")
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	severityStr := c.QueryParam("severity")
	var severity *domain.Severity
	if severityStr != "" {
		s := domain.Severity(severityStr)
		severity = &s
	}

	findings, err := h.deps.Operator.ListConsistencyFindings(c.Request().Context(), principal.MerchantID, domain.StoreID(storeID), cursor, limit, severity)
	if err != nil {
		h.log.Error("list consistency findings failed", "error", err, "merchant_id", principal.MerchantID, "store_id", storeID)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to retrieve findings")
	}

	return c.JSON(http.StatusOK, findings)
}
