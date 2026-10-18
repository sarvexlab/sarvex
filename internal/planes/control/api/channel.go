// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/planes/control/channel"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// This file is the control-plane read API for the Channel Dashboard
// (R13): GET /channel/summary, /channel/funnel, /channel/daily,
// /channel/comparison. Gated at the analytics tier (requireAnalyticsTier —
// reused from the Lost Revenue Ledger, R15: "consistent with the existing
// gate"). R15 names the gate tier "growth"; no plan literally named
// "growth" exists in this codebase's three commercial tiers
// (domain.PlanSelfServe/Concierge/Scale — see middleware.go's
// requireAnalyticsTier doc comment) — this reuses that same
// IsConcierge()-backed gate rather than inventing a fourth plan
// identifier, exactly as R15 asks.
//
// R14: every handler here reads only from pre-computed aggregates
// (channel.Store's DailyAggregate methods, channel.BaselineReader) —
// never a raw scan of sessions or events.

const channelDateLayout = "2006-01-02"
const channelMaxRangeDays = 92

func parseChannelDate(v string, fallback time.Time) (time.Time, error) {
	if v == "" {
		return fallback, nil
	}
	return time.Parse(channelDateLayout, v)
}

// channelDateRange parses from/to query params (yyyy-MM-dd), defaulting to
// the trailing 30 days, and enforces R13's 92-day cap.
func channelDateRange(c echo.Context) (from, to time.Time, err error) {
	now := time.Now().UTC()
	to, err = parseChannelDate(c.QueryParam("to"), now)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	from, err = parseChannelDate(c.QueryParam("from"), to.AddDate(0, 0, -29))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if to.Before(from) {
		return time.Time{}, time.Time{}, errInvalidChannelRange
	}
	if to.Sub(from) > time.Duration(channelMaxRangeDays)*24*time.Hour {
		return time.Time{}, time.Time{}, errInvalidChannelRange
	}
	return from, to, nil
}

var errInvalidChannelRange = echo.NewHTTPError(http.StatusUnprocessableEntity, "invalid range")

type channelRatesView struct {
	CartRate       float64 `json:"cart_rate"`
	CheckoutRate   float64 `json:"checkout_rate"`
	CompletionRate float64 `json:"completion_rate"`
	AOVMinor       int64   `json:"aov_minor"`
}

func toRatesView(a channel.DailyAggregate) channelRatesView {
	r := channel.ComputeFunnelRates(a)
	return channelRatesView{CartRate: r.CartRate, CheckoutRate: r.CheckoutRate, CompletionRate: r.CompletionRate, AOVMinor: r.AOVMinor}
}

// sumAggregates folds aggs into one DailyAggregate and reports whether the
// folded days/surfaces disagreed on currency (currencyMixed) — the caller
// must thread that bool through to the view layer itself; it cannot be
// recovered afterward from the summed result alone (a single resolved
// Currency value is indistinguishable from "only one currency ever
// appeared" once collapsed).
func sumAggregates(aggs []channel.DailyAggregate) (sum channel.DailyAggregate, currencyMixed bool) {
	for _, a := range aggs {
		sum.Sessions += a.Sessions
		sum.SessionsReachedCart += a.SessionsReachedCart
		sum.SessionsReachedCheckout += a.SessionsReachedCheckout
		sum.CheckoutsInitiated += a.CheckoutsInitiated
		sum.CheckoutsCompleted += a.CheckoutsCompleted
		sum.SessionsInferred += a.SessionsInferred
		sum.RevenueMinor += a.RevenueMinor
		sum.DiscoveryRequests += a.DiscoveryRequests
	}
	sum.Currency, currencyMixed = aggregateCurrency(aggs)
	return sum, currencyMixed
}

// aggregateCurrency resolves a single currency across aggs, distinguishing
// "no revenue-bearing day yet" (empty, not mixed) from a genuine conflict
// across days/surfaces summed together (empty, mixed=true) — a gap the UI
// consuming this API had no way to detect before: the field wasn't
// exposed at all, and a silently-picked-first-seen currency would have
// misrepresented a mixed-currency range as single-currency. Every
// DailyAggregate is currently populated from CheckoutCompletedEvent's
// order total, which today only ever carries "USD" (see
// internal/planes/serving/checkout_lifecycle_events.go's cartSummaryFrom
// doc comment) — so a real conflict cannot occur yet, but the read API
// must not assume that will always hold.
func aggregateCurrency(aggs []channel.DailyAggregate) (currency string, mixed bool) {
	seen := map[string]bool{}
	for _, a := range aggs {
		if a.Currency != "" {
			seen[a.Currency] = true
		}
	}
	if len(seen) == 0 {
		return "", false
	}
	if len(seen) > 1 {
		return "", true
	}
	for c := range seen {
		return c, false
	}
	return "", false
}

// sessionsConfirmed is R2's explicit confirmed count, derived rather than
// stored (Sessions - SessionsInferred) — the categorical
// confirmed-vs-inferred split R2 requires is genuinely two counts, not one
// flag: an aggregate rolls up many sessions that can honestly be a mix of
// both, so a single "attribution: confirmed|inferred" field on an
// aggregate would misrepresent it. Exposing both counts explicitly (rather
// than requiring the caller to subtract sessions_inferred from sessions
// itself) is the fix: the caller no longer has to reconstruct this from
// two other fields, which is what forced it to fall back to a summary
// numeric-only signal.
func sessionsConfirmed(a channel.DailyAggregate) int64 {
	c := a.Sessions - a.SessionsInferred
	if c < 0 {
		return 0
	}
	return c
}

func filterBySurface(aggs []channel.DailyAggregate, surface string) []channel.DailyAggregate {
	if surface == "" || surface == channel.SurfaceAll {
		return aggs
	}
	out := make([]channel.DailyAggregate, 0, len(aggs))
	for _, a := range aggs {
		if string(a.Surface) == surface {
			out = append(out, a)
		}
	}
	return out
}

func distinctSurfaces(aggs []channel.DailyAggregate) []string {
	seen := map[string]bool{}
	var order []string
	for _, a := range aggs {
		s := string(a.Surface)
		if !seen[s] {
			seen[s] = true
			order = append(order, s)
		}
	}
	return order
}

// ── GET /channel/summary?month=&surface=&store_id= ─────────────────────

type channelSummaryView struct {
	// Surface is a real Surface value, or the literal "all" for the
	// combined row — genuinely a wider domain than the channel_surface
	// enum used elsewhere (query filters, channel_daily_entry), which
	// never emits "all". See the OpenAPI schema (channel_surface_or_all)
	// added alongside this fix.
	Surface            string `json:"surface"`
	Month              string `json:"month"`
	Sessions           int64  `json:"sessions"`
	SessionsConfirmed  int64  `json:"sessions_confirmed"`
	SessionsInferred   int64  `json:"sessions_inferred"`
	CheckoutsCompleted int64  `json:"checkouts_completed"`
	RevenueMinor       int64  `json:"revenue_minor"`
	// Currency is "" (with CurrencyMixed=false) when no revenue-bearing
	// day exists yet in this row, or "" with CurrencyMixed=true when the
	// summed days/surfaces genuinely disagree on currency — never a
	// silently-picked first-seen value.
	Currency      string `json:"currency,omitempty"`
	CurrencyMixed bool   `json:"currency_mixed,omitempty"`
	channelRatesView
	PriorPeriod struct {
		SessionsDelta           int64   `json:"sessions_delta"`
		CheckoutsCompletedDelta int64   `json:"checkouts_completed_delta"`
		RevenueMinorDelta       int64   `json:"revenue_minor_delta"`
		AOVMinorDelta           int64   `json:"aov_minor_delta"`
		CompletionRateDelta     float64 `json:"completion_rate_delta"`
	} `json:"prior_period"`
}

func buildChannelSummaryView(surface, month string, current, prior channel.DailyAggregate, currentCurrencyMixed bool) channelSummaryView {
	v := channelSummaryView{
		Surface: surface, Month: month,
		Sessions: current.Sessions, SessionsConfirmed: sessionsConfirmed(current), SessionsInferred: current.SessionsInferred,
		CheckoutsCompleted: current.CheckoutsCompleted, RevenueMinor: current.RevenueMinor,
		Currency: current.Currency, CurrencyMixed: currentCurrencyMixed,
		channelRatesView: toRatesView(current),
	}
	priorRates := channel.ComputeFunnelRates(prior)
	v.PriorPeriod.SessionsDelta = current.Sessions - prior.Sessions
	v.PriorPeriod.CheckoutsCompletedDelta = current.CheckoutsCompleted - prior.CheckoutsCompleted
	v.PriorPeriod.RevenueMinorDelta = current.RevenueMinor - prior.RevenueMinor
	v.PriorPeriod.AOVMinorDelta = v.AOVMinor - priorRates.AOVMinor
	v.PriorPeriod.CompletionRateDelta = v.CompletionRate - priorRates.CompletionRate
	return v
}

// GET /channel/summary?month=yyyy-MM&store_id=
//
// Returns one row per surface the merchant has data for that month, plus
// one combined "all" row, each with prior-calendar-month deltas computed
// server-side (R13).
func (h *handler) getChannelSummary(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}
	if h.deps.Channel == nil {
		return c.JSON(http.StatusOK, domain.ListOf[channelSummaryView]{Data: []channelSummaryView{}, Page: domain.Page{HasMore: false}})
	}

	monthParam := c.QueryParam("month")
	month := time.Now().UTC()
	if monthParam != "" {
		parsed, err := time.Parse("2006-01", monthParam)
		if err != nil {
			return errValidation(c, "month must be yyyy-MM")
		}
		month = parsed
	}
	monthStart := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, -1)
	priorStart := monthStart.AddDate(0, -1, 0)
	priorEnd := priorStart.AddDate(0, 1, -1)

	merchantID := string(principal.MerchantID)
	storeID := c.QueryParam("store_id")

	current, err := h.deps.Channel.ListDailyAggregates(c.Request().Context(), merchantID, storeID, "", channel.DateKey(monthStart), channel.DateKey(monthEnd))
	if err != nil {
		h.log.Error("get channel summary failed", "error", err, "merchant_id", merchantID)
		return errInternal(c)
	}
	prior, err := h.deps.Channel.ListDailyAggregates(c.Request().Context(), merchantID, storeID, "", channel.DateKey(priorStart), channel.DateKey(priorEnd))
	if err != nil {
		h.log.Error("get channel prior summary failed", "error", err, "merchant_id", merchantID)
		return errInternal(c)
	}

	monthKey := monthStart.Format("2006-01")
	allCurrent, allMixed := sumAggregates(current)
	allPrior, _ := sumAggregates(prior)
	views := []channelSummaryView{buildChannelSummaryView(channel.SurfaceAll, monthKey, allCurrent, allPrior, allMixed)}
	for _, surface := range distinctSurfaces(current) {
		curSurf, curMixed := sumAggregates(filterBySurface(current, surface))
		priorSurf, _ := sumAggregates(filterBySurface(prior, surface))
		views = append(views, buildChannelSummaryView(surface, monthKey, curSurf, priorSurf, curMixed))
	}
	return c.JSON(http.StatusOK, domain.ListOf[channelSummaryView]{Data: views, Page: domain.Page{HasMore: false}})
}

// ── GET /channel/funnel?from=&to=&surface=&store_id= ───────────────────

type channelFunnelView struct {
	From                    string           `json:"from"`
	To                      string           `json:"to"`
	Surface                 string           `json:"surface"` // a real Surface value, or "all" (channel_surface_or_all)
	Sessions                int64            `json:"sessions"`
	SessionsConfirmed       int64            `json:"sessions_confirmed"`
	SessionsInferred        int64            `json:"sessions_inferred"`
	SessionsReachedCart     int64            `json:"sessions_reached_cart"`
	SessionsReachedCheckout int64            `json:"sessions_reached_checkout"`
	CheckoutsInitiated      int64            `json:"checkouts_initiated"`
	CheckoutsCompleted      int64            `json:"checkouts_completed"`
	Currency                string           `json:"currency,omitempty"`
	CurrencyMixed           bool             `json:"currency_mixed,omitempty"`
	Rates                   channelRatesView `json:"rates"`
}

func (h *handler) getChannelFunnel(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}
	if h.deps.Channel == nil {
		return c.JSON(http.StatusOK, channelFunnelView{})
	}
	from, to, err := channelDateRange(c)
	if err != nil {
		return errValidation(c, "from/to must be yyyy-MM-dd and span at most 92 days")
	}
	surfaceParam := c.QueryParam("surface")

	aggs, err := h.deps.Channel.ListDailyAggregates(c.Request().Context(), string(principal.MerchantID), c.QueryParam("store_id"), "", channel.DateKey(from), channel.DateKey(to))
	if err != nil {
		h.log.Error("get channel funnel failed", "error", err, "merchant_id", principal.MerchantID)
		return errInternal(c)
	}
	aggs = filterBySurface(aggs, surfaceParam)
	sum, mixed := sumAggregates(aggs)

	view := channelFunnelView{
		From: channel.DateKey(from), To: channel.DateKey(to),
		Surface:                 surfaceOrAll(surfaceParam),
		Sessions:                sum.Sessions,
		SessionsConfirmed:       sessionsConfirmed(sum),
		SessionsInferred:        sum.SessionsInferred,
		SessionsReachedCart:     sum.SessionsReachedCart,
		SessionsReachedCheckout: sum.SessionsReachedCheckout,
		CheckoutsInitiated:      sum.CheckoutsInitiated,
		CheckoutsCompleted:      sum.CheckoutsCompleted,
		Currency:                sum.Currency,
		CurrencyMixed:           mixed,
		Rates:                   toRatesView(sum),
	}
	return c.JSON(http.StatusOK, view)
}

func surfaceOrAll(s string) string {
	if s == "" {
		return channel.SurfaceAll
	}
	return s
}

// ── GET /channel/daily?from=&to=&surface=&store_id= ────────────────────

type channelDailyView struct {
	Date                    string `json:"date"`
	Surface                 string `json:"surface"` // always a real Surface value — a daily row is never a combined "all" row
	StoreID                 string `json:"store_id"`
	Sessions                int64  `json:"sessions"`
	SessionsConfirmed       int64  `json:"sessions_confirmed"`
	SessionsInferred        int64  `json:"sessions_inferred"`
	SessionsReachedCart     int64  `json:"sessions_reached_cart"`
	SessionsReachedCheckout int64  `json:"sessions_reached_checkout"`
	CheckoutsInitiated      int64  `json:"checkouts_initiated"`
	CheckoutsCompleted      int64  `json:"checkouts_completed"`
	RevenueMinor            int64  `json:"revenue_minor"`
	// Currency is a single day/store/surface's own value — never
	// ambiguous the way a summed range can be (see channelSummaryView's
	// Currency/CurrencyMixed), so no mixed flag is needed here.
	Currency          string           `json:"currency,omitempty"`
	DiscoveryRequests int64            `json:"discovery_requests"`
	Rates             channelRatesView `json:"rates"`
}

func toDailyView(a channel.DailyAggregate) channelDailyView {
	return channelDailyView{
		Date: a.Date, Surface: string(a.Surface), StoreID: a.StoreID,
		Sessions: a.Sessions, SessionsConfirmed: sessionsConfirmed(a), SessionsInferred: a.SessionsInferred,
		SessionsReachedCart: a.SessionsReachedCart, SessionsReachedCheckout: a.SessionsReachedCheckout,
		CheckoutsInitiated: a.CheckoutsInitiated, CheckoutsCompleted: a.CheckoutsCompleted,
		RevenueMinor: a.RevenueMinor, Currency: a.Currency, DiscoveryRequests: a.DiscoveryRequests,
		Rates: toRatesView(a),
	}
}

func (h *handler) getChannelDaily(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}
	if h.deps.Channel == nil {
		return c.JSON(http.StatusOK, domain.ListOf[channelDailyView]{Data: []channelDailyView{}, Page: domain.Page{HasMore: false}})
	}
	from, to, err := channelDateRange(c)
	if err != nil {
		return errValidation(c, "from/to must be yyyy-MM-dd and span at most 92 days")
	}
	aggs, err := h.deps.Channel.ListDailyAggregates(c.Request().Context(), string(principal.MerchantID), c.QueryParam("store_id"), channel.Surface(c.QueryParam("surface")), channel.DateKey(from), channel.DateKey(to))
	if err != nil {
		h.log.Error("get channel daily failed", "error", err, "merchant_id", principal.MerchantID)
		return errInternal(c)
	}
	views := make([]channelDailyView, 0, len(aggs))
	for _, a := range aggs {
		views = append(views, toDailyView(a))
	}
	return c.JSON(http.StatusOK, domain.ListOf[channelDailyView]{Data: views, Page: domain.Page{HasMore: false}})
}

// ── GET /channel/comparison?from=&to=&store_id= ─────────────────────────

// baselineConfidence is R11's tri-state confidence indicator, rolled up
// across every day in the requested range — not collapsed into a binary
// available/unavailable. R12's "never substitute an estimate" is honored
// by what confidenceUnavailable means (zero usable days: nothing is
// computed or shown), not by discarding a genuinely partial-but-real
// result: a range where most days are connector_reported and a few are
// partial/missing still has real numbers worth showing, honestly labeled
// as incomplete, rather than being flattened to the same "unavailable"
// state as a range with zero data at all — those are materially different
// situations a merchant needs to be able to tell apart.
type baselineConfidence string

const (
	confidenceConnectorReported baselineConfidence = "connector_reported"
	confidencePartial           baselineConfidence = "partial"
	confidenceUnavailable       baselineConfidence = "unavailable"
)

const baselineUnavailableReasonNoData = "no baseline data synced for this period"

// metricComparisonView is R12's per-metric available/reason shape — never
// a bare number, so a client can never mistake "unavailable" for zero.
// Confidence is populated whenever Available is true, distinguishing a
// fully connector-reported range from one built from a partial/incomplete
// set of days (R11) — Available alone collapsed that distinction before.
type metricComparisonView struct {
	Available     bool               `json:"available"`
	Confidence    baselineConfidence `json:"confidence"`
	Reason        string             `json:"reason,omitempty"`
	AgenticMinor  int64              `json:"agentic_minor,omitempty"`
	BaselineMinor int64              `json:"baseline_minor,omitempty"`
	DeltaMinor    int64              `json:"delta_minor,omitempty"`
}

type shareComparisonView struct {
	Available  bool               `json:"available"`
	Confidence baselineConfidence `json:"confidence"`
	Reason     string             `json:"reason,omitempty"`
	Share      float64            `json:"share,omitempty"`
}

type channelComparisonView struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Agentic struct {
		RevenueMinor       int64  `json:"revenue_minor"`
		CheckoutsCompleted int64  `json:"checkouts_completed"`
		AOVMinor           int64  `json:"aov_minor"`
		Currency           string `json:"currency,omitempty"`
		CurrencyMixed      bool   `json:"currency_mixed,omitempty"`
	} `json:"agentic"`
	// WebBaseline mirrors Agentic's shape so the UI can render both sides
	// symmetrically — previously only reachable piecemeal through each
	// metricComparisonView's baseline_minor fields, with no single place
	// to read the baseline's own currency or confidence.
	WebBaseline struct {
		Orders        int64              `json:"orders"`
		RevenueMinor  int64              `json:"revenue_minor"`
		AOVMinor      int64              `json:"aov_minor"`
		Currency      string             `json:"currency,omitempty"`
		CurrencyMixed bool               `json:"currency_mixed,omitempty"`
		Confidence    baselineConfidence `json:"confidence"`
	} `json:"web_baseline"`
	// Revenue/AOV are comparable (both sides measure the same thing in
	// the same unit); agent sessions and web sessions are deliberately
	// never compared here (R12: different definitions, different
	// measurement — no session-count field exists on this response at
	// all, not even an unavailable one).
	Revenue             metricComparisonView `json:"revenue"`
	AOV                 metricComparisonView `json:"aov"`
	AgenticShareOfTotal shareComparisonView  `json:"agentic_share_of_total"`
}

// baselineRollup is the range-level fold of every BaselineDay in
// [from,to]: how many of the expected days are connector_reported vs.
// partial/missing, the summed orders/revenue from whatever real data
// exists (partial days included — they are real, if incomplete, numbers,
// not estimates), and whether those days agree on currency.
type baselineRollup struct {
	confidence           baselineConfidence
	reason               string
	orders, revenueMinor int64
	currency             string
	currencyMixed        bool
}

func rollupBaseline(days []channel.BaselineDay, expectedDays int) baselineRollup {
	reported, usable := 0, 0
	var orders, revenueMinor int64
	seenCurrencies := map[string]bool{}
	for _, d := range days {
		if d.Confidence == channel.BaselineUnavailable {
			continue
		}
		usable++
		if d.Confidence == channel.BaselineConnectorReported {
			reported++
		}
		orders += d.Orders
		revenueMinor += d.RevenueMinor
		if d.Currency != "" {
			seenCurrencies[d.Currency] = true
		}
	}

	currency, mixed := "", false
	if len(seenCurrencies) == 1 {
		for c := range seenCurrencies {
			currency = c
		}
	} else if len(seenCurrencies) > 1 {
		mixed = true
	}

	if usable == 0 {
		return baselineRollup{confidence: confidenceUnavailable, reason: baselineUnavailableReasonNoData}
	}
	if reported == expectedDays {
		return baselineRollup{confidence: confidenceConnectorReported, orders: orders, revenueMinor: revenueMinor, currency: currency, currencyMixed: mixed}
	}
	incomplete := expectedDays - reported
	reason := fmt.Sprintf("%d of %d days in this period have incomplete or missing web-baseline data — figures below are computed from what was reported, not estimated for the gap", incomplete, expectedDays)
	return baselineRollup{confidence: confidencePartial, reason: reason, orders: orders, revenueMinor: revenueMinor, currency: currency, currencyMixed: mixed}
}

func (h *handler) getChannelComparison(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}
	if h.deps.Channel == nil || h.deps.WebBaseline == nil {
		view := channelComparisonView{
			Revenue:             metricComparisonView{Available: false, Confidence: confidenceUnavailable, Reason: baselineUnavailableReasonNoData},
			AOV:                 metricComparisonView{Available: false, Confidence: confidenceUnavailable, Reason: baselineUnavailableReasonNoData},
			AgenticShareOfTotal: shareComparisonView{Available: false, Confidence: confidenceUnavailable, Reason: baselineUnavailableReasonNoData},
		}
		view.WebBaseline.Confidence = confidenceUnavailable
		return c.JSON(http.StatusOK, view)
	}
	from, to, err := channelDateRange(c)
	if err != nil {
		return errValidation(c, "from/to must be yyyy-MM-dd and span at most 92 days")
	}
	merchantID := string(principal.MerchantID)
	storeID := c.QueryParam("store_id")

	aggs, err := h.deps.Channel.ListDailyAggregates(c.Request().Context(), merchantID, storeID, "", channel.DateKey(from), channel.DateKey(to))
	if err != nil {
		h.log.Error("get channel comparison (agentic) failed", "error", err, "merchant_id", merchantID)
		return errInternal(c)
	}
	agentic, agenticMixed := sumAggregates(aggs)

	baselineDays, err := h.deps.WebBaseline.ListRange(c.Request().Context(), merchantID, storeID, channel.DateKey(from), channel.DateKey(to))
	if err != nil {
		h.log.Error("get channel comparison (baseline) failed", "error", err, "merchant_id", merchantID)
		return errInternal(c)
	}

	view := channelComparisonView{From: channel.DateKey(from), To: channel.DateKey(to)}
	view.Agentic.RevenueMinor = agentic.RevenueMinor
	view.Agentic.CheckoutsCompleted = agentic.CheckoutsCompleted
	rates := channel.ComputeFunnelRates(agentic)
	view.Agentic.AOVMinor = rates.AOVMinor
	view.Agentic.Currency = agentic.Currency
	view.Agentic.CurrencyMixed = agenticMixed

	expectedDays := int(to.Sub(from).Hours()/24) + 1
	baseline := rollupBaseline(baselineDays, expectedDays)

	view.WebBaseline.Orders = baseline.orders
	view.WebBaseline.RevenueMinor = baseline.revenueMinor
	view.WebBaseline.Currency = baseline.currency
	view.WebBaseline.CurrencyMixed = baseline.currencyMixed
	view.WebBaseline.Confidence = baseline.confidence
	if baseline.orders > 0 {
		view.WebBaseline.AOVMinor = baseline.revenueMinor / baseline.orders
	}

	if baseline.confidence == confidenceUnavailable {
		view.Revenue = metricComparisonView{Available: false, Confidence: confidenceUnavailable, Reason: baseline.reason}
		view.AOV = metricComparisonView{Available: false, Confidence: confidenceUnavailable, Reason: baseline.reason}
		view.AgenticShareOfTotal = shareComparisonView{Available: false, Confidence: confidenceUnavailable, Reason: baseline.reason}
		return c.JSON(http.StatusOK, view)
	}

	// R12: even at confidence=partial, these are real connector-reported
	// numbers (possibly missing a few days), never an estimate filled in
	// for the gap — the Reason/Confidence pair is what tells the caller
	// it's incomplete, not a substituted number.
	view.Revenue = metricComparisonView{
		Available: true, Confidence: baseline.confidence, Reason: baseline.reason,
		AgenticMinor: agentic.RevenueMinor, BaselineMinor: baseline.revenueMinor,
		DeltaMinor: agentic.RevenueMinor - baseline.revenueMinor,
	}
	view.AOV = metricComparisonView{
		Available: true, Confidence: baseline.confidence, Reason: baseline.reason,
		AgenticMinor: rates.AOVMinor, BaselineMinor: view.WebBaseline.AOVMinor,
		DeltaMinor: rates.AOVMinor - view.WebBaseline.AOVMinor,
	}

	// R12's audit: Sarvex's checkout completion places the order directly
	// in the merchant's own platform (e.g. WooCommerce POST /orders) —
	// agentic orders already land in the connector's own order stream, so
	// a real connector-reported baseline total already includes them.
	// The share of total is therefore agentic / baseline (the connector's
	// total), never agentic / (agentic + baseline) — the latter would
	// double-count agentic orders that are already inside baseline.revenueMinor.
	if baseline.revenueMinor > 0 {
		view.AgenticShareOfTotal = shareComparisonView{
			Available: true, Confidence: baseline.confidence, Reason: baseline.reason,
			Share: float64(agentic.RevenueMinor) / float64(baseline.revenueMinor),
		}
	} else {
		view.AgenticShareOfTotal = shareComparisonView{Available: false, Confidence: confidenceUnavailable, Reason: baselineUnavailableReasonNoData}
	}
	return c.JSON(http.StatusOK, view)
}
