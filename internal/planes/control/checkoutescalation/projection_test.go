// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package checkoutescalation

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
)

// ─── fakes ──────────────────────────────────────────────────────────────────
// In-memory test doubles implementing the same interfaces as the Mongo
// repositories, replicating the same business rules the real Mongo
// aggregation pipelines enforce (terminal-state guard, monotonic
// last_activity_at, escalation returned/resolved inference) — see
// mongo_sessions.go's ApplyEvent doc for the authoritative version of
// this logic. make test-integration (against real Mongo) is what verifies
// the actual pipeline; these fakes only need to agree with it closely
// enough to exercise Projector/Sweeper's own logic in isolation.

type fakeSessionRepo struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

func newFakeSessionRepo() *fakeSessionRepo {
	return &fakeSessionRepo{sessions: map[string]*Session{}}
}

func (f *fakeSessionRepo) ApplyEvent(_ context.Context, p ApplyEventParams) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	s, ok := f.sessions[p.CheckoutID]
	if !ok {
		s = &Session{CheckoutID: p.CheckoutID, MerchantID: p.MerchantID, StoreID: p.StoreID, StartedAt: p.OccurredAt}
		if p.Surface != "" {
			s.Surface = p.Surface
		}
		if p.AgentPlatform != "" {
			s.AgentPlatform = p.AgentPlatform
		}
		f.sessions[p.CheckoutID] = s
	}

	s.EventCount++
	if s.LastActivityAt.IsZero() || p.OccurredAt.After(s.LastActivityAt) {
		s.LastActivityAt = p.OccurredAt
	}
	if !s.State.Terminal() {
		s.State = p.NewState
		s.CurrentStage = p.Stage
	} else if s.State == "" {
		s.State = p.NewState
		s.CurrentStage = p.Stage
	}
	if p.Cart != nil {
		s.Cart = *p.Cart
	}
	if p.Failure != nil {
		s.Failure = p.Failure
	}
	if p.Diagnosis != nil {
		s.Diagnosis = p.Diagnosis
	}
	if p.Terminal {
		if s.TerminalAt == nil {
			t := p.OccurredAt
			s.TerminalAt = &t
		}
		if s.Escalation != nil {
			s.Escalation.Resolved = true
		}
		if p.Retention > 0 && s.ExpiresAt == nil {
			t := p.OccurredAt.Add(p.Retention)
			s.ExpiresAt = &t
		}
	}
	if p.Escalation != nil {
		s.Escalation = p.Escalation
	} else if !p.Terminal && s.Escalation != nil && s.Escalation.ReturnedAt == nil {
		t := p.OccurredAt
		s.Escalation.ReturnedAt = &t
	}

	return s.EventCount, nil
}

func (f *fakeSessionRepo) MarkAbandoned(_ context.Context, checkoutID string, at time.Time, diagnosis Diagnosis, retention time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[checkoutID]
	if !ok {
		return ErrNotFound
	}
	if s.State.Terminal() {
		return nil
	}
	s.State = StateAbandoned
	t := at
	s.TerminalAt = &t
	exp := at.Add(retention)
	s.ExpiresAt = &exp
	d := diagnosis
	s.Diagnosis = &d
	if s.Escalation != nil {
		s.Escalation.Resolved = true
	}
	return nil
}

func (f *fakeSessionRepo) SetDiagnosis(_ context.Context, checkoutID string, diagnosis Diagnosis) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[checkoutID]
	if !ok {
		return ErrNotFound
	}
	d := diagnosis
	s.Diagnosis = &d
	return nil
}

func (f *fakeSessionRepo) Get(_ context.Context, merchantID, checkoutID string) (*Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[checkoutID]
	if !ok || s.MerchantID != merchantID {
		return nil, ErrNotFound
	}
	cp := *s
	return &cp, nil
}

func (f *fakeSessionRepo) List(context.Context, string, ListFilter, string, int) ([]Session, string, bool, error) {
	return nil, "", false, nil
}
func (f *fakeSessionRepo) ListLive(context.Context, string, time.Duration, int) ([]Session, bool, error) {
	return nil, false, nil
}

func (f *fakeSessionRepo) ListStaleActive(_ context.Context, cutoff time.Time, limit int) ([]Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Session
	for _, s := range f.sessions {
		if !s.State.Terminal() && !s.LastActivityAt.After(cutoff) {
			out = append(out, *s)
		}
	}
	return out, nil
}

func (f *fakeSessionRepo) ListStalledEscalations(_ context.Context, cutoff time.Time, limit int) ([]Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Session
	for _, s := range f.sessions {
		if s.State != StateEscalated || s.Escalation == nil {
			continue
		}
		if !s.Escalation.ContinueURLIssued || s.Escalation.ReturnedAt != nil {
			continue
		}
		if s.Escalation.EscalatedAt.After(cutoff) {
			continue
		}
		if s.Diagnosis != nil && s.Diagnosis.Cause == "escalation_stalled" {
			continue
		}
		out = append(out, *s)
	}
	return out, nil
}

type fakeTimelineRepo struct {
	mu      sync.Mutex
	entries map[string]map[string]*TimelineEntry // checkoutID -> eventID -> entry
}

func newFakeTimelineRepo() *fakeTimelineRepo {
	return &fakeTimelineRepo{entries: map[string]map[string]*TimelineEntry{}}
}

func (f *fakeTimelineRepo) Insert(_ context.Context, entry TimelineEntry) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	byEvent, ok := f.entries[entry.CheckoutID]
	if !ok {
		byEvent = map[string]*TimelineEntry{}
		f.entries[entry.CheckoutID] = byEvent
	}
	if _, exists := byEvent[entry.EventID]; exists {
		return false, nil
	}
	cp := entry
	byEvent[entry.EventID] = &cp
	return true, nil
}

func (f *fakeTimelineRepo) SetSeq(_ context.Context, checkoutID, eventID string, seq int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if byEvent, ok := f.entries[checkoutID]; ok {
		if e, ok := byEvent[eventID]; ok {
			e.Seq = seq
		}
	}
	return nil
}

func (f *fakeTimelineRepo) SetExpiry(_ context.Context, checkoutID string, expiresAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.entries[checkoutID] {
		e.ExpiresAt = &expiresAt
	}
	return nil
}

func (f *fakeTimelineRepo) List(_ context.Context, checkoutID string, afterSeq int64, limit int) ([]TimelineEntry, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []TimelineEntry
	for _, e := range f.entries[checkoutID] {
		if e.Seq > afterSeq {
			out = append(out, *e)
		}
	}
	return out, false, nil
}

func (f *fakeTimelineRepo) ListEvidence(context.Context, string, []string) ([]TimelineEntry, error) {
	return nil, nil
}

// domainEventStub implements eventbus.DomainEvent for test-constructed
// events. Projector's handler methods are called directly (they're
// unexported, reachable from within this package's tests) rather than
// through a fake bus's Subscribe/Publish — simpler, and Start/Stop aren't
// under test here.
type domainEventStub struct {
	eventType  string
	storeID    string
	merchantID string
	occurredAt time.Time
	payload    []byte
}

func (e domainEventStub) EventType() string     { return e.eventType }
func (e domainEventStub) StoreID() string       { return e.storeID }
func (e domainEventStub) MerchantID() string    { return e.merchantID }
func (e domainEventStub) OccurredAt() time.Time { return e.occurredAt }
func (e domainEventStub) Payload() []byte       { return e.payload }

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

func newTestProjector(sessions *fakeSessionRepo, timeline *fakeTimelineRepo) *Projector {
	return NewProjector(sessions, timeline, nil, 30*24*time.Hour, discardLog())
}

// ─── A1: full lifecycle, out-of-order, duplicate delivery ─────────────────

const testCheckoutID = "checkout-1"

func sessionInitiatedEvent(t *testing.T, eventID string, at time.Time) domainEventStub {
	return domainEventStub{
		eventType: ucp.EventCheckoutSessionInitiated, storeID: "store-1", merchantID: "merchant-1", occurredAt: at,
		payload: mustJSON(t, map[string]any{
			"event_id": eventID, "checkout_session_id": testCheckoutID, "store_id": "store-1", "merchant_id": "merchant-1",
			"surface": "ucp_checkout", "agent_platform": "google.com",
			"cart":  map[string]any{"currency": "USD", "total_minor": 1000, "item_count": 1},
			"stage": "initiated", "emitted_at": at,
		}),
	}
}

func cartUpdatedEvent(t *testing.T, eventID string, at time.Time) domainEventStub {
	return domainEventStub{
		eventType: ucp.EventCheckoutCartUpdated, storeID: "store-1", merchantID: "merchant-1", occurredAt: at,
		payload: mustJSON(t, map[string]any{
			"event_id": eventID, "checkout_session_id": testCheckoutID, "store_id": "store-1", "merchant_id": "merchant-1",
			"cart":  map[string]any{"currency": "USD", "total_minor": 1500, "item_count": 2},
			"stage": "cart_active", "emitted_at": at,
		}),
	}
}

func paymentSubmittedEvent(t *testing.T, eventID string, at time.Time) domainEventStub {
	return domainEventStub{
		eventType: ucp.EventCheckoutPaymentSubmitted, storeID: "store-1", merchantID: "merchant-1", occurredAt: at,
		payload: mustJSON(t, map[string]any{
			"event_id": eventID, "checkout_session_id": testCheckoutID, "store_id": "store-1", "merchant_id": "merchant-1",
			"stage": "awaiting_payment", "emitted_at": at,
		}),
	}
}

func escalatedEvent(t *testing.T, eventID string, at time.Time, continueURL string) domainEventStub {
	return domainEventStub{
		eventType: ucp.EventCheckoutEscalated, storeID: "store-1", merchantID: "merchant-1", occurredAt: at,
		payload: mustJSON(t, map[string]any{
			"event_id": eventID, "checkout_session_id": testCheckoutID, "store_id": "store-1", "merchant_id": "merchant-1",
			"reason": "requires 3DS", "continue_url": continueURL, "stage": "escalated", "emitted_at": at,
		}),
	}
}

func failedEvent(t *testing.T, eventID string, at time.Time, code string) domainEventStub {
	return domainEventStub{
		eventType: ucp.EventCheckoutFailed, storeID: "store-1", merchantID: "merchant-1", occurredAt: at,
		payload: mustJSON(t, map[string]any{
			"event_id": eventID, "checkout_session_id": testCheckoutID, "store_id": "store-1", "merchant_id": "merchant-1",
			"code": code, "message": "boom", "stage": "complete", "emitted_at": at,
		}),
	}
}

func orderIntentEvent(t *testing.T, orderID string, at time.Time) domainEventStub {
	return domainEventStub{
		eventType: ucp.EventOrderIntent, storeID: "store-1", merchantID: "merchant-1", occurredAt: at,
		payload: mustJSON(t, map[string]any{
			"checkout_session_id": testCheckoutID, "order_id": orderID, "store_order_ref": orderID, "order_status": "completed",
			"store_id": "store-1", "merchant_id": "merchant-1",
			"line_items": []any{map[string]any{"sku": "SKU-1", "quantity": 1}},
			"total":      map[string]any{"amount_minor": 1500, "currency": "USD"},
			"emitted_at": at,
		}),
	}
}

func TestProjector_FullLifecycle_ProducesCorrectTerminalState(t *testing.T) {
	sessions, timeline := newFakeSessionRepo(), newFakeTimelineRepo()
	p := newTestProjector(sessions, timeline)
	ctx := context.Background()
	t0 := time.Now().UTC()

	must(t, p.handleSessionInitiated(ctx, sessionInitiatedEvent(t, "ev-1", t0)))
	must(t, p.handleCartUpdated(ctx, cartUpdatedEvent(t, "ev-2", t0.Add(time.Minute))))
	must(t, p.handlePaymentSubmitted(ctx, paymentSubmittedEvent(t, "ev-3", t0.Add(2*time.Minute))))
	must(t, p.handleOrderIntent(ctx, orderIntentEvent(t, "order-1", t0.Add(3*time.Minute))))

	s, err := sessions.Get(ctx, "merchant-1", testCheckoutID)
	must(t, err)
	if s.State != StateCompleted {
		t.Fatalf("expected state completed, got %q", s.State)
	}
	if s.EventCount != 4 {
		t.Fatalf("expected event_count 4, got %d", s.EventCount)
	}
	if s.Cart.TotalMinor != 1500 || s.Cart.ItemCount != 1 {
		t.Fatalf("expected cart from order_intent (1500/1 item), got %+v", s.Cart)
	}
	if s.TerminalAt == nil {
		t.Fatalf("expected terminal_at to be set")
	}
	if s.ExpiresAt == nil {
		t.Fatalf("expected expires_at to be set once terminal")
	}
}

// TestProjector_OutOfOrderDeliveryDoesNotRegressTerminalState is A1/R3: a
// terminal session must never be regressed by a late, non-terminal event —
// but the late event must still be counted and appear in the timeline.
func TestProjector_OutOfOrderDeliveryDoesNotRegressTerminalState(t *testing.T) {
	sessions, timeline := newFakeSessionRepo(), newFakeTimelineRepo()
	p := newTestProjector(sessions, timeline)
	ctx := context.Background()
	t0 := time.Now().UTC()

	must(t, p.handleSessionInitiated(ctx, sessionInitiatedEvent(t, "ev-1", t0)))
	must(t, p.handleFailed(ctx, failedEvent(t, "ev-2", t0.Add(time.Minute), "ORDER_CREATION_FAILED")))

	s, err := sessions.Get(ctx, "merchant-1", testCheckoutID)
	must(t, err)
	if s.State != StateFailed {
		t.Fatalf("expected state failed after ev-2, got %q", s.State)
	}
	if s.EventCount != 2 {
		t.Fatalf("expected event_count 2 after ev-2, got %d", s.EventCount)
	}

	// A late cart_updated arrives after the session is already terminal —
	// e.g. redelivery of a message that was in flight before the failure.
	// occurred_at is even earlier than the failure, making the
	// out-of-order-ness explicit.
	must(t, p.handleCartUpdated(ctx, cartUpdatedEvent(t, "ev-3-late", t0.Add(30*time.Second))))

	s, err = sessions.Get(ctx, "merchant-1", testCheckoutID)
	must(t, err)
	if s.State != StateFailed {
		t.Fatalf("terminal state regressed: expected failed, got %q", s.State)
	}
	if s.EventCount != 3 {
		t.Fatalf("expected the late event to still increment event_count to 3, got %d", s.EventCount)
	}

	entries, _, err := timeline.List(ctx, testCheckoutID, 0, 100)
	must(t, err)
	if len(entries) != 3 {
		t.Fatalf("expected the late event to still appear in the timeline (3 entries), got %d", len(entries))
	}
}

// TestProjector_DuplicateDeliveryIsANoOp is A1/R3: redelivering the exact
// same event_id must not double-count or corrupt state.
func TestProjector_DuplicateDeliveryIsANoOp(t *testing.T) {
	sessions, timeline := newFakeSessionRepo(), newFakeTimelineRepo()
	p := newTestProjector(sessions, timeline)
	ctx := context.Background()
	t0 := time.Now().UTC()

	ev := sessionInitiatedEvent(t, "ev-1", t0)
	must(t, p.handleSessionInitiated(ctx, ev))
	must(t, p.handleSessionInitiated(ctx, ev)) // exact redelivery
	must(t, p.handleSessionInitiated(ctx, ev)) // and again

	s, err := sessions.Get(ctx, "merchant-1", testCheckoutID)
	must(t, err)
	if s.EventCount != 1 {
		t.Fatalf("expected duplicate delivery to be a no-op, event_count = %d, want 1", s.EventCount)
	}

	entries, _, err := timeline.List(ctx, testCheckoutID, 0, 100)
	must(t, err)
	if len(entries) != 1 {
		t.Fatalf("expected exactly one timeline entry despite 3 deliveries, got %d", len(entries))
	}
}

func TestProjector_EscalationLifecycle_ReturnedAndResolvedInference(t *testing.T) {
	sessions, timeline := newFakeSessionRepo(), newFakeTimelineRepo()
	p := newTestProjector(sessions, timeline)
	ctx := context.Background()
	t0 := time.Now().UTC()

	must(t, p.handleSessionInitiated(ctx, sessionInitiatedEvent(t, "ev-1", t0)))
	must(t, p.handleEscalated(ctx, escalatedEvent(t, "ev-2", t0.Add(time.Minute), "https://shop.example/3ds")))

	s, err := sessions.Get(ctx, "merchant-1", testCheckoutID)
	must(t, err)
	if s.State != StateEscalated {
		t.Fatalf("expected state escalated, got %q", s.State)
	}
	if s.Escalation == nil || !s.Escalation.ContinueURLIssued || s.Escalation.ReturnedAt != nil {
		t.Fatalf("expected a fresh, unreturned escalation, got %+v", s.Escalation)
	}

	// The agent retries complete after the buyer returns — represented
	// here as a payment_submitted event (any further non-terminal
	// activity is the "returned" proxy; see mongo_sessions.go).
	must(t, p.handlePaymentSubmitted(ctx, paymentSubmittedEvent(t, "ev-3", t0.Add(5*time.Minute))))

	s, err = sessions.Get(ctx, "merchant-1", testCheckoutID)
	must(t, err)
	if s.Escalation.ReturnedAt == nil {
		t.Fatalf("expected escalation.returned_at to be set after further activity")
	}
	if s.Escalation.Resolved {
		t.Fatalf("expected escalation.resolved to still be false — session hasn't reached a terminal state yet")
	}

	must(t, p.handleOrderIntent(ctx, orderIntentEvent(t, "order-1", t0.Add(6*time.Minute))))

	s, err = sessions.Get(ctx, "merchant-1", testCheckoutID)
	must(t, err)
	if s.State != StateCompleted {
		t.Fatalf("expected state completed, got %q", s.State)
	}
	if s.Escalation == nil || !s.Escalation.Resolved {
		t.Fatalf("expected escalation.resolved to be true once the session went terminal")
	}
	_ = timeline
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
