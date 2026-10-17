// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"testing"
)

// noMappingResolver resolves nothing: these tests exercise the score stage's
// reaction to a targeted (price/stock) change, not the ontology.
type noMappingResolver struct{}

func (noMappingResolver) Resolve(context.Context, *CatalogProduct) (*MappedProduct, error) {
	return &MappedProduct{Fields: map[string]FieldResolution{}}, nil
}

func newSignalTestService(store *fakeCatalogStore, bus *fakeEventBus) *syncService {
	return &syncService{
		bus:               bus,
		log:               handlerTestLogger(),
		catalogStore:      store,
		readinessStore:    newFakeReadinessAggregateStore(),
		resolver:          noMappingResolver{},
		consistencyEngine: NewConsistencyEngine(handlerTestLogger()),
	}
}

func healthyProduct() *CatalogProduct {
	q := 5
	return &CatalogProduct{
		MerchantID: "merchant-1", StoreID: "store-1", SKU: "SKU-1",
		Title: "Widget", Description: "A widget", Images: []string{"https://x/y.png"},
		PriceMinor: 1000, Currency: "USD", InStock: true, Quantity: &q,
		RawAttributes: map[string]string{}, MappedFields: map[string]string{"shipping_weight": "1", "shipping_dimensions": "1x1x1"},
	}
}

// A price edited to zero is a blocking defect: the price signal must re-score
// the product (flag it, raise its task, rebuild the cache), not just patch the
// price and leave the score and tasks as they were.
func TestOnPriceChanged_RescoresAndRaisesBlockingTask(t *testing.T) {
	ctx := context.Background()
	store := newFakeCatalogStore()
	_ = store.UpsertProduct(ctx, healthyProduct())
	bus := newFakeEventBus()
	svc := newSignalTestService(store, bus)

	err := svc.OnPriceChanged(ctx, "store-1", "merchant-1", PriceChangedPayload{SKU: "SKU-1", NewPrice: moneyPayload{AmountMinor: 0, Currency: "USD"}})
	if err != nil {
		t.Fatalf("OnPriceChanged: %v", err)
	}

	p, _ := store.GetProduct(ctx, "store-1", "SKU-1")
	if p.PriceMinor != 0 || !p.BlocksCheckout {
		t.Fatalf("expected price 0 and a blocked product, got price=%d blocks=%v", p.PriceMinor, p.BlocksCheckout)
	}
	tasks := taskRaisedEvents(bus)
	if len(tasks) != 1 || tasks[0].Resolved || tasks[0].IssueID != "rule_price_missing" {
		t.Fatalf("expected a raised rule_price_missing task, got %+v", tasks)
	}
	if got := rebuildEvents(t, bus); len(got) == 0 {
		t.Error("expected a serving cache rebuild request")
	}
}

// The correction closes the task the bad price opened.
func TestOnPriceChanged_CorrectedPriceResolvesTask(t *testing.T) {
	ctx := context.Background()
	store := newFakeCatalogStore()
	bad := healthyProduct()
	bad.PriceMinor = 0
	_ = store.UpsertProduct(ctx, bad)
	bus := newFakeEventBus()
	svc := newSignalTestService(store, bus)
	// Score the broken state first so the product carries its issue.
	if err := svc.OnPriceChanged(ctx, "store-1", "merchant-1", PriceChangedPayload{SKU: "SKU-1", NewPrice: moneyPayload{AmountMinor: 0, Currency: "USD"}}); err != nil {
		t.Fatal(err)
	}
	bus.published = nil

	if err := svc.OnPriceChanged(ctx, "store-1", "merchant-1", PriceChangedPayload{SKU: "SKU-1", NewPrice: moneyPayload{AmountMinor: 1500, Currency: "USD"}}); err != nil {
		t.Fatalf("OnPriceChanged: %v", err)
	}

	tasks := taskRaisedEvents(bus)
	if len(tasks) != 1 || !tasks[0].Resolved || tasks[0].IssueID != "rule_price_missing" {
		t.Fatalf("expected the price task resolved, got %+v", tasks)
	}
	if p, _ := store.GetProduct(ctx, "store-1", "SKU-1"); p.BlocksCheckout {
		t.Error("product should no longer be blocked")
	}
}

// A stock change is re-scored too, and a SKU the catalog does not hold is
// dropped without error.
func TestOnInventoryChanged_RescoresAndIgnoresUnknownSKU(t *testing.T) {
	ctx := context.Background()
	store := newFakeCatalogStore()
	_ = store.UpsertProduct(ctx, healthyProduct())
	bus := newFakeEventBus()
	svc := newSignalTestService(store, bus)

	// Out of stock with no quantity: nothing says how many, so inventory is "missing".
	if err := svc.OnInventoryChanged(ctx, "store-1", "merchant-1", InventoryChangedPayload{SKU: "SKU-1", InStock: false, Quantity: nil}); err != nil {
		t.Fatalf("OnInventoryChanged: %v", err)
	}
	p, _ := store.GetProduct(ctx, "store-1", "SKU-1")
	if p.InStock || p.Quantity != nil {
		t.Fatalf("stock not applied: %+v", p)
	}
	found := false
	for _, i := range p.Issues {
		found = found || i.IssueID == "rule_inventory_missing"
	}
	if !found {
		t.Errorf("expected rule_inventory_missing after the stock change, got %+v", p.Issues)
	}

	if err := svc.OnInventoryChanged(ctx, "store-1", "merchant-1", InventoryChangedPayload{SKU: "NOPE"}); err != nil {
		t.Errorf("unknown SKU must be dropped without error, got %v", err)
	}
}
