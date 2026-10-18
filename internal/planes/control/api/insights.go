// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/insights"
)

// This file is the control-plane read API for Agent Behavior Insights
// (R15): GET /insights/capability_gaps, /insights/product_demand,
// /insights/negotiation. Gated at the analytics tier (requireAnalyticsTier
// — reused from the Ledger, same as Channel Dashboard, R16). No
// /insights/attribute_gaps route: see internal/planes/control/insights'
// package doc comment for why R3 is not implemented.
//
// Every handler here reads only from pre-computed aggregates
// (insights.Store's List* methods, folded into a view by
// insights.Compute*) — never a raw scan of events (R15).

const insightsDateLayout = "2006-01-02"
const insightsMaxRangeDays = 92
const insightsMonthLayout = "2006-01"

// insightsDateRange parses from/to query params (yyyy-MM-dd), defaulting
// to the trailing 30 days, and enforces R15's 92-day cap. Mirrors
// channelDateRange's identical rule exactly (same param names, same
// default window, same cap) — both live in this package, so this stays a
// distinct, small function per plane/feature-area precedent (see
// channel.go's own doc comment on why each feature area keeps its own
// copy) rather than a shared one two unrelated read APIs would couple on.
func insightsDateRange(c echo.Context) (from, to time.Time, err error) {
	now := time.Now().UTC()
	to, err = parseInsightsDate(c.QueryParam("to"), now)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	from, err = parseInsightsDate(c.QueryParam("from"), to.AddDate(0, 0, -29))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if to.Before(from) {
		return time.Time{}, time.Time{}, errInvalidInsightsRange
	}
	if to.Sub(from) > time.Duration(insightsMaxRangeDays)*24*time.Hour {
		return time.Time{}, time.Time{}, errInvalidInsightsRange
	}
	return from, to, nil
}

func parseInsightsDate(v string, fallback time.Time) (time.Time, error) {
	if v == "" {
		return fallback, nil
	}
	return time.Parse(insightsDateLayout, v)
}

var errInvalidInsightsRange = echo.NewHTTPError(http.StatusUnprocessableEntity, "invalid range")

// ── GET /insights/product_demand?from=&to=&surface=&limit= ──────────────

func (h *handler) getProductDemand(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}
	if h.deps.Insights == nil {
		return c.JSON(http.StatusOK, domain.ListOf[insights.ProductDemandView]{Data: []insights.ProductDemandView{}, Page: domain.Page{HasMore: false}})
	}

	from, to, err := insightsDateRange(c)
	if err != nil {
		return errValidation(c, "from/to must be yyyy-MM-dd and span at most 92 days")
	}

	limit := insights.DefaultTopN
	if v := c.QueryParam("limit"); v != "" {
		if l, perr := strconv.Atoi(v); perr == nil && l > 0 {
			limit = l
		}
	}

	rows, err := h.deps.Insights.ListProductDemand(c.Request().Context(), string(principal.MerchantID), "", insights.DateKey(from), insights.DateKey(to))
	if err != nil {
		h.log.Error("list product demand failed", "error", err, "merchant_id", principal.MerchantID)
		return errInternal(c)
	}

	// 0 -> ComputeProductDemand applies R14's DefaultSuppressionThreshold;
	// no client override exposed today.
	views := insights.ComputeProductDemand(rows, c.QueryParam("surface"), 0, limit)
	return c.JSON(http.StatusOK, domain.ListOf[insights.ProductDemandView]{Data: views, Page: domain.Page{HasMore: false}})
}

// ── GET /insights/negotiation?from=&to= ──────────────────────────────────

func (h *handler) getNegotiationInsights(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}
	if h.deps.Insights == nil {
		return c.JSON(http.StatusOK, insights.ComputeNegotiationView(nil, nil, 0))
	}

	from, to, err := insightsDateRange(c)
	if err != nil {
		return errValidation(c, "from/to must be yyyy-MM-dd and span at most 92 days")
	}

	ctx := c.Request().Context()
	merchantID := string(principal.MerchantID)
	fromKey, toKey := insights.DateKey(from), insights.DateKey(to)

	daily, err := h.deps.Insights.ListNegotiationDaily(ctx, merchantID, "", fromKey, toKey)
	if err != nil {
		h.log.Error("list negotiation daily failed", "error", err, "merchant_id", merchantID)
		return errInternal(c)
	}
	mismatch, err := h.deps.Insights.ListPaymentHandlerMismatchDaily(ctx, merchantID, "", fromKey, toKey)
	if err != nil {
		h.log.Error("list payment handler mismatch daily failed", "error", err, "merchant_id", merchantID)
		return errInternal(c)
	}

	// 0 -> ComputeNegotiationView applies R14's DefaultSuppressionThreshold.
	return c.JSON(http.StatusOK, insights.ComputeNegotiationView(daily, mismatch, 0))
}

// ── GET /insights/capability_gaps?month= ─────────────────────────────────

func (h *handler) getCapabilityGaps(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}
	if h.deps.Insights == nil {
		return c.JSON(http.StatusOK, domain.ListOf[insights.CapabilityGapView]{Data: []insights.CapabilityGapView{}, Page: domain.Page{HasMore: false}})
	}

	monthParam := c.QueryParam("month")
	if monthParam == "" {
		monthParam = time.Now().UTC().Format(insightsMonthLayout)
	}
	t, err := time.Parse(insightsMonthLayout, monthParam)
	if err != nil {
		return errValidation(c, "month must be yyyy-MM")
	}
	month := t.Format("2006_01")

	rows, err := h.deps.Insights.ListCapabilityGaps(c.Request().Context(), string(principal.MerchantID), "", month)
	if err != nil {
		h.log.Error("list capability gaps failed", "error", err, "merchant_id", principal.MerchantID)
		return errInternal(c)
	}

	// R11/A4: default sort is estimated_impact_minor descending,
	// request_count descending as tiebreak — ComputeCapabilityGaps applies
	// this unconditionally; there is no sort query param to override it,
	// mirroring the Ledger's identical "one product invariant this
	// endpoint must never silently drop" stance (api/ledger.go).
	// 0 -> ComputeCapabilityGaps applies R14's DefaultSuppressionThreshold.
	views := insights.ComputeCapabilityGaps(rows, 0)
	return c.JSON(http.StatusOK, domain.ListOf[insights.CapabilityGapView]{Data: views, Page: domain.Page{HasMore: false}})
}
