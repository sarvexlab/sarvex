// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/journey"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
)

// taskIDParam returns the task_id path param, decoded.
//
// Task IDs are a compound key ("source:store_id:source_ref", see
// task/subscriber.go) and so routinely contain colons. Colons don't require
// percent-encoding in a URL path segment (RFC 3986), but a client that
// encodes them anyway (e.g. JS's encodeURIComponent) causes Go's net/url to
// populate Request.URL.RawPath, which Echo's router prefers over the
// decoded Path (echo.GetPath) — so c.Param would otherwise return the
// still-escaped string and every lookup by ID would silently 404. Decode
// defensively; fall back to the raw value if it isn't valid percent-encoding.
func taskIDParam(c echo.Context) string {
	raw := c.Param("task_id")
	if decoded, err := url.PathUnescape(raw); err == nil {
		return decoded
	}
	return raw
}

func (h *handler) listTasks(c echo.Context) error {
	p := principalFrom(c)
	ctx := c.Request().Context()

	cursor := c.QueryParam("cursor")
	limitStr := c.QueryParam("limit")
	limit := 20
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	var owner *domain.TaskOwner
	if ownerStr := c.QueryParam("owner"); ownerStr != "" {
		o := domain.TaskOwner(ownerStr)
		owner = &o
	}

	var state *domain.TaskState
	if stateStr := c.QueryParam("state"); stateStr != "" {
		s := domain.TaskState(stateStr)
		state = &s
	}

	var storeID *domain.StoreID
	if storeIDStr := c.QueryParam("store_id"); storeIDStr != "" {
		s := domain.StoreID(storeIDStr)
		storeID = &s
	}

	result, err := h.deps.Task.List(ctx, p.MerchantID, storeID, cursor, limit, owner, state)
	if err != nil {
		if errors.Is(err, task.ErrNotImplemented) {
			return notImplemented(c)
		}
		if errors.Is(err, task.ErrInvalidCursor) {
			return errValidation(c, map[string]string{"cursor": "not a valid page cursor"})
		}
		h.log.Error("listTasks failed", "error", err, "merchant_id", p.MerchantID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, result)
}

func (h *handler) getTask(c echo.Context) error {
	p := principalFrom(c)
	taskID := taskIDParam(c)
	ctx := c.Request().Context()

	t, err := h.deps.Task.Get(ctx, p.MerchantID, taskID)
	if err != nil {
		if errors.Is(err, task.ErrNotImplemented) {
			return notImplemented(c)
		}
		if errors.Is(err, task.ErrNotFound) {
			return errNotFound(c)
		}
		h.log.Error("getTask failed", "error", err, "merchant_id", p.MerchantID, "task_id", taskID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, t)
}

func (h *handler) resolveTask(c echo.Context) error {
	p := principalFrom(c)
	taskID := taskIDParam(c)
	ctx := c.Request().Context()

	var req domain.ResolveTaskRequest
	if err := c.Bind(&req); err != nil {
		return errValidation(c, nil)
	}

	updated, err := h.deps.Task.Resolve(ctx, p.MerchantID, taskID, req)
	if err != nil {
		if errors.Is(err, task.ErrNotImplemented) {
			return notImplemented(c)
		}
		if errors.Is(err, task.ErrNotFound) {
			return errNotFound(c)
		}
		if errors.Is(err, task.ErrNotResolvable) {
			return errConflict(c, "Task is not in a resolvable state")
		}
		h.log.Error("resolveTask failed", "error", err, "merchant_id", p.MerchantID, "task_id", taskID)
		return errInternal(c)
	}

	return c.JSON(http.StatusOK, updated)
}

func (h *handler) getJourney(c echo.Context) error {
	p := principalFrom(c)

	storeID := c.Param("store_id")
	if storeID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "store_id required")
	}

	j, err := h.deps.Journey.Get(c.Request().Context(), p.MerchantID, domain.StoreID(storeID))
	if err != nil {
		if errors.Is(err, journey.ErrNotFound) {
			return errNotFound(c)
		}
		h.log.Error("getJourney failed", "error", err, "merchant_id", p.MerchantID, "store_id", storeID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, j)
}
