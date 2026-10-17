// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package magento_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ucptype "github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/resilience"
	"github.com/sarvexlabs/sarvex.cp/internal/platform/magento"
)

// newTestConnector creates a connector pointing at a mock Magento server.
// credentials may be nil for tests that only exercise the checkout/catalog/
// cart/order methods (which use the baked-in cfg, not credentials).
func newTestConnector(t *testing.T, mockServer *httptest.Server, credentials ucptype.CredentialsProvider, guardCfg resilience.Config) *magento.Connector {
	t.Helper()

	cfg := magento.MerchantConfig{
		MerchantID:        "test_merchant",
		StoreID:           "test_instance",
		BaseURL:           mockServer.URL,
		AdminUser:         "admin@example.com",
		AdminPassword:     "password123",
		StoreCode:         "default",
		StoreCurrency:     "USD",
		PaymentMethodCode: "checkmo",
		WebhookSecret:     "test-webhook-secret",
		ProfileCacheTTL:   5 * time.Minute,
		TokenTTL:          4 * time.Hour,
		TokenTTLBuffer:    60 * time.Second,
		RequestTimeout:    10 * time.Second,
		MaxRetries:        3,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	conn, err := magento.NewConnector(cfg, credentials, guardCfg, logger)
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	return conn
}

// withTokenEndpoint wraps a handler so it also answers Magento's admin token
// endpoint, which every authenticated request (checkout/cart or store-API
// pull) triggers via ensureToken.
func withTokenEndpoint(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/integration/admin/token") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode("test-admin-token")
			return
		}
		next(w, r)
	}
}

// fakeCredentialsProvider resolves per-store credentials from an in-memory
// map, standing in for the control-plane connector service.
type fakeCredentialsProvider struct {
	byStore map[string]map[string]any
}

func newFakeCredentialsProvider() *fakeCredentialsProvider {
	return &fakeCredentialsProvider{byStore: map[string]map[string]any{}}
}

func (f *fakeCredentialsProvider) set(storeID, serverURL, adminUser, adminPassword string) {
	f.byStore[storeID] = map[string]any{
		"base_url":       serverURL,
		"admin_user":     adminUser,
		"admin_password": adminPassword,
		"store_code":     "default",
		"store_currency": "USD",
		"webhook_secret": "test-webhook-secret",
	}
}

func (f *fakeCredentialsProvider) GetStoreCredentials(ctx context.Context, storeID string) (map[string]any, error) {
	creds, ok := f.byStore[storeID]
	if !ok {
		return nil, fmt.Errorf("no credentials for store %s", storeID)
	}
	return creds, nil
}

func fastGuardConfig() resilience.Config {
	return resilience.Config{
		MaxRetries:              2,
		BaseBackoff:             time.Millisecond,
		MaxBackoff:              5 * time.Millisecond,
		BreakerFailureThreshold: 3,
		BreakerCooldown:         20 * time.Millisecond,
		RatePerSecond:           1000,
		RateBurst:               1000,
	}
}

// ─── Catalog ──────────────────────────────────────────────────────────────────

func TestConnector_CatalogSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBearerAuth(t, r)

		// Token endpoint
		if strings.Contains(r.URL.Path, "/integration/admin/token") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(`"test-admin-token"`)
			return
		}

		if !strings.Contains(r.URL.Path, "/products") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"id":     1,
					"sku":    "blue-shirt",
					"name":   "Blue T-Shirt",
					"price":  29.99,
					"status": 1,
					"custom_attributes": []map[string]any{
						{"attribute_code": "short_description", "value": "A nice shirt"},
						{"attribute_code": "url_key", "value": "blue-shirt"},
					},
					"media_gallery_entries": []map[string]any{
						{"file": "/b/l/blue-shirt.jpg"},
					},
				},
			},
			"total_count": 1,
		})
	}))
	defer server.Close()

	conn := newTestConnector(t, server, nil, resilience.Config{})
	resp, err := conn.CatalogSearch(context.Background(), "test_instance", &ucptype.CatalogSearchRequest{
		Query: "shirt",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("CatalogSearch: %v", err)
	}

	if len(resp.Products) != 1 {
		t.Fatalf("products: got %d, want 1", len(resp.Products))
	}
	item := resp.Products[0]
	if item.Title != "Blue T-Shirt" {
		t.Errorf("title: %s", item.Title)
	}
	if item.PriceRange.Min.Amount != 2999 {
		t.Errorf("price: %d, want 2999", item.PriceRange.Min.Amount)
	}
}

func TestConnector_CatalogLookup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBearerAuth(t, r)

		if strings.Contains(r.URL.Path, "/integration/admin/token") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(`"test-admin-token"`)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":     1,
			"sku":    "blue-shirt",
			"name":   "Blue T-Shirt",
			"price":  29.99,
			"status": 1,
			"custom_attributes": []map[string]any{
				{"attribute_code": "short_description", "value": "A nice shirt"},
			},
			"media_gallery_entries": []map[string]any{},
		})
	}))
	defer server.Close()

	conn := newTestConnector(t, server, nil, resilience.Config{})
	resp, err := conn.CatalogLookup(context.Background(), "test_instance", "blue-shirt")
	if err != nil {
		t.Fatalf("CatalogLookup: %v", err)
	}

	if resp.Product.Title != "Blue T-Shirt" {
		t.Errorf("title: %s", resp.Product.Title)
	}
	if resp.Product.PriceRange.Min.Amount != 2999 {
		t.Errorf("price: %d, want 2999", resp.Product.PriceRange.Min.Amount)
	}
}

// ─── Cart ────────────────────────────────────────────────────────────────────

func TestConnector_CartCreate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBearerAuth(t, r)

		if strings.Contains(r.URL.Path, "/integration/admin/token") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(`"test-admin-token"`)
			return
		}

		if strings.Contains(r.URL.Path, "/guest-carts") && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			// Magento returns a bare quoted string
			json.NewEncoder(w).Encode(`"test-cart-id-123"`)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "test-cart-id-123",
			"items": []any{},
			"totals": map[string]any{
				"grand_total":     0.00,
				"subtotal":        0.00,
				"shipping_amount": 0.00,
				"tax_amount":      0.00,
				"discount_amount": 0.00,
			},
		})
	}))
	defer server.Close()

	conn := newTestConnector(t, server, nil, resilience.Config{})
	resp, err := conn.CartCreate(context.Background(), "test_instance", &ucptype.CreateCartRequest{})
	if err != nil {
		t.Fatalf("CartCreate: %v", err)
	}

	if resp.ID == "" {
		t.Fatal("empty cart id")
	}
}

// ─── Store-API pull ─────────────────────────────────────────────────────────

func TestConnector_FetchProduct_Normalizes(t *testing.T) {
	server := httptest.NewServer(withTokenEndpoint(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/products/") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sku": "blue-shirt", "name": "Blue T-Shirt", "price": 29.99, "status": 1, "type_id": "simple",
			"extension_attributes": map[string]any{"stock_item": map[string]any{"is_in_stock": true, "qty": 12}},
		})
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "admin_a", "pw_a")
	conn := newTestConnector(t, server, creds, fastGuardConfig())

	entity, err := conn.FetchProduct(context.Background(), "store-a", "blue-shirt")
	if err != nil {
		t.Fatalf("FetchProduct: %v", err)
	}
	if entity.Name != "Blue T-Shirt" {
		t.Errorf("name: %s", entity.Name)
	}
	if entity.PriceMinor != 2999 {
		t.Errorf("price: %d, want 2999", entity.PriceMinor)
	}
	if !entity.InStock || entity.Quantity == nil || *entity.Quantity != 12 {
		t.Errorf("stock: inStock=%v qty=%v", entity.InStock, entity.Quantity)
	}
}

func TestConnector_FetchInventory_Normalizes(t *testing.T) {
	server := httptest.NewServer(withTokenEndpoint(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sku": "blue-shirt", "name": "Blue T-Shirt", "price": 29.99, "status": 1,
			"extension_attributes": map[string]any{"stock_item": map[string]any{"is_in_stock": false, "qty": 0}},
		})
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "admin_a", "pw_a")
	conn := newTestConnector(t, server, creds, fastGuardConfig())

	inv, err := conn.FetchInventory(context.Background(), "store-a", "blue-shirt")
	if err != nil {
		t.Fatalf("FetchInventory: %v", err)
	}
	if inv.InStock {
		t.Error("expected out of stock")
	}
	if inv.Quantity == nil || *inv.Quantity != 0 {
		t.Errorf("quantity: %v", inv.Quantity)
	}
}

func TestConnector_FetchPricing_Normalizes(t *testing.T) {
	server := httptest.NewServer(withTokenEndpoint(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sku": "blue-shirt", "name": "Blue T-Shirt", "price": 29.99, "status": 1})
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "admin_a", "pw_a")
	conn := newTestConnector(t, server, creds, fastGuardConfig())

	pricing, err := conn.FetchPricing(context.Background(), "store-a", "blue-shirt")
	if err != nil {
		t.Fatalf("FetchPricing: %v", err)
	}
	if pricing.PriceMinor != 2999 {
		t.Errorf("price: %d, want 2999", pricing.PriceMinor)
	}
	if pricing.Currency != "USD" {
		t.Errorf("currency: %s", pricing.Currency)
	}
}

func TestConnector_FetchCatalog_PagesThroughAllItems(t *testing.T) {
	server := httptest.NewServer(withTokenEndpoint(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("searchCriteria[current_page]") {
		case "1":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
				{"sku": "sku-1", "name": "One", "price": 1.00, "status": 1},
				{"sku": "sku-2", "name": "Two", "price": 2.00, "status": 1},
			}, "total_count": 3})
		case "2":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
				{"sku": "sku-3", "name": "Three", "price": 3.00, "status": 1},
			}, "total_count": 3})
		default:
			t.Errorf("unexpected page %q", r.URL.Query().Get("searchCriteria[current_page]"))
		}
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "admin_a", "pw_a")
	conn := newTestConnector(t, server, creds, fastGuardConfig())

	entities, err := conn.FetchCatalog(context.Background(), "store-a", 2)
	if err != nil {
		t.Fatalf("FetchCatalog: %v", err)
	}
	if len(entities) != 3 {
		t.Fatalf("entities: got %d, want 3", len(entities))
	}
}

func TestConnector_Ping(t *testing.T) {
	var fail atomic.Bool
	server := httptest.NewServer(withTokenEndpoint(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{}, "total_count": 0})
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "admin_a", "pw_a")
	conn := newTestConnector(t, server, creds, fastGuardConfig())

	status, err := conn.Ping(context.Background(), "store-a")
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if status != ucptype.HealthHealthy {
		t.Errorf("status: %s", status)
	}

	fail.Store(true)
	status, err = conn.Ping(context.Background(), "store-a")
	if err == nil {
		t.Fatal("expected error when store is failing")
	}
	if status == ucptype.HealthHealthy {
		t.Errorf("status: %s, want degraded or failed", status)
	}
}

func TestConnector_CredentialIsolation(t *testing.T) {
	serverA := httptest.NewServer(withTokenEndpoint(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sku": "SKU-A", "name": "Store A Product", "price": 10.00, "status": 1})
	}))
	defer serverA.Close()

	serverB := httptest.NewServer(withTokenEndpoint(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sku": "SKU-B", "name": "Store B Product", "price": 20.00, "status": 1})
	}))
	defer serverB.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", serverA.URL, "admin_a", "pw_a")
	creds.set("store-b", serverB.URL, "admin_b", "pw_b")
	conn := newTestConnector(t, serverA, creds, fastGuardConfig())

	entityA, err := conn.FetchProduct(context.Background(), "store-a", "SKU-A")
	if err != nil {
		t.Fatalf("FetchProduct store-a: %v", err)
	}
	entityB, err := conn.FetchProduct(context.Background(), "store-b", "SKU-B")
	if err != nil {
		t.Fatalf("FetchProduct store-b: %v", err)
	}
	if entityA.Name != "Store A Product" || entityB.Name != "Store B Product" {
		t.Fatalf("got mismatched entities: a=%+v b=%+v", entityA, entityB)
	}
}

func TestConnector_UnknownStoreReturnsCredentialsError(t *testing.T) {
	server := httptest.NewServer(withTokenEndpoint(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	conn := newTestConnector(t, server, creds, fastGuardConfig())

	_, err := conn.FetchProduct(context.Background(), "does-not-exist", "SKU-1")
	if err == nil {
		t.Fatal("expected error for unknown store")
	}
	var connErr *ucptype.ConnectorError
	if !errors.As(err, &connErr) {
		t.Fatalf("expected *base.ConnectorError, got %T: %v", err, err)
	}
	if connErr.Kind != ucptype.ConnectorErrCredentials {
		t.Errorf("kind = %s, want credentials", connErr.Kind)
	}
}

func TestConnector_FetchProduct_RetriesTransientFailureThenSucceeds(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(withTokenEndpoint(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sku": "SKU-1", "name": "Recovered", "price": 5.00, "status": 1})
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "admin_a", "pw_a")
	conn := newTestConnector(t, server, creds, fastGuardConfig())

	entity, err := conn.FetchProduct(context.Background(), "store-a", "SKU-1")
	if err != nil {
		t.Fatalf("expected eventual success after retries, got %v", err)
	}
	if entity.Name != "Recovered" {
		t.Errorf("unexpected entity: %+v", entity)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestConnector_FetchProduct_ExhaustedRetriesReturnTypedUnavailableError(t *testing.T) {
	server := httptest.NewServer(withTokenEndpoint(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "admin_a", "pw_a")
	conn := newTestConnector(t, server, creds, resilience.Config{
		MaxRetries:              1,
		BaseBackoff:             time.Millisecond,
		MaxBackoff:              time.Millisecond,
		BreakerFailureThreshold: 100, // keep the breaker out of this test
		RatePerSecond:           1000,
		RateBurst:               1000,
	})

	_, err := conn.FetchProduct(context.Background(), "store-a", "SKU-1")
	if err == nil {
		t.Fatal("expected error")
	}
	var connErr *ucptype.ConnectorError
	if !errors.As(err, &connErr) {
		t.Fatalf("expected *base.ConnectorError, got %T: %v", err, err)
	}
	if connErr.Kind != ucptype.ConnectorErrUnavailable {
		t.Errorf("kind = %s, want unavailable", connErr.Kind)
	}
}

func TestConnector_UpdateProduct(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(withTokenEndpoint(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.Contains(r.URL.Path, "/products/") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Errorf("unmarshal request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sku": "blue-shirt", "name": "Updated Name", "price": 19.99, "status": 1})
	}))
	defer server.Close()

	conn := newTestConnector(t, server, nil, resilience.Config{})
	err := conn.UpdateProduct(context.Background(), "test_instance", "blue-shirt", map[string]any{
		"name":           "Updated Name",
		"regular_price":  "19.99",
		"stock_quantity": float64(7),
	})
	if err != nil {
		t.Fatalf("UpdateProduct: %v", err)
	}

	product, ok := gotBody["product"].(map[string]any)
	if !ok {
		t.Fatalf("expected product payload, got %+v", gotBody)
	}
	if product["name"] != "Updated Name" {
		t.Errorf("name: %v", product["name"])
	}
	if product["price"] != 19.99 {
		t.Errorf("price: %v", product["price"])
	}
	extAttrs, ok := product["extension_attributes"].(map[string]any)
	if !ok {
		t.Fatalf("expected extension_attributes, got %+v", product)
	}
	stockItem, ok := extAttrs["stock_item"].(map[string]any)
	if !ok || stockItem["qty"] != 7.0 {
		t.Errorf("stock_item: %+v", extAttrs)
	}
}

func TestConnector_UpdateProduct_EmptyItemIDErrors(t *testing.T) {
	server := httptest.NewServer(withTokenEndpoint(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	conn := newTestConnector(t, server, nil, resilience.Config{})
	if err := conn.UpdateProduct(context.Background(), "test_instance", "", map[string]any{"name": "x"}); err == nil {
		t.Fatal("expected error for empty item id")
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func assertBearerAuth(t *testing.T, r *http.Request) {
	t.Helper()
	auth := r.Header.Get("Authorization")
	if auth == "" && !strings.Contains(r.URL.Path, "/integration/admin/token") {
		t.Error("missing Authorization header")
	}
}
