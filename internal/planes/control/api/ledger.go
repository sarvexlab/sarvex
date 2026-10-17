// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/samber/lo"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/ledger"
)

// This file is the control-plane read API for the Lost Revenue Ledger
// (R13): GET /ledger/summary, GET /ledger/entries, PATCH
// /ledger/entries/{entry_id}. Gated at the analytics tier (requireAnalyticsTier,
// see route registration in handler.go) — R15: the ledger worker writes for
// every tier regardless, only these reads are gated, so a merchant who
// upgrades sees their full backlog immediately.

const ledgerMonthLayout = "2006-01"

func monthKeyFromQuery(v string) (string, error) {
	if v == "" {
		v = time.Now().UTC().Format(ledgerMonthLayout)
	}
	t, err := time.Parse(ledgerMonthLayout, v)
	if err != nil {
		return "", err
	}
	return t.Format("2006_01"), nil
}

func priorMonthKey(monthKey string) (string, error) {
	t, err := time.Parse("2006_01", monthKey)
	if err != nil {
		return "", err
	}
	return t.AddDate(0, -1, 0).Format("2006_01"), nil
}

type causeBreakdownView struct {
	Cause            string `json:"cause"`
	Count            int64  `json:"count"`
	LostRevenueMinor int64  `json:"lost_revenue_minor"`
}

type ledgerSummaryView struct {
	Month                     string               `json:"month"`
	Currency                  string               `json:"currency"`
	Causes                    []causeBreakdownView `json:"causes"`
	TotalCount                int64                `json:"total_count"`
	TotalLostRevenueMinor     int64                `json:"total_lost_revenue_minor"`
	EntriesWithUnknownRevenue int64                `json:"entries_with_unknown_revenue"`
	PriorMonthDeltaMinor      int64                `json:"prior_month_delta_minor"`
}

// ledgerSummaryListView wraps GET /ledger/summary's response — always a
// list, never a bare object (see getLedgerSummary's doc comment on why),
// so a client that wants "every currency this merchant has data for" and
// a client that filters to one currency both parse the identical shape.
type ledgerSummaryListView struct {
	Data []ledgerSummaryView `json:"data"`
}

func buildSummaryView(month, currency string, current, prior *ledger.Rollup) ledgerSummaryView {
	view := ledgerSummaryView{Month: month, Currency: currency, Causes: []causeBreakdownView{}}
	if current != nil {
		view.TotalCount = current.TotalCount
		view.TotalLostRevenueMinor = current.TotalLostRevenueMinor
		view.EntriesWithUnknownRevenue = current.EntriesWithUnknownRevenue
		view.Causes = lo.Map(current.Causes, func(cb ledger.CauseBreakdown, _ int) causeBreakdownView {
			return causeBreakdownView{Cause: string(cb.Cause), Count: cb.Count, LostRevenueMinor: cb.LostRevenueMinor}
		})
	}
	// R14's sort invariant applies to the causes breakdown too — money order
	// is the point of the feature regardless of which endpoint returns it.
	sort.Slice(view.Causes, func(i, j int) bool { return view.Causes[i].LostRevenueMinor > view.Causes[j].LostRevenueMinor })

	priorTotal := int64(0)
	if prior != nil {
		priorTotal = prior.TotalLostRevenueMinor
	}
	view.PriorMonthDeltaMinor = view.TotalLostRevenueMinor - priorTotal
	return view
}

// GET /ledger/summary?month=&currency=
//
// currency is optional. Given, the response is a one-element list scoped
// to that currency (unchanged single-currency behavior). Omitted, the
// response holds one element per currency the merchant actually has a
// rollup for that month (R9: read straight from the ledger's own rollups,
// never derived from store configuration — a store's configured currency
// isn't necessarily every currency a checkout failed in, and this avoids
// making the console enumerate stores and call this endpoint once per
// currency just to build the R4 per-currency grouping). Never summed
// across currencies either way — each element is independently computed,
// exactly as GetRollup already returns it.
func (h *handler) getLedgerSummary(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}
	month, err := monthKeyFromQuery(c.QueryParam("month"))
	if err != nil {
		return errValidation(c, "month must be yyyy-MM")
	}
	prevMonth, err := priorMonthKey(month)
	if err != nil {
		return errValidation(c, "month must be yyyy-MM")
	}

	if h.deps.Ledger == nil {
		return c.JSON(http.StatusOK, ledgerSummaryListView{Data: []ledgerSummaryView{}})
	}

	ctx := c.Request().Context()
	merchantID := string(principal.MerchantID)

	if currency := c.QueryParam("currency"); currency != "" {
		current, err := h.deps.Ledger.GetRollup(ctx, merchantID, month, currency)
		if err != nil && !errors.Is(err, ledger.ErrNotFound) {
			h.log.Error("get ledger summary failed", "error", err, "merchant_id", merchantID)
			return errInternal(c)
		}
		prior, err := h.deps.Ledger.GetRollup(ctx, merchantID, prevMonth, currency)
		if err != nil && !errors.Is(err, ledger.ErrNotFound) {
			h.log.Error("get prior ledger rollup failed", "error", err, "merchant_id", merchantID)
			return errInternal(c)
		}
		return c.JSON(http.StatusOK, ledgerSummaryListView{Data: []ledgerSummaryView{buildSummaryView(month, currency, current, prior)}})
	}

	currentRollups, err := h.deps.Ledger.ListRollups(ctx, merchantID, month)
	if err != nil {
		h.log.Error("list ledger summaries failed", "error", err, "merchant_id", merchantID)
		return errInternal(c)
	}
	views := make([]ledgerSummaryView, 0, len(currentRollups))
	for _, current := range currentRollups {
		prior, err := h.deps.Ledger.GetRollup(ctx, merchantID, prevMonth, current.Currency)
		if err != nil && !errors.Is(err, ledger.ErrNotFound) {
			h.log.Error("get prior ledger rollup failed", "error", err, "merchant_id", merchantID, "currency", current.Currency)
			return errInternal(c)
		}
		cur := current
		views = append(views, buildSummaryView(month, current.Currency, &cur, prior))
	}
	return c.JSON(http.StatusOK, ledgerSummaryListView{Data: views})
}

type ledgerEntryView struct {
	EntryID           string    `json:"entry_id"`
	OccurredAt        time.Time `json:"occurred_at"`
	CheckoutID        string    `json:"checkout_id"`
	StoreID           string    `json:"store_id"`
	Surface           string    `json:"surface,omitempty"`
	Stage             string    `json:"stage"`
	FailureCode       string    `json:"failure_code"`
	Cause             string    `json:"cause"`
	RecommendedAction string    `json:"recommended_action"`
	LostRevenueMinor  int64     `json:"lost_revenue_minor"`
	Currency          string    `json:"currency,omitempty"`
	RevenueUnknown    bool      `json:"revenue_unknown"`
	Status            string    `json:"status"`
	StatusChangedAt   time.Time `json:"status_changed_at"`
	CreatedAt         time.Time `json:"created_at"`
}

func toLedgerEntryView(e ledger.Entry) ledgerEntryView {
	return ledgerEntryView{
		EntryID: e.EntryID, OccurredAt: e.OccurredAt, CheckoutID: e.CheckoutID, StoreID: e.StoreID,
		Surface: e.Surface, Stage: e.Stage, FailureCode: e.FailureCode, Cause: string(e.Cause),
		RecommendedAction: e.RecommendedAction, LostRevenueMinor: e.LostRevenueMinor, Currency: e.Currency,
		RevenueUnknown: e.RevenueUnknown, Status: string(e.Status), StatusChangedAt: e.StatusChangedAt, CreatedAt: e.CreatedAt,
	}
}

// GET /ledger/entries?cause=&status=&store_id=&surface=&from=&to=&sort=&cursor=&limit=
//
// Default sort is lost_revenue_minor descending, occurred_at descending as
// tiebreak (R14) — the one product invariant this endpoint must never
// silently drop, so sortMode is left empty unless the client explicitly asks
// for ledger.SortOccurredAtDesc.
func (h *handler) listLedgerEntries(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}
	if h.deps.Ledger == nil {
		return c.JSON(http.StatusOK, domain.ListOf[ledgerEntryView]{Data: []ledgerEntryView{}, Page: domain.Page{HasMore: false}})
	}

	limit := 25
	if v := c.QueryParam("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}
	filter := ledger.ListFilter{
		Cause:   ledger.Cause(c.QueryParam("cause")),
		Status:  ledger.Status(c.QueryParam("status")),
		StoreID: c.QueryParam("store_id"),
		Surface: c.QueryParam("surface"),
	}
	if v := c.QueryParam("from"); v != "" {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return errValidation(c, "from must be RFC3339")
		}
		filter.From = &parsed
	}
	if v := c.QueryParam("to"); v != "" {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return errValidation(c, "to must be RFC3339")
		}
		filter.To = &parsed
	}

	sortMode := c.QueryParam("sort")
	switch sortMode {
	case "", ledger.SortRevenueDesc, ledger.SortOccurredAtDesc:
	default:
		return errValidation(c, "sort must be lost_revenue_minor or occurred_at")
	}

	entries, cursor, hasMore, err := h.deps.Ledger.ListEntries(c.Request().Context(), string(principal.MerchantID), filter, sortMode, c.QueryParam("cursor"), limit)
	if errors.Is(err, ledger.ErrInvalidCursor) {
		return errValidation(c, "invalid cursor")
	}
	if err != nil {
		h.log.Error("list ledger entries failed", "error", err, "merchant_id", principal.MerchantID)
		return errInternal(c)
	}

	var cursorPtr *string
	if hasMore {
		cursorPtr = &cursor
	}
	return c.JSON(http.StatusOK, domain.ListOf[ledgerEntryView]{
		Data: lo.Map(entries, func(e ledger.Entry, _ int) ledgerEntryView { return toLedgerEntryView(e) }),
		Page: domain.Page{Cursor: cursorPtr, HasMore: hasMore},
	})
}

type ledgerEntryStatusPatch struct {
	Status string `json:"status"`
}

// PATCH /ledger/entries/{entry_id}
//
// Status transitions only, forward-only (R13): open -> acknowledged ->
// resolved, never a reversal.
func (h *handler) updateLedgerEntryStatus(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}
	entryID := c.Param("entry_id")
	if entryID == "" {
		return errValidation(c, "entry_id required")
	}
	if h.deps.Ledger == nil {
		return errNotFound(c)
	}

	var body ledgerEntryStatusPatch
	if err := c.Bind(&body); err != nil {
		return errValidation(c, "invalid request body")
	}
	next := ledger.Status(body.Status)
	switch next {
	case ledger.StatusOpen, ledger.StatusAcknowledged, ledger.StatusResolved:
	default:
		return errValidation(c, "status must be one of: open, acknowledged, resolved")
	}

	updated, err := h.deps.Ledger.UpdateEntryStatus(c.Request().Context(), string(principal.MerchantID), entryID, next, time.Now().UTC())
	if errors.Is(err, ledger.ErrNotFound) {
		return errNotFound(c)
	}
	if errors.Is(err, ledger.ErrInvalidTransition) {
		return errConflict(c, "status transitions are forward-only (open -> acknowledged -> resolved)")
	}
	if err != nil {
		h.log.Error("update ledger entry status failed", "error", err, "entry_id", entryID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, toLedgerEntryView(*updated))
}
