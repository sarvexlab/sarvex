// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// fakeIngestConnector is a configurable base.Connector test double: each
// Fetch* call can be scripted to return a canned entity, block until
// released, or fail with a given error. Call counts are tracked per store
// for assertions. The embedded nil base.Connector satisfies the rest of the
// interface (checkout/catalog/cart/order), unused by ingestion tests.
type fakeIngestConnector struct {
	base.Connector

	mu sync.Mutex

	products  map[string]*base.CatalogEntity // key: storeID+":"+sku
	inventory map[string]*base.InventoryEntity
	pricing   map[string]*base.PricingEntity

	err error // if set, every Fetch* call fails with this error

	block   <-chan struct{} // if set, Fetch* waits on this channel before proceeding
	blockOn string          // storeID that should block; empty blocks all calls

	productCalls   int
	inventoryCalls int
	pricingCalls   int
}

func newFakeIngestConnector() *fakeIngestConnector {
	return &fakeIngestConnector{
		products:  make(map[string]*base.CatalogEntity),
		inventory: make(map[string]*base.InventoryEntity),
		pricing:   make(map[string]*base.PricingEntity),
	}
}

func (f *fakeIngestConnector) key(storeID, sku string) string { return storeID + ":" + sku }

func (f *fakeIngestConnector) maybeBlock(storeID string) {
	if f.block != nil && (f.blockOn == "" || f.blockOn == storeID) {
		<-f.block
	}
}

func (f *fakeIngestConnector) FetchProduct(ctx context.Context, storeID string, sku string) (*base.CatalogEntity, error) {
	f.maybeBlock(storeID)
	f.mu.Lock()
	f.productCalls++
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	e, ok := f.products[f.key(storeID, sku)]
	if !ok {
		return nil, &base.ConnectorError{Kind: base.ConnectorErrNotFound, StoreID: storeID, Op: "FetchProduct", Err: errors.New("not found")}
	}
	cp := *e
	return &cp, nil
}

func (f *fakeIngestConnector) FetchInventory(ctx context.Context, storeID string, sku string) (*base.InventoryEntity, error) {
	f.maybeBlock(storeID)
	f.mu.Lock()
	f.inventoryCalls++
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	e, ok := f.inventory[f.key(storeID, sku)]
	if !ok {
		return nil, &base.ConnectorError{Kind: base.ConnectorErrNotFound, StoreID: storeID, Op: "FetchInventory", Err: errors.New("not found")}
	}
	cp := *e
	return &cp, nil
}

func (f *fakeIngestConnector) FetchPricing(ctx context.Context, storeID string, sku string) (*base.PricingEntity, error) {
	f.maybeBlock(storeID)
	f.mu.Lock()
	f.pricingCalls++
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	e, ok := f.pricing[f.key(storeID, sku)]
	if !ok {
		return nil, &base.ConnectorError{Kind: base.ConnectorErrNotFound, StoreID: storeID, Op: "FetchPricing", Err: errors.New("not found")}
	}
	cp := *e
	return &cp, nil
}

func (f *fakeIngestConnector) FetchCatalog(ctx context.Context, storeID string, pageSize int) ([]base.CatalogEntity, error) {
	return nil, errors.New("fakeIngestConnector: FetchCatalog not used by ingestion tests")
}

func (f *fakeIngestConnector) Ping(ctx context.Context, storeID string) (base.HealthStatus, error) {
	return base.HealthHealthy, nil
}

var _ base.Connector = (*fakeIngestConnector)(nil)

func newSignalEvent(storeID, merchantID, connectorID, sku, entityKind, change string, sequence int64) *simpleDomainEvent {
	payload, _ := json.Marshal(signalReceivedPayload{
		ConnectorID: connectorID,
		StoreID:     storeID,
		SKU:         sku,
		EntityKind:  entityKind,
		Sequence:    sequence,
		Change:      change,
	})
	return &simpleDomainEvent{
		topic:      "connector.signal_received",
		storeID:    storeID,
		merchantID: merchantID,
		occurredAt: time.Now().UTC(),
		payload:    payload,
	}
}

func TestIngestionPipeline_ProductChanged_PullsAndUpsertsAuthoritativeData(t *testing.T) {
	store := newFakeCatalogStore()
	connector := newFakeIngestConnector()
	connector.products["store-a:SKU-1"] = &base.CatalogEntity{
		SKU: "SKU-1", Name: "Authoritative Name", PriceMinor: 5000, Currency: "AUD", InStock: true,
	}
	pipeline := newFakeMappingPipeline()
	reg, provider := newTestRegistry("store-a", connector, "store-b")
	p := NewIngestionPipeline(store, reg, provider, pipeline, nil, testLogger())

	// The signal itself carries no product data — only identity. Confirms
	// the signal payload is never treated as data of record.
	evt := newSignalEvent("store-a", "merch-a", "conn-1", "SKU-1", "product", "updated", 1)
	if err := p.HandleSignalReceived(context.Background(), evt); err != nil {
		t.Fatalf("HandleSignalReceived: %v", err)
	}

	if connector.productCalls != 1 {
		t.Fatalf("expected 1 FetchProduct call, got %d", connector.productCalls)
	}

	product, err := store.GetProduct(context.Background(), "store-a", "SKU-1")
	if err != nil {
		t.Fatalf("GetProduct: %v", err)
	}
	if product.Title != "Authoritative Name" || product.PriceMinor != 5000 {
		t.Errorf("expected pulled entity to be written, got title=%q price=%d", product.Title, product.PriceMinor)
	}

	if len(pipeline.calls) != 1 || pipeline.calls[0].kind != "product" {
		t.Fatalf("expected 1 product mapping call, got %+v", pipeline.calls)
	}
	if pipeline.calls[0].storeID != "store-a" {
		t.Errorf("expected call keyed by store_id=store-a, got %q", pipeline.calls[0].storeID)
	}
}

func TestIngestionPipeline_PriceChanged_UpdatesStoreAndHandsOffPriceChange(t *testing.T) {
	store := newFakeCatalogStore()
	connector := newFakeIngestConnector()
	// Seed an existing product; the price signal must only touch price
	// fields and preserve everything else (title survives).
	existing := &CatalogProduct{
		MerchantID: "merch-a", StoreID: "store-a", SKU: "SKU-1",
		Title: "Existing Title", PriceMinor: 100, Currency: "USD",
		RawAttributes: map[string]string{}, MappedFields: map[string]string{},
	}
	_ = store.UpsertProduct(context.Background(), existing)
	connector.pricing["store-a:SKU-1"] = &base.PricingEntity{SKU: "SKU-1", PriceMinor: 7500, Currency: "AUD"}

	pipeline := newFakeMappingPipeline()
	reg, provider := newTestRegistry("store-a", connector, "store-b")
	p := NewIngestionPipeline(store, reg, provider, pipeline, nil, testLogger())

	evt := newSignalEvent("store-a", "merch-a", "conn-1", "SKU-1", "price", "updated", 1)
	if err := p.HandleSignalReceived(context.Background(), evt); err != nil {
		t.Fatalf("HandleSignalReceived: %v", err)
	}

	product, err := store.GetProduct(context.Background(), "store-a", "SKU-1")
	if err != nil {
		t.Fatalf("GetProduct: %v", err)
	}
	if product.PriceMinor != 7500 || product.Currency != "AUD" {
		t.Errorf("expected pulled price, got %d %s", product.PriceMinor, product.Currency)
	}
	if product.Title != "Existing Title" {
		t.Errorf("price-only pull must not wipe other fields, title=%q", product.Title)
	}

	if len(pipeline.calls) != 1 || pipeline.calls[0].kind != "price" {
		t.Fatalf("expected 1 price mapping call, got %+v", pipeline.calls)
	}
}

func TestIngestionPipeline_InventoryChanged_UpdatesStoreAndHandsOffInventoryChange(t *testing.T) {
	store := newFakeCatalogStore()
	connector := newFakeIngestConnector()
	qty := 3
	connector.inventory["store-a:SKU-1"] = &base.InventoryEntity{SKU: "SKU-1", InStock: false, Quantity: &qty}

	pipeline := newFakeMappingPipeline()
	reg, provider := newTestRegistry("store-a", connector, "store-b")
	p := NewIngestionPipeline(store, reg, provider, pipeline, nil, testLogger())

	evt := newSignalEvent("store-a", "merch-a", "conn-1", "SKU-1", "inventory", "updated", 1)
	if err := p.HandleSignalReceived(context.Background(), evt); err != nil {
		t.Fatalf("HandleSignalReceived: %v", err)
	}

	product, err := store.GetProduct(context.Background(), "store-a", "SKU-1")
	if err != nil {
		t.Fatalf("GetProduct: %v", err)
	}
	if product.InStock != false || product.Quantity == nil || *product.Quantity != 3 {
		t.Errorf("expected pulled inventory, got in_stock=%v qty=%v", product.InStock, product.Quantity)
	}

	if len(pipeline.calls) != 1 || pipeline.calls[0].kind != "inventory" {
		t.Fatalf("expected 1 inventory mapping call, got %+v", pipeline.calls)
	}
}

func TestIngestionPipeline_DuplicateMessageID_IsNoOp(t *testing.T) {
	store := newFakeCatalogStore()
	connector := newFakeIngestConnector()
	connector.products["store-a:SKU-1"] = &base.CatalogEntity{SKU: "SKU-1", Name: "N", PriceMinor: 100, Currency: "USD"}
	pipeline := newFakeMappingPipeline()
	reg, provider := newTestRegistry("store-a", connector, "store-b")
	p := NewIngestionPipeline(store, reg, provider, pipeline, nil, testLogger())

	evt := newSignalEvent("store-a", "merch-a", "conn-1", "SKU-1", "product", "updated", 1)

	if err := p.HandleSignalReceived(context.Background(), evt); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Exact same signal (same connector_id/sku/sequence) redelivered.
	if err := p.HandleSignalReceived(context.Background(), evt); err != nil {
		t.Fatalf("duplicate call: %v", err)
	}

	if connector.productCalls != 1 {
		t.Errorf("expected exactly 1 pull (duplicate must not re-pull), got %d", connector.productCalls)
	}
	if len(pipeline.calls) != 1 {
		t.Errorf("expected exactly 1 mapping call, got %d", len(pipeline.calls))
	}
}

func TestIngestionPipeline_StaleOrOutOfOrderSequence_IsDropped(t *testing.T) {
	store := newFakeCatalogStore()
	connector := newFakeIngestConnector()
	connector.products["store-a:SKU-1"] = &base.CatalogEntity{SKU: "SKU-1", Name: "N", PriceMinor: 100, Currency: "USD"}
	pipeline := newFakeMappingPipeline()
	reg, provider := newTestRegistry("store-a", connector, "store-b")
	p := NewIngestionPipeline(store, reg, provider, pipeline, nil, testLogger())

	newer := newSignalEvent("store-a", "merch-a", "conn-1", "SKU-1", "product", "updated", 5)
	if err := p.HandleSignalReceived(context.Background(), newer); err != nil {
		t.Fatalf("newer signal: %v", err)
	}

	// A different message_id (different sku, so dedup alone would not catch
	// it) but a stale/lower sequence for the same store must still be dropped.
	stale := newSignalEvent("store-a", "merch-a", "conn-1", "SKU-2", "product", "updated", 3)
	if err := p.HandleSignalReceived(context.Background(), stale); err != nil {
		t.Fatalf("stale signal: %v", err)
	}

	if connector.productCalls != 1 {
		t.Errorf("expected stale signal to be dropped without a pull, got %d pulls", connector.productCalls)
	}
	if _, err := store.GetProduct(context.Background(), "store-a", "SKU-2"); err != ErrProductNotFound {
		t.Errorf("stale signal must not write to the catalog store, got err=%v", err)
	}
}

func TestIngestionPipeline_DeleteSignal_RemovesProduct(t *testing.T) {
	store := newFakeCatalogStore()
	_ = store.UpsertProduct(context.Background(), &CatalogProduct{
		MerchantID: "merch-a", StoreID: "store-a", SKU: "SKU-1", Title: "T",
		RawAttributes: map[string]string{}, MappedFields: map[string]string{},
	})
	connector := newFakeIngestConnector()
	pipeline := newFakeMappingPipeline()
	reg, provider := newTestRegistry("store-a", connector, "store-b")
	p := NewIngestionPipeline(store, reg, provider, pipeline, nil, testLogger())

	evt := newSignalEvent("store-a", "merch-a", "conn-1", "SKU-1", "product", "deleted", 1)
	if err := p.HandleSignalReceived(context.Background(), evt); err != nil {
		t.Fatalf("HandleSignalReceived: %v", err)
	}

	if _, err := store.GetProduct(context.Background(), "store-a", "SKU-1"); err != ErrProductNotFound {
		t.Errorf("expected product deleted, got err=%v", err)
	}
	if connector.productCalls != 0 {
		t.Errorf("delete must not pull, got %d pulls", connector.productCalls)
	}
	if len(pipeline.calls) != 1 || pipeline.calls[0].kind != "product_deleted" || pipeline.calls[0].sku != "SKU-1" {
		t.Fatalf("expected 1 product_deleted mapping call for SKU-1, got %+v", pipeline.calls)
	}
}

func TestIngestionPipeline_StoreIsolation_StoreASignalNeverWritesStoreB(t *testing.T) {
	store := newFakeCatalogStore()
	connector := newFakeIngestConnector()
	connector.products["store-a:SKU-1"] = &base.CatalogEntity{SKU: "SKU-1", Name: "A", PriceMinor: 100, Currency: "USD"}
	pipeline := newFakeMappingPipeline()
	reg, provider := newTestRegistry("store-a", connector, "store-b")
	p := NewIngestionPipeline(store, reg, provider, pipeline, nil, testLogger())

	evt := newSignalEvent("store-a", "merch-a", "conn-1", "SKU-1", "product", "updated", 1)
	if err := p.HandleSignalReceived(context.Background(), evt); err != nil {
		t.Fatalf("HandleSignalReceived: %v", err)
	}

	if _, err := store.GetProduct(context.Background(), "store-b", "SKU-1"); err != ErrProductNotFound {
		t.Errorf("store-a signal must not be visible under store-b, got err=%v", err)
	}
	got, err := store.GetProduct(context.Background(), "store-a", "SKU-1")
	if err != nil || got.MerchantID != "merch-a" {
		t.Errorf("expected product scoped to store-a/merch-a, got %+v err=%v", got, err)
	}
}

func TestIngestionPipeline_TwoStoresProcessInParallel_OneStoreInOrder(t *testing.T) {
	store := newFakeCatalogStore()
	connector := newFakeIngestConnector()
	connector.products["store-a:SKU-1"] = &base.CatalogEntity{SKU: "SKU-1", Name: "A1", PriceMinor: 100, Currency: "USD"}
	connector.products["store-a:SKU-2"] = &base.CatalogEntity{SKU: "SKU-2", Name: "A2", PriceMinor: 200, Currency: "USD"}
	connector.products["store-b:SKU-1"] = &base.CatalogEntity{SKU: "SKU-1", Name: "B1", PriceMinor: 300, Currency: "USD"}

	// store-a's pulls block until released, to prove store-b is not stuck
	// waiting behind store-a.
	block := make(chan struct{})
	connector.block = block
	connector.blockOn = "store-a"

	pipeline := newFakeMappingPipeline()
	reg, provider := newTestRegistry("store-a", connector, "store-b")
	p := NewIngestionPipeline(store, reg, provider, pipeline, nil, testLogger())

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		evt1 := newSignalEvent("store-a", "merch-a", "conn-1", "SKU-1", "product", "updated", 1)
		_ = p.HandleSignalReceived(context.Background(), evt1)
		evt2 := newSignalEvent("store-a", "merch-a", "conn-1", "SKU-2", "product", "updated", 2)
		_ = p.HandleSignalReceived(context.Background(), evt2)
	}()

	storeBDone := make(chan struct{})
	go func() {
		defer wg.Done()
		evtB := newSignalEvent("store-b", "merch-b", "conn-2", "SKU-1", "product", "updated", 1)
		_ = p.HandleSignalReceived(context.Background(), evtB)
		close(storeBDone)
	}()

	select {
	case <-storeBDone:
		// store-b completed while store-a's first pull is still blocked.
	case <-time.After(2 * time.Second):
		t.Fatal("store-b signal was blocked behind store-a's slow pull")
	}

	close(block)
	wg.Wait()

	if _, err := store.GetProduct(context.Background(), "store-b", "SKU-1"); err != nil {
		t.Errorf("expected store-b product written, got err=%v", err)
	}
	p1, err := store.GetProduct(context.Background(), "store-a", "SKU-1")
	if err != nil || p1.Title != "A1" {
		t.Errorf("expected store-a SKU-1 written, got %+v err=%v", p1, err)
	}
	p2, err := store.GetProduct(context.Background(), "store-a", "SKU-2")
	if err != nil || p2.Title != "A2" {
		t.Errorf("expected store-a SKU-2 written, got %+v err=%v", p2, err)
	}
}

func TestIngestionPipeline_FailedPull_ReturnsTypedRetryableError_DoesNotWriteOrBlockOtherStores(t *testing.T) {
	store := newFakeCatalogStore()
	connector := newFakeIngestConnector()
	connector.err = &base.ConnectorError{Kind: base.ConnectorErrUnavailable, StoreID: "store-a", Op: "FetchProduct", Err: errors.New("store unreachable")}
	pipeline := newFakeMappingPipeline()
	reg, provider := newTestRegistry("store-a", connector, "store-b")
	p := NewIngestionPipeline(store, reg, provider, pipeline, nil, testLogger())

	evt := newSignalEvent("store-a", "merch-a", "conn-1", "SKU-1", "product", "updated", 1)
	err := p.HandleSignalReceived(context.Background(), evt)
	if err == nil {
		t.Fatal("expected an error from a failed pull")
	}
	var pullErr *PullError
	if !errors.As(err, &pullErr) {
		t.Fatalf("expected a *PullError, got %T: %v", err, err)
	}
	var connErr *base.ConnectorError
	if !errors.As(err, &connErr) {
		t.Errorf("expected the underlying *base.ConnectorError to unwrap, got %v", err)
	}

	if _, getErr := store.GetProduct(context.Background(), "store-a", "SKU-1"); getErr != ErrProductNotFound {
		t.Errorf("failed pull must not write partial data, got err=%v", getErr)
	}
	if len(pipeline.calls) != 0 {
		t.Errorf("failed pull must not hand off a change, got %+v", pipeline.calls)
	}

	// A different store is unaffected by store-a's failure.
	connector.err = nil
	connector.products["store-b:SKU-1"] = &base.CatalogEntity{SKU: "SKU-1", Name: "B", PriceMinor: 100, Currency: "USD"}
	evtB := newSignalEvent("store-b", "merch-b", "conn-2", "SKU-1", "product", "updated", 1)
	if err := p.HandleSignalReceived(context.Background(), evtB); err != nil {
		t.Fatalf("store-b must succeed independently of store-a's failure: %v", err)
	}
}

func TestIngestionPipeline_OrderStatusSignal_DeferredNotProcessed(t *testing.T) {
	store := newFakeCatalogStore()
	connector := newFakeIngestConnector()
	pipeline := newFakeMappingPipeline()
	reg, provider := newTestRegistry("store-a", connector, "store-b")
	p := NewIngestionPipeline(store, reg, provider, pipeline, nil, testLogger())

	evt := newSignalEvent("store-a", "merch-a", "conn-1", "ORDER-1", "order_status", "updated", 1)
	if err := p.HandleSignalReceived(context.Background(), evt); err != nil {
		t.Fatalf("HandleSignalReceived: %v", err)
	}
	if connector.productCalls+connector.pricingCalls+connector.inventoryCalls != 0 {
		t.Errorf("order_status signal must not trigger a catalog pull")
	}
	if len(pipeline.calls) != 0 {
		t.Errorf("order_status signal must not hand off a catalog change, got %+v", pipeline.calls)
	}
}

// A delete for a SKU the mirror never had (or already removed) has nothing to
// clean up and must not reach the mapping pipeline.
func TestIngestionPipeline_DeleteSignal_UnknownProductIsNoOp(t *testing.T) {
	store := newFakeCatalogStore()
	pipeline := newFakeMappingPipeline()
	reg, provider := newTestRegistry("store-a", newFakeIngestConnector(), "store-b")
	p := NewIngestionPipeline(store, reg, provider, pipeline, nil, testLogger())

	evt := newSignalEvent("store-a", "merch-a", "conn-1", "SKU-404", "product", "deleted", 1)
	if err := p.HandleSignalReceived(context.Background(), evt); err != nil {
		t.Fatalf("HandleSignalReceived: %v", err)
	}
	if len(pipeline.calls) != 0 {
		t.Fatalf("expected no mapping calls, got %+v", pipeline.calls)
	}
}
