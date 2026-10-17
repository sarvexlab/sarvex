// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package operator_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/serving"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/operator"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/store"
	specall "github.com/sarvexlabs/sarvex.cp/internal/spec/all"
)

// fakeServingCacheReader is a test double for operator.ServingCacheReader.
type fakeServingCacheReader struct {
	profile      *serving.CacheProfile
	profileErr   error
	watermark    *serving.CacheWatermark
	watermarkErr error
}

func (f *fakeServingCacheReader) GetProfile(ctx context.Context, storeID string) (*serving.CacheProfile, error) {
	if f.profileErr != nil {
		return nil, f.profileErr
	}
	return f.profile, nil
}

func (f *fakeServingCacheReader) Watermark(ctx context.Context, storeID string) (*serving.CacheWatermark, error) {
	if f.watermarkErr != nil {
		return nil, f.watermarkErr
	}
	return f.watermark, nil
}

// fakePresenceRepo is a test double for operator.PresenceRepository.
type fakePresenceRepo struct {
	rec    *operator.PresenceRecord
	getErr error
}

func (f *fakePresenceRepo) Upsert(ctx context.Context, rec *operator.PresenceRecord) error {
	f.rec = rec
	return nil
}

func (f *fakePresenceRepo) Get(ctx context.Context, merchantID, storeID string) (*operator.PresenceRecord, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.rec == nil {
		return nil, operator.ErrPresenceNotFound
	}
	// Tenant ownership check: return not found if merchant doesn't match
	if f.rec.MerchantID != merchantID {
		return nil, operator.ErrPresenceNotFound
	}
	return f.rec, nil
}

// fakeOrderReader is a test double for operator.OrderReader.
type fakeOrderReader struct {
	list     domain.ListOf[domain.Order]
	listErr  error
	order    domain.Order
	orderErr error
}

func (f *fakeOrderReader) List(ctx context.Context, storeID domain.StoreID, cursor string, limit int, status *domain.OrderStatus) (domain.ListOf[domain.Order], error) {
	if f.listErr != nil {
		return domain.ListOf[domain.Order]{}, f.listErr
	}
	return f.list, nil
}

func (f *fakeOrderReader) Get(ctx context.Context, storeID domain.StoreID, orderID string) (domain.Order, error) {
	if f.orderErr != nil {
		return domain.Order{}, f.orderErr
	}
	return f.order, nil
}

// fakeOperatorEventBus is a test double for eventbus.EventBus, recording
// published events (Subscribe/Unsubscribe unused by OperatorService).
type fakeOperatorEventBus struct {
	published  []eventbus.DomainEvent
	publishErr error
}

func (f *fakeOperatorEventBus) Publish(ctx context.Context, event eventbus.DomainEvent) error {
	if f.publishErr != nil {
		return f.publishErr
	}
	f.published = append(f.published, event)
	return nil
}

func (f *fakeOperatorEventBus) Subscribe(ctx context.Context, topic string, handler eventbus.EventHandler) (eventbus.Subscription, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeOperatorEventBus) Unsubscribe(ctx context.Context, sub eventbus.Subscription) error {
	return nil
}

func (f *fakeOperatorEventBus) Close() error { return nil }

// newTestOperatorServiceWithPresence builds an operator.Service with real
// presence/manifest/order dependencies (fakes) and stub board/task/journey
// dependencies, for tests exercising GetPresence/GetManifest/RebuildCache/
// ListOrders/GetOrder specifically. stores is nil, so GetManifest's
// ownership check is skipped — use
// newTestOperatorServiceWithPresenceAndStores for tests exercising that
// check.
func newTestOperatorServiceWithPresence(servingCache operator.ServingCacheReader, presenceRepo operator.PresenceRepository, orderReader operator.OrderReader, bus eventbus.EventBus) operator.Service {
	return newTestOperatorServiceWithPresenceAndStores(servingCache, presenceRepo, orderReader, nil, bus)
}

// fakeStoreOwnershipChecker is a test double for operator.StoreOwnershipChecker.
type fakeStoreOwnershipChecker struct {
	// owned maps storeID -> the merchantID that owns it.
	owned map[domain.StoreID]domain.MerchantID
}

func (f *fakeStoreOwnershipChecker) Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (*store.StoreModel, error) {
	owner, ok := f.owned[storeID]
	if !ok || owner != merchantID {
		return nil, store.ErrNotFound
	}
	return &store.StoreModel{ID: string(storeID), MerchantID: string(merchantID)}, nil
}

// newTestOperatorServiceWithPresenceAndStores is
// newTestOperatorServiceWithPresence plus a StoreOwnershipChecker, for tests
// exercising GetManifest's cross-tenant ownership check.
func newTestOperatorServiceWithPresenceAndStores(servingCache operator.ServingCacheReader, presenceRepo operator.PresenceRepository, orderReader operator.OrderReader, stores operator.StoreOwnershipChecker, bus eventbus.EventBus) operator.Service {
	steps := &fakeManualStepRepo{docs: make(map[string]*operator.ManualStepModel)}
	audit := &fakeAuditLogRepo{}
	board := &fakeBoardRepo{}
	tasks := &fakeTaskService{}
	journey := &fakeJourneyService{journey: domain.Journey{State: domain.JourneySynced, Stage: domain.StageRemediation}}
	return operator.NewOperatorService(steps, audit, board, tasks, journey, servingCache, presenceRepo, orderReader, nil, nil, stores, bus, nil)
}

func TestOperatorService_GetPresence_OK(t *testing.T) {
	presenceRepo := &fakePresenceRepo{rec: &operator.PresenceRecord{
		StoreID: "store-1", MerchantID: "merch-1", ProductCount: 42,
		BuiltAt: time.Now().UTC(), SourceSync: time.Now().UTC(),
	}}
	svc := newTestOperatorServiceWithPresence(&fakeServingCacheReader{
		profile: &serving.CacheProfile{
			StorefrontURL: "https://baysidetrail.com/",
			UCP:           serving.UCPBlockCache{Version: "2026-04-08"},
		},
		watermark: &serving.CacheWatermark{BuiltAt: time.Now().UTC(), SourceSync: time.Now().UTC(), IsStale: false},
	}, presenceRepo, &fakeOrderReader{}, &fakeOperatorEventBus{})

	presence, err := svc.GetPresence(context.Background(), "merch-1", "store-1")
	require.NoError(t, err)
	assert.Equal(t, 42, presence.ProductCount)
	assert.Equal(t, domain.ServingStateActive, presence.Serving)
	assert.True(t, presence.ProfileReachable)
	assert.Equal(t, "https://baysidetrail.com/.well-known/ucp", presence.ProfileURL)
	// published_version reflects the single version baked into the cached
	// /.well-known/ucp document; supported_versions is the broader,
	// store-independent set negotiation can actually serve (specall.Default().Servable())
	// — see sarvex-v1-architecture.md §D R3.
	assert.Equal(t, "2026-04-08", presence.PublishedVersion)
	var servable []string
	for _, v := range specall.Default().Servable() {
		servable = append(servable, string(v))
	}
	assert.Equal(t, servable, presence.SupportedVersions)
	assert.Contains(t, presence.SupportedVersions, "2026-04-08")
}

func TestOperatorService_GetPresence_NeverSynced(t *testing.T) {
	svc := newTestOperatorServiceWithPresence(&fakeServingCacheReader{}, &fakePresenceRepo{}, &fakeOrderReader{}, &fakeOperatorEventBus{})

	presence, err := svc.GetPresence(context.Background(), "merch-1", "store-new")
	require.NoError(t, err)
	assert.Equal(t, domain.ServingStateNotServing, presence.Serving)
	assert.Equal(t, 0, presence.ProductCount)
}

func TestOperatorService_GetPresence_StaleCacheIsDegraded(t *testing.T) {
	presenceRepo := &fakePresenceRepo{rec: &operator.PresenceRecord{StoreID: "store-1", MerchantID: "merch-1", ProductCount: 5}}
	servingCache := &fakeServingCacheReader{
		profile:   &serving.CacheProfile{StorefrontURL: "https://example.com"},
		watermark: &serving.CacheWatermark{IsStale: true},
	}
	svc := newTestOperatorServiceWithPresence(servingCache, presenceRepo, &fakeOrderReader{}, &fakeOperatorEventBus{})

	presence, err := svc.GetPresence(context.Background(), "merch-1", "store-1")
	require.NoError(t, err)
	assert.Equal(t, domain.ServingStateDegraded, presence.Serving)
}

func TestOperatorService_GetManifest_OK(t *testing.T) {
	servingCache := &fakeServingCacheReader{
		profile:   &serving.CacheProfile{StorefrontURL: "https://example.com", UCP: serving.UCPBlockCache{Version: "2026-04-08"}},
		watermark: &serving.CacheWatermark{BuiltAt: time.Now().UTC(), SourceSync: time.Now().UTC(), IsStale: false},
	}
	svc := newTestOperatorServiceWithPresence(servingCache, &fakePresenceRepo{}, &fakeOrderReader{}, &fakeOperatorEventBus{})

	manifest, err := svc.GetManifest(context.Background(), "merch-1", "store-1")
	require.NoError(t, err)
	assert.NotNil(t, manifest.Manifest)
	assert.False(t, manifest.Cache.Stale)

	// Manifest is json.RawMessage now (marshalled once, not decoded into a
	// map and re-encoded) — decode it back to confirm the content survives
	// unchanged, and that StorefrontURL (json:"-" on CacheProfile) never
	// leaks into the served manifest.
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(manifest.Manifest, &decoded))
	ucp, ok := decoded["ucp"].(map[string]any)
	require.True(t, ok, "expected a ucp object in the manifest")
	assert.Equal(t, "2026-04-08", ucp["version"])
	assert.NotContains(t, decoded, "storefront_url")
}

func TestOperatorService_GetManifest_NeverSynced(t *testing.T) {
	svc := newTestOperatorServiceWithPresence(&fakeServingCacheReader{profileErr: serving.ErrNotFound}, &fakePresenceRepo{}, &fakeOrderReader{}, &fakeOperatorEventBus{})

	manifest, err := svc.GetManifest(context.Background(), "merch-1", "store-new")
	require.NoError(t, err)
	assert.JSONEq(t, "{}", string(manifest.Manifest))
	assert.True(t, manifest.Cache.Stale)
}

// TestOperatorService_GetManifest_CrossTenantIsNotFound is the regression
// test for F-007: merchantID used to be accepted and never referenced, so
// any authenticated merchant could read another merchant's manifest by
// guessing a store_id. The serving cache itself has no merchant_id concept
// (it's keyed by store_id alone), so this can only be caught by the
// StoreOwnershipChecker GetManifest now consults first.
func TestOperatorService_GetManifest_CrossTenantIsNotFound(t *testing.T) {
	servingCache := &fakeServingCacheReader{
		profile:   &serving.CacheProfile{StorefrontURL: "https://example.com", UCP: serving.UCPBlockCache{Version: "2026-04-08"}},
		watermark: &serving.CacheWatermark{BuiltAt: time.Now().UTC(), SourceSync: time.Now().UTC(), IsStale: false},
	}
	stores := &fakeStoreOwnershipChecker{owned: map[domain.StoreID]domain.MerchantID{
		"store-1": "merch-1",
	}}
	svc := newTestOperatorServiceWithPresenceAndStores(servingCache, &fakePresenceRepo{}, &fakeOrderReader{}, stores, &fakeOperatorEventBus{})

	_, err := svc.GetManifest(context.Background(), "merch-2", "store-1")
	require.ErrorIs(t, err, operator.ErrNotFound)
}

func TestOperatorService_GetManifest_OwnStoreOK(t *testing.T) {
	servingCache := &fakeServingCacheReader{
		profile:   &serving.CacheProfile{StorefrontURL: "https://example.com", UCP: serving.UCPBlockCache{Version: "2026-04-08"}},
		watermark: &serving.CacheWatermark{BuiltAt: time.Now().UTC(), SourceSync: time.Now().UTC(), IsStale: false},
	}
	stores := &fakeStoreOwnershipChecker{owned: map[domain.StoreID]domain.MerchantID{
		"store-1": "merch-1",
	}}
	svc := newTestOperatorServiceWithPresenceAndStores(servingCache, &fakePresenceRepo{}, &fakeOrderReader{}, stores, &fakeOperatorEventBus{})

	manifest, err := svc.GetManifest(context.Background(), "merch-1", "store-1")
	require.NoError(t, err)
	assert.False(t, manifest.Cache.Stale)
}

func TestOperatorService_RebuildCache_PublishesEvent(t *testing.T) {
	bus := &fakeOperatorEventBus{}
	svc := newTestOperatorServiceWithPresence(&fakeServingCacheReader{}, &fakePresenceRepo{}, &fakeOrderReader{}, bus)

	job, err := svc.RebuildCache(context.Background(), "merch-1", "store-1")
	require.NoError(t, err)
	assert.NotEmpty(t, job.JobID)
	assert.Equal(t, domain.JobQueued, job.Status)

	require.Len(t, bus.published, 1)
	evt := bus.published[0]
	assert.Equal(t, ucp.EventRebuildRequested, evt.EventType())
	assert.Equal(t, "store-1", evt.StoreID())

	var payload struct {
		Reason   string `json:"reason"`
		Priority bool   `json:"priority"`
	}
	require.NoError(t, json.Unmarshal(evt.Payload(), &payload))
	assert.Equal(t, "operator_requested", payload.Reason)
	assert.True(t, payload.Priority)
}

func TestOperatorService_ListOrders_DelegatesToReader(t *testing.T) {
	reader := &fakeOrderReader{list: domain.ListOf[domain.Order]{Data: []domain.Order{{OrderID: "ord-1"}}}}
	svc := newTestOperatorServiceWithPresence(&fakeServingCacheReader{}, &fakePresenceRepo{}, reader, &fakeOperatorEventBus{})

	result, err := svc.ListOrders(context.Background(), "merch-1", "store-1", "", 20, nil)
	require.NoError(t, err)
	assert.Len(t, result.Data, 1)
	assert.Equal(t, "ord-1", result.Data[0].OrderID)
}

func TestOperatorService_GetOrder_NotFoundPropagates(t *testing.T) {
	reader := &fakeOrderReader{orderErr: operator.ErrOrderNotFound}
	svc := newTestOperatorServiceWithPresence(&fakeServingCacheReader{}, &fakePresenceRepo{}, reader, &fakeOperatorEventBus{})

	_, err := svc.GetOrder(context.Background(), "merch-1", "store-1", "missing")
	assert.ErrorIs(t, err, operator.ErrOrderNotFound)
}
