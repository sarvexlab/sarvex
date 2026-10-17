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

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/checkoutescalation"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// This file is the control-plane read API for the Checkout Escalation
// Console (R11): GET /checkout-sessions, /checkout-sessions/{id},
// /checkout-sessions/{id}/timeline, /checkout-sessions/live. Available at
// every billing tier (see the route registration in handler.go) — it is
// diagnostic, and the support burden falls on Sarvex otherwise (R13).
//
// "Live" is polling, not streaming (R12) — no websockets/SSE here; the
// console is expected to poll GET /checkout-sessions/live on an interval
// (documented in sarvex-v1-control-plane.yaml, ~15-30s), same as every
// other console list view.

type cartSummaryView struct {
	Currency   string `json:"currency"`
	TotalMinor int64  `json:"total_minor"`
	ItemCount  int    `json:"item_count"`
}

type escalationView struct {
	Reason            string     `json:"reason"`
	EscalatedAt       time.Time  `json:"escalated_at"`
	ContinueURLIssued bool       `json:"continue_url_issued"`
	ReturnedAt        *time.Time `json:"returned_at,omitempty"`
	Resolved          bool       `json:"resolved"`
}

type failureView struct {
	Code  string `json:"code"`
	Cause string `json:"cause"`
	Stage string `json:"stage"`
}

type diagnosisView struct {
	StageReached         string  `json:"stage_reached"`
	Cause                string  `json:"cause"`
	PlainLanguageSummary string  `json:"plain_language_summary"`
	RecommendedAction    string  `json:"recommended_action"`
	Evidence             []int64 `json:"evidence"`
}

// checkoutSessionView is the wire shape of one checkout session (R1's
// field list, plus the diagnosis embedded on GET .../checkout-sessions/{id}
// per R11 — it's already stored on the document, so no extra query).
type checkoutSessionView struct {
	CheckoutID     string          `json:"checkout_id"`
	StoreID        string          `json:"store_id"`
	Surface        string          `json:"surface,omitempty"`
	AgentPlatform  string          `json:"agent_platform,omitempty"`
	State          string          `json:"state"`
	CurrentStage   string          `json:"current_stage"`
	StartedAt      time.Time       `json:"started_at"`
	LastActivityAt time.Time       `json:"last_activity_at"`
	TerminalAt     *time.Time      `json:"terminal_at,omitempty"`
	Cart           cartSummaryView `json:"cart_summary"`
	Escalation     *escalationView `json:"escalation,omitempty"`
	Failure        *failureView    `json:"failure,omitempty"`
	Diagnosis      *diagnosisView  `json:"diagnosis,omitempty"`
	EventCount     int64           `json:"event_count"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

func toCheckoutSessionView(s checkoutescalation.Session) checkoutSessionView {
	v := checkoutSessionView{
		CheckoutID:     s.CheckoutID,
		StoreID:        s.StoreID,
		Surface:        s.Surface,
		AgentPlatform:  s.AgentPlatform,
		State:          string(s.State),
		CurrentStage:   s.CurrentStage,
		StartedAt:      s.StartedAt,
		LastActivityAt: s.LastActivityAt,
		TerminalAt:     s.TerminalAt,
		Cart:           cartSummaryView{Currency: s.Cart.Currency, TotalMinor: s.Cart.TotalMinor, ItemCount: s.Cart.ItemCount},
		EventCount:     s.EventCount,
		UpdatedAt:      s.UpdatedAt,
	}
	if s.Escalation != nil {
		v.Escalation = &escalationView{
			Reason: s.Escalation.Reason, EscalatedAt: s.Escalation.EscalatedAt,
			ContinueURLIssued: s.Escalation.ContinueURLIssued, ReturnedAt: s.Escalation.ReturnedAt, Resolved: s.Escalation.Resolved,
		}
	}
	if s.Failure != nil {
		v.Failure = &failureView{Code: s.Failure.Code, Cause: s.Failure.Cause, Stage: s.Failure.Stage}
	}
	if s.Diagnosis != nil {
		v.Diagnosis = &diagnosisView{
			StageReached: s.Diagnosis.StageReached, Cause: s.Diagnosis.Cause,
			PlainLanguageSummary: s.Diagnosis.PlainLanguageSummary, RecommendedAction: s.Diagnosis.RecommendedAction,
			Evidence: s.Diagnosis.Evidence,
		}
	}
	return v
}

// timelineEntryView is the wire shape of one GET
// .../checkout-sessions/{id}/timeline row (R5) — Payload is already
// redacted at write time (R6), not here.
type timelineEntryView struct {
	Seq        int64          `json:"seq"`
	OccurredAt time.Time      `json:"occurred_at"`
	EventType  string         `json:"event_type"`
	Stage      string         `json:"stage"`
	Payload    map[string]any `json:"payload"`
}

func toTimelineEntryView(e checkoutescalation.TimelineEntry) timelineEntryView {
	return timelineEntryView{Seq: e.Seq, OccurredAt: e.OccurredAt, EventType: e.EventType, Stage: e.Stage, Payload: e.Payload}
}

const defaultLiveWindow = 30 * time.Minute

// GET /checkout-sessions?state=&surface=&store_id=&cause=&escalated_only=&from=&to=&cursor=&limit=
func (h *handler) listCheckoutSessions(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}
	if h.deps.CheckoutSessions == nil {
		return c.JSON(http.StatusOK, domain.ListOf[checkoutSessionView]{Data: []checkoutSessionView{}, Page: domain.Page{HasMore: false}})
	}

	limit := 25
	if v := c.QueryParam("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}
	filter := checkoutescalation.ListFilter{
		State:         checkoutescalation.State(c.QueryParam("state")),
		Surface:       c.QueryParam("surface"),
		StoreID:       c.QueryParam("store_id"),
		Cause:         c.QueryParam("cause"),
		EscalatedOnly: c.QueryParam("escalated_only") == "true",
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

	sessions, cursor, hasMore, err := h.deps.CheckoutSessions.List(c.Request().Context(), string(principal.MerchantID), filter, c.QueryParam("cursor"), limit)
	if errors.Is(err, checkoutescalation.ErrInvalidCursor) {
		return errValidation(c, "invalid cursor")
	}
	if err != nil {
		h.log.Error("list checkout sessions failed", "error", err, "merchant_id", principal.MerchantID)
		return errInternal(c)
	}

	var cursorPtr *string
	if hasMore {
		cursorPtr = &cursor
	}
	return c.JSON(http.StatusOK, domain.ListOf[checkoutSessionView]{
		Data: lo.Map(sessions, func(s checkoutescalation.Session, _ int) checkoutSessionView { return toCheckoutSessionView(s) }),
		Page: domain.Page{Cursor: cursorPtr, HasMore: hasMore},
	})
}

// GET /checkout-sessions/live?window_minutes=&limit=
//
// Registered before /checkout-sessions/:checkout_id in handler.go so the
// literal "live" segment matches this route rather than being captured as
// a checkout_id — Echo's radix router already prefers a static segment
// over a param one regardless of registration order, but the ordering is
// kept explicit for readability.
func (h *handler) listLiveCheckoutSessions(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}
	if h.deps.CheckoutSessions == nil {
		return c.JSON(http.StatusOK, domain.ListOf[checkoutSessionView]{Data: []checkoutSessionView{}, Page: domain.Page{HasMore: false}})
	}

	window := defaultLiveWindow
	if v := c.QueryParam("window_minutes"); v != "" {
		if m, err := strconv.Atoi(v); err == nil && m > 0 {
			window = time.Duration(m) * time.Minute
		}
	}
	limit := 200
	if v := c.QueryParam("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil && l > 0 && l <= 200 {
			limit = l
		}
	}

	sessions, hasMore, err := h.deps.CheckoutSessions.ListLive(c.Request().Context(), string(principal.MerchantID), window, limit)
	if err != nil {
		h.log.Error("list live checkout sessions failed", "error", err, "merchant_id", principal.MerchantID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, domain.ListOf[checkoutSessionView]{
		Data: lo.Map(sessions, func(s checkoutescalation.Session, _ int) checkoutSessionView { return toCheckoutSessionView(s) }),
		Page: domain.Page{HasMore: hasMore},
	})
}

// GET /checkout-sessions/:checkout_id
func (h *handler) getCheckoutSession(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}
	checkoutID := c.Param("checkout_id")
	if checkoutID == "" {
		return errValidation(c, "checkout_id required")
	}
	if h.deps.CheckoutSessions == nil {
		return errNotFound(c)
	}

	session, err := h.deps.CheckoutSessions.Get(c.Request().Context(), string(principal.MerchantID), checkoutID)
	if errors.Is(err, checkoutescalation.ErrNotFound) {
		return errNotFound(c)
	}
	if err != nil {
		h.log.Error("get checkout session failed", "error", err, "checkout_id", checkoutID)
		return errInternal(c)
	}
	return c.JSON(http.StatusOK, toCheckoutSessionView(*session))
}

// GET /checkout-sessions/:checkout_id/timeline?cursor=&limit=
func (h *handler) getCheckoutSessionTimeline(c echo.Context) error {
	principal, ok := c.Get("principal").(domain.Principal)
	if !ok {
		return errUnauthorized(c)
	}
	checkoutID := c.Param("checkout_id")
	if checkoutID == "" {
		return errValidation(c, "checkout_id required")
	}
	if h.deps.CheckoutSessions == nil || h.deps.CheckoutSessionTimeline == nil {
		return errNotFound(c)
	}

	// Tenancy: the timeline collection has no merchant_id field of its own
	// (it's keyed by checkout_id only), so ownership is verified against
	// the parent session first — same pattern getHealthHistory uses for
	// endpoint-health history (health.go), which has the identical shape
	// of problem.
	if _, err := h.deps.CheckoutSessions.Get(c.Request().Context(), string(principal.MerchantID), checkoutID); err != nil {
		return errNotFound(c)
	}

	afterSeq := int64(0)
	if v := c.QueryParam("cursor"); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return errValidation(c, "cursor must be an integer seq")
		}
		afterSeq = parsed
	}
	limit := 100
	if v := c.QueryParam("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil && l > 0 && l <= 200 {
			limit = l
		}
	}

	entries, hasMore, err := h.deps.CheckoutSessionTimeline.List(c.Request().Context(), checkoutID, afterSeq, limit)
	if err != nil {
		h.log.Error("get checkout session timeline failed", "error", err, "checkout_id", checkoutID)
		return errInternal(c)
	}

	var cursor *string
	if hasMore && len(entries) > 0 {
		s := strconv.FormatInt(entries[len(entries)-1].Seq, 10)
		cursor = &s
	}
	return c.JSON(http.StatusOK, domain.ListOf[timelineEntryView]{
		Data: lo.Map(entries, func(e checkoutescalation.TimelineEntry, _ int) timelineEntryView { return toTimelineEntryView(e) }),
		Page: domain.Page{Cursor: cursor, HasMore: hasMore},
	})
}
