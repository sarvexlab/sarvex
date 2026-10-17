// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
)

// rebuildRequestedEvent is a serving.rebuild.requested as the sync plane
// publishes it on every catalog change.
type rebuildRequestedEvent struct {
	storeID string
}

func (e *rebuildRequestedEvent) EventType() string     { return "serving.rebuild.requested" }
func (e *rebuildRequestedEvent) StoreID() string       { return e.storeID }
func (e *rebuildRequestedEvent) MerchantID() string    { return "merchant-1" }
func (e *rebuildRequestedEvent) OccurredAt() time.Time { return time.Now().UTC() }
func (e *rebuildRequestedEvent) Payload() []byte       { return []byte(`{}`) }

var _ eventbus.DomainEvent = (*rebuildRequestedEvent)(nil)

func newPendingTestService(cache CacheWriter) *servingService {
	return &servingService{
		cacheWriter: cache,
		log:         slog.New(slog.NewTextHandler(discardWriter{}, nil)),
	}
}

// A catalog change asks for a rebuild before one happens. That request is the
// only signal telling the serving plane its snapshot is behind — it cannot
// read the catalog to work this out, since the read path serves only the
// cache. Without the marker, freshness had to be inferred from the newest
// product's age, which reports every quiet store as permanently stale.
func TestHandleRebuildRequested_MarksSnapshotBehind(t *testing.T) {
	cache := &stubRedisCache{}
	svc := newPendingTestService(cache)

	if err := svc.handleRebuildRequested(context.Background(), &rebuildRequestedEvent{storeID: "store-1"}); err != nil {
		t.Fatalf("handleRebuildRequested: %v", err)
	}

	if len(cache.pendingMarked) != 1 || cache.pendingMarked[0] != "store-1" {
		t.Errorf("expected store-1 to be marked behind, got %v", cache.pendingMarked)
	}
}

// An event with no store is not something to mark; it must not reach the
// cache with an empty key.
func TestHandleRebuildRequested_IgnoresEventWithoutStore(t *testing.T) {
	cache := &stubRedisCache{}
	svc := newPendingTestService(cache)

	if err := svc.handleRebuildRequested(context.Background(), &rebuildRequestedEvent{}); err != nil {
		t.Fatalf("handleRebuildRequested: %v", err)
	}
	if len(cache.pendingMarked) != 0 {
		t.Errorf("expected no marking for an event with no store, got %v", cache.pendingMarked)
	}
}

// A merchant's payment-config or profile edit is not a catalog change, so no
// rebuild request follows it; the serving plane must rebuild on the event
// itself, or the edit never reaches /.well-known/ucp.
func TestHandleStoreConfigChanged_RebuildsThatStoresSnapshot(t *testing.T) {
	cache := &stubRedisCache{}
	src := &stubCatalogSource{profile: &StoreProfileData{
		StoreID: "store-1", MerchantID: "merchant-1", Platform: "woocommerce",
		PaymentHandlers: map[string]PaymentHandlerSetting{}, // merchant disabled everything
	}}
	svc := newPendingTestService(cache)
	svc.cacheRebuilder = NewCacheRebuilder(cache, src, testCatalogue(t), "https://serving.test", &stubEventBus{}, svc.log)

	if err := svc.handleStoreConfigChanged(context.Background(), &rebuildRequestedEvent{storeID: "store-1"}); err != nil {
		t.Fatalf("handleStoreConfigChanged: %v", err)
	}
	if len(cache.swappedSnapshots) != 1 {
		t.Fatalf("want one rebuilt snapshot, got %d", len(cache.swappedSnapshots))
	}
	if got := cache.swappedSnapshots[0].Profile.UCP.PaymentHandlers; len(got) != 0 {
		t.Errorf("rebuilt profile still advertises %v after the merchant disabled every handler", got)
	}

	// No store, nothing to rebuild.
	if err := svc.handleStoreConfigChanged(context.Background(), &rebuildRequestedEvent{}); err != nil || len(cache.swappedSnapshots) != 1 {
		t.Errorf("an event without a store must be ignored (err=%v snapshots=%d)", err, len(cache.swappedSnapshots))
	}
}

// Idempotency keys are chosen by callers, so two stores routinely reuse one.
// The memory store must scope them per store: a key already used by store A
// must neither conflict with nor hand back store A's session to store B.
func TestMemorySessionStore_IdempotencyKeysAreStoreScoped(t *testing.T) {
	st := NewMemorySessionStore(time.Minute)
	defer st.Close()
	ctx := context.Background()

	now := time.Now().UTC()
	a := &CheckoutSession{SessionID: "sess-a", StoreID: "store-a", MerchantID: "m-a", IdempotencyKey: "k1", BodyHash: "hash-a", CreatedAt: now, UpdatedAt: now}
	b := &CheckoutSession{SessionID: "sess-b", StoreID: "store-b", MerchantID: "m-b", IdempotencyKey: "k1", BodyHash: "hash-b", CreatedAt: now, UpdatedAt: now}
	if err := st.Create(ctx, a); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := st.Create(ctx, b); err != nil {
		t.Fatalf("store b reusing store a's key must not conflict: %v", err)
	}
	if b.SessionID != "sess-b" || b.StoreID != "store-b" {
		t.Fatalf("store b was handed another store's session: %+v", b)
	}
	got, err := st.GetByIdempotencyKey(ctx, "store-b", "k1")
	if err != nil || got.SessionID != "sess-b" {
		t.Fatalf("GetByIdempotencyKey(store-b) = %+v, %v; want sess-b", got, err)
	}
}
