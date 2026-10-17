// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/samber/lo"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/endpointhealth"
)

// healthResponse composes the pre-existing consistency-focused
// domain.Health with the new endpoint-health composite status and active
// alerts. Health is embedded so its "statements" field flattens to the top
// level — existing consumers of GET /stores/{id}/health see no change to
// that field; endpoint_status/active_alerts are new, additive, and omitted
// entirely (not just empty) when EndpointHealthStatus isn't wired.
type healthResponse struct {
	domain.Health
	EndpointStatus *endpointStatusView `json:"endpoint_status,omitempty"`
	ActiveAlerts   []alertView         `json:"active_alerts,omitempty"`
}

type endpointStatusView struct {
	Status      string               `json:"status"`
	EvaluatedAt time.Time            `json:"evaluated_at"`
	Checks      map[string]checkView `json:"checks"`
}

type checkView struct {
	State        string     `json:"state"`
	LastPassAt   *time.Time `json:"last_pass_at,omitempty"`
	LastFailAt   *time.Time `json:"last_fail_at,omitempty"`
	CurrentValue float64    `json:"current_value"`
}

type alertView struct {
	AlertID        string     `json:"alert_id"`
	StoreID        string     `json:"store_id"`
	CheckType      string     `json:"check_type"`
	Severity       string     `json:"severity"`
	Status         string     `json:"status"`
	Detail         string     `json:"detail"`
	OpenedAt       time.Time  `json:"opened_at"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	AcknowledgedBy string     `json:"acknowledged_by,omitempty"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	AutoResolved   bool       `json:"auto_resolved,omitempty"`
}

func toAlertView(a *endpointhealth.Alert) alertView {
	return alertView{
		AlertID:        a.ID,
		StoreID:        a.StoreID,
		CheckType:      string(a.CheckType),
		Severity:       string(a.Severity),
		Status:         string(a.Status),
		Detail:         a.Detail,
		OpenedAt:       a.OpenedAt,
		AcknowledgedAt: a.AcknowledgedAt,
		AcknowledgedBy: a.AcknowledgedBy,
		ResolvedAt:     a.ResolvedAt,
		AutoResolved:   a.AutoResolved,
	}
}

// GET /stores/:store_id/health — get a store's overall health status:
// the pre-existing consistency statements plus (additively) the
// endpoint-health composite status and its active alerts.
func (h *handler) getHealth(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}

	storeID := c.Param("store_id")
	if storeID == "" {
		return errValidation(c, "store_id required")
	}

	health, err := h.deps.Operator.GetHealth(c.Request().Context(), principal.MerchantID, domain.StoreID(storeID))
	if err != nil {
		h.log.Error("get health failed", "error", err, "merchant_id", principal.MerchantID, "store_id", storeID)
		return errInternal(c)
	}

	resp := healthResponse{Health: health}

	if h.deps.EndpointHealthStatus != nil {
		doc, err := h.deps.EndpointHealthStatus.Get(c.Request().Context(), string(principal.MerchantID), storeID)
		if err != nil {
			h.log.Error("get endpoint health status failed", "error", err, "merchant_id", principal.MerchantID, "store_id", storeID)
			return errInternal(c)
		}
		if doc != nil {
			checks := make(map[string]checkView, len(doc.Checks))
			for k, v := range doc.Checks {
				checks[string(k)] = checkView{State: string(v.State), LastPassAt: v.LastPassAt, LastFailAt: v.LastFailAt, CurrentValue: v.CurrentValue}
			}
			resp.EndpointStatus = &endpointStatusView{Status: string(doc.Status), EvaluatedAt: doc.EvaluatedAt, Checks: checks}
		}
	}

	if h.deps.EndpointHealthAlerts != nil {
		alerts, _, err := h.deps.EndpointHealthAlerts.ListOpenForStore(c.Request().Context(), string(principal.MerchantID), storeID)
		if err != nil {
			h.log.Error("list active alerts failed", "error", err, "store_id", storeID)
			return errInternal(c)
		}
		resp.ActiveAlerts = lo.Map(alerts, func(a *endpointhealth.Alert, _ int) alertView { return toAlertView(a) })
	}

	return c.JSON(http.StatusOK, resp)
}

// historyEntryView is the wire shape for one GET .../health/history row.
type historyEntryView struct {
	Kind      string  `json:"kind"`
	ProbeType *string `json:"probe_type,omitempty"`
	CheckType *string `json:"check_type,omitempty"`
	Outcome   *string `json:"outcome,omitempty"`
	Severity  *string `json:"severity,omitempty"`
	Detail    string  `json:"detail,omitempty"`
	At        string  `json:"at"`
}

const maxHistoryRange = 30 * 24 * time.Hour

// GET /stores/:store_id/health/history?from=&to= — probe outcomes and
// status-check transitions over a bounded time range.
func (h *handler) getHealthHistory(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}
	storeID := c.Param("store_id")
	if storeID == "" {
		return errValidation(c, "store_id required")
	}
	// Ownership check: the health/consistency read path already trusts
	// storeID scoped by principal via Operator.GetHealth; history has no
	// separate store-ownership service to call, so verify the store exists
	// for this merchant the same way GetStore does before proceeding.
	if _, err := h.deps.Store.Get(c.Request().Context(), principal.MerchantID, domain.StoreID(storeID)); err != nil {
		return errNotFound(c)
	}

	to := time.Now().UTC()
	from := to.Add(-24 * time.Hour)
	if v := c.QueryParam("to"); v != "" {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return errValidation(c, "to must be RFC3339")
		}
		to = parsed
	}
	if v := c.QueryParam("from"); v != "" {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return errValidation(c, "from must be RFC3339")
		}
		from = parsed
	}
	if to.Before(from) {
		return errValidation(c, "to must not be before from")
	}
	if to.Sub(from) > maxHistoryRange {
		return errValidation(c, "range must not exceed 30 days")
	}

	if h.deps.EndpointHealthHistory == nil {
		return c.JSON(http.StatusOK, domain.ListOf[historyEntryView]{Data: []historyEntryView{}, Page: domain.Page{HasMore: false}})
	}

	entries, err := h.deps.EndpointHealthHistory.List(c.Request().Context(), storeID, from, to)
	if err != nil {
		h.log.Error("get health history failed", "error", err, "store_id", storeID)
		return errInternal(c)
	}

	views := lo.Map(entries, func(e endpointhealth.HistoryEntry, _ int) historyEntryView {
		v := historyEntryView{Kind: e.Kind, Detail: e.Detail, At: e.At.Format(time.RFC3339)}
		if e.ProbeType != "" {
			s := string(e.ProbeType)
			v.ProbeType = &s
		}
		if e.CheckType != "" {
			s := string(e.CheckType)
			v.CheckType = &s
		}
		if e.Outcome != "" {
			s := string(e.Outcome)
			v.Outcome = &s
		}
		if e.Severity != "" {
			s := string(e.Severity)
			v.Severity = &s
		}
		return v
	})

	return c.JSON(http.StatusOK, domain.ListOf[historyEntryView]{Data: views, Page: domain.Page{HasMore: false}})
}

// GET /_probe/.well-known/ucp — the endpoint-health probe's own reference
// UCP profile, fetched by merchants' serving-plane instances to verify
// signed probe requests and negotiate against them.
func (h *handler) getProbeProfile(c echo.Context) error {
	if h.deps.EndpointHealthIdentity == nil {
		return errNotFound(c)
	}
	body, err := h.deps.EndpointHealthIdentity.Document()
	if err != nil {
		return errInternal(c)
	}
	return c.JSONBlob(http.StatusOK, body)
}

// GET /alerts?status=&severity=&store_id=&cursor=&limit= — list this
// merchant's endpoint-health alerts.
func (h *handler) listAlerts(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}
	if h.deps.EndpointHealthAlerts == nil {
		return c.JSON(http.StatusOK, domain.ListOf[alertView]{Data: []alertView{}, Page: domain.Page{HasMore: false}})
	}

	limit := 25
	if v := c.QueryParam("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}
	filter := endpointhealth.AlertListFilter{
		Status:   endpointhealth.AlertStatus(c.QueryParam("status")),
		Severity: endpointhealth.Severity(c.QueryParam("severity")),
		StoreID:  c.QueryParam("store_id"),
	}

	alerts, hasMore, err := h.deps.EndpointHealthAlerts.List(c.Request().Context(), string(principal.MerchantID), filter, c.QueryParam("cursor"), limit)
	if err != nil {
		h.log.Error("list alerts failed", "error", err, "merchant_id", principal.MerchantID)
		return errInternal(c)
	}

	var cursor *string
	if hasMore && len(alerts) > 0 {
		last := alerts[len(alerts)-1].ID
		cursor = &last
	}

	return c.JSON(http.StatusOK, domain.ListOf[alertView]{
		Data: lo.Map(alerts, func(a *endpointhealth.Alert, _ int) alertView { return toAlertView(a) }),
		Page: domain.Page{Cursor: cursor, HasMore: hasMore},
	})
}

type patchAlertRequest struct {
	Status string `json:"status"` // "acknowledged" or "resolved"
}

// PATCH /alerts/:alert_id — acknowledge or resolve an alert. Forward-only:
// open -> acknowledged -> resolved (or open -> resolved directly).
func (h *handler) patchAlert(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}
	alertID := c.Param("alert_id")
	if alertID == "" {
		return errValidation(c, "alert_id required")
	}
	if h.deps.EndpointHealthAlerts == nil {
		return errNotFound(c)
	}

	var req patchAlertRequest
	if err := c.Bind(&req); err != nil {
		return errValidation(c, err.Error())
	}

	var alert *endpointhealth.Alert
	var err error
	switch req.Status {
	case string(endpointhealth.AlertAcknowledged):
		alert, err = h.deps.EndpointHealthAlerts.Acknowledge(c.Request().Context(), string(principal.MerchantID), alertID, principal.Email)
	case string(endpointhealth.AlertResolved):
		alert, err = h.deps.EndpointHealthAlerts.Resolve(c.Request().Context(), string(principal.MerchantID), alertID, false)
	default:
		return errValidation(c, "status must be \"acknowledged\" or \"resolved\"")
	}

	switch {
	case errors.Is(err, endpointhealth.ErrAlertNotFound):
		return errNotFound(c)
	case errors.Is(err, endpointhealth.ErrInvalidTransition):
		return errConflict(c, "alert is not in a state that allows this transition")
	case err != nil:
		h.log.Error("patch alert failed", "error", err, "alert_id", alertID)
		return errInternal(c)
	}

	return c.JSON(http.StatusOK, toAlertView(alert))
}
