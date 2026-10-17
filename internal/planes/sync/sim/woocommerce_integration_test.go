// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sim_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/sarvexlab/sarvex/internal/pkg/simstore"
	ucptype "github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/resilience"
	syncsim "github.com/sarvexlab/sarvex/internal/planes/sync/sim"
	"github.com/sarvexlab/sarvex/internal/platform/woocommerce"
	woosim "github.com/sarvexlab/sarvex/internal/platform/woocommerce/sim"
)

// This test proves the persistent sim store is genuinely pullable by the
// real production woocommerce.Connector — not just shaped like it. It
// exercises the exact same code path CatalogImportHandler.RunImport and
// IngestionPipeline.HandleSignalReceived use in production
// (internal/planes/sync/catalog_import.go, ingestion.go), just with the
// real connector wired directly at this package's Server instead of a
// registry lookup.
type memRepo struct {
	stores map[string]*syncsim.Store
}

func newMemRepo() *memRepo { return &memRepo{stores: map[string]*syncsim.Store{}} }

func (m *memRepo) Get(_ context.Context, storeID string) (*syncsim.Store, error) {
	s, ok := m.stores[storeID]
	if !ok {
		return nil, syncsim.ErrStoreNotFound
	}
	return s, nil
}
func (m *memRepo) Upsert(_ context.Context, s *syncsim.Store) error {
	m.stores[s.StoreID] = s
	return nil
}
func (m *memRepo) List(_ context.Context) ([]*syncsim.Store, error) {
	out := make([]*syncsim.Store, 0, len(m.stores))
	for _, s := range m.stores {
		out = append(out, s)
	}
	return out, nil
}

func TestRealWooCommerceConnector_PullsFromSimStore(t *testing.T) {
	const storeID = "store-integration-1"

	repo := newMemRepo()
	store := syncsim.NewStore(storeID)
	store.Seed(5, true) // messy, so we can also assert the broken product's shape round-trips
	repo.Upsert(context.Background(), store)

	srv := syncsim.NewServer(repo, "http://sim-server.internal", nil, nil, nil, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	cfg := woocommerce.MerchantConfig{
		MerchantID:     "merchant-1",
		StoreID:        storeID,
		StoreBaseURL:   simstore.StoreBaseURL(ts.URL, storeID),
		ConsumerKey:    woosim.ConsumerKey(storeID),
		ConsumerSecret: woosim.ConsumerSecret(storeID),
		StoreCurrency:  "USD",
		APIVersion:     "v3",
		RequestTimeout: 5 * time.Second,
		MaxRetries:     1,
	}

	conn, err := woocommerce.NewConnector(cfg, nil, resilience.Config{}, nil)
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}

	ctx := context.Background()

	// FetchCatalog: the real initial-import path.
	entities, err := conn.FetchCatalog(ctx, storeID, 50)
	if err != nil {
		t.Fatalf("FetchCatalog: %v", err)
	}
	if len(entities) != 5 {
		t.Fatalf("want 5 catalog entities, got %d", len(entities))
	}

	var cleanSKU string
	for sku, p := range store.Products {
		if p.PriceMinor > 0 && p.Name != "" {
			cleanSKU = sku
			break
		}
	}
	if cleanSKU == "" {
		t.Fatal("expected at least one clean product from a messy seed")
	}

	// FetchProduct: the real per-signal pull path (ingestion.go's
	// handleProductChanged).
	entity, err := conn.FetchProduct(ctx, storeID, cleanSKU)
	if err != nil {
		t.Fatalf("FetchProduct(%s): %v", cleanSKU, err)
	}
	want := store.Products[cleanSKU]
	if entity.SKU != want.SKU || entity.Name != want.Name || entity.PriceMinor != want.PriceMinor {
		t.Fatalf("FetchProduct mismatch: got %+v, want sku/name/price from %+v", entity, want)
	}

	// FetchPricing / FetchInventory: the price/inventory-signal pull paths.
	pricing, err := conn.FetchPricing(ctx, storeID, cleanSKU)
	if err != nil {
		t.Fatalf("FetchPricing: %v", err)
	}
	if pricing.PriceMinor != want.PriceMinor {
		t.Fatalf("FetchPricing: got %d, want %d", pricing.PriceMinor, want.PriceMinor)
	}

	inv, err := conn.FetchInventory(ctx, storeID, cleanSKU)
	if err != nil {
		t.Fatalf("FetchInventory: %v", err)
	}
	if inv.InStock != want.InStock {
		t.Fatalf("FetchInventory.InStock: got %v, want %v", inv.InStock, want.InStock)
	}

	// UpdateProduct: the real remediation write-back path.
	if err := conn.UpdateProduct(ctx, storeID, cleanSKU, map[string]any{
		"name":           "Remediated Name",
		"regular_price":  "12.34",
		"stock_quantity": float64(7),
	}); err != nil {
		t.Fatalf("UpdateProduct: %v", err)
	}
	after := store.Products[cleanSKU]
	if after.Name != "Remediated Name" {
		t.Errorf("UpdateProduct: name not applied, got %q", after.Name)
	}
	if after.PriceMinor != 1234 {
		t.Errorf("UpdateProduct: price not applied, got %d", after.PriceMinor)
	}

	// Ping: connector health reporting.
	if _, err := conn.Ping(ctx, storeID); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

// Pickup end to end: the real connector detects Local Pickup on the sim store
// through the same REST calls it would make against WooCommerce, offers it in
// the checkout's fulfillment object, and places a pickup order that costs
// nothing to ship. A store without Local Pickup is not offered it.
func TestRealWooCommerceConnector_Pickup_DetectedOfferedAndOrdered(t *testing.T) {
	const storeID = "store-pickup-1"

	repo := newMemRepo()
	store := syncsim.NewStore(storeID)
	store.Seed(1, false)
	if err := repo.Upsert(context.Background(), store); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	var sku string
	for s := range store.Products {
		sku = s
	}

	srv := syncsim.NewServer(repo, "http://sim-server.internal", nil, nil, nil, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// The connector caches its view of the store's pickup setting, so each
	// phase of the test uses a fresh one.
	newConn := func() *woocommerce.Connector {
		conn, err := woocommerce.NewConnector(woocommerce.MerchantConfig{
			MerchantID:     "merchant-1",
			StoreID:        storeID,
			StoreBaseURL:   simstore.StoreBaseURL(ts.URL, storeID),
			ConsumerKey:    woosim.ConsumerKey(storeID),
			ConsumerSecret: woosim.ConsumerSecret(storeID),
			StoreCurrency:  "USD",
			APIVersion:     "v3",
			RequestTimeout: 5 * time.Second,
			MaxRetries:     1,
		}, nil, resilience.Config{}, nil)
		if err != nil {
			t.Fatalf("NewConnector: %v", err)
		}
		return conn
	}
	ctx := context.Background()
	items := []ucptype.LineItemCreateRequest{{Item: ucptype.ItemRef{ID: sku}, Quantity: 1}}
	hasPickup := func(c *ucptype.CheckoutResponse) bool {
		for _, m := range c.Fulfillment.Methods {
			if m.Type == model.FulfillmentMethodTypePickup {
				return true
			}
		}
		return false
	}

	// Off by default: a merchant that has not enabled pickup does not offer it.
	off, err := newConn().CheckoutCreate(ctx, storeID, &ucptype.CreateCheckoutRequest{LineItems: items})
	if err != nil {
		t.Fatalf("CheckoutCreate (pickup off): %v", err)
	}
	if hasPickup(off) {
		t.Fatalf("pickup offered although the store has not enabled it: %+v", off.Fulfillment)
	}

	store.LocalPickup = true
	conn := newConn()
	on, err := conn.CheckoutCreate(ctx, storeID, &ucptype.CreateCheckoutRequest{LineItems: items})
	if err != nil {
		t.Fatalf("CheckoutCreate (pickup on): %v", err)
	}
	if !hasPickup(on) {
		t.Fatalf("pickup enabled at the store but not offered: %+v", on.Fulfillment)
	}
	for _, m := range on.Fulfillment.Methods {
		if m.Type == model.FulfillmentMethodTypePickup {
			loc := m.Destinations[0].Retail
			if loc == nil || loc.Address == nil || loc.Address.StreetAddress != "1 Sim Street" {
				t.Errorf("pickup location = %+v, want the sim store's address", loc)
			}
		}
	}

	checkout, err := conn.CheckoutCreate(ctx, storeID, &ucptype.CreateCheckoutRequest{
		LineItems: items,
		Fulfillment: &ucptype.FulfillmentRequest{
			MethodType:            "pickup",
			SelectedDestinationID: "pickup_store",
			SelectedMethodID:      "local_pickup",
		},
	})
	if err != nil {
		t.Fatalf("CheckoutCreate (pickup selected): %v", err)
	}

	var shipping int64
	for _, tot := range checkout.Totals {
		if tot.Type == model.TotalTypeFulfillment {
			shipping = int64(tot.Amount)
		}
	}
	if shipping != 0 {
		t.Errorf("collecting in store must not be charged shipping, got %d", shipping)
	}
	var selected bool
	for _, m := range checkout.Fulfillment.Methods {
		if m.Type == model.FulfillmentMethodTypePickup && m.SelectedDestinationID != nil && *m.SelectedDestinationID == "pickup_store" {
			selected = true
		}
	}
	if !selected {
		t.Fatalf("pickup selection not reflected in the checkout: %+v", checkout.Fulfillment)
	}

	completed, err := conn.CheckoutComplete(ctx, storeID, checkout.ID, &ucptype.CompleteCheckoutRequest{})
	if err != nil {
		t.Fatalf("CheckoutComplete (pickup order): %v", err)
	}
	if completed.Status != ucptype.CheckoutStatusCompleted {
		t.Errorf("pickup order status = %q, want completed", completed.Status)
	}
}
