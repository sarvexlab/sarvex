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
	"github.com/sarvexlab/sarvex/internal/platform/magento"
	magentosim "github.com/sarvexlab/sarvex/internal/platform/magento/sim"
)

// TestRealMagentoConnector_PullsFromSimStore proves the persistent sim store
// is genuinely pullable by the real production magento.Connector — not just
// shaped like it — exercising the exact same store-API pull path
// CatalogImportHandler.RunImport and IngestionPipeline.HandleSignalReceived
// use in production, mirroring woocommerce_integration_test.go's
// TestRealWooCommerceConnector_PullsFromSimStore for the Magento surface
// added in http_magento.go.
func TestRealMagentoConnector_PullsFromSimStore(t *testing.T) {
	const storeID = "store-magento-integration-1"

	repo := newMemRepo()
	store := syncsim.NewStore(storeID)
	store.Seed(5, true) // messy, so we can also assert the broken product's shape round-trips
	if err := repo.Upsert(context.Background(), store); err != nil {
		t.Fatalf("seed upsert: %v", err)
	}

	srv := syncsim.NewServer(repo, "http://sim-server.internal", nil, nil, nil, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	cfg := magento.MerchantConfig{
		MerchantID:     "merchant-1",
		StoreID:        storeID,
		BaseURL:        simstore.StoreBaseURL(ts.URL, storeID),
		AdminUser:      magentosim.AdminUser(storeID),
		AdminPassword:  magentosim.AdminPassword(storeID),
		StoreCode:      magentosim.StoreCode,
		StoreCurrency:  "USD",
		WebhookSecret:  simstore.WebhookSecret(storeID),
		RequestTimeout: 5 * time.Second,
		MaxRetries:     1,
	}

	conn, err := magento.NewConnector(cfg, nil, resilience.Config{}, nil)
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

	// FetchProduct: the real per-signal pull path (ingestion.go's handleProductChanged).
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
	if after.StockQuantity == nil || *after.StockQuantity != 7 {
		t.Errorf("UpdateProduct: stock_quantity not applied, got %+v", after.StockQuantity)
	}

	// Ping: connector health reporting.
	if status, err := conn.Ping(ctx, storeID); err != nil || status != ucptype.HealthHealthy {
		t.Fatalf("Ping: status=%s err=%v", status, err)
	}

	// CredentialIsolation-equivalent: a second store must never see the
	// first store's admin token or catalog.
	const storeID2 = "store-magento-integration-2"
	store2 := syncsim.NewStore(storeID2)
	store2.Seed(2, false)
	if err := repo.Upsert(context.Background(), store2); err != nil {
		t.Fatalf("seed upsert store2: %v", err)
	}
	creds := &fakeMagentoCredentials{byStore: map[string]magento.MerchantConfig{
		storeID:  cfg,
		storeID2: {StoreID: storeID2, BaseURL: simstore.StoreBaseURL(ts.URL, storeID2), AdminUser: magentosim.AdminUser(storeID2), AdminPassword: magentosim.AdminPassword(storeID2), StoreCode: magentosim.StoreCode, StoreCurrency: "USD", WebhookSecret: simstore.WebhookSecret(storeID2)},
	}}
	sharedConn, err := magento.NewConnector(cfg, creds, resilience.Config{}, nil)
	if err != nil {
		t.Fatalf("NewConnector (shared): %v", err)
	}
	var sku2 string
	for sku := range store2.Products {
		sku2 = sku
		break
	}
	entity2, err := sharedConn.FetchProduct(ctx, storeID2, sku2)
	if err != nil {
		t.Fatalf("FetchProduct store2 via shared connector: %v", err)
	}
	if entity2.SKU != sku2 {
		t.Fatalf("FetchProduct store2: got sku %q, want %q", entity2.SKU, sku2)
	}
}

// TestRealMagentoConnector_ChecksOutAgainstSimStore proves the real
// magento.Connector's cart/checkout lifecycle — structurally different from
// WooCommerce's (Magento models checkout as a guest-cart, never a single
// order resource) — round-trips against http_magento.go's guest-cart
// surface end to end: create, add items, set shipping, complete, and poll
// the resulting order.
func TestRealMagentoConnector_ChecksOutAgainstSimStore(t *testing.T) {
	const storeID = "store-magento-checkout-1"

	repo := newMemRepo()
	store := syncsim.NewStore(storeID)
	store.Seed(3, false)
	if err := repo.Upsert(context.Background(), store); err != nil {
		t.Fatalf("seed upsert: %v", err)
	}

	srv := syncsim.NewServer(repo, "http://sim-server.internal", nil, nil, nil, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	var sku string
	for s := range store.Products {
		sku = s
		break
	}

	cfg := magento.MerchantConfig{
		MerchantID:        "merchant-1",
		StoreID:           storeID,
		BaseURL:           simstore.StoreBaseURL(ts.URL, storeID),
		AdminUser:         magentosim.AdminUser(storeID),
		AdminPassword:     magentosim.AdminPassword(storeID),
		StoreCode:         magentosim.StoreCode,
		StoreCurrency:     "USD",
		WebhookSecret:     simstore.WebhookSecret(storeID),
		PaymentMethodCode: "checkmo",
		RequestTimeout:    5 * time.Second,
		MaxRetries:        1,
	}

	conn, err := magento.NewConnector(cfg, nil, resilience.Config{}, nil)
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	ctx := context.Background()

	checkout, err := conn.CheckoutCreate(ctx, storeID, &ucptype.CreateCheckoutRequest{
		LineItems: []ucptype.LineItemCreateRequest{{Item: ucptype.ItemRef{ID: sku}, Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("CheckoutCreate: %v", err)
	}
	if checkout.ID == "" {
		t.Fatal("CheckoutCreate: empty checkout id")
	}
	if len(checkout.LineItems) != 1 || checkout.LineItems[0].Quantity != 2 {
		t.Fatalf("CheckoutCreate: unexpected line items %+v", checkout.LineItems)
	}

	updated, err := conn.CheckoutUpdate(ctx, storeID, checkout.ID, &ucptype.UpdateCheckoutRequest{
		Fulfillment: &ucptype.FulfillmentRequest{
			SelectedMethodID: "flatrate_flatrate",
			SelectedDestination: &model.PostalAddress{
				FirstName:       "Ada",
				LastName:        "Lovelace",
				StreetAddress:   "123 Analytical Engine Way",
				AddressLocality: "London",
				AddressRegion:   "LDN",
				PostalCode:      "SW1A 1AA",
				AddressCountry:  "GB",
				PhoneNumber:     "+441234567890",
			},
		},
	})
	if err != nil {
		t.Fatalf("CheckoutUpdate: %v", err)
	}
	if updated.Totals == nil {
		t.Fatal("CheckoutUpdate: expected totals after shipping selection")
	}

	fetched, err := conn.CheckoutGet(ctx, storeID, checkout.ID)
	if err != nil {
		t.Fatalf("CheckoutGet: %v", err)
	}
	if fetched.ID != checkout.ID {
		t.Fatalf("CheckoutGet: id mismatch got %q want %q", fetched.ID, checkout.ID)
	}
	if fetched.Fulfillment == nil || len(fetched.Fulfillment.Methods) == 0 {
		t.Errorf("CheckoutGet: expected an available shipping method now that an address is set, got %+v", fetched.Fulfillment)
	}

	completed, err := conn.CheckoutComplete(ctx, storeID, checkout.ID, &ucptype.CompleteCheckoutRequest{
		Buyer: &model.BuyerWithConsent{Buyer: model.Buyer{Email: "ada@example.com", FirstName: "Ada", LastName: "Lovelace"}},
	})
	if err != nil {
		t.Fatalf("CheckoutComplete: %v", err)
	}
	if completed.Status != ucptype.CheckoutStatusCompleted {
		t.Fatalf("CheckoutComplete: status = %s, want completed", completed.Status)
	}
	if completed.Order == nil || completed.Order.ID == "" {
		t.Fatal("CheckoutComplete: no order confirmation")
	}

	order, err := conn.OrderGet(ctx, storeID, completed.Order.ID)
	if err != nil {
		t.Fatalf("OrderGet: %v", err)
	}
	if order.Status != string(ucptype.CheckoutStatusCompleted) {
		t.Fatalf("OrderGet: status = %s, want %s", order.Status, ucptype.CheckoutStatusCompleted)
	}
}

// fakeMagentoCredentials resolves per-store credentials from an in-memory
// map, standing in for the control-plane connector service — used here only
// to prove one connector instance keeps each store's admin token isolated.
type fakeMagentoCredentials struct {
	byStore map[string]magento.MerchantConfig
}

func (f *fakeMagentoCredentials) GetStoreCredentials(_ context.Context, storeID string) (map[string]any, error) {
	cfg, ok := f.byStore[storeID]
	if !ok {
		return nil, syncsim.ErrStoreNotFound
	}
	return map[string]any{
		"base_url":       cfg.BaseURL,
		"admin_user":     cfg.AdminUser,
		"admin_password": cfg.AdminPassword,
		"store_code":     cfg.StoreCode,
		"store_currency": cfg.StoreCurrency,
		"webhook_secret": cfg.WebhookSecret,
	}, nil
}
