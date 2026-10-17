// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package insights_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/insights"
)

// fakeEvent adapts a raw JSON payload to eventbus.DomainEvent for direct
// handler invocation in tests (bypassing the real bus) — mirrors
// ledger/channel's identical consumer_test.go helper. Unlike channel's
// version, StoreID/MerchantID are configurable: the insights Consumer
// reads negotiation events' store_id/merchant_id off these interface
// methods rather than out of the JSON payload (neither negotiation event
// carries merchant_id on the wire — see consumer.go's doc comment).
type fakeEvent struct {
	eventType  string
	storeID    string
	merchantID string
	payload    []byte
	occurredAt time.Time
}

func (e fakeEvent) EventType() string     { return e.eventType }
func (e fakeEvent) StoreID() string       { return e.storeID }
func (e fakeEvent) MerchantID() string    { return e.merchantID }
func (e fakeEvent) OccurredAt() time.Time { return e.occurredAt }
func (e fakeEvent) Payload() []byte       { return e.payload }

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// recordingBus captures the handler Consumer.Start registers per topic.
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

func newTestConsumer(t *testing.T, store insights.Store) (*insights.Consumer, *recordingBus) {
	t.Helper()
	bus := &recordingBus{}
	c := insights.NewConsumer(store, bus, nil)
	require.NoError(t, c.Start(context.Background()))
	return c, bus
}

// TestConsumer_CatalogQuery_IdempotentUnderRedelivery is A2: a redelivered
// serving.catalog.query_recorded event (same event_id) must not
// double-count request_count.
func TestConsumer_CatalogQuery_IdempotentUnderRedelivery(t *testing.T) {
	store := insights.NewInMemoryStoreForTest()
	_, bus := newTestConsumer(t, store)
	ctx := context.Background()
	day := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)

	ev := fakeEvent{eventType: ucp.EventCatalogQueryRecorded, storeID: "store-1", merchantID: "merch-1", occurredAt: day, payload: mustJSON(t, map[string]any{
		"event_id": "evt-1", "store_id": "store-1", "merchant_id": "merch-1",
		"agent_platform": "gemini.google.com", "requested_product_ids": []string{"sku-1", "sku-2"},
		"occurred_at": day,
	})}

	handler := bus.handlers[ucp.EventCatalogQueryRecorded]
	require.NoError(t, handler(ctx, ev))
	require.NoError(t, handler(ctx, ev)) // redelivery

	rows, err := store.ListProductDemand(ctx, "merch-1", "", "2026_08_01", "2026_08_31")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, r := range rows {
		assert.Equal(t, int64(1), r.RequestCount, "product %s should be counted once despite redelivery", r.ProductID)
	}
}

// TestConsumer_CatalogQuery_FreeTextProductIDDropped is A6/R12 end-to-end:
// if a free-text-looking value ever ended up in requested_product_ids
// (e.g. a future handler bug), the consumer's sanitizeIdentifiers pass
// must drop it before it reaches the Store — a genuine id in the same
// event still persists normally.
func TestConsumer_CatalogQuery_FreeTextProductIDDropped(t *testing.T) {
	store := insights.NewInMemoryStoreForTest()
	_, bus := newTestConsumer(t, store)
	ctx := context.Background()
	day := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)

	ev := fakeEvent{eventType: ucp.EventCatalogQueryRecorded, storeID: "store-1", merchantID: "merch-1", occurredAt: day, payload: mustJSON(t, map[string]any{
		"event_id": "evt-1", "store_id": "store-1", "merchant_id": "merch-1",
		"agent_platform":        "gemini.google.com",
		"requested_product_ids": []string{"sku-1", "red running shoes size 10 under $50"},
		"occurred_at":           day,
	})}
	require.NoError(t, bus.handlers[ucp.EventCatalogQueryRecorded](ctx, ev))

	rows, err := store.ListProductDemand(ctx, "merch-1", "", "2026_08_01", "2026_08_31")
	require.NoError(t, err)
	require.Len(t, rows, 1, "the free-text-looking value must never persist as a product_id")
	assert.Equal(t, "sku-1", rows[0].ProductID)
}

// TestConsumer_CatalogQuery_MultipleProductsInOneEvent verifies a single
// search event listing several matched product ids increments each one.
func TestConsumer_CatalogQuery_MultipleProductsInOneEvent(t *testing.T) {
	store := insights.NewInMemoryStoreForTest()
	_, bus := newTestConsumer(t, store)
	ctx := context.Background()
	day := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)

	ev := fakeEvent{eventType: ucp.EventCatalogQueryRecorded, storeID: "store-1", merchantID: "merch-1", occurredAt: day, payload: mustJSON(t, map[string]any{
		"event_id": "evt-1", "store_id": "store-1", "merchant_id": "merch-1",
		"agent_platform": "gemini.google.com", "requested_product_ids": []string{"sku-1", "sku-2", "sku-3"},
		"occurred_at": day,
	})}
	require.NoError(t, bus.handlers[ucp.EventCatalogQueryRecorded](ctx, ev))

	rows, err := store.ListProductDemand(ctx, "merch-1", "", "2026_08_01", "2026_08_31")
	require.NoError(t, err)
	assert.Len(t, rows, 3)
}

// TestConsumer_NegotiationFailed_CapabilitiesIncompatible_RecordsGap is
// R8's write path: a capabilities_incompatible failure with a requested
// capability the merchant doesn't declare produces a control_capability_gaps_monthly
// row, and its control_negotiation_daily failure breakdown records the same delta.
func TestConsumer_NegotiationFailed_CapabilitiesIncompatible_RecordsGap(t *testing.T) {
	store := insights.NewInMemoryStoreForTest()
	_, bus := newTestConsumer(t, store)
	ctx := context.Background()
	day := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)

	ev := fakeEvent{eventType: domain.EventTypeUCPNegotiationFailed, storeID: "store-1", merchantID: "merch-1", occurredAt: day, payload: mustJSON(t, map[string]any{
		"store_id": "store-1", "platform_profile_uri": "https://gemini.google.com/.well-known/ucp", "reason": "capabilities_incompatible",
		"requested_capabilities": []string{"dev.ucp.shopping.checkout", "dev.ucp.shopping.loyalty"},
		"declared_capabilities":  []string{"dev.ucp.shopping.checkout"},
	})}
	require.NoError(t, bus.handlers[domain.EventTypeUCPNegotiationFailed](ctx, ev))

	gaps, err := store.ListCapabilityGaps(ctx, "merch-1", "", "2026_08")
	require.NoError(t, err)
	require.Len(t, gaps, 1)
	assert.Equal(t, "dev.ucp.shopping.loyalty", gaps[0].CapabilityID)
	assert.Equal(t, int64(1), gaps[0].RequestCount)
	assert.Equal(t, insights.ImpactUnmeasurable, gaps[0].ImpactConfidence)
	assert.Equal(t, int64(0), gaps[0].EstimatedImpactMinor)
	assert.False(t, gaps[0].MerchantSupported)
	assert.False(t, gaps[0].RemediationAvailable)

	daily, err := store.ListNegotiationDaily(ctx, "merch-1", "", "2026_08_06", "2026_08_06")
	require.NoError(t, err)
	require.Len(t, daily, 1)
	assert.Equal(t, int64(1), daily[0].Attempts)
	assert.Equal(t, int64(0), daily[0].Successes)
	require.Len(t, daily[0].Failures, 1)
	assert.Equal(t, insights.NegotiationFailureCapabilitiesIncompatible, daily[0].Failures[0].Reason)
	assert.Equal(t, int64(1), daily[0].Failures[0].MissingCapabilityCounts["dev.ucp.shopping.loyalty"])
}

// TestConsumer_Negotiation_IdempotentUnderRedelivery is A2 for negotiation
// events: since neither event carries an event_id on the wire, dedup uses
// a composite key of store_id/platform_profile_uri/occurred_at (see
// consumer.go's negotiationDedupKey) — the exact same event object
// redelivered (identical occurred_at) must not double-count.
func TestConsumer_Negotiation_IdempotentUnderRedelivery(t *testing.T) {
	store := insights.NewInMemoryStoreForTest()
	_, bus := newTestConsumer(t, store)
	ctx := context.Background()
	day := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)

	ev := fakeEvent{eventType: domain.EventTypeUCPNegotiationFailed, storeID: "store-1", merchantID: "merch-1", occurredAt: day, payload: mustJSON(t, map[string]any{
		"store_id": "store-1", "platform_profile_uri": "https://gemini.google.com/.well-known/ucp", "reason": "capabilities_incompatible",
		"requested_capabilities": []string{"dev.ucp.shopping.loyalty"}, "declared_capabilities": []string{},
	})}
	handler := bus.handlers[domain.EventTypeUCPNegotiationFailed]
	require.NoError(t, handler(ctx, ev))
	require.NoError(t, handler(ctx, ev)) // redelivery

	gaps, err := store.ListCapabilityGaps(ctx, "merch-1", "", "2026_08")
	require.NoError(t, err)
	require.Len(t, gaps, 1)
	assert.Equal(t, int64(1), gaps[0].RequestCount, "redelivered negotiation event must not double-count")
}

// TestConsumer_Negotiation_NoCommonPaymentHandler_RecordsMismatch is R6:
// an empty payment-handler intersection is recorded observationally even
// on a SUCCEEDED negotiation (per negotiation/events.go's CapabilitySets
// doc comment — an empty intersection doesn't fail negotiation today).
func TestConsumer_Negotiation_NoCommonPaymentHandler_RecordsMismatch(t *testing.T) {
	store := insights.NewInMemoryStoreForTest()
	_, bus := newTestConsumer(t, store)
	ctx := context.Background()
	day := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)

	ev := fakeEvent{eventType: domain.EventTypeUCPNegotiationSucceeded, storeID: "store-1", merchantID: "merch-1", occurredAt: day, payload: mustJSON(t, map[string]any{
		"instance_id": "store-1", "platform_profile_uri": "https://gemini.google.com/.well-known/ucp",
		"requested_capabilities": []string{"dev.ucp.shopping.checkout"}, "declared_capabilities": []string{"dev.ucp.shopping.checkout"},
		"active_capabilities":        []string{"dev.ucp.shopping.checkout"},
		"requested_payment_handlers": []string{"dev.ucp.payments.paypal"},
		"declared_payment_handlers":  []string{"dev.ucp.payments.card"},
		"active_payment_handlers":    []string{},
	})}
	require.NoError(t, bus.handlers[domain.EventTypeUCPNegotiationSucceeded](ctx, ev))

	mismatch, err := store.ListPaymentHandlerMismatchDaily(ctx, "merch-1", "", "2026_08_06", "2026_08_06")
	require.NoError(t, err)
	require.Len(t, mismatch, 1)
	assert.Equal(t, int64(1), mismatch[0].Occurrences)
	assert.Equal(t, int64(1), mismatch[0].OfferedHandlerCounts["dev.ucp.payments.paypal"])
	assert.Equal(t, []string{"dev.ucp.payments.card"}, mismatch[0].MerchantDeclaredHandlers)

	// A succeeded negotiation with no capability delta must not produce a
	// capability gap — only capabilities_incompatible failures do (R8).
	gaps, err := store.ListCapabilityGaps(ctx, "merch-1", "", "2026_08")
	require.NoError(t, err)
	assert.Empty(t, gaps)
}

// TestConsumer_Negotiation_VersionUnsupported_NoMissingCapabilityCounts is
// the model.go doc comment's contract: version_unsupported never
// populates MissingCapabilityCounts, since the version field (not a
// capability delta) caused that failure.
func TestConsumer_Negotiation_VersionUnsupported_NoMissingCapabilityCounts(t *testing.T) {
	store := insights.NewInMemoryStoreForTest()
	_, bus := newTestConsumer(t, store)
	ctx := context.Background()
	day := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)

	ev := fakeEvent{eventType: domain.EventTypeUCPNegotiationFailed, storeID: "store-1", merchantID: "merch-1", occurredAt: day, payload: mustJSON(t, map[string]any{
		"store_id": "store-1", "platform_profile_uri": "https://gemini.google.com/.well-known/ucp", "reason": "version_unsupported",
		"requested_capabilities": []string{"dev.ucp.shopping.checkout"}, "declared_capabilities": []string{},
	})}
	require.NoError(t, bus.handlers[domain.EventTypeUCPNegotiationFailed](ctx, ev))

	daily, err := store.ListNegotiationDaily(ctx, "merch-1", "", "2026_08_06", "2026_08_06")
	require.NoError(t, err)
	require.Len(t, daily, 1)
	require.Len(t, daily[0].Failures, 1)
	assert.Equal(t, insights.NegotiationFailureVersionUnsupported, daily[0].Failures[0].Reason)
	assert.Empty(t, daily[0].Failures[0].MissingCapabilityCounts)

	// version_unsupported never writes a capability gap either (R8 only
	// keys off capabilities_incompatible).
	gaps, err := store.ListCapabilityGaps(ctx, "merch-1", "", "2026_08")
	require.NoError(t, err)
	assert.Empty(t, gaps)
}
