// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package channel_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/channel"
)

// channel.NewInMemoryStoreForTest (testutil_test.go, package channel)
// provides an in-memory Store exercising the same pure fold logic
// production code uses (see fold.go's foldSessionEvent) without a real
// MongoDB — mongo_store_integration_test.go (tag integration) covers the
// real transactional/index behavior. It's defined in an internal _test.go
// file so it can be exported to this external channel_test package within
// the same test binary without polluting the package's real public API.

// fakeEvent adapts a raw JSON payload to eventbus.DomainEvent for direct
// handler invocation in tests (bypassing the real bus) — mirrors
// internal/planes/control/ledger/consumer_test.go's identical helper.
type fakeEvent struct {
	eventType  string
	payload    []byte
	occurredAt time.Time
}

func (e fakeEvent) EventType() string     { return e.eventType }
func (e fakeEvent) StoreID() string       { return "" }
func (e fakeEvent) MerchantID() string    { return "" }
func (e fakeEvent) OccurredAt() time.Time { return e.occurredAt }
func (e fakeEvent) Payload() []byte       { return e.payload }

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// recordingBus captures the handler Consumer.Start registers per topic —
// mirrors ledger's identical test double.
type recordingBus struct {
	eventbus.EventBus
	mu       sync.Mutex
	handlers map[string]eventbus.EventHandler
}

func (b *recordingBus) Subscribe(_ context.Context, topic string, handler eventbus.EventHandler) (eventbus.Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
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

func newTestConsumer(t *testing.T, store channel.Store) (*channel.Consumer, *recordingBus) {
	t.Helper()
	bus := &recordingBus{}
	c := channel.NewConsumer(store, bus, nil)
	require.NoError(t, c.Start(context.Background()))
	return c, bus
}

// TestConsumer_FullFunnel_ConfirmedSession is A1's correlation test: a
// generated request stream for one checkout_session_id (initiated ->
// updated -> payment_submitted -> order_intent) folds into a single
// Confirmed session with the expected funnel stages and revenue, and the
// matching DailyAggregate advances one full stage per event.
func TestConsumer_FullFunnel_ConfirmedSession(t *testing.T) {
	store := channel.NewInMemoryStoreForTest()
	_, bus := newTestConsumer(t, store)
	ctx := context.Background()
	day := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)

	initiated := fakeEvent{eventType: ucp.EventCheckoutInitiated, occurredAt: day, payload: mustJSON(t, map[string]any{
		"event_id": "evt-1", "checkout_id": "chk-1", "store_id": "store-1", "merchant_id": "merch-1",
		"surface": "google_gemini", "occurred_at": day,
	})}
	require.NoError(t, bus.handlers[ucp.EventCheckoutInitiated](ctx, initiated))

	updated := fakeEvent{eventType: ucp.EventCheckoutUpdated, occurredAt: day.Add(time.Minute), payload: mustJSON(t, map[string]any{
		"event_id": "evt-2", "checkout_id": "chk-1", "store_id": "store-1", "merchant_id": "merch-1",
		"surface": "google_gemini", "occurred_at": day.Add(time.Minute),
	})}
	require.NoError(t, bus.handlers[ucp.EventCheckoutUpdated](ctx, updated))

	paymentSubmitted := fakeEvent{eventType: ucp.EventCheckoutPaymentSubmitted, occurredAt: day.Add(2 * time.Minute), payload: mustJSON(t, map[string]any{
		"event_id": "evt-3", "checkout_session_id": "chk-1", "store_id": "store-1", "merchant_id": "merch-1",
		"emitted_at": day.Add(2 * time.Minute),
	})}
	require.NoError(t, bus.handlers[ucp.EventCheckoutPaymentSubmitted](ctx, paymentSubmitted))

	orderIntent := fakeEvent{eventType: ucp.EventOrderIntent, occurredAt: day.Add(3 * time.Minute), payload: mustJSON(t, map[string]any{
		"checkout_session_id": "chk-1", "order_id": "ord-1", "store_id": "store-1", "merchant_id": "merch-1",
		"total": map[string]any{"amount_minor": 4999, "currency": "USD"},
	})}
	require.NoError(t, bus.handlers[ucp.EventOrderIntent](ctx, orderIntent))

	sess, err := store.GetSession(ctx, "chk-1")
	require.NoError(t, err)
	assert.Equal(t, channel.AttributionConfirmed, sess.Attribution)
	assert.True(t, sess.ReachedCart)
	assert.True(t, sess.ReachedCheckout)
	assert.True(t, sess.Completed)
	assert.Equal(t, int64(4999), sess.RevenueMinor)
	assert.Equal(t, int64(4), sess.RequestCount)

	agg, err := store.GetDailyAggregate(ctx, "merch-1", "store-1", channel.SurfaceGoogleGemini, "2026_08_06")
	require.NoError(t, err)
	assert.Equal(t, int64(1), agg.Sessions)
	assert.Equal(t, int64(1), agg.CheckoutsInitiated)
	assert.Equal(t, int64(1), agg.SessionsReachedCart)
	assert.Equal(t, int64(1), agg.SessionsReachedCheckout)
	assert.Equal(t, int64(1), agg.CheckoutsCompleted)
	assert.Equal(t, int64(0), agg.SessionsInferred)
	assert.Equal(t, int64(4999), agg.RevenueMinor)
}

// TestConsumer_DuplicateAndOutOfOrderDelivery is A1: redelivering the same
// event_id is a no-op, and delivering events out of order (updated before
// initiated) still produces exactly one session with correct stage flags,
// marked inferred until the initiating event arrives.
func TestConsumer_DuplicateAndOutOfOrderDelivery(t *testing.T) {
	store := channel.NewInMemoryStoreForTest()
	_, bus := newTestConsumer(t, store)
	ctx := context.Background()
	day := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)

	updated := fakeEvent{eventType: ucp.EventCheckoutUpdated, occurredAt: day, payload: mustJSON(t, map[string]any{
		"event_id": "evt-updated", "checkout_id": "chk-ooo", "store_id": "store-1", "merchant_id": "merch-1",
		"surface": "google_ai_mode", "occurred_at": day,
	})}
	// Deliver the "updated" event three times before "initiated" ever
	// arrives (out-of-order + duplicate redelivery in one).
	for range 3 {
		require.NoError(t, bus.handlers[ucp.EventCheckoutUpdated](ctx, updated))
	}

	sess, err := store.GetSession(ctx, "chk-ooo")
	require.NoError(t, err)
	assert.Equal(t, channel.AttributionInferred, sess.Attribution, "session observed before its initiating event must be marked inferred")
	assert.True(t, sess.ReachedCart)
	assert.Equal(t, int64(1), sess.RequestCount, "redelivery of the same event_id must not double-count requests")

	agg, err := store.GetDailyAggregate(ctx, "merch-1", "store-1", channel.SurfaceGoogleAIMode, "2026_08_06")
	require.NoError(t, err)
	assert.Equal(t, int64(1), agg.Sessions, "duplicate delivery must not double-count sessions")
	assert.Equal(t, int64(1), agg.SessionsInferred)

	// The true initiating event arrives late — the session upgrades to
	// confirmed and sessions_inferred corrects back down.
	initiated := fakeEvent{eventType: ucp.EventCheckoutInitiated, occurredAt: day.Add(-time.Second), payload: mustJSON(t, map[string]any{
		"event_id": "evt-initiated", "checkout_id": "chk-ooo", "store_id": "store-1", "merchant_id": "merch-1",
		"surface": "google_ai_mode", "occurred_at": day.Add(-time.Second),
	})}
	require.NoError(t, bus.handlers[ucp.EventCheckoutInitiated](ctx, initiated))

	sess, err = store.GetSession(ctx, "chk-ooo")
	require.NoError(t, err)
	assert.Equal(t, channel.AttributionConfirmed, sess.Attribution)

	agg, err = store.GetDailyAggregate(ctx, "merch-1", "store-1", channel.SurfaceGoogleAIMode, "2026_08_06")
	require.NoError(t, err)
	assert.Equal(t, int64(1), agg.Sessions, "the late initiated event must not create a second session")
	assert.Equal(t, int64(0), agg.SessionsInferred, "confirmation must correct sessions_inferred back down")
}

// TestConsumer_FailedSessionNeverReachesCheckoutOrCompletes is A3: a
// session that fails after reaching cart but before submitting payment
// must count toward sessions_reached_cart but never
// sessions_reached_checkout or checkouts_completed — the funnel's
// non-trivial drop-off gate.
func TestConsumer_FailedSessionNeverReachesCheckoutOrCompletes(t *testing.T) {
	store := channel.NewInMemoryStoreForTest()
	_, bus := newTestConsumer(t, store)
	ctx := context.Background()
	day := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)

	require.NoError(t, bus.handlers[ucp.EventCheckoutInitiated](ctx, fakeEvent{eventType: ucp.EventCheckoutInitiated, occurredAt: day, payload: mustJSON(t, map[string]any{
		"event_id": "e1", "checkout_id": "chk-fail", "store_id": "store-1", "merchant_id": "merch-1", "surface": "other_agent", "occurred_at": day,
	})}))
	require.NoError(t, bus.handlers[ucp.EventCheckoutUpdated](ctx, fakeEvent{eventType: ucp.EventCheckoutUpdated, occurredAt: day.Add(time.Minute), payload: mustJSON(t, map[string]any{
		"event_id": "e2", "checkout_id": "chk-fail", "store_id": "store-1", "merchant_id": "merch-1", "surface": "other_agent", "occurred_at": day.Add(time.Minute),
	})}))
	require.NoError(t, bus.handlers[ucp.EventCheckoutFailed](ctx, fakeEvent{eventType: ucp.EventCheckoutFailed, occurredAt: day.Add(2 * time.Minute), payload: mustJSON(t, map[string]any{
		"event_id": "e3", "checkout_id": "chk-fail", "store_id": "store-1", "merchant_id": "merch-1", "surface": "other_agent", "occurred_at": day.Add(2 * time.Minute),
	})}))

	agg, err := store.GetDailyAggregate(ctx, "merch-1", "store-1", channel.SurfaceOtherAgent, "2026_08_06")
	require.NoError(t, err)
	assert.Equal(t, int64(1), agg.Sessions)
	assert.Equal(t, int64(1), agg.SessionsReachedCart)
	assert.Equal(t, int64(0), agg.SessionsReachedCheckout)
	assert.Equal(t, int64(0), agg.CheckoutsCompleted)
	// A3: monotonically non-increasing through the funnel.
	assert.LessOrEqual(t, agg.SessionsReachedCheckout, agg.SessionsReachedCart)
	assert.LessOrEqual(t, agg.SessionsReachedCart, agg.Sessions)
	assert.LessOrEqual(t, agg.CheckoutsCompleted, agg.SessionsReachedCheckout)
}

// TestConsumer_DiscoveryTelemetry_NeverBecomesASession is R2: discovery
// (catalog search) telemetry increments the request-level
// discovery_requests counter but never creates a Session — no correlation
// identifier exists to sessionize it (see consumer.go's doc comment).
func TestConsumer_DiscoveryTelemetry_NeverBecomesASession(t *testing.T) {
	store := channel.NewInMemoryStoreForTest()
	_, bus := newTestConsumer(t, store)
	ctx := context.Background()
	day := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)

	for i, route := range []string{"/catalog/search", "/catalog/:itemId", "/.well-known/ucp"} {
		require.NoError(t, bus.handlers[ucp.EventTelemetryRecorded](ctx, fakeEvent{eventType: ucp.EventTelemetryRecorded, occurredAt: day, payload: mustJSON(t, map[string]any{
			"event_id": "tel-" + string(rune('a'+i)), "store_id": "store-1", "merchant_id": "merch-1",
			"surface": "google_gemini", "route_template": route, "occurred_at": day,
		})}))
	}
	// A checkout route must not be counted as discovery (already
	// represented via Session/funnel counters).
	require.NoError(t, bus.handlers[ucp.EventTelemetryRecorded](ctx, fakeEvent{eventType: ucp.EventTelemetryRecorded, occurredAt: day, payload: mustJSON(t, map[string]any{
		"event_id": "tel-checkout", "store_id": "store-1", "merchant_id": "merch-1",
		"surface": "google_gemini", "route_template": "/checkout-sessions", "occurred_at": day,
	})}))

	agg, err := store.GetDailyAggregate(ctx, "merch-1", "store-1", channel.SurfaceGoogleGemini, "2026_08_06")
	require.NoError(t, err)
	assert.Equal(t, int64(3), agg.DiscoveryRequests)
	assert.Equal(t, int64(0), agg.Sessions, "discovery telemetry must never fabricate a session")

	_, err = store.GetSession(ctx, "tel-a")
	assert.ErrorIs(t, err, channel.ErrNotFound)
}
