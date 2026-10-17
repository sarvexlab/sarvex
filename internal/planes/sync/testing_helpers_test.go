// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wire"
	"github.com/sarvexlabs/sarvex.cp/internal/platform/woocommerce"
)

// stubStoreProvider is a minimal ucp.StoreProvider for tests that only need
// registry.Get(...) to resolve to a given (platform, storeID) key.
type stubStoreProvider struct {
	storeID string
}

func (s *stubStoreProvider) StoreID() string                                   { return s.storeID }
func (s *stubStoreProvider) Version() string                                   { return "2024-01-01" }
func (s *stubStoreProvider) Services() map[string][]wire.Service               { return nil }
func (s *stubStoreProvider) Capabilities() map[string][]wire.Capability        { return nil }
func (s *stubStoreProvider) PaymentHandlers() map[string][]wire.PaymentHandler { return nil }
func (s *stubStoreProvider) SigningKeys() []wire.JWK                           { return nil }
func (s *stubStoreProvider) StorefrontURL() string                             { return "" }
func (s *stubStoreProvider) Configuration() ucp.Configuration {
	return ucp.Configuration{
		MerchantID: "merch-a",
		StoreID:    s.storeID,
		Platform:   woocommerce.PlatformName,
	}
}

// newTestRegistry builds a *base.Registry with connector pre-injected (via
// Registry.Set, bypassing factories) for storeID and any extraStoreIDs —
// tests that use one connector double to simulate serving several stores
// (e.g. proving store isolation) pass the additional store IDs — plus the
// ucp.StoreProviderByStoreIDFunc that resolves them, for tests that
// construct CatalogImportHandler/IngestionPipeline/SweepHandler directly.
func newTestRegistry(storeID string, connector base.Connector, extraStoreIDs ...string) (*base.Registry, ucp.StoreProviderByStoreIDFunc) {
	noCreds := base.CredentialsFunc(func(_ context.Context, _ string) (map[string]any, error) {
		return nil, errors.New("no credentials provider configured for this test")
	})
	reg := base.New(noCreds, nil)
	reg.Set(woocommerce.PlatformName, storeID, connector)
	for _, id := range extraStoreIDs {
		reg.Set(woocommerce.PlatformName, id, connector)
	}

	provider := func(merchantID, sid string) (ucp.StoreProvider, error) {
		return &stubStoreProvider{storeID: sid}, nil
	}
	return reg, provider
}

// fakeCatalogStore is an in-memory CatalogStore for unit tests across the
// sync package (ingestion, catalog import, ...).
type fakeCatalogStore struct {
	products map[string]*CatalogProduct
}

func newFakeCatalogStore() *fakeCatalogStore {
	return &fakeCatalogStore{
		products: make(map[string]*CatalogProduct),
	}
}

// UpsertProduct mirrors MongoDBCatalogStore: the id is derived from
// MerchantID/StoreID/SKU, and lookups key on (StoreID, SKU) rather than a
// caller-assembled id.
func (fcs *fakeCatalogStore) UpsertProduct(ctx context.Context, product *CatalogProduct) error {
	if product.MerchantID == "" || product.StoreID == "" || product.SKU == "" {
		return ErrMissingTenantKey
	}
	product.ID = docID(product.MerchantID, product.StoreID, product.SKU)
	fcs.products[product.ID] = product
	return nil
}

func (fcs *fakeCatalogStore) DeleteProduct(ctx context.Context, storeID, sku string) error {
	for id, p := range fcs.products {
		if p.StoreID == storeID && p.SKU == sku {
			delete(fcs.products, id)
			return nil
		}
	}
	return ErrProductNotFound
}

func (fcs *fakeCatalogStore) GetProduct(ctx context.Context, storeID, sku string) (*CatalogProduct, error) {
	for _, p := range fcs.products {
		if p.StoreID == storeID && p.SKU == sku {
			return p, nil
		}
	}
	return nil, ErrProductNotFound
}

func (fcs *fakeCatalogStore) ListByStore(ctx context.Context, storeID string, limit int, cursor string) ([]*CatalogProduct, *string, error) {
	var products []*CatalogProduct
	for _, p := range fcs.products {
		if p.StoreID == storeID {
			products = append(products, p)
		}
	}
	return products, nil, nil
}

func (fcs *fakeCatalogStore) LatestUpdateTime(ctx context.Context, storeID string) (time.Time, error) {
	var latest time.Time
	for _, p := range fcs.products {
		if p.StoreID == storeID && p.UpdatedAt.After(latest) {
			latest = p.UpdatedAt
		}
	}
	return latest, nil
}

// ListStoreIDs mirrors MongoDBCatalogStore.ListStoreIDs: the distinct set of
// store_ids with at least one record.
func (fcs *fakeCatalogStore) ListStoreIDs(ctx context.Context) ([]string, error) {
	seen := make(map[string]struct{})
	var storeIDs []string
	for _, p := range fcs.products {
		if _, ok := seen[p.StoreID]; ok {
			continue
		}
		seen[p.StoreID] = struct{}{}
		storeIDs = append(storeIDs, p.StoreID)
	}
	return storeIDs, nil
}

// fakeLease is an in-memory concurrency.Lease for unit tests. A single
// fakeLease instance simulates the shared distributed backend (e.g. Redis)
// that real process instances would coordinate through: multiple
// SweepScheduler/SweepHandler values constructed with the *same* fakeLease
// behave like multiple process instances racing for the same lease.
type fakeLease struct {
	mu      sync.Mutex
	holders map[string]string // key -> holder
}

func newFakeLease() *fakeLease {
	return &fakeLease{holders: make(map[string]string)}
}

func (l *fakeLease) TryAcquire(ctx context.Context, key, holder string, ttl time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cur, held := l.holders[key]
	if held && cur != holder {
		return false, nil
	}
	l.holders[key] = holder
	return true, nil
}

func (l *fakeLease) Release(ctx context.Context, key, holder string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if cur, held := l.holders[key]; held && cur == holder {
		delete(l.holders, key)
	}
	return nil
}

// fakeEventBus is an in-memory EventBus for unit tests. Publish only;
// Subscribe is a no-op since unit tests invoke handlers directly.
type fakeEventBus struct {
	published []eventbus.DomainEvent
}

func newFakeEventBus() *fakeEventBus {
	return &fakeEventBus{
		published: make([]eventbus.DomainEvent, 0),
	}
}

func (feb *fakeEventBus) Publish(ctx context.Context, event eventbus.DomainEvent) error {
	feb.published = append(feb.published, event)
	return nil
}

func (feb *fakeEventBus) Subscribe(ctx context.Context, topic string, handler eventbus.EventHandler) (eventbus.Subscription, error) {
	return nil, nil
}

func (feb *fakeEventBus) Unsubscribe(ctx context.Context, sub eventbus.Subscription) error {
	return nil
}

func (feb *fakeEventBus) Close() error {
	return nil
}

// mappingCall records one call ingestion handed to a fakeMappingPipeline,
// keyed by the kind of change so tests can assert on it without depending
// on the exact payload struct.
type mappingCall struct {
	kind       string // "product", "price", "inventory"
	storeID    string
	merchantID string
	sku        string
	change     string // product only
}

// fakeMappingPipeline is a mappingPipeline test double standing in for
// syncService: ingestion tests assert on the direct calls it recorded
// instead of bus-published events, since ingestion no longer publishes.
type fakeMappingPipeline struct {
	mu    sync.Mutex
	calls []mappingCall
}

func newFakeMappingPipeline() *fakeMappingPipeline {
	return &fakeMappingPipeline{}
}

func (f *fakeMappingPipeline) record(c mappingCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
}

func (f *fakeMappingPipeline) OnProductChanged(_ context.Context, storeID, merchantID string, payload ProductChangedPayload) error {
	f.record(mappingCall{kind: "product", storeID: storeID, merchantID: merchantID, sku: payload.SKU, change: payload.Change})
	return nil
}

func (f *fakeMappingPipeline) OnPriceChanged(_ context.Context, storeID, merchantID string, payload PriceChangedPayload) error {
	f.record(mappingCall{kind: "price", storeID: storeID, merchantID: merchantID, sku: payload.SKU})
	return nil
}

func (f *fakeMappingPipeline) OnInventoryChanged(_ context.Context, storeID, merchantID string, payload InventoryChangedPayload) error {
	f.record(mappingCall{kind: "inventory", storeID: storeID, merchantID: merchantID, sku: payload.SKU})
	return nil
}

func (f *fakeMappingPipeline) OnProductDeleted(_ context.Context, storeID, merchantID string, p *CatalogProduct) error {
	f.record(mappingCall{kind: "product_deleted", storeID: storeID, merchantID: merchantID, sku: p.SKU})
	return nil
}

// fakeProductRescorer is a productRescorer/productBatchRescorer test double
// for SweepHandler and CatalogImportHandler tests: it records which
// products it was asked to rescore (and any FinalizeBatchReadiness calls)
// without running the real resolve/score pipeline (which needs a loaded
// catalogue).
type fakeProductRescorer struct {
	mu                 sync.Mutex
	rescored           []string      // SKUs, in call order
	deleted            []string      // SKUs passed to OnProductDeleted, in call order
	finalized          []string      // store_ids passed to FinalizeBatchReadiness, in call order
	finalizedFreshConn []bool        // freshConnection passed to FinalizeBatchReadiness, parallel to finalized
	err                error         // if set, RescoreProduct(Batch) returns this instead of recording
	finalizeErr        error         // if set, FinalizeBatchReadiness returns this instead of recording
	bus                *fakeEventBus // optional: if set, FinalizeBatchReadiness snapshots len(bus.published) at call time
	finalizedAtBusLen  []int         // len(bus.published) at each FinalizeBatchReadiness call, parallel to finalized
}

func newFakeProductRescorer() *fakeProductRescorer {
	return &fakeProductRescorer{}
}

func (f *fakeProductRescorer) RescoreProduct(_ context.Context, p *CatalogProduct, _ *ScoringRubric) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.rescored = append(f.rescored, p.SKU)
	return nil
}

func (f *fakeProductRescorer) OnProductDeleted(_ context.Context, _, _ string, p *CatalogProduct) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, p.SKU)
	return nil
}

func (f *fakeProductRescorer) RescoreProductBatch(ctx context.Context, p *CatalogProduct, rubric *ScoringRubric) error {
	return f.RescoreProduct(ctx, p, rubric)
}

func (f *fakeProductRescorer) FinalizeBatchReadiness(_ context.Context, _, storeID string, freshConnection bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.finalizeErr != nil {
		return f.finalizeErr
	}
	f.finalized = append(f.finalized, storeID)
	f.finalizedFreshConn = append(f.finalizedFreshConn, freshConnection)
	if f.bus != nil {
		f.finalizedAtBusLen = append(f.finalizedAtBusLen, len(f.bus.published))
	}
	return nil
}
