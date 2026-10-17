// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package checkoutescalation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
)

// Projector consumes checkout lifecycle events
// (internal/planes/serving/checkout_lifecycle_events.go, plus the existing
// serving.checkout.order_intent) and maintains the session projection +
// timeline. It never writes anywhere but its own two collections — see
// doc.go for the import-boundary test enforcing that.
type Projector struct {
	sessions  SessionRepository
	timeline  TimelineRepository
	events    eventbus.EventBus
	retention time.Duration
	log       *slog.Logger

	mu   sync.Mutex
	subs []eventbus.Subscription
}

// NewProjector constructs a Projector. retention is how long a session (and
// its timeline) survives past terminal_at before the TTL indexes purge it
// (R4) — 30 days by default, configurable.
func NewProjector(sessions SessionRepository, timeline TimelineRepository, events eventbus.EventBus, retention time.Duration, log *slog.Logger) *Projector {
	if log == nil {
		log = slog.Default()
	}
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	return &Projector{sessions: sessions, timeline: timeline, events: events, retention: retention, log: log}
}

// Start subscribes to every checkout lifecycle topic this projector
// consumes.
func (p *Projector) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	handlers := map[string]eventbus.EventHandler{
		ucp.EventCheckoutSessionInitiated: p.handleSessionInitiated,
		ucp.EventCheckoutCartUpdated:      p.handleCartUpdated,
		ucp.EventCheckoutPaymentSubmitted: p.handlePaymentSubmitted,
		ucp.EventCheckoutEscalated:        p.handleEscalated,
		ucp.EventCheckoutFailed:           p.handleFailed,
		ucp.EventOrderIntent:              p.handleOrderIntent,
	}
	for topic, handler := range handlers {
		sub, err := p.events.Subscribe(ctx, topic, handler)
		if err != nil {
			return fmt.Errorf("subscribe %s: %w", topic, err)
		}
		p.subs = append(p.subs, sub)
	}
	return nil
}

// Stop unsubscribes from every topic.
func (p *Projector) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, sub := range p.subs {
		if err := p.events.Unsubscribe(ctx, sub); err != nil {
			p.log.Error("checkoutescalation: unsubscribe failed", "topic", sub.Topic(), "error", err)
		}
	}
	p.subs = nil
	return nil
}

// appendTimeline is the idempotency gate every handler goes through before
// touching the parent session document: a duplicate (checkout_id,
// event_id) — a redelivered message, or the same business event published
// twice — is a silent no-op (R3), reported via inserted=false so the
// caller skips applying it to the session too. Redaction (R6) happens here,
// at write time, before the entry is ever persisted — there is no later
// point where an unredacted payload could leak.
func (p *Projector) appendTimeline(ctx context.Context, checkoutID, eventID, eventType, stage string, rawPayload []byte, occurredAt time.Time) (inserted bool, err error) {
	var decoded map[string]any
	if unmarshalErr := json.Unmarshal(rawPayload, &decoded); unmarshalErr != nil {
		p.log.Error("checkoutescalation: failed to decode payload for redaction", "error", unmarshalErr, "event_type", eventType)
		decoded = map[string]any{}
	}
	entry := TimelineEntry{
		CheckoutID: checkoutID,
		EventID:    eventID,
		OccurredAt: occurredAt,
		EventType:  eventType,
		Stage:      stage,
		Payload:    Redact(decoded),
	}
	return p.timeline.Insert(ctx, entry)
}

// jsonEventID pulls "event_id" out of a decoded lifecycle event payload.
type baseEventFields struct {
	EventID           string    `json:"event_id"`
	CheckoutSessionID string    `json:"checkout_session_id"`
	StoreID           string    `json:"store_id"`
	MerchantID        string    `json:"merchant_id"`
	Stage             string    `json:"stage"`
	EmittedAt         time.Time `json:"emitted_at"`
}

type cartFields struct {
	Currency   string `json:"currency"`
	TotalMinor int64  `json:"total_minor"`
	ItemCount  int    `json:"item_count"`
}

func (c cartFields) toCartSummary() *CartSummary {
	return &CartSummary{Currency: c.Currency, TotalMinor: c.TotalMinor, ItemCount: c.ItemCount}
}

func (p *Projector) handleSessionInitiated(ctx context.Context, ev eventbus.DomainEvent) error {
	var payload struct {
		baseEventFields
		Surface       string     `json:"surface"`
		AgentPlatform string     `json:"agent_platform"`
		Cart          cartFields `json:"cart"`
	}
	if err := json.Unmarshal(ev.Payload(), &payload); err != nil {
		p.log.Error("checkoutescalation: unmarshal session_initiated failed", "error", err)
		return nil
	}
	inserted, err := p.appendTimeline(ctx, payload.CheckoutSessionID, payload.EventID, ev.EventType(), payload.Stage, ev.Payload(), payload.EmittedAt)
	if err != nil {
		return err
	}
	if !inserted {
		return nil
	}
	seq, err := p.sessions.ApplyEvent(ctx, ApplyEventParams{
		CheckoutID: payload.CheckoutSessionID, MerchantID: payload.MerchantID, StoreID: payload.StoreID,
		Surface: payload.Surface, AgentPlatform: payload.AgentPlatform,
		OccurredAt: payload.EmittedAt, NewState: StateInitiated, Stage: payload.Stage,
		Cart: payload.Cart.toCartSummary(),
	})
	if err != nil {
		return err
	}
	p.backfillSeq(ctx, payload.CheckoutSessionID, payload.EventID, seq)
	return nil
}

func (p *Projector) handleCartUpdated(ctx context.Context, ev eventbus.DomainEvent) error {
	var payload struct {
		baseEventFields
		Cart cartFields `json:"cart"`
	}
	if err := json.Unmarshal(ev.Payload(), &payload); err != nil {
		p.log.Error("checkoutescalation: unmarshal cart_updated failed", "error", err)
		return nil
	}
	inserted, err := p.appendTimeline(ctx, payload.CheckoutSessionID, payload.EventID, ev.EventType(), payload.Stage, ev.Payload(), payload.EmittedAt)
	if err != nil {
		return err
	}
	if !inserted {
		return nil
	}
	seq, err := p.sessions.ApplyEvent(ctx, ApplyEventParams{
		CheckoutID: payload.CheckoutSessionID, MerchantID: payload.MerchantID, StoreID: payload.StoreID,
		OccurredAt: payload.EmittedAt, NewState: StateCartActive, Stage: payload.Stage,
		Cart: payload.Cart.toCartSummary(),
	})
	if err != nil {
		return err
	}
	p.backfillSeq(ctx, payload.CheckoutSessionID, payload.EventID, seq)
	return nil
}

func (p *Projector) handlePaymentSubmitted(ctx context.Context, ev eventbus.DomainEvent) error {
	var payload baseEventFields
	if err := json.Unmarshal(ev.Payload(), &payload); err != nil {
		p.log.Error("checkoutescalation: unmarshal payment_submitted failed", "error", err)
		return nil
	}
	inserted, err := p.appendTimeline(ctx, payload.CheckoutSessionID, payload.EventID, ev.EventType(), payload.Stage, ev.Payload(), payload.EmittedAt)
	if err != nil {
		return err
	}
	if !inserted {
		return nil
	}
	seq, err := p.sessions.ApplyEvent(ctx, ApplyEventParams{
		CheckoutID: payload.CheckoutSessionID, MerchantID: payload.MerchantID, StoreID: payload.StoreID,
		OccurredAt: payload.EmittedAt, NewState: StateAwaitingPayment, Stage: payload.Stage,
	})
	if err != nil {
		return err
	}
	p.backfillSeq(ctx, payload.CheckoutSessionID, payload.EventID, seq)
	return nil
}

func (p *Projector) handleEscalated(ctx context.Context, ev eventbus.DomainEvent) error {
	var payload struct {
		baseEventFields
		Reason      string `json:"reason"`
		ContinueURL string `json:"continue_url"`
	}
	if err := json.Unmarshal(ev.Payload(), &payload); err != nil {
		p.log.Error("checkoutescalation: unmarshal escalated failed", "error", err)
		return nil
	}
	inserted, err := p.appendTimeline(ctx, payload.CheckoutSessionID, payload.EventID, ev.EventType(), payload.Stage, ev.Payload(), payload.EmittedAt)
	if err != nil {
		return err
	}
	if !inserted {
		return nil
	}
	seq, err := p.sessions.ApplyEvent(ctx, ApplyEventParams{
		CheckoutID: payload.CheckoutSessionID, MerchantID: payload.MerchantID, StoreID: payload.StoreID,
		OccurredAt: payload.EmittedAt, NewState: StateEscalated, Stage: payload.Stage,
		Escalation: &Escalation{
			Reason:            payload.Reason,
			EscalatedAt:       payload.EmittedAt,
			ContinueURLIssued: payload.ContinueURL != "",
		},
	})
	if err != nil {
		return err
	}
	p.backfillSeq(ctx, payload.CheckoutSessionID, payload.EventID, seq)
	return nil
}

func (p *Projector) handleFailed(ctx context.Context, ev eventbus.DomainEvent) error {
	var payload struct {
		baseEventFields
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(ev.Payload(), &payload); err != nil {
		p.log.Error("checkoutescalation: unmarshal failed failed", "error", err)
		return nil
	}
	inserted, err := p.appendTimeline(ctx, payload.CheckoutSessionID, payload.EventID, ev.EventType(), payload.Stage, ev.Payload(), payload.EmittedAt)
	if err != nil {
		return err
	}
	if !inserted {
		return nil
	}

	diag := Diagnose(payload.Code, payload.Stage, nil)
	seq, err := p.sessions.ApplyEvent(ctx, ApplyEventParams{
		CheckoutID: payload.CheckoutSessionID, MerchantID: payload.MerchantID, StoreID: payload.StoreID,
		OccurredAt: payload.EmittedAt, NewState: StateFailed, Stage: payload.Stage,
		Failure:   &Failure{Code: payload.Code, Cause: diag.Cause, Stage: payload.Stage},
		Diagnosis: &diag,
		Terminal:  true, Retention: p.retention,
	})
	if err != nil {
		return err
	}
	p.backfillSeq(ctx, payload.CheckoutSessionID, payload.EventID, seq)

	diag.Evidence = []int64{seq}
	if err := p.sessions.SetDiagnosis(ctx, payload.CheckoutSessionID, diag); err != nil {
		p.log.Error("checkoutescalation: backfill failure evidence failed", "error", err, "checkout_id", payload.CheckoutSessionID)
	}
	if err := p.timeline.SetExpiry(ctx, payload.CheckoutSessionID, payload.EmittedAt.Add(p.retention)); err != nil {
		p.log.Error("checkoutescalation: backfill timeline expiry failed", "error", err, "checkout_id", payload.CheckoutSessionID)
	}
	return nil
}

// handleOrderIntent consumes the existing serving.checkout.order_intent
// event as the "completed" transition — it has no event_id of its own
// (predates this projector), so OrderID (already unique, already relied on
// for idempotent order recording elsewhere) is the dedup key here.
func (p *Projector) handleOrderIntent(ctx context.Context, ev eventbus.DomainEvent) error {
	var payload struct {
		CheckoutSessionID string    `json:"checkout_session_id"`
		OrderID           string    `json:"order_id"`
		StoreID           string    `json:"store_id"`
		MerchantID        string    `json:"merchant_id"`
		LineItems         []any     `json:"line_items"`
		Total             cartTotal `json:"total"`
		EmittedAt         time.Time `json:"emitted_at"`
	}
	if err := json.Unmarshal(ev.Payload(), &payload); err != nil {
		p.log.Error("checkoutescalation: unmarshal order_intent failed", "error", err)
		return nil
	}
	dedupKey := "order:" + payload.OrderID
	inserted, err := p.appendTimeline(ctx, payload.CheckoutSessionID, dedupKey, ev.EventType(), "completed", ev.Payload(), payload.EmittedAt)
	if err != nil {
		return err
	}
	if !inserted {
		return nil
	}
	seq, err := p.sessions.ApplyEvent(ctx, ApplyEventParams{
		CheckoutID: payload.CheckoutSessionID, MerchantID: payload.MerchantID, StoreID: payload.StoreID,
		OccurredAt: payload.EmittedAt, NewState: StateCompleted, Stage: "completed",
		Cart:     &CartSummary{Currency: payload.Total.Currency, TotalMinor: payload.Total.AmountMinor, ItemCount: len(payload.LineItems)},
		Terminal: true, Retention: p.retention,
	})
	if err != nil {
		return err
	}
	p.backfillSeq(ctx, payload.CheckoutSessionID, dedupKey, seq)
	if err := p.timeline.SetExpiry(ctx, payload.CheckoutSessionID, payload.EmittedAt.Add(p.retention)); err != nil {
		p.log.Error("checkoutescalation: backfill timeline expiry failed", "error", err, "checkout_id", payload.CheckoutSessionID)
	}
	return nil
}

type cartTotal struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

func (p *Projector) backfillSeq(ctx context.Context, checkoutID, eventID string, seq int64) {
	if err := p.timeline.SetSeq(ctx, checkoutID, eventID, seq); err != nil {
		p.log.Error("checkoutescalation: backfill seq failed", "error", err, "checkout_id", checkoutID, "event_id", eventID)
	}
}
