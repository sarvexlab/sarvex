// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package journey_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/journey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEvent implements eventbus.DomainEvent for testing.
type fakeEvent struct {
	merchantID string
	storeID    string
	eventType  string
	payload    []byte
}

func (e *fakeEvent) EventType() string {
	return e.eventType
}

func (e *fakeEvent) StoreID() string {
	return e.storeID
}

func (e *fakeEvent) MerchantID() string {
	return e.merchantID
}

func (e *fakeEvent) OccurredAt() time.Time {
	return time.Now().UTC()
}

func (e *fakeEvent) Payload() []byte {
	return e.payload
}

func newJourneySignalEvent(merchantID string, signal string) eventbus.DomainEvent {
	payload, _ := json.Marshal(journey.JourneySignalPayload{Signal: signal})
	return &fakeEvent{
		merchantID: merchantID,
		storeID:    merchantID, // tests initialize with storeID = merchantID
		eventType:  "journey.signal",
		payload:    payload,
	}
}

// fakeEventBus is a stub EventBus for testing.
type fakeEventBus struct{}

func (f *fakeEventBus) Publish(_ context.Context, _ eventbus.DomainEvent) error {
	return nil
}

func (f *fakeEventBus) Subscribe(_ context.Context, _ string, _ eventbus.EventHandler) (eventbus.Subscription, error) {
	return &fakeSubscription{}, nil
}

func (f *fakeEventBus) Unsubscribe(_ context.Context, _ eventbus.Subscription) error {
	return nil
}

func (f *fakeEventBus) Close() error {
	return nil
}

// fakeConnectorSyncRecorder is a test double for journey.ConnectorSyncRecorder.
type fakeConnectorSyncRecorder struct {
	recorded map[string]time.Time
	err      error
}

func newFakeConnectorSyncRecorder() *fakeConnectorSyncRecorder {
	return &fakeConnectorSyncRecorder{recorded: make(map[string]time.Time)}
}

func (f *fakeConnectorSyncRecorder) RecordFullSync(_ context.Context, storeID domain.StoreID, at time.Time) error {
	if f.err != nil {
		return f.err
	}
	f.recorded[string(storeID)] = at
	return nil
}

// fakeSubscription is a stub Subscription for testing.
type fakeSubscription struct{}

func (f *fakeSubscription) Topic() string {
	return "journey.signal"
}

func (f *fakeSubscription) ID() string {
	return "fake-subscription"
}

func (f *fakeSubscription) Close() error {
	return nil
}

func TestSubscriber_Handle_ValidSignal(t *testing.T) {
	_, _, svc := newTestJourneyService()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	connectors := newFakeConnectorSyncRecorder()
	subscriber := journey.NewSubscriber(svc, connectors, &fakeEventBus{}, log)
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	// Initialize journey.
	svc.Initialize(ctx, id, domain.StoreID(id))

	// Send connector_created signal first (prospect → connected)
	event1 := newJourneySignalEvent(string(id), string(journey.SignalConnectorCreated))
	err := subscriber.Handle(ctx, event1)
	require.NoError(t, err)

	// Now send first_sync_complete signal (connected → synced)
	event2 := newJourneySignalEvent(string(id), string(journey.SignalFirstSyncComplete))
	err = subscriber.Handle(ctx, event2)
	require.NoError(t, err)

	// Verify state changed.
	j, _ := svc.Get(ctx, id, domain.StoreID(id))
	assert.Equal(t, domain.JourneySynced, j.State)
	assert.Equal(t, domain.StageRemediation, j.Stage)

	// SignalFirstSyncComplete must also record the connector's full sync.
	_, recorded := connectors.recorded[string(id)]
	assert.True(t, recorded, "expected RecordFullSync to be called for the store")
}

// TestSubscriber_Handle_RecordFullSyncErrorDoesNotFailHandling verifies a
// connector-record failure doesn't prevent journey state from advancing —
// RecordFullSync is a side effect, not a precondition.
func TestSubscriber_Handle_RecordFullSyncErrorDoesNotFailHandling(t *testing.T) {
	_, _, svc := newTestJourneyService()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	connectors := newFakeConnectorSyncRecorder()
	connectors.err = assert.AnError
	subscriber := journey.NewSubscriber(svc, connectors, &fakeEventBus{}, log)
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	svc.Initialize(ctx, id, domain.StoreID(id))
	subscriber.Handle(ctx, newJourneySignalEvent(string(id), string(journey.SignalConnectorCreated)))

	err := subscriber.Handle(ctx, newJourneySignalEvent(string(id), string(journey.SignalFirstSyncComplete)))
	require.NoError(t, err)

	j, _ := svc.Get(ctx, id, domain.StoreID(id))
	assert.Equal(t, domain.JourneySynced, j.State)
}

// TestSubscriber_Handle_NilConnectorRecorder verifies a nil
// ConnectorSyncRecorder (the default) doesn't panic — journey handling
// stays fully functional without it.
func TestSubscriber_Handle_NilConnectorRecorder(t *testing.T) {
	_, _, svc := newTestJourneyService()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	subscriber := journey.NewSubscriber(svc, nil, &fakeEventBus{}, log)
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	svc.Initialize(ctx, id, domain.StoreID(id))
	subscriber.Handle(ctx, newJourneySignalEvent(string(id), string(journey.SignalConnectorCreated)))

	require.NotPanics(t, func() {
		err := subscriber.Handle(ctx, newJourneySignalEvent(string(id), string(journey.SignalFirstSyncComplete)))
		require.NoError(t, err)
	})
}

func TestSubscriber_Handle_InvalidSignal(t *testing.T) {
	_, _, svc := newTestJourneyService()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	subscriber := journey.NewSubscriber(svc, nil, &fakeEventBus{}, log)
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	svc.Initialize(ctx, id, domain.StoreID(id))
	// Move to connected state so we can test invalid signal
	validEvent := newJourneySignalEvent(string(id), string(journey.SignalConnectorCreated))
	subscriber.Handle(ctx, validEvent)

	// Send a signal that doesn't apply to the current state.
	event := newJourneySignalEvent(string(id), string(journey.SignalStoreActivated))
	err := subscriber.Handle(ctx, event)
	require.NoError(t, err)

	// State should not change.
	j, _ := svc.Get(ctx, id, domain.StoreID(id))
	assert.Equal(t, domain.JourneyConnected, j.State)
}

func TestSubscriber_Handle_MalformedPayload(t *testing.T) {
	_, _, svc := newTestJourneyService()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	subscriber := journey.NewSubscriber(svc, nil, &fakeEventBus{}, log)
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	svc.Initialize(ctx, id, domain.StoreID(id))
	// Move to connected state
	validEvent := newJourneySignalEvent(string(id), string(journey.SignalConnectorCreated))
	subscriber.Handle(ctx, validEvent)

	// Create event with malformed payload.
	invalidEvent := &fakeEvent{
		merchantID: string(id),
		storeID:    string(id),
		eventType:  "journey.signal",
		payload:    []byte("not json"),
	}

	// Should not fail, just log.
	err := subscriber.Handle(ctx, invalidEvent)
	require.NoError(t, err)

	// State should not change.
	j, _ := svc.Get(ctx, id, domain.StoreID(id))
	assert.Equal(t, domain.JourneyConnected, j.State)
}

func TestSubscriber_Handle_NonexistentMerchant(t *testing.T) {
	_, _, svc := newTestJourneyService()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	subscriber := journey.NewSubscriber(svc, nil, &fakeEventBus{}, log)
	ctx := context.Background()

	// Send signal for nonexistent merchant.
	event := newJourneySignalEvent("nonexistent", string(journey.SignalFirstSyncComplete))
	err := subscriber.Handle(ctx, event)
	require.NoError(t, err)
}
