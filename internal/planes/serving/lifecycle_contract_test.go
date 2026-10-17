// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/commerce/model"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/connectors/base"
)

// ─── helpers ────────────────────────────────────────────────────────────────

// decodePayload unmarshals a published event's payload and strips the
// non-deterministic envelope fields (event_id, occurred_at/emitted_at)
// after asserting their shape, so the remainder can be compared against a
// literal golden map (A2).
func decodePayload(t *testing.T, ev interface{ Payload() []byte }) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(ev.Payload(), &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if id, ok := decoded["event_id"]; ok {
		s, _ := id.(string)
		if !strings.HasPrefix(s, "evt_") || len(s) != len("evt_")+26 {
			t.Errorf("event_id %q does not look like an evt_-prefixed ULID", s)
		}
		delete(decoded, "event_id")
	}
	for _, tsKey := range []string{"occurred_at", "emitted_at"} {
		if raw, ok := decoded[tsKey]; ok {
			s, _ := raw.(string)
			if _, err := time.Parse(time.RFC3339, s); err != nil {
				t.Errorf("%s %q is not RFC3339: %v", tsKey, s, err)
			}
			delete(decoded, tsKey)
		}
	}
	return decoded
}

// requireEventType asserts ev's concrete type via the two-value form (a
// single-value type assertion panics on mismatch, which is what errcheck
// flags on a bare ev.(*T) — this fails the test cleanly instead).
func requireEventType[T eventbus.DomainEvent](t *testing.T, ev eventbus.DomainEvent) T {
	t.Helper()
	typed, ok := ev.(T)
	if !ok {
		t.Fatalf("event has unexpected type %T, want %T", ev, typed)
	}
	return typed
}

func assertGolden(t *testing.T, got, want map[string]any) {
	t.Helper()
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("payload mismatch:\n got:  %s\n want: %s", gotJSON, wantJSON)
	}
}

func newFixtureRegistryAndSessions(storeID string, conn base.Connector) (*base.Registry, SessionStore) {
	return newTestRegistry(storeID, conn), NewMemorySessionStore(time.Minute)
}

// ─── A2: golden fixtures, one per lifecycle transition ─────────────────────

func TestCheckoutService_Create_EmitsInitiatedV1(t *testing.T) {
	const storeID = "store-1"
	conn := &testConnector{createResp: &base.CheckoutResponse{ID: "backend-1", Status: base.CheckoutStatusIncomplete}}
	registry, sessions := newFixtureRegistryAndSessions(storeID, conn)
	defer sessions.Close()
	events := &noopEventBus{}
	svc := NewCheckoutService(sessions, &noopCache{}, registry, events, discardLogger())

	resp, err := svc.Create(context.Background(), &testStoreProvider{storeID: storeID}, storeID, CreateCheckoutInput{
		LineItems:     []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		AgentPlatform: "gemini.google.com",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ev := waitForEventType(t, events, ucp.EventCheckoutInitiated, time.Second)
	got := decodePayload(t, requireEventType[*CheckoutInitiatedEvent](t, ev))
	assertGolden(t, got, map[string]any{
		"event_version": "1.0",
		"merchant_id":   storeID,
		"store_id":      storeID,
		"surface":       "google_gemini",
		"checkout_id":   resp.ID,
	})
}

func TestCheckoutService_Update_EmitsUpdatedV1(t *testing.T) {
	const storeID = "store-1"
	conn := &testConnector{updateResp: &base.CheckoutResponse{Status: base.CheckoutStatusReadyForComplete}}
	registry, sessions := newFixtureRegistryAndSessions(storeID, conn)
	defer sessions.Close()
	events := &noopEventBus{}
	svc := NewCheckoutService(sessions, &noopCache{}, registry, events, discardLogger())

	seed := &CheckoutSession{
		SessionID: "sess-1", StoreID: storeID, MerchantID: storeID, BackendCheckoutID: "backend-1",
		State: SessionStateIncomplete, Surface: string(SurfaceGoogleAIMode),
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := sessions.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := svc.Update(context.Background(), &testStoreProvider{storeID: storeID}, storeID, "sess-1", UpdateCheckoutInput{
		Payment: &model.Payment{},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	ev := waitForEventType(t, events, ucp.EventCheckoutUpdated, time.Second)
	got := decodePayload(t, requireEventType[*CheckoutUpdatedEvent](t, ev))
	assertGolden(t, got, map[string]any{
		"event_version": "1.0",
		"merchant_id":   storeID,
		"store_id":      storeID,
		"surface":       "google_ai_mode",
		"checkout_id":   "sess-1",
	})
}

func TestCheckoutService_Complete_EmitsCompletedV1(t *testing.T) {
	const storeID = "store-1"
	conn := &testConnector{completeResp: &base.CheckoutResponse{
		Status: base.CheckoutStatusCompleted,
		Totals: model.Totals{{Type: model.TotalTypeTotal, Amount: 1000}},
		Order:  &model.OrderConfirmation{ID: "order-1"},
	}}
	registry, sessions := newFixtureRegistryAndSessions(storeID, conn)
	defer sessions.Close()
	events := &noopEventBus{}
	svc := NewCheckoutService(sessions, &noopCache{}, registry, events, discardLogger())

	seed := &CheckoutSession{
		SessionID: "sess-1", StoreID: storeID, MerchantID: storeID, BackendCheckoutID: "backend-1",
		State: SessionStateReadyForComplete, Surface: string(SurfaceUnknown),
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := sessions.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := svc.Complete(context.Background(), &testStoreProvider{storeID: storeID}, storeID, "sess-1", &model.Payment{}, ""); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	ev := waitForEventType(t, events, ucp.EventCheckoutCompleted, time.Second)
	got := decodePayload(t, requireEventType[*CheckoutCompletedEvent](t, ev))
	assertGolden(t, got, map[string]any{
		"event_version": "1.0",
		"merchant_id":   storeID,
		"store_id":      storeID,
		"surface":       "unknown",
		"checkout_id":   "sess-1",
	})
}

func TestCheckoutService_Complete_Escalation_EmitsEscalatedV1(t *testing.T) {
	const storeID = "store-1"
	conn := &testConnector{completeResp: &base.CheckoutResponse{
		Status:      base.CheckoutStatusRequiresEscalation,
		ContinueURL: "https://shop.example/3ds",
		Messages: []model.Message{model.NewErrorMessage(model.MessageError{
			Content: "3DS verification required", Severity: model.MessageSeverityRequiresBuyerInput,
		})},
	}}
	registry, sessions := newFixtureRegistryAndSessions(storeID, conn)
	defer sessions.Close()
	events := &noopEventBus{}
	svc := NewCheckoutService(sessions, &noopCache{}, registry, events, discardLogger())

	seed := &CheckoutSession{
		SessionID: "sess-1", StoreID: storeID, MerchantID: storeID, BackendCheckoutID: "backend-1",
		State: SessionStateReadyForComplete, Surface: string(SurfaceGoogleAIMode),
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := sessions.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := svc.Complete(context.Background(), &testStoreProvider{storeID: storeID}, storeID, "sess-1", &model.Payment{}, ""); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	ev := waitForEventType(t, events, ucp.EventCheckoutEscalated, time.Second)
	got := decodePayload(t, requireEventType[*CheckoutEscalatedEvent](t, ev))
	assertGolden(t, got, map[string]any{
		"event_version":       "1.0",
		"merchant_id":         storeID,
		"store_id":            storeID,
		"surface":             "google_ai_mode",
		"checkout_id":         "sess-1",
		"checkout_session_id": "sess-1",
		"reason":              "3DS verification required",
		"continue_url":        "https://shop.example/3ds",
		"stage":               "escalated",
	})
}

func TestCheckoutService_Create_Failure_EmitsFailedV1(t *testing.T) {
	const storeID = "store-1"
	conn := &testConnector{createErr: errors.New("simulated store rejection")}
	registry, sessions := newFixtureRegistryAndSessions(storeID, conn)
	defer sessions.Close()
	events := &noopEventBus{}
	svc := NewCheckoutService(sessions, &noopCache{}, registry, events, discardLogger())

	_, err := svc.Create(context.Background(), &testStoreProvider{storeID: storeID}, storeID, CreateCheckoutInput{
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 2}},
	})
	if err == nil {
		t.Fatal("expected Create to fail when the connector rejects it")
	}

	ev := waitForEventType(t, events, ucp.EventCheckoutFailed, time.Second)
	got := decodePayload(t, requireEventType[*CheckoutFailedEvent](t, ev))
	checkoutID, _ := got["checkout_id"].(string)
	checkoutSessionID, _ := got["checkout_session_id"].(string)
	delete(got, "checkout_id")
	delete(got, "checkout_session_id")
	assertGolden(t, got, map[string]any{
		"event_version": "1.0",
		"merchant_id":   storeID,
		"store_id":      storeID,
		"surface":       "unknown",
		"code":          "CONNECTOR_CREATE_FAILED",
		"message":       "store rejected checkout creation",
		"stage":         "create",
		"failure": map[string]any{
			"code":       "CONNECTOR_CREATE_FAILED",
			"stage":      "session_create",
			"handler_id": nil,
			"detail":     "store rejected checkout creation",
		},
		"cart_snapshot": map[string]any{
			"currency":    "USD",
			"total_minor": float64(0),
			"line_items": []any{
				map[string]any{"sku": "SKU-1", "quantity": float64(2), "unit_price_minor": float64(0)},
			},
		},
	})
	// checkout_id/checkout_session_id are the connector-assigned session id,
	// generated inside Create — assert non-empty and equal rather than golden.
	if checkoutID == "" || checkoutID != checkoutSessionID {
		t.Errorf("expected checkout_id == checkout_session_id, got %q / %q", checkoutID, checkoutSessionID)
	}
}

// ─── A3: publish failure never alters the checkout response ────────────────

func TestCheckoutService_PublishFailureDoesNotAlterResponse(t *testing.T) {
	const storeID = "store-1"
	conn := &testConnector{createResp: &base.CheckoutResponse{ID: "backend-1", Status: base.CheckoutStatusIncomplete}}
	registry, sessions := newFixtureRegistryAndSessions(storeID, conn)
	defer sessions.Close()
	events := &noopEventBus{publishErr: errors.New("simulated bus outage")}
	svc := NewCheckoutService(sessions, &noopCache{}, registry, events, discardLogger())

	before := LifecyclePublishFailureCount()

	resp, err := svc.Create(context.Background(), &testStoreProvider{storeID: storeID}, storeID, CreateCheckoutInput{
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("Create must succeed even though every publish fails, got error: %v", err)
	}
	if resp == nil || resp.ID == "" {
		t.Fatalf("expected a normal successful response, got %+v", resp)
	}

	// The failure must have been counted (R6) — poll briefly since the
	// publish (and thus the counter increment) happens off the request
	// goroutine.
	deadline := time.Now().Add(time.Second)
	for LifecyclePublishFailureCount() <= before {
		if time.Now().After(deadline) {
			t.Fatal("expected LifecyclePublishFailureCount to increase after a failed publish")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestCheckoutService_PublishPanicDoesNotEscapeHandler exercises R6's other
// half: a panic during publish/serialization must never reach the caller.
// panicOnPublishBus (below) panics inside Publish, which safego.Go's
// recover (inside publishLifecycleEventV1) must swallow.
func TestCheckoutService_PublishPanicDoesNotEscapeHandler(t *testing.T) {
	const storeID = "store-1"
	conn := &testConnector{createResp: &base.CheckoutResponse{ID: "backend-1", Status: base.CheckoutStatusIncomplete}}
	registry, sessions := newFixtureRegistryAndSessions(storeID, conn)
	defer sessions.Close()
	events := &panicOnPublishBus{}
	svc := NewCheckoutService(sessions, &noopCache{}, registry, events, discardLogger())

	resp, err := svc.Create(context.Background(), &testStoreProvider{storeID: storeID}, storeID, CreateCheckoutInput{
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("Create must succeed even though the publisher panics, got error: %v", err)
	}
	if resp == nil || resp.ID == "" {
		t.Fatalf("expected a normal successful response, got %+v", resp)
	}
	// Give the async goroutine (which panics and recovers) a moment to run
	// so a genuinely-escaping panic would have crashed the test process by
	// the time this test function returns.
	time.Sleep(20 * time.Millisecond)
}

// panicOnPublishBus panics inside Publish for versioned-contract event
// types only, simulating a serialization bug — used to prove
// publishLifecycleEventV1's safego.Go wrapper contains it. It only panics
// on the new contract's own topics (not the pre-existing, unrelated
// session_initiated/cart_updated/payment_submitted topics
// checkout_lifecycle_events.go also still emits on the same call paths —
// see this file's package-level doc comment) so this test isolates R6's
// panic-safety guarantee to the code this task actually changed.
type panicOnPublishBus struct{}

func (b *panicOnPublishBus) Publish(ctx context.Context, event eventbus.DomainEvent) error {
	switch event.EventType() {
	case ucp.EventCheckoutInitiated, ucp.EventCheckoutUpdated, ucp.EventCheckoutCompleted,
		ucp.EventCheckoutAbandoned, ucp.EventCheckoutEscalated, ucp.EventCheckoutFailed, ucp.EventTelemetryRecorded:
		panic("simulated panic during publish/serialization")
	}
	return nil
}
func (b *panicOnPublishBus) Subscribe(ctx context.Context, topic string, handler eventbus.EventHandler) (eventbus.Subscription, error) {
	return nil, nil
}
func (b *panicOnPublishBus) Unsubscribe(ctx context.Context, sub eventbus.Subscription) error {
	return nil
}
func (b *panicOnPublishBus) Close() error { return nil }

// ─── A4: abandonment sweep ───────────────────────────────────────────────

func TestAbandonmentSweeper_EmitsOncePerSession(t *testing.T) {
	sessions := NewMemorySessionStore(time.Hour)
	defer sessions.Close()
	events := &noopEventBus{}
	fixedNow := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	seed := &CheckoutSession{
		SessionID: "sess-1", StoreID: "store-1", MerchantID: "store-1",
		State: SessionStateIncomplete, Surface: string(SurfaceGoogleGemini),
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt: fixedNow.Add(-time.Hour), UpdatedAt: fixedNow.Add(-45 * time.Minute), // stale
	}
	if err := sessions.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	sweeper := NewAbandonmentSweeper(sessions, events, AbandonmentSweepConfig{AbandonmentThreshold: 30 * time.Minute}, discardLogger())
	sweeper.now = func() time.Time { return fixedNow }

	sweeper.runOnce(context.Background())
	ev := waitForEventType(t, events, ucp.EventCheckoutAbandoned, time.Second)
	first := requireEventType[*CheckoutAbandonedEvent](t, ev)
	if first.CheckoutID != "sess-1" || first.Surface != SurfaceGoogleGemini {
		t.Errorf("unexpected abandoned event: %+v", first)
	}

	// Second pass: the session is now marked (AbandonedNotifiedAt set) and
	// still stale/non-terminal — it must not be re-selected, so no second
	// abandoned event should ever appear.
	sweeper.runOnce(context.Background())
	time.Sleep(20 * time.Millisecond) // let any (wrongly re-emitted) async publish land
	count := 0
	for _, e := range events.Events() {
		if e.EventType() == ucp.EventCheckoutAbandoned {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 serving.checkout.abandoned event across two sweep passes, got %d", count)
	}
}

func TestAbandonmentSweeper_SkipsTerminalSessions(t *testing.T) {
	sessions := NewMemorySessionStore(time.Hour)
	defer sessions.Close()
	events := &noopEventBus{}
	fixedNow := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	seed := &CheckoutSession{
		SessionID: "sess-done", StoreID: "store-1", MerchantID: "store-1",
		State:     SessionStateCompleted, // terminal — never a candidate
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt: fixedNow.Add(-time.Hour), UpdatedAt: fixedNow.Add(-45 * time.Minute),
	}
	if err := sessions.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	sweeper := NewAbandonmentSweeper(sessions, events, AbandonmentSweepConfig{AbandonmentThreshold: 30 * time.Minute}, discardLogger())
	sweeper.now = func() time.Time { return fixedNow }
	sweeper.runOnce(context.Background())

	time.Sleep(20 * time.Millisecond)
	for _, e := range events.Events() {
		if e.EventType() == ucp.EventCheckoutAbandoned {
			t.Fatalf("terminal session must never be swept as abandoned, got %+v", e)
		}
	}
}

// ─── A5: no payment-credential shaped field in any lifecycle payload ───────

// credentialPatterns mirrors what a reviewer would grep a payload for: any
// JSON key that names raw payment-credential material. The checkout schema
// has no field able to hold one (architecture invariant 8) — this test
// exists to keep it that way for every event this package emits.
var credentialPatterns = []string{
	"credential", "card_number", "cardnumber", "cvv", "cvc", "cryptogram",
	"pan", "account_number", "security_code", "expiry",
}

func assertNoCredentialShapedFields(t *testing.T, label string, payload []byte) {
	t.Helper()
	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("%s: unmarshal payload: %v", label, err)
	}
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch val := v.(type) {
		case map[string]any:
			for k, sub := range val {
				lk := strings.ToLower(k)
				for _, pat := range credentialPatterns {
					if strings.Contains(lk, pat) {
						t.Errorf("%s: field %q at %s matches a payment-credential pattern (%q)", label, k, path, pat)
					}
				}
				walk(path+"."+k, sub)
			}
		case []any:
			for i, item := range val {
				walk(path, item)
				_ = i
			}
		case string:
			if strings.Contains(val, "4111111111111111") {
				t.Errorf("%s: field at %s leaked the raw test card number", label, path)
			}
		}
	}
	walk("$", decoded)
}

func TestLifecyclePayloads_NeverCarryPaymentCredentialFields(t *testing.T) {
	const storeID = "store-1"
	securedPayment := &model.Payment{
		Instruments: []model.SelectedPaymentInstrument{{
			PaymentInstrument: model.PaymentInstrument{
				ID: "pi_1", HandlerID: "handler-1", Type: "card",
				Credential: &model.PaymentCredential{
					Type: "card",
					Card: &model.CardCredential{
						Type: "card", Number: "4111111111111111", CVC: "999", Name: "Test Buyer",
					},
				},
			},
			Selected: true,
		}},
	}

	session := &CheckoutSession{
		SessionID: "sess-1", StoreID: storeID, MerchantID: storeID,
		State:     SessionStateReadyForComplete,
		Surface:   string(SurfaceGoogleAIMode),
		Payment:   securedPayment,
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}

	failed := &CheckoutFailedEvent{
		EventVersion: eventContractVersion, EventID: newEventID(),
		CheckoutSessionID: session.SessionID, CheckoutID: session.SessionID,
		StoreID_: session.StoreID, MerchantID_: session.MerchantID,
		Surface: Surface(session.Surface),
		Code:    string(FailureCodeOrderCreationFailed), Message: "failed to create order in your store",
		Stage: "complete",
		Failure: FailureDetail{
			Code: FailureCodeOrderCreationFailed, Stage: stageForFailureCode(FailureCodeOrderCreationFailed),
			HandlerID: ptr(handlerIDFromPayment(session.Payment)), Detail: "failed to create order in your store",
		},
		Cart:        cartSnapshotFromSession(session),
		EmittedAt_:  time.Now().UTC(),
		OccurredAt_: time.Now().UTC(),
	}
	assertNoCredentialShapedFields(t, "CheckoutFailedEvent", failed.Payload())

	abandoned := &CheckoutAbandonedEvent{
		EventVersion: eventContractVersion, EventID: newEventID(), OccurredAt_: time.Now().UTC(),
		MerchantID_: session.MerchantID, StoreID_: session.StoreID, Surface: Surface(session.Surface),
		CheckoutID: session.SessionID, Cart: cartSnapshotFromSession(session),
	}
	assertNoCredentialShapedFields(t, "CheckoutAbandonedEvent", abandoned.Payload())

	// handlerIDFromPayment itself must only ever surface the handler_id —
	// never the credential it's paired with.
	if id := handlerIDFromPayment(session.Payment); id != "handler-1" {
		t.Errorf("expected handler_id handler-1, got %q", id)
	}
}

func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
