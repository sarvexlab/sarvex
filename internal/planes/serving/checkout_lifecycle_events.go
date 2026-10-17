// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/google/uuid"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// This file holds the checkout lifecycle events consumed by the control
// plane's session-projection worker (Checkout Escalation Console). They are
// a read-model concern only: emitted best-effort, alongside the existing
// EmitOrderIntent call, at points checkout_service.go already reaches on
// every request — they never gate, retry, or alter the checkout response,
// and nothing in this package ever subscribes to them. "Completed" has no
// event of its own here; EventOrderIntent (events.go) already fires exactly
// once at that transition and carries everything the projection needs.
//
// Each event carries an EventID (fresh per emission) — the eventbus's
// DomainEvent envelope has no event-id field of its own (see
// internal/pkg/eventbus/envelope.go), so a consumer that needs idempotent,
// duplicate-safe processing has to be given one explicitly. EventOrderIntent
// doesn't need this: its OrderID is already a natural, unique idempotency
// key (and is already relied on elsewhere for idempotent order recording).

// CartSummary is the minimal cart snapshot carried on checkout lifecycle
// events — enough for a read model to show currency/total/item-count
// without re-deriving it from line items.
type CartSummary struct {
	Currency   string `json:"currency"`
	TotalMinor int64  `json:"total_minor"`
	ItemCount  int    `json:"item_count"`
}

// cartSummaryFrom extracts a CartSummary from a connector response. Mirrors
// buildCompletedOrder's total-extraction loop (checkout_service.go) —
// including its "USD" fallback, since model.Total carries no per-total
// currency field today; not something this change is trying to fix.
func cartSummaryFrom(totals model.Totals, itemCount int) CartSummary {
	var total int64
	for _, t := range totals {
		if t.Type == model.TotalTypeTotal {
			total = int64(t.Amount)
			break
		}
	}
	return CartSummary{Currency: "USD", TotalMinor: total, ItemCount: itemCount}
}

// ─── session_initiated ──────────────────────────────────────────────────────

// CheckoutSessionInitiatedEvent is emitted once, at the end of a successful
// Create.
type CheckoutSessionInitiatedEvent struct {
	EventID           string      `json:"event_id"`
	CheckoutSessionID string      `json:"checkout_session_id"`
	StoreID_          string      `json:"store_id"`
	MerchantID_       string      `json:"merchant_id"`
	Surface           string      `json:"surface"`
	AgentPlatform     string      `json:"agent_platform,omitempty"`
	Cart              CartSummary `json:"cart"`
	Stage             string      `json:"stage"`
	EmittedAt_        time.Time   `json:"emitted_at"`
	Payload_          []byte      `json:"-"`
}

func (e *CheckoutSessionInitiatedEvent) EventType() string     { return ucp.EventCheckoutSessionInitiated }
func (e *CheckoutSessionInitiatedEvent) StoreID() string       { return e.StoreID_ }
func (e *CheckoutSessionInitiatedEvent) MerchantID() string    { return e.MerchantID_ }
func (e *CheckoutSessionInitiatedEvent) OccurredAt() time.Time { return e.EmittedAt_ }
func (e *CheckoutSessionInitiatedEvent) Payload() []byte {
	if e.Payload_ == nil {
		body, _ := json.Marshal(map[string]any{
			"event_id":            e.EventID,
			"checkout_session_id": e.CheckoutSessionID,
			"store_id":            e.StoreID_,
			"merchant_id":         e.MerchantID_,
			"surface":             e.Surface,
			"agent_platform":      e.AgentPlatform,
			"cart":                e.Cart,
			"stage":               e.Stage,
			"emitted_at":          e.EmittedAt_,
		})
		e.Payload_ = body
	}
	return e.Payload_
}

// ─── cart_updated ───────────────────────────────────────────────────────────

// CheckoutCartUpdatedEvent is emitted at the end of every successful Update.
type CheckoutCartUpdatedEvent struct {
	EventID           string      `json:"event_id"`
	CheckoutSessionID string      `json:"checkout_session_id"`
	StoreID_          string      `json:"store_id"`
	MerchantID_       string      `json:"merchant_id"`
	Cart              CartSummary `json:"cart"`
	Stage             string      `json:"stage"`
	EmittedAt_        time.Time   `json:"emitted_at"`
	Payload_          []byte      `json:"-"`
}

func (e *CheckoutCartUpdatedEvent) EventType() string     { return ucp.EventCheckoutCartUpdated }
func (e *CheckoutCartUpdatedEvent) StoreID() string       { return e.StoreID_ }
func (e *CheckoutCartUpdatedEvent) MerchantID() string    { return e.MerchantID_ }
func (e *CheckoutCartUpdatedEvent) OccurredAt() time.Time { return e.EmittedAt_ }
func (e *CheckoutCartUpdatedEvent) Payload() []byte {
	if e.Payload_ == nil {
		body, _ := json.Marshal(map[string]any{
			"event_id":            e.EventID,
			"checkout_session_id": e.CheckoutSessionID,
			"store_id":            e.StoreID_,
			"merchant_id":         e.MerchantID_,
			"cart":                e.Cart,
			"stage":               e.Stage,
			"emitted_at":          e.EmittedAt_,
		})
		e.Payload_ = body
	}
	return e.Payload_
}

// ─── payment_submitted ──────────────────────────────────────────────────────

// CheckoutPaymentSubmittedEvent is emitted right before Complete invokes the
// connector's synchronous CheckoutComplete call — the one live external call
// in this flow, and therefore the one place a real "awaiting payment"
// window exists.
type CheckoutPaymentSubmittedEvent struct {
	EventID           string    `json:"event_id"`
	CheckoutSessionID string    `json:"checkout_session_id"`
	StoreID_          string    `json:"store_id"`
	MerchantID_       string    `json:"merchant_id"`
	Stage             string    `json:"stage"`
	EmittedAt_        time.Time `json:"emitted_at"`
	Payload_          []byte    `json:"-"`
}

func (e *CheckoutPaymentSubmittedEvent) EventType() string     { return ucp.EventCheckoutPaymentSubmitted }
func (e *CheckoutPaymentSubmittedEvent) StoreID() string       { return e.StoreID_ }
func (e *CheckoutPaymentSubmittedEvent) MerchantID() string    { return e.MerchantID_ }
func (e *CheckoutPaymentSubmittedEvent) OccurredAt() time.Time { return e.EmittedAt_ }
func (e *CheckoutPaymentSubmittedEvent) Payload() []byte {
	if e.Payload_ == nil {
		body, _ := json.Marshal(map[string]any{
			"event_id":            e.EventID,
			"checkout_session_id": e.CheckoutSessionID,
			"store_id":            e.StoreID_,
			"merchant_id":         e.MerchantID_,
			"stage":               e.Stage,
			"emitted_at":          e.EmittedAt_,
		})
		e.Payload_ = body
	}
	return e.Payload_
}

// ─── escalated ───────────────────────────────────────────────────────────────

// CheckoutEscalatedEvent is emitted when Complete's connector call returns
// requires_escalation (e.g. 3DS) — the buyer must complete an out-of-band
// action at ContinueURL before the agent can retry Complete.
//
// This event is shared by the pre-existing session-projection contract and
// the versioned checkout lifecycle contract (lifecycle_contract.go,
// event_version "1.0"): EventVersion/OccurredAt_/CheckoutID/Surface are
// that contract's additions, added alongside (never replacing)
// EventID/CheckoutSessionID/EmittedAt_, which the session-projection
// worker (internal/planes/control/checkoutescalation) already parses and
// must keep parsing unchanged.
type CheckoutEscalatedEvent struct {
	EventVersion      string    `json:"event_version"`
	EventID           string    `json:"event_id"`
	CheckoutSessionID string    `json:"checkout_session_id"`
	CheckoutID        string    `json:"checkout_id"`
	StoreID_          string    `json:"store_id"`
	MerchantID_       string    `json:"merchant_id"`
	Surface           Surface   `json:"surface"`
	Reason            string    `json:"reason"`
	ContinueURL       string    `json:"continue_url"`
	Stage             string    `json:"stage"`
	EmittedAt_        time.Time `json:"emitted_at"`
	OccurredAt_       time.Time `json:"occurred_at"`
	Payload_          []byte    `json:"-"`
}

func (e *CheckoutEscalatedEvent) EventType() string     { return ucp.EventCheckoutEscalated }
func (e *CheckoutEscalatedEvent) StoreID() string       { return e.StoreID_ }
func (e *CheckoutEscalatedEvent) MerchantID() string    { return e.MerchantID_ }
func (e *CheckoutEscalatedEvent) OccurredAt() time.Time { return e.EmittedAt_ }
func (e *CheckoutEscalatedEvent) Payload() []byte {
	if e.Payload_ == nil {
		body, _ := json.Marshal(map[string]any{
			"event_version":       e.EventVersion,
			"event_id":            e.EventID,
			"checkout_session_id": e.CheckoutSessionID,
			"checkout_id":         e.CheckoutID,
			"store_id":            e.StoreID_,
			"merchant_id":         e.MerchantID_,
			"surface":             e.Surface,
			"reason":              e.Reason,
			"continue_url":        e.ContinueURL,
			"stage":               e.Stage,
			"emitted_at":          e.EmittedAt_,
			"occurred_at":         e.OccurredAt_,
		})
		e.Payload_ = body
	}
	return e.Payload_
}

// escalationReason picks the first qualifying error message's text as the
// human-readable escalation reason, matching the same qualification rule
// base.NewEscalationResponse enforces (severity requires_buyer_input or
// requires_buyer_review).
func escalationReason(messages []model.Message) string {
	for _, m := range messages {
		if me, ok := m.AsError(); ok && me.Severity.RequiresEscalation() {
			return me.Content
		}
	}
	return ""
}

// ─── failed ──────────────────────────────────────────────────────────────────

// CheckoutFailedEvent is emitted at every point checkout_service.go
// currently gives up on a session. It carries the raw signal (code,
// message, stage) plus, for the versioned contract, a structured Failure
// sub-object and CartSnapshot (R3) — classification into a Cause still
// happens once, downstream, in the diagnosis layer
// (internal/pkg/checkoutcause), not here.
//
// Like CheckoutEscalatedEvent, this struct is shared by the pre-existing
// session-projection contract and the versioned contract: the flat
// EventID/CheckoutSessionID/Code/Message/Stage/EmittedAt_ fields are
// unchanged and still what the session-projection worker parses;
// EventVersion/OccurredAt_/CheckoutID/Surface/Failure/CartSnapshot are
// additive.
type CheckoutFailedEvent struct {
	EventVersion      string        `json:"event_version"`
	EventID           string        `json:"event_id"`
	CheckoutSessionID string        `json:"checkout_session_id"`
	CheckoutID        string        `json:"checkout_id"`
	StoreID_          string        `json:"store_id"`
	MerchantID_       string        `json:"merchant_id"`
	Surface           Surface       `json:"surface"`
	Code              string        `json:"code"`
	Message           string        `json:"message"`
	Stage             string        `json:"stage"`
	Failure           FailureDetail `json:"failure"`
	Cart              CartSnapshot  `json:"cart_snapshot"`
	EmittedAt_        time.Time     `json:"emitted_at"`
	OccurredAt_       time.Time     `json:"occurred_at"`
	Payload_          []byte        `json:"-"`
}

// FailureDetail is checkout.failed's R3 structured failure payload,
// distinct from the flat code/message/stage fields kept for the
// pre-existing session-projection consumer.
type FailureDetail struct {
	Code               FailureCode  `json:"code"`
	Stage              FailureStage `json:"stage"`
	HandlerID          *string      `json:"handler_id"`
	Detail             string       `json:"detail"`
	UpstreamStatusCode *int         `json:"upstream_status_code,omitempty"`   // HTTP status from merchant backend
	UpstreamBodyRedact string       `json:"upstream_body_redacted,omitempty"` // Redacted response body for diagnostics
}

func (e *CheckoutFailedEvent) EventType() string     { return ucp.EventCheckoutFailed }
func (e *CheckoutFailedEvent) StoreID() string       { return e.StoreID_ }
func (e *CheckoutFailedEvent) MerchantID() string    { return e.MerchantID_ }
func (e *CheckoutFailedEvent) OccurredAt() time.Time { return e.EmittedAt_ }
func (e *CheckoutFailedEvent) Payload() []byte {
	if e.Payload_ == nil {
		body, _ := json.Marshal(map[string]any{
			"event_version":       e.EventVersion,
			"event_id":            e.EventID,
			"checkout_session_id": e.CheckoutSessionID,
			"checkout_id":         e.CheckoutID,
			"store_id":            e.StoreID_,
			"merchant_id":         e.MerchantID_,
			"surface":             e.Surface,
			"code":                e.Code,
			"message":             e.Message,
			"stage":               e.Stage,
			"failure":             e.Failure,
			"cart_snapshot":       e.Cart,
			"emitted_at":          e.EmittedAt_,
			"occurred_at":         e.OccurredAt_,
		})
		e.Payload_ = body
	}
	return e.Payload_
}

// ─── emission helpers ───────────────────────────────────────────────────────
// All follow EmitOrderIntent's existing precedent exactly: synchronous
// publish, log-and-continue on failure, never fail or delay the caller's
// response beyond the publish call itself (the same latency EmitOrderIntent
// already carries in Complete today).

func emitLifecycleEvent(ctx context.Context, pub eventbus.EventBus, ev eventbus.DomainEvent, log *slog.Logger, sessionID string) {
	if err := pub.Publish(ctx, ev); err != nil {
		log.Error("failed to emit checkout lifecycle event", "error", err, "event_type", ev.EventType(), "session_id", sessionID)
	}
}

func (s *CheckoutService) emitSessionInitiated(ctx context.Context, session *CheckoutSession, connResp *base.CheckoutResponse, surface, agentPlatform string) {
	ev := &CheckoutSessionInitiatedEvent{
		EventID:           uuid.New().String(),
		CheckoutSessionID: session.SessionID,
		StoreID_:          session.StoreID,
		MerchantID_:       session.MerchantID,
		Surface:           surface,
		AgentPlatform:     agentPlatform,
		Cart:              cartSummaryFrom(connResp.Totals, len(session.LineItems)),
		Stage:             "initiated",
		EmittedAt_:        time.Now().UTC(),
	}
	emitLifecycleEvent(ctx, s.events, ev, s.logger, session.SessionID)
}

func (s *CheckoutService) emitCartUpdated(ctx context.Context, session *CheckoutSession, connResp *base.CheckoutResponse) {
	ev := &CheckoutCartUpdatedEvent{
		EventID:           uuid.New().String(),
		CheckoutSessionID: session.SessionID,
		StoreID_:          session.StoreID,
		MerchantID_:       session.MerchantID,
		Cart:              cartSummaryFrom(connResp.Totals, len(session.LineItems)),
		Stage:             "cart_active",
		EmittedAt_:        time.Now().UTC(),
	}
	emitLifecycleEvent(ctx, s.events, ev, s.logger, session.SessionID)
}

func (s *CheckoutService) emitPaymentSubmitted(ctx context.Context, session *CheckoutSession) {
	ev := &CheckoutPaymentSubmittedEvent{
		EventID:           uuid.New().String(),
		CheckoutSessionID: session.SessionID,
		StoreID_:          session.StoreID,
		MerchantID_:       session.MerchantID,
		Stage:             "awaiting_payment",
		EmittedAt_:        time.Now().UTC(),
	}
	emitLifecycleEvent(ctx, s.events, ev, s.logger, session.SessionID)
}

func (s *CheckoutService) emitEscalated(ctx context.Context, session *CheckoutSession, checkoutResp *base.CheckoutResponse) {
	now := time.Now().UTC()
	ev := &CheckoutEscalatedEvent{
		EventVersion:      eventContractVersion,
		EventID:           newEventID(),
		CheckoutSessionID: session.SessionID,
		CheckoutID:        session.SessionID,
		StoreID_:          session.StoreID,
		MerchantID_:       session.MerchantID,
		Surface:           Surface(session.Surface),
		Reason:            escalationReason(checkoutResp.Messages),
		ContinueURL:       checkoutResp.ContinueURL,
		Stage:             "escalated",
		EmittedAt_:        now,
		OccurredAt_:       now,
	}
	publishLifecycleEventV1(s.events, ev, s.logger, session.SessionID)
}

// emitFailed emits checkout.failed. code must be one of the FailureCode
// constants (lifecycle_contract.go) — checkout_service.go's call sites
// already only ever pass one of those four literal strings.
// emitFailed creates and publishes a CheckoutFailedEvent. upstreamStatus and
// upstreamBodyRedact are optional diagnostics from the merchant backend response.
func (s *CheckoutService) emitFailed(ctx context.Context, session *CheckoutSession, code, message, stage string,
	upstreamStatus *int, upstreamBodyRedact string) {
	now := time.Now().UTC()
	failureCode := FailureCode(code)
	var handlerID *string
	if id := handlerIDFromPayment(session.Payment); id != "" {
		handlerID = &id
	}
	ev := &CheckoutFailedEvent{
		EventVersion:      eventContractVersion,
		EventID:           newEventID(),
		CheckoutSessionID: session.SessionID,
		CheckoutID:        session.SessionID,
		StoreID_:          session.StoreID,
		MerchantID_:       session.MerchantID,
		Surface:           Surface(session.Surface),
		Code:              code,
		Message:           message,
		Stage:             stage,
		Failure: FailureDetail{
			Code:               failureCode,
			Stage:              stageForFailureCode(failureCode),
			HandlerID:          handlerID,
			Detail:             message,
			UpstreamStatusCode: upstreamStatus,
			UpstreamBodyRedact: upstreamBodyRedact,
		},
		Cart:        lastKnownOrFreshSnapshot(session),
		EmittedAt_:  now,
		OccurredAt_: now,
	}
	publishLifecycleEventV1(s.events, ev, s.logger, session.SessionID)
}

// ─── initiated / updated / completed (versioned contract only) ────────────
// New channels with no pre-existing analog to stay backward compatible
// with — see lifecycle_contract.go's doc comment. Envelope-only payloads:
// nothing beyond R2's common envelope is specified for these three, so
// nothing beyond it is added.

// CheckoutInitiatedEvent is the versioned contract's serving.checkout.initiated.
type CheckoutInitiatedEvent struct {
	EventVersion string    `json:"event_version"`
	EventID      string    `json:"event_id"`
	OccurredAt_  time.Time `json:"occurred_at"`
	MerchantID_  string    `json:"merchant_id"`
	StoreID_     string    `json:"store_id"`
	Surface      Surface   `json:"surface"`
	CheckoutID   string    `json:"checkout_id"`
	Payload_     []byte    `json:"-"`
}

func (e *CheckoutInitiatedEvent) EventType() string     { return ucp.EventCheckoutInitiated }
func (e *CheckoutInitiatedEvent) StoreID() string       { return e.StoreID_ }
func (e *CheckoutInitiatedEvent) MerchantID() string    { return e.MerchantID_ }
func (e *CheckoutInitiatedEvent) OccurredAt() time.Time { return e.OccurredAt_ }
func (e *CheckoutInitiatedEvent) Payload() []byte {
	if e.Payload_ == nil {
		body, _ := json.Marshal(map[string]any{
			"event_version": e.EventVersion,
			"event_id":      e.EventID,
			"occurred_at":   e.OccurredAt_,
			"merchant_id":   e.MerchantID_,
			"store_id":      e.StoreID_,
			"surface":       e.Surface,
			"checkout_id":   e.CheckoutID,
		})
		e.Payload_ = body
	}
	return e.Payload_
}

// CheckoutUpdatedEvent is the versioned contract's serving.checkout.updated.
type CheckoutUpdatedEvent struct {
	EventVersion string    `json:"event_version"`
	EventID      string    `json:"event_id"`
	OccurredAt_  time.Time `json:"occurred_at"`
	MerchantID_  string    `json:"merchant_id"`
	StoreID_     string    `json:"store_id"`
	Surface      Surface   `json:"surface"`
	CheckoutID   string    `json:"checkout_id"`
	Payload_     []byte    `json:"-"`
}

func (e *CheckoutUpdatedEvent) EventType() string     { return ucp.EventCheckoutUpdated }
func (e *CheckoutUpdatedEvent) StoreID() string       { return e.StoreID_ }
func (e *CheckoutUpdatedEvent) MerchantID() string    { return e.MerchantID_ }
func (e *CheckoutUpdatedEvent) OccurredAt() time.Time { return e.OccurredAt_ }
func (e *CheckoutUpdatedEvent) Payload() []byte {
	if e.Payload_ == nil {
		body, _ := json.Marshal(map[string]any{
			"event_version": e.EventVersion,
			"event_id":      e.EventID,
			"occurred_at":   e.OccurredAt_,
			"merchant_id":   e.MerchantID_,
			"store_id":      e.StoreID_,
			"surface":       e.Surface,
			"checkout_id":   e.CheckoutID,
		})
		e.Payload_ = body
	}
	return e.Payload_
}

// CheckoutCompletedEvent is the versioned contract's serving.checkout.completed,
// emitted once a connector CheckoutComplete call has genuinely placed an
// order at the merchant — the same transition serving.checkout.order_intent
// (events.go) already reports to the sync plane's order-intent handler;
// this is the same moment, reported to the versioned lifecycle contract.
type CheckoutCompletedEvent struct {
	EventVersion string    `json:"event_version"`
	EventID      string    `json:"event_id"`
	OccurredAt_  time.Time `json:"occurred_at"`
	MerchantID_  string    `json:"merchant_id"`
	StoreID_     string    `json:"store_id"`
	Surface      Surface   `json:"surface"`
	CheckoutID   string    `json:"checkout_id"`
	Payload_     []byte    `json:"-"`
}

func (e *CheckoutCompletedEvent) EventType() string     { return ucp.EventCheckoutCompleted }
func (e *CheckoutCompletedEvent) StoreID() string       { return e.StoreID_ }
func (e *CheckoutCompletedEvent) MerchantID() string    { return e.MerchantID_ }
func (e *CheckoutCompletedEvent) OccurredAt() time.Time { return e.OccurredAt_ }
func (e *CheckoutCompletedEvent) Payload() []byte {
	if e.Payload_ == nil {
		body, _ := json.Marshal(map[string]any{
			"event_version": e.EventVersion,
			"event_id":      e.EventID,
			"occurred_at":   e.OccurredAt_,
			"merchant_id":   e.MerchantID_,
			"store_id":      e.StoreID_,
			"surface":       e.Surface,
			"checkout_id":   e.CheckoutID,
		})
		e.Payload_ = body
	}
	return e.Payload_
}

// CheckoutAbandonedEvent is the versioned contract's serving.checkout.abandoned
// (R4), emitted by the abandonment sweep (abandonment_sweep.go) rather than
// inline from CheckoutService. Stage is additive to the original contract
// (nothing consumed it yet — sarvex-v1-syncing-plane-async.yaml's
// x-consumers was empty for this channel): the last SessionState the
// session reached, which the Lost Revenue Ledger uses to distinguish
// ordinary drop-off from a stall right before the final complete call.
type CheckoutAbandonedEvent struct {
	EventVersion string       `json:"event_version"`
	EventID      string       `json:"event_id"`
	OccurredAt_  time.Time    `json:"occurred_at"`
	MerchantID_  string       `json:"merchant_id"`
	StoreID_     string       `json:"store_id"`
	Surface      Surface      `json:"surface"`
	CheckoutID   string       `json:"checkout_id"`
	Stage        string       `json:"stage"`
	Cart         CartSnapshot `json:"cart_snapshot"`
	Payload_     []byte       `json:"-"`
}

func (e *CheckoutAbandonedEvent) EventType() string     { return ucp.EventCheckoutAbandoned }
func (e *CheckoutAbandonedEvent) StoreID() string       { return e.StoreID_ }
func (e *CheckoutAbandonedEvent) MerchantID() string    { return e.MerchantID_ }
func (e *CheckoutAbandonedEvent) OccurredAt() time.Time { return e.OccurredAt_ }
func (e *CheckoutAbandonedEvent) Payload() []byte {
	if e.Payload_ == nil {
		body, _ := json.Marshal(map[string]any{
			"event_version": e.EventVersion,
			"event_id":      e.EventID,
			"occurred_at":   e.OccurredAt_,
			"merchant_id":   e.MerchantID_,
			"store_id":      e.StoreID_,
			"surface":       e.Surface,
			"checkout_id":   e.CheckoutID,
			"stage":         e.Stage,
			"cart_snapshot": e.Cart,
		})
		e.Payload_ = body
	}
	return e.Payload_
}

func (s *CheckoutService) emitInitiatedV1(session *CheckoutSession) {
	now := time.Now().UTC()
	ev := &CheckoutInitiatedEvent{
		EventVersion: eventContractVersion,
		EventID:      newEventID(),
		OccurredAt_:  now,
		MerchantID_:  session.MerchantID,
		StoreID_:     session.StoreID,
		Surface:      Surface(session.Surface),
		CheckoutID:   session.SessionID,
	}
	publishLifecycleEventV1(s.events, ev, s.logger, session.SessionID)
}

func (s *CheckoutService) emitUpdatedV1(session *CheckoutSession) {
	now := time.Now().UTC()
	ev := &CheckoutUpdatedEvent{
		EventVersion: eventContractVersion,
		EventID:      newEventID(),
		OccurredAt_:  now,
		MerchantID_:  session.MerchantID,
		StoreID_:     session.StoreID,
		Surface:      Surface(session.Surface),
		CheckoutID:   session.SessionID,
	}
	publishLifecycleEventV1(s.events, ev, s.logger, session.SessionID)
}

func (s *CheckoutService) emitCompletedV1(session *CheckoutSession) {
	now := time.Now().UTC()
	ev := &CheckoutCompletedEvent{
		EventVersion: eventContractVersion,
		EventID:      newEventID(),
		OccurredAt_:  now,
		MerchantID_:  session.MerchantID,
		StoreID_:     session.StoreID,
		Surface:      Surface(session.Surface),
		CheckoutID:   session.SessionID,
	}
	publishLifecycleEventV1(s.events, ev, s.logger, session.SessionID)
}
