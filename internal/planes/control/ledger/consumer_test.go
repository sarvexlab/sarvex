// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package ledger_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/planes/control/ledger"
)

// fakeStore is an in-memory ledger.Store standing in for Mongo in these
// unit tests — it exists to test Consumer's own logic (does it classify,
// price, and hand off exactly what Store's contract expects), not to
// re-verify Store's real transactional/index behavior, which is covered by
// mongo_store_integration_test.go (tag integration) against real MongoDB.
type fakeStore struct {
	mu       sync.Mutex
	byEvent  map[string]ledger.Entry // event_id -> entry, the same dedup key the real unique index enforces
	rollups  map[string]*ledger.Rollup
	inserted int // count of genuinely new inserts, for asserting "exactly once"
}

func newFakeStore() *fakeStore {
	return &fakeStore{byEvent: map[string]ledger.Entry{}, rollups: map[string]*ledger.Rollup{}}
}

func (f *fakeStore) InsertEntryAndRollup(_ context.Context, e ledger.Entry) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.byEvent[e.EventID]; exists {
		return false, nil
	}
	f.byEvent[e.EventID] = e
	f.inserted++

	month := e.OccurredAt.UTC().Format("2006_01")
	key := e.MerchantID + ":" + month + "__" + e.Currency
	r, ok := f.rollups[key]
	if !ok {
		r = &ledger.Rollup{RollupID: key, MerchantID: e.MerchantID, Month: month, Currency: e.Currency}
		f.rollups[key] = r
	}
	r.TotalCount++
	r.TotalLostRevenueMinor += e.LostRevenueMinor
	if e.RevenueUnknown {
		r.EntriesWithUnknownRevenue++
	}
	found := false
	for i := range r.Causes {
		if r.Causes[i].Cause == e.Cause {
			r.Causes[i].Count++
			r.Causes[i].LostRevenueMinor += e.LostRevenueMinor
			found = true
			break
		}
	}
	if !found {
		r.Causes = append(r.Causes, ledger.CauseBreakdown{Cause: e.Cause, Count: 1, LostRevenueMinor: e.LostRevenueMinor})
	}
	return true, nil
}

func (f *fakeStore) GetEntry(_ context.Context, merchantID, entryID string) (*ledger.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.byEvent {
		if e.EntryID == entryID && e.MerchantID == merchantID {
			cp := e
			return &cp, nil
		}
	}
	return nil, ledger.ErrNotFound
}

func (f *fakeStore) ListEntries(context.Context, string, ledger.ListFilter, string, string, int) ([]ledger.Entry, string, bool, error) {
	return nil, "", false, nil
}

func (f *fakeStore) UpdateEntryStatus(context.Context, string, string, ledger.Status, time.Time) (*ledger.Entry, error) {
	return nil, ledger.ErrNotFound
}

func (f *fakeStore) GetRollup(_ context.Context, merchantID, month, currency string) (*ledger.Rollup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rollups[merchantID+":"+month+"__"+currency]
	if !ok {
		return nil, ledger.ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (f *fakeStore) ListRollups(_ context.Context, merchantID, month string) ([]ledger.Rollup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []ledger.Rollup
	for _, r := range f.rollups {
		if r.MerchantID == merchantID && r.Month == month {
			out = append(out, *r)
		}
	}
	return out, nil
}

// fakeEvent adapts a raw JSON payload to eventbus.DomainEvent for direct
// handler invocation in tests (bypassing the real bus).
type fakeEvent struct {
	eventType string
	payload   []byte
}

func (e fakeEvent) EventType() string     { return e.eventType }
func (e fakeEvent) StoreID() string       { return "" }
func (e fakeEvent) MerchantID() string    { return "" }
func (e fakeEvent) OccurredAt() time.Time { return time.Time{} }
func (e fakeEvent) Payload() []byte       { return e.payload }

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// recordingBus is a minimal eventbus.EventBus double that captures the
// handler Consumer.Start registers per topic, so tests can invoke it
// directly (through the same eventbus.EventHandler signature production
// code uses) without a real event-bus backend.
type recordingBus struct {
	eventbus.EventBus
	handlers map[string]eventbus.EventHandler
}

func (b *recordingBus) Subscribe(_ context.Context, topic string, handler eventbus.EventHandler) (eventbus.Subscription, error) {
	if b.handlers == nil {
		b.handlers = map[string]eventbus.EventHandler{}
	}
	b.handlers[topic] = handler
	return fakeSub{topic: topic}, nil
}

func (b *recordingBus) Unsubscribe(context.Context, eventbus.Subscription) error { return nil }

type fakeSub struct{ topic string }

func (s fakeSub) Topic() string { return s.topic }
func (s fakeSub) ID() string    { return s.topic }
func (s fakeSub) Close() error  { return nil }

// TestConsumer_Idempotency is A1: the same event_id delivered three times
// produces exactly one entry, with the rollup incremented once.
func TestConsumer_Idempotency(t *testing.T) {
	store := newFakeStore()
	bus := &recordingBus{}
	c := ledger.NewConsumer(store, bus, nil)
	require.NoError(t, c.Start(context.Background()))
	handler := bus.handlers[ucp.EventCheckoutFailed]
	require.NotNil(t, handler)

	occurredAt := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	payload := map[string]any{
		"event_id": "evt-dup-1", "checkout_id": "chk-1", "store_id": "store-1", "merchant_id": "merch-1",
		"code": "CONNECTOR_CREATE_FAILED", "stage": "create",
		"cart_snapshot": map[string]any{"currency": "USD", "total_minor": 5000},
		"occurred_at":   occurredAt,
	}
	ev := fakeEvent{eventType: ucp.EventCheckoutFailed, payload: mustJSON(t, payload)}

	for range 3 {
		require.NoError(t, handler(context.Background(), ev))
	}

	assert.Equal(t, 1, store.inserted, "duplicate event_id delivery must produce exactly one entry")

	rollup, err := store.GetRollup(context.Background(), "merch-1", "2026_08", "USD")
	require.NoError(t, err)
	assert.Equal(t, int64(1), rollup.TotalCount, "rollup must be incremented exactly once, not once per delivery")
	assert.Equal(t, int64(5000), rollup.TotalLostRevenueMinor)
}

// TestConsumer_UnclassifiedStillWritesEntry is R2/A2: an event this
// package's classification table has never seen still produces an entry
// (cause=unclassified), never a silent drop.
func TestConsumer_UnclassifiedStillWritesEntry(t *testing.T) {
	store := newFakeStore()
	bus := &recordingBus{}
	c := ledger.NewConsumer(store, bus, nil)
	require.NoError(t, c.Start(context.Background()))
	handler := bus.handlers[ucp.EventCheckoutFailed]

	payload := map[string]any{
		"event_id": "evt-unknown-1", "checkout_id": "chk-2", "store_id": "store-1", "merchant_id": "merch-1",
		"code": "SOME_BRAND_NEW_CODE", "stage": "create",
		"occurred_at": time.Now().UTC(),
	}
	ev := fakeEvent{eventType: ucp.EventCheckoutFailed, payload: mustJSON(t, payload)}
	require.NoError(t, handler(context.Background(), ev))

	assert.Equal(t, 1, store.inserted)
	var entry ledger.Entry
	for _, e := range store.byEvent {
		entry = e
	}
	assert.Equal(t, ledger.CauseUnclassified, entry.Cause)
}

// TestConsumer_RevenueUnknownWhenCartAbsent is A4: an absent cart snapshot
// must produce lost_revenue_minor=0 and revenue_unknown=true — never an
// estimate.
func TestConsumer_RevenueUnknownWhenCartAbsent(t *testing.T) {
	store := newFakeStore()
	bus := &recordingBus{}
	c := ledger.NewConsumer(store, bus, nil)
	require.NoError(t, c.Start(context.Background()))
	handler := bus.handlers[ucp.EventCheckoutFailed]

	payload := map[string]any{
		"event_id": "evt-no-cart", "checkout_id": "chk-3", "store_id": "store-1", "merchant_id": "merch-1",
		"code": "CONNECTOR_CREATE_FAILED", "stage": "create",
		"occurred_at": time.Now().UTC(),
		// no "cart" field at all
	}
	ev := fakeEvent{eventType: ucp.EventCheckoutFailed, payload: mustJSON(t, payload)}
	require.NoError(t, handler(context.Background(), ev))

	var entry ledger.Entry
	for _, e := range store.byEvent {
		entry = e
	}
	assert.True(t, entry.RevenueUnknown)
	assert.Equal(t, int64(0), entry.LostRevenueMinor)
}

// TestConsumer_MultiCurrencyNeverSummed is A4/R9: entries in different
// currencies land in separate rollups, never combined into one total.
func TestConsumer_MultiCurrencyNeverSummed(t *testing.T) {
	store := newFakeStore()
	bus := &recordingBus{}
	c := ledger.NewConsumer(store, bus, nil)
	require.NoError(t, c.Start(context.Background()))
	handler := bus.handlers[ucp.EventCheckoutFailed]

	occurredAt := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	usd := map[string]any{
		"event_id": "evt-usd", "checkout_id": "chk-usd", "store_id": "store-1", "merchant_id": "merch-1",
		"code": "ORDER_CREATION_FAILED", "stage": "complete",
		"cart_snapshot": map[string]any{"currency": "USD", "total_minor": 10000},
		"occurred_at":   occurredAt,
	}
	aud := map[string]any{
		"event_id": "evt-aud", "checkout_id": "chk-aud", "store_id": "store-1", "merchant_id": "merch-1",
		"code": "ORDER_CREATION_FAILED", "stage": "complete",
		"cart_snapshot": map[string]any{"currency": "AUD", "total_minor": 20000},
		"occurred_at":   occurredAt,
	}
	require.NoError(t, handler(context.Background(), fakeEvent{eventType: ucp.EventCheckoutFailed, payload: mustJSON(t, usd)}))
	require.NoError(t, handler(context.Background(), fakeEvent{eventType: ucp.EventCheckoutFailed, payload: mustJSON(t, aud)}))

	usdRollup, err := store.GetRollup(context.Background(), "merch-1", "2026_08", "USD")
	require.NoError(t, err)
	audRollup, err := store.GetRollup(context.Background(), "merch-1", "2026_08", "AUD")
	require.NoError(t, err)

	assert.Equal(t, int64(10000), usdRollup.TotalLostRevenueMinor)
	assert.Equal(t, int64(20000), audRollup.TotalLostRevenueMinor)
}

// TestClassifyAbandoned_ThroughConsumer is A3, exercised end-to-end through
// the consumer rather than the pure Classify function.
func TestClassifyAbandoned_ThroughConsumer(t *testing.T) {
	store := newFakeStore()
	bus := &recordingBus{}
	c := ledger.NewConsumer(store, bus, nil)
	require.NoError(t, c.Start(context.Background()))
	handler := bus.handlers[ucp.EventCheckoutAbandoned]

	payload := map[string]any{
		"event_id": "evt-abandoned-1", "checkout_id": "chk-4", "store_id": "store-1", "merchant_id": "merch-1",
		"stage":         "incomplete",
		"cart_snapshot": map[string]any{"currency": "USD", "total_minor": 3000},
		"occurred_at":   time.Now().UTC(),
	}
	ev := fakeEvent{eventType: ucp.EventCheckoutAbandoned, payload: mustJSON(t, payload)}
	require.NoError(t, handler(context.Background(), ev))

	var entry ledger.Entry
	for _, e := range store.byEvent {
		entry = e
	}
	assert.Equal(t, ledger.CauseBuyerAbandoned, entry.Cause)
	assert.Equal(t, ledger.FailureCodeAbandoned, entry.FailureCode)
}

// TestConsumer_WritesRegardlessOfTier is R15/A7: the worker writes for
// every merchant tier — entitlement is a read-API concern only
// (requireAnalyticsTier, internal/planes/control/api/middleware.go), never
// the pipeline's. This is structural, not merely behavioral: NewConsumer's
// signature (store, events, log) has no billing/tier dependency to gate
// on at all, so there is nothing here a merchant's plan could switch off.
// This test constructs and runs a Consumer exactly as production wires it
// (see entry.go) and confirms it writes successfully with no tier
// information supplied anywhere in the call chain.
func TestConsumer_WritesRegardlessOfTier(t *testing.T) {
	store := newFakeStore()
	bus := &recordingBus{}
	c := ledger.NewConsumer(store, bus, nil) // no billing.Service, no domain.Plan, anywhere in this construction
	require.NoError(t, c.Start(context.Background()))
	handler := bus.handlers[ucp.EventCheckoutFailed]

	payload := map[string]any{
		"event_id": "evt-any-tier", "checkout_id": "chk-5", "store_id": "store-1", "merchant_id": "merch-on-self-serve",
		"code": "CONNECTOR_CREATE_FAILED", "stage": "create",
		"cart_snapshot": map[string]any{"currency": "USD", "total_minor": 1500},
		"occurred_at":   time.Now().UTC(),
	}
	require.NoError(t, handler(context.Background(), fakeEvent{eventType: ucp.EventCheckoutFailed, payload: mustJSON(t, payload)}))

	assert.Equal(t, 1, store.inserted, "the worker must write for a merchant regardless of plan tier")
}
