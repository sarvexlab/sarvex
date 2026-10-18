// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package realtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/journey"
)

// fakeJourneyUpdatedEvent implements eventbus.DomainEvent for testing,
// standing in for control.journey.updated as published by JourneyService.
type fakeJourneyUpdatedEvent struct {
	merchantID string
	storeID    string
	payload    []byte
}

func (e *fakeJourneyUpdatedEvent) EventType() string     { return "control.journey.updated" }
func (e *fakeJourneyUpdatedEvent) StoreID() string       { return e.storeID }
func (e *fakeJourneyUpdatedEvent) MerchantID() string    { return e.merchantID }
func (e *fakeJourneyUpdatedEvent) OccurredAt() time.Time { return time.Now().UTC() }
func (e *fakeJourneyUpdatedEvent) Payload() []byte       { return e.payload }

func newJourneyUpdatedEvent(t *testing.T, storeID, merchantID, state string) eventbus.DomainEvent {
	t.Helper()
	b, err := json.Marshal(journeyUpdatedPayload{StoreID: storeID, State: state})
	if err != nil {
		t.Fatalf("marshal journey.updated payload: %v", err)
	}
	return &fakeJourneyUpdatedEvent{merchantID: merchantID, storeID: storeID, payload: b}
}

// TestSubscriber_Handle_BroadcastsToHub verifies the genuine reason this
// component exists: turning a control.journey.updated event into a
// journey_stream_message frame delivered to a live WebSocket client watching
// the same store, via a real socket end-to-end.
func TestSubscriber_Handle_BroadcastsToHub(t *testing.T) {
	hub := NewHub()
	srv := newUpgradeServer(t, hub, "store-1")
	_, conn := dialClient(t, srv)
	time.Sleep(20 * time.Millisecond)

	subscriber := NewSubscriber(hub, nil, nil, testLog)
	event := newJourneyUpdatedEvent(t, "store-1", "merchant-1", "connected")
	if err := subscriber.Handle(context.Background(), event); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("expected a message, got error: %v", err)
	}

	var msg journeyStreamMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal stream message: %v", err)
	}
	if msg.Type != "journey.updated" || msg.StoreID != "store-1" || msg.State != "connected" {
		t.Errorf("unexpected stream message: %+v", msg)
	}
}

// TestSubscriber_Handle_MalformedPayload verifies a malformed payload is
// logged and dropped, not propagated as an error (consistent with the rest
// of the codebase's bus-handler convention — see journey.Subscriber.Handle).
func TestSubscriber_Handle_MalformedPayload(t *testing.T) {
	hub := NewHub()
	subscriber := NewSubscriber(hub, nil, nil, testLog)

	event := &fakeJourneyUpdatedEvent{merchantID: "merchant-1", storeID: "store-1", payload: []byte("not json")}

	if err := subscriber.Handle(context.Background(), event); err != nil {
		t.Fatalf("expected malformed payload to be swallowed, got %v", err)
	}
}

// fakeTaskUpdatedEvent implements eventbus.DomainEvent for testing, standing
// in for control.task.updated as published by TaskService.
type fakeTaskUpdatedEvent struct {
	merchantID string
	storeID    string
	payload    []byte
}

func (e *fakeTaskUpdatedEvent) EventType() string     { return "control.task.updated" }
func (e *fakeTaskUpdatedEvent) StoreID() string       { return e.storeID }
func (e *fakeTaskUpdatedEvent) MerchantID() string    { return e.merchantID }
func (e *fakeTaskUpdatedEvent) OccurredAt() time.Time { return time.Now().UTC() }
func (e *fakeTaskUpdatedEvent) Payload() []byte       { return e.payload }

func newTaskUpdatedEvent(t *testing.T, taskID, storeID, merchantID, state string) eventbus.DomainEvent {
	t.Helper()
	b, err := json.Marshal(taskUpdatedPayload{TaskID: taskID, StoreID: storeID, State: state})
	if err != nil {
		t.Fatalf("marshal task.updated payload: %v", err)
	}
	return &fakeTaskUpdatedEvent{merchantID: merchantID, storeID: storeID, payload: b}
}

// stubJourneyLookup is a JourneyLookup test double: it "has a journey" for
// every store_id in exists, and reports journey.ErrNotFound for every other
// store — the exact shape the relevance check in HandleTaskUpdated branches
// on.
type stubJourneyLookup struct {
	exists map[string]bool
}

func (s stubJourneyLookup) Get(_ context.Context, _ domain.MerchantID, storeID domain.StoreID) (domain.Journey, error) {
	if s.exists[string(storeID)] {
		return domain.Journey{}, nil
	}
	return domain.Journey{}, journey.ErrNotFound
}

// TestSubscriber_HandleTaskUpdated_BroadcastsWhenStoreHasJourney verifies the
// relevance rule: a task change for a store that has an active journey
// record is pushed to that store's WebSocket clients as a task.updated frame.
func TestSubscriber_HandleTaskUpdated_BroadcastsWhenStoreHasJourney(t *testing.T) {
	hub := NewHub()
	srv := newUpgradeServer(t, hub, "store-1")
	_, conn := dialClient(t, srv)
	time.Sleep(20 * time.Millisecond)

	subscriber := NewSubscriber(hub, stubJourneyLookup{exists: map[string]bool{"store-1": true}}, nil, testLog)
	event := newTaskUpdatedEvent(t, "task-1", "store-1", "merchant-1", "done")
	if err := subscriber.HandleTaskUpdated(context.Background(), event); err != nil {
		t.Fatalf("HandleTaskUpdated: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("expected a message, got error: %v", err)
	}

	var msg journeyStreamMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal stream message: %v", err)
	}
	if msg.Type != "task.updated" || msg.StoreID != "store-1" || msg.TaskID != "task-1" || msg.State != "done" {
		t.Errorf("unexpected stream message: %+v", msg)
	}
}

// TestSubscriber_HandleTaskUpdated_SkipsWhenStoreHasNoJourney verifies a task
// change for a store with no journey record yet (never paired, or since
// disconnected) is not pushed — nothing could be watching that store's Path.
func TestSubscriber_HandleTaskUpdated_SkipsWhenStoreHasNoJourney(t *testing.T) {
	hub := NewHub()
	srv := newUpgradeServer(t, hub, "store-2")
	_, conn := dialClient(t, srv)
	time.Sleep(20 * time.Millisecond)

	subscriber := NewSubscriber(hub, stubJourneyLookup{exists: map[string]bool{}}, nil, testLog)
	event := newTaskUpdatedEvent(t, "task-1", "store-2", "merchant-1", "done")
	if err := subscriber.HandleTaskUpdated(context.Background(), event); err != nil {
		t.Fatalf("HandleTaskUpdated: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatalf("expected no message for a store with no journey record")
	}
}

// TestSubscriber_HandleTaskUpdated_NilJourneyLookupBroadcastsUnconditionally
// documents the test-only escape hatch: a nil JourneyLookup (as used by
// tests that only exercise control.journey.updated) skips the relevance
// check entirely rather than panicking.
func TestSubscriber_HandleTaskUpdated_NilJourneyLookupBroadcastsUnconditionally(t *testing.T) {
	hub := NewHub()
	srv := newUpgradeServer(t, hub, "store-3")
	_, conn := dialClient(t, srv)
	time.Sleep(20 * time.Millisecond)

	subscriber := NewSubscriber(hub, nil, nil, testLog)
	event := newTaskUpdatedEvent(t, "task-1", "store-3", "merchant-1", "open")
	if err := subscriber.HandleTaskUpdated(context.Background(), event); err != nil {
		t.Fatalf("HandleTaskUpdated: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("expected a message, got error: %v", err)
	}
}

// fakeActivationStateChangedEvent implements eventbus.DomainEvent for testing,
// standing in for control.store.activation_state_changed as published by ActivationService.
type fakeActivationStateChangedEvent struct {
	merchantID string
	storeID    string
	payload    []byte
}

func (e *fakeActivationStateChangedEvent) EventType() string {
	return "control.store.activation_state_changed"
}
func (e *fakeActivationStateChangedEvent) StoreID() string       { return e.storeID }
func (e *fakeActivationStateChangedEvent) MerchantID() string    { return e.merchantID }
func (e *fakeActivationStateChangedEvent) OccurredAt() time.Time { return time.Now().UTC() }
func (e *fakeActivationStateChangedEvent) Payload() []byte       { return e.payload }

func newActivationStateChangedEvent(t *testing.T, storeID, merchantID, fromState, toState, signal string) eventbus.DomainEvent {
	t.Helper()
	b, err := json.Marshal(activationStateChangedPayload{
		StoreID:   storeID,
		FromState: fromState,
		ToState:   toState,
		Signal:    signal,
	})
	if err != nil {
		t.Fatalf("marshal activation_state_changed payload: %v", err)
	}
	return &fakeActivationStateChangedEvent{merchantID: merchantID, storeID: storeID, payload: b}
}

// TestSubscriber_HandleActivationStateChanged_BroadcastsToHub verifies that
// activation state change events are pushed to WebSocket clients watching
// the store, enabling real-time UCP path updates.
func TestSubscriber_HandleActivationStateChanged_BroadcastsToHub(t *testing.T) {
	hub := NewHub()
	srv := newUpgradeServer(t, hub, "store-1")
	_, conn := dialClient(t, srv)
	time.Sleep(20 * time.Millisecond)

	subscriber := NewSubscriber(hub, nil, nil, testLog)
	event := newActivationStateChangedEvent(t, "store-1", "merchant-1", "draft", "connector_linked", "connector_linked")
	if err := subscriber.HandleActivationStateChanged(context.Background(), event); err != nil {
		t.Fatalf("HandleActivationStateChanged: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("expected a message, got error: %v", err)
	}

	var msg journeyStreamMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal stream message: %v", err)
	}
	if msg.Type != "activation.updated" || msg.StoreID != "store-1" || msg.State != "connector_linked" {
		t.Errorf("unexpected stream message: %+v", msg)
	}
}

// fakeCatalogProductsReadyEvent implements eventbus.DomainEvent for testing,
// standing in for sync.catalog.products_ready as published by the sync plane.
type fakeCatalogProductsReadyEvent struct {
	merchantID string
	storeID    string
	payload    []byte
}

func (e *fakeCatalogProductsReadyEvent) EventType() string     { return "sync.catalog.products_ready" }
func (e *fakeCatalogProductsReadyEvent) StoreID() string       { return e.storeID }
func (e *fakeCatalogProductsReadyEvent) MerchantID() string    { return e.merchantID }
func (e *fakeCatalogProductsReadyEvent) OccurredAt() time.Time { return time.Now().UTC() }
func (e *fakeCatalogProductsReadyEvent) Payload() []byte       { return e.payload }

// TestSubscriber_HandleCatalogProductsReady_BroadcastsToHub verifies that a
// finished rebuild reaches the console, which is what lets the readiness pill
// and the import task's product count settle without a page reload.
func TestSubscriber_HandleCatalogProductsReady_BroadcastsToHub(t *testing.T) {
	hub := NewHub()
	srv := newUpgradeServer(t, hub, "store-1")
	_, conn := dialClient(t, srv)
	time.Sleep(20 * time.Millisecond)

	// The sync plane's payload carries more than store_id (product_count,
	// source_sync); the handler must read the one field it needs and ignore
	// the rest rather than failing to unmarshal.
	payload := []byte(`{"merchant_id":"merchant-1","store_id":"store-1","product_count":42,"source_sync":"full"}`)
	subscriber := NewSubscriber(hub, nil, nil, testLog)
	event := &fakeCatalogProductsReadyEvent{merchantID: "merchant-1", storeID: "store-1", payload: payload}
	if err := subscriber.HandleCatalogProductsReady(context.Background(), event); err != nil {
		t.Fatalf("HandleCatalogProductsReady: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("expected a message, got error: %v", err)
	}

	var msg journeyStreamMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal stream message: %v", err)
	}
	if msg.Type != "catalog.updated" || msg.StoreID != "store-1" {
		t.Errorf("unexpected stream message: %+v", msg)
	}
	// The catalog has no single state to report; the frame must not invent one.
	if msg.State != "" {
		t.Errorf("catalog.updated should carry no state, got %q", msg.State)
	}
}

// fakeHealthEvent stands in for any of the three ops.health.* events. They
// carry different payload shapes and all set StoreID on the envelope, which
// is what the handler reads.
type fakeHealthEvent struct {
	kind       string
	merchantID string
	storeID    string
}

func (e *fakeHealthEvent) EventType() string     { return e.kind }
func (e *fakeHealthEvent) StoreID() string       { return e.storeID }
func (e *fakeHealthEvent) MerchantID() string    { return e.merchantID }
func (e *fakeHealthEvent) OccurredAt() time.Time { return time.Now().UTC() }
func (e *fakeHealthEvent) Payload() []byte {
	return []byte(`{"probe_type":"profile_reachable","outcome":"pass"}`)
}

// Health was the last surface with no push at all — it fetched on mount and
// then sat frozen while probes ran underneath it every few minutes.
func TestSubscriber_HandleHealthChanged_BroadcastsToHub(t *testing.T) {
	hub := NewHub()
	srv := newUpgradeServer(t, hub, "store-1")
	_, conn := dialClient(t, srv)
	time.Sleep(20 * time.Millisecond)

	subscriber := NewSubscriber(hub, nil, nil, testLog)
	event := &fakeHealthEvent{kind: "ops.health.probe_completed", merchantID: "merchant-1", storeID: "store-1"}
	if err := subscriber.HandleHealthChanged(context.Background(), event); err != nil {
		t.Fatalf("HandleHealthChanged: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("expected a message, got error: %v", err)
	}

	var msg journeyStreamMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal stream message: %v", err)
	}
	if msg.Type != "health.updated" || msg.StoreID != "store-1" {
		t.Errorf("unexpected stream message: %+v", msg)
	}
	// Health has no single state to report; the client re-reads statements,
	// endpoint status, alerts and findings for itself.
	if msg.State != "" {
		t.Errorf("health.updated should carry no state, got %q", msg.State)
	}
}

// An event with no store cannot be routed to a watcher, and must not be
// broadcast under an empty key.
func TestSubscriber_HandleHealthChanged_IgnoresEventWithoutStore(t *testing.T) {
	hub := NewHub()
	srv := newUpgradeServer(t, hub, "store-1")
	_, conn := dialClient(t, srv)
	time.Sleep(20 * time.Millisecond)

	subscriber := NewSubscriber(hub, nil, nil, testLog)
	if err := subscriber.HandleHealthChanged(context.Background(), &fakeHealthEvent{kind: "ops.health.recovered"}); err != nil {
		t.Fatalf("HandleHealthChanged: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected no message for an event with no store")
	}
}

// fakeOrderCreatedEvent stands in for sync.order.created, published by the
// sync plane's checkout order-intent handler once an order is recorded.
type fakeOrderCreatedEvent struct {
	merchantID string
	storeID    string
}

func (e *fakeOrderCreatedEvent) EventType() string     { return "sync.order.created" }
func (e *fakeOrderCreatedEvent) StoreID() string       { return e.storeID }
func (e *fakeOrderCreatedEvent) MerchantID() string    { return e.merchantID }
func (e *fakeOrderCreatedEvent) OccurredAt() time.Time { return time.Now().UTC() }
func (e *fakeOrderCreatedEvent) Payload() []byte {
	return []byte(`{"order_id":"order-1","checkout_session_id":"cs-1","store_order_ref":"1001"}`)
}

// The Path's "Agent orders this week" tile rolled up GET /orders once on
// mount and never again, so an order placed while the page sat open was
// invisible until the merchant reloaded — the same gap Health had before
// health.updated.
func TestSubscriber_HandleOrderCreated_BroadcastsToHub(t *testing.T) {
	hub := NewHub()
	srv := newUpgradeServer(t, hub, "store-1")
	_, conn := dialClient(t, srv)
	time.Sleep(20 * time.Millisecond)

	subscriber := NewSubscriber(hub, nil, nil, testLog)
	event := &fakeOrderCreatedEvent{merchantID: "merchant-1", storeID: "store-1"}
	if err := subscriber.HandleOrderCreated(context.Background(), event); err != nil {
		t.Fatalf("HandleOrderCreated: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("expected a message, got error: %v", err)
	}

	var msg journeyStreamMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal stream message: %v", err)
	}
	if msg.Type != "orders.updated" || msg.StoreID != "store-1" {
		t.Errorf("unexpected stream message: %+v", msg)
	}
	// One order among many has no single state worth naming; the client
	// re-reads the whole order list for itself.
	if msg.State != "" {
		t.Errorf("orders.updated should carry no state, got %q", msg.State)
	}
}

// An event with no store cannot be routed to a watcher, and must not be
// broadcast under an empty key.
func TestSubscriber_HandleOrderCreated_IgnoresEventWithoutStore(t *testing.T) {
	hub := NewHub()
	srv := newUpgradeServer(t, hub, "store-1")
	_, conn := dialClient(t, srv)
	time.Sleep(20 * time.Millisecond)

	subscriber := NewSubscriber(hub, nil, nil, testLog)
	if err := subscriber.HandleOrderCreated(context.Background(), &fakeOrderCreatedEvent{merchantID: "merchant-1"}); err != nil {
		t.Fatalf("HandleOrderCreated: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected no message for an event with no store")
	}
}
