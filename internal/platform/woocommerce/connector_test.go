// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package woocommerce_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ucptype "github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/resilience"
	"github.com/sarvexlab/sarvex/internal/platform/woocommerce"
)

// newTestConnector creates a connector pointing at a mock WooCommerce server.
// credentials may be nil for tests that only exercise the checkout/catalog/
// cart/order methods (which use the baked-in cfg, not credentials).
func newTestConnector(t *testing.T, mockServer *httptest.Server, credentials ucptype.CredentialsProvider, guardCfg resilience.Config) *woocommerce.Connector {
	t.Helper()

	cfg := woocommerce.MerchantConfig{
		MerchantID:      "test_merchant",
		StoreBaseURL:    mockServer.URL,
		ConsumerKey:     "test-consumer-key",
		ConsumerSecret:  "test-consumer-secret",
		StoreCurrency:   "USD",
		APIVersion:      "v3",
		ProfileCacheTTL: 5 * time.Minute,
		RequestTimeout:  10 * time.Second,
		MaxRetries:      3,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	conn, err := woocommerce.NewConnector(cfg, credentials, guardCfg, logger)
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	return conn
}

// fakeCredentialsProvider resolves per-store credentials from an in-memory
// map, standing in for the control-plane connector service.
type fakeCredentialsProvider struct {
	byStore map[string]map[string]any
	calls   map[string]int
}

func newFakeCredentialsProvider() *fakeCredentialsProvider {
	return &fakeCredentialsProvider{byStore: map[string]map[string]any{}, calls: map[string]int{}}
}

func (f *fakeCredentialsProvider) set(storeID, serverURL, key, secret string) {
	f.byStore[storeID] = map[string]any{
		"store_base_url":  serverURL,
		"consumer_key":    key,
		"consumer_secret": secret,
		"store_currency":  "USD",
		"api_version":     "v3",
	}
}

func (f *fakeCredentialsProvider) GetStoreCredentials(ctx context.Context, storeID string) (map[string]any, error) {
	f.calls[storeID]++
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
		assertBasicAuth(t, r, "test-consumer-key", "test-consumer-secret")
		if !strings.Contains(r.URL.Path, "/products") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":                1,
				"name":              "Blue T-Shirt",
				"price":             "29.99",
				"in_stock":          true,
				"stock_status":      "instock",
				"short_description": "A nice shirt",
				"permalink":         "https://example.com/product/blue-shirt",
				"images": []map[string]any{
					{"src": "https://example.com/image.jpg"},
				},
			},
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
		assertBasicAuth(t, r, "test-consumer-key", "test-consumer-secret")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":                1,
			"name":              "Blue T-Shirt",
			"price":             "29.99",
			"in_stock":          true,
			"stock_status":      "instock",
			"short_description": "A nice shirt",
			"permalink":         "https://example.com/product/blue-shirt",
			"variations":        []int{},
			"images": []map[string]any{
				{"src": "https://example.com/image.jpg"},
			},
		})
	}))
	defer server.Close()

	conn := newTestConnector(t, server, nil, resilience.Config{})
	resp, err := conn.CatalogLookup(context.Background(), "test_instance", "1")
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
		assertBasicAuth(t, r, "test-consumer-key", "test-consumer-secret")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":             100,
			"status":         "pending",
			"line_items":     []any{},
			"billing":        map[string]any{},
			"shipping":       map[string]any{},
			"total":          "0.00",
			"subtotal":       "0.00",
			"shipping_total": "0.00",
			"tax_total":      "0.00",
			"discount_total": "0.00",
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

// ─── Store-API pull: normalisation ─────────────────────────────────────────────

func TestConnector_FetchProduct_Normalizes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBasicAuth(t, r, "ck_a", "cs_a")
		if r.URL.Query().Get("sku") != "SKU-1" {
			http.Error(w, "unexpected sku", http.StatusBadRequest)
			return
		}
		qty := 7
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":             1,
				"sku":            "SKU-1",
				"name":           "Blue T-Shirt",
				"price":          "29.99",
				"regular_price":  "29.99",
				"in_stock":       true,
				"stock_status":   "instock",
				"stock_quantity": qty,
				"type":           "simple",
				"slug":           "blue-t-shirt",
			},
		})
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "ck_a", "cs_a")
	conn := newTestConnector(t, server, creds, fastGuardConfig())

	entity, err := conn.FetchProduct(context.Background(), "store-a", "SKU-1")
	if err != nil {
		t.Fatalf("FetchProduct: %v", err)
	}
	if entity.SKU != "SKU-1" || entity.Name != "Blue T-Shirt" {
		t.Errorf("unexpected entity: %+v", entity)
	}
	if entity.PriceMinor != 2999 {
		t.Errorf("price_minor = %d, want 2999", entity.PriceMinor)
	}
	if entity.Currency != "USD" {
		t.Errorf("currency = %s, want USD", entity.Currency)
	}
	if !entity.InStock {
		t.Error("expected InStock = true")
	}
	if entity.Quantity == nil || *entity.Quantity != 7 {
		t.Errorf("quantity = %v, want 7", entity.Quantity)
	}
}

func TestConnector_FetchInventory_Normalizes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": 2, "sku": "SKU-2", "in_stock": false, "stock_status": "outofstock", "stock_quantity": nil},
		})
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "ck_a", "cs_a")
	conn := newTestConnector(t, server, creds, fastGuardConfig())

	inv, err := conn.FetchInventory(context.Background(), "store-a", "SKU-2")
	if err != nil {
		t.Fatalf("FetchInventory: %v", err)
	}
	if inv.InStock {
		t.Error("expected InStock = false")
	}
	if inv.Quantity != nil {
		t.Errorf("expected nil quantity (not managed), got %v", *inv.Quantity)
	}
}

func TestConnector_FetchPricing_Normalizes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": 3, "sku": "SKU-3", "price": "", "regular_price": "15.50"},
		})
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "ck_a", "cs_a")
	conn := newTestConnector(t, server, creds, fastGuardConfig())

	pricing, err := conn.FetchPricing(context.Background(), "store-a", "SKU-3")
	if err != nil {
		t.Fatalf("FetchPricing: %v", err)
	}
	if pricing.PriceMinor != 1550 {
		t.Errorf("price_minor = %d, want 1550 (falls back to regular_price)", pricing.PriceMinor)
	}
}

func TestConnector_FetchProduct_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "ck_a", "cs_a")
	conn := newTestConnector(t, server, creds, fastGuardConfig())

	_, err := conn.FetchProduct(context.Background(), "store-a", "MISSING")
	if err == nil {
		t.Fatal("expected not-found error")
	}
	var connErr *ucptype.ConnectorError
	if !errors.As(err, &connErr) {
		t.Fatalf("expected *base.ConnectorError, got %T: %v", err, err)
	}
	if connErr.Kind != ucptype.ConnectorErrNotFound {
		t.Errorf("kind = %s, want not_found", connErr.Kind)
	}
}

// ─── Store-API pull: credential isolation ──────────────────────────────────────

func TestConnector_CredentialIsolation(t *testing.T) {
	var serverASeenKey, serverBSeenKey string

	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverASeenKey = basicAuthUser(t, r)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "sku": "SKU-A", "name": "Store A Product", "price": "10.00"}})
	}))
	defer serverA.Close()

	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverBSeenKey = basicAuthUser(t, r)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{{"id": 2, "sku": "SKU-B", "name": "Store B Product", "price": "20.00"}})
	}))
	defer serverB.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", serverA.URL, "ck_a", "cs_a")
	creds.set("store-b", serverB.URL, "ck_b", "cs_b")
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
	if serverASeenKey != "ck_a" {
		t.Errorf("server A saw consumer key %q, want ck_a", serverASeenKey)
	}
	if serverBSeenKey != "ck_b" {
		t.Errorf("server B saw consumer key %q, want ck_b", serverBSeenKey)
	}
	if serverASeenKey == serverBSeenKey {
		t.Fatal("store B's call must never use store A's credentials")
	}
}

func TestConnector_UnknownStoreReturnsCredentialsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
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

// ─── Store-API pull: resilience, end to end ────────────────────────────────────

func TestConnector_FetchProduct_RetriesTransientFailureThenSucceeds(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "sku": "SKU-1", "name": "Recovered", "price": "5.00"}})
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "ck_a", "cs_a")
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "ck_a", "cs_a")
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

func TestConnector_FetchProduct_CircuitOpensThenRecoversAfterCooldown(t *testing.T) {
	var healthy atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "sku": "SKU-1", "name": "Back up", "price": "5.00"}})
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "ck_a", "cs_a")
	conn := newTestConnector(t, server, creds, resilience.Config{
		MaxRetries:              -1, // isolate breaker from retry
		BreakerFailureThreshold: 2,
		BreakerCooldown:         20 * time.Millisecond,
		RatePerSecond:           1000,
		RateBurst:               1000,
	})

	// Two failures trip the breaker.
	for i := 0; i < 2; i++ {
		if _, err := conn.FetchProduct(context.Background(), "store-a", "SKU-1"); err == nil {
			t.Fatalf("attempt %d: expected failure", i)
		}
	}

	_, err := conn.FetchProduct(context.Background(), "store-a", "SKU-1")
	var connErr *ucptype.ConnectorError
	if !errors.As(err, &connErr) {
		t.Fatalf("expected *base.ConnectorError, got %T: %v", err, err)
	}
	if connErr.Kind != ucptype.ConnectorErrCircuitOpen {
		t.Errorf("kind = %s, want circuit_open", connErr.Kind)
	}

	healthy.Store(true)
	time.Sleep(30 * time.Millisecond)

	entity, err := conn.FetchProduct(context.Background(), "store-a", "SKU-1")
	if err != nil {
		t.Fatalf("expected recovery after cooldown, got %v", err)
	}
	if entity.Name != "Back up" {
		t.Errorf("unexpected entity after recovery: %+v", entity)
	}
}

func TestConnector_Ping(t *testing.T) {
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "ck_a", "cs_a")
	conn := newTestConnector(t, server, creds, resilience.Config{
		MaxRetries:              -1, // disable retries: this test only cares about health status
		BreakerFailureThreshold: 100,
		RatePerSecond:           1000,
		RateBurst:               1000,
	})

	status, err := conn.Ping(context.Background(), "store-a")
	if err != nil || status != ucptype.HealthHealthy {
		t.Fatalf("expected healthy ping, got status=%s err=%v", status, err)
	}

	fail.Store(true)
	status, err = conn.Ping(context.Background(), "store-a")
	if err == nil || status != ucptype.HealthDegraded {
		t.Fatalf("expected degraded ping on failure, got status=%s err=%v", status, err)
	}
}

// ─── Store-API pull: paged FetchCatalog (integration) ──────────────────────────

func TestConnector_FetchCatalog_PagesThroughAllItems(t *testing.T) {
	const totalItems = 5
	const pageSize = 2

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBasicAuth(t, r, "ck_a", "cs_a")
		q := r.URL.Query()
		perPage := q.Get("per_page")
		page := q.Get("page")
		if perPage != fmt.Sprintf("%d", pageSize) {
			http.Error(w, "unexpected per_page", http.StatusBadRequest)
			return
		}
		pageNum := 1
		fmt.Sscanf(page, "%d", &pageNum)

		start := (pageNum - 1) * pageSize
		var items []map[string]any
		for i := start; i < start+pageSize && i < totalItems; i++ {
			items = append(items, map[string]any{
				"id":    i + 1,
				"sku":   fmt.Sprintf("SKU-%d", i+1),
				"name":  fmt.Sprintf("Product %d", i+1),
				"price": "9.99",
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items)
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "ck_a", "cs_a")
	conn := newTestConnector(t, server, creds, fastGuardConfig())

	entities, err := conn.FetchCatalog(context.Background(), "store-a", pageSize)
	if err != nil {
		t.Fatalf("FetchCatalog: %v", err)
	}
	if len(entities) != totalItems {
		t.Fatalf("got %d items, want %d", len(entities), totalItems)
	}
	for i, e := range entities {
		wantSKU := fmt.Sprintf("SKU-%d", i+1)
		if e.SKU != wantSKU {
			t.Errorf("item %d: sku = %s, want %s", i, e.SKU, wantSKU)
		}
	}
}

// TestConnector_FetchCatalog_BulkYieldsToPricedCall exercises the same yield
// semantics as resilience_test.go's Guard-level test, but through the real
// WooCommerce Connector: a concurrent FetchPricing call (priority) must be
// allowed to complete before a bulk FetchCatalog page fetch that is still
// waiting on the shared per-store rate limiter proceeds.
func TestConnector_FetchCatalog_BulkYieldsToPricedCall(t *testing.T) {
	priceStarted := make(chan struct{})
	releasePrice := make(chan struct{})
	var pageFetchedBeforePriceReleased atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()

		if sku := q.Get("sku"); sku != "" {
			close(priceStarted)
			<-releasePrice
			json.NewEncoder(w).Encode([]map[string]any{{"id": 99, "sku": sku, "name": "Priced", "price": "1.00"}})
			return
		}

		// Any /products page fetch received while the priced call is still
		// holding the in-flight marker is a yield violation.
		select {
		case <-releasePrice:
		default:
			select {
			case <-priceStarted:
				pageFetchedBeforePriceReleased.Store(true)
			default:
			}
		}
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "ck_a", "cs_a")
	conn := newTestConnector(t, server, creds, resilience.Config{
		MaxRetries:        0,
		RatePerSecond:     1000,
		RateBurst:         1000,
		BulkYieldInterval: 2 * time.Millisecond,
	})

	priceDone := make(chan error, 1)
	go func() {
		_, err := conn.FetchPricing(context.Background(), "store-a", "SKU-1")
		priceDone <- err
	}()
	<-priceStarted

	// Start the bulk catalog fetch while the priced call is still in
	// flight; it must not reach the server until the priced call finishes.
	catalogDone := make(chan error, 1)
	go func() {
		_, err := conn.FetchCatalog(context.Background(), "store-a", 10)
		catalogDone <- err
	}()

	time.Sleep(15 * time.Millisecond)
	close(releasePrice)

	if err := <-priceDone; err != nil {
		t.Fatalf("FetchPricing: %v", err)
	}
	if err := <-catalogDone; err != nil {
		t.Fatalf("FetchCatalog: %v", err)
	}
	if pageFetchedBeforePriceReleased.Load() {
		t.Fatal("bulk FetchCatalog page fetch proceeded while the priced call was in flight")
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func assertBasicAuth(t *testing.T, r *http.Request, key, secret string) {
	t.Helper()
	auth := r.Header.Get("Authorization")
	if auth == "" {
		t.Error("missing Authorization header")
	}
}

func basicAuthUser(t *testing.T, r *http.Request) string {
	t.Helper()
	user, _, ok := r.BasicAuth()
	if !ok {
		t.Fatal("missing basic auth")
	}
	return user
}

// ─── SKU lookup is by SKU, not by position ───────────────────────────────────

// A store (or proxy) that ignores ?sku= returns the whole catalog. The
// connector must pick the product with the requested SKU, not the first
// entry — otherwise a signal for one SKU ingests another product's data and a
// write-back edits the wrong product.
func TestConnector_SKULookup_IgnoresFirstResultWhenFilterIsIgnored(t *testing.T) {
	var puts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet: // ignores the sku filter and lists everything, other product first
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "sku": "OTHER", "name": "Other", "price": "1.00", "type": "simple"},
				{"id": 2, "sku": "SKU-2", "name": "Wanted", "price": "2.00", "type": "simple"},
			})
		case http.MethodPut:
			puts = append(puts, r.URL.Path)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 2, "sku": "SKU-2", "name": "Fixed"})
		}
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "ck_a", "cs_a")
	conn := newTestConnector(t, server, creds, fastGuardConfig())

	entity, err := conn.FetchProduct(context.Background(), "store-a", "SKU-2")
	if err != nil {
		t.Fatalf("FetchProduct: %v", err)
	}
	if entity.SKU != "SKU-2" || entity.Name != "Wanted" {
		t.Errorf("fetched %+v, want the SKU-2 product", entity)
	}

	if err := conn.UpdateProduct(context.Background(), "store-a", "SKU-2", map[string]any{"name": "Fixed"}); err != nil {
		t.Fatalf("UpdateProduct: %v", err)
	}
	if len(puts) != 1 || !strings.HasSuffix(puts[0], "/products/2") {
		t.Errorf("write-back went to %v, want /products/2 (the SKU-2 product)", puts)
	}
}

// With several results and none matching, the filter was ignored and the
// product is simply not there: not found, and nothing is written.
func TestConnector_SKULookup_NoMatchAmongManyIsNotFound(t *testing.T) {
	var puts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPut {
			puts++
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "sku": "A", "name": "A", "type": "simple"},
			{"id": 2, "sku": "B", "name": "B", "type": "simple"},
		})
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "ck_a", "cs_a")
	conn := newTestConnector(t, server, creds, fastGuardConfig())

	if _, err := conn.FetchProduct(context.Background(), "store-a", "MISSING"); err == nil {
		t.Error("expected not found")
	}
	if err := conn.UpdateProduct(context.Background(), "store-a", "MISSING", map[string]any{"name": "x"}); err == nil {
		t.Error("expected not found")
	}
	if puts != 0 {
		t.Errorf("nothing may be written for an unknown SKU, got %d PUT(s)", puts)
	}
}

// A lone non-matching result is accepted: the store resolved the SKU itself
// (a variation SKU answered with its parent product).
func TestConnector_SKULookup_SingleResultIsAccepted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 7, "sku": "PARENT", "name": "Parent", "type": "variable"}})
	}))
	defer server.Close()

	creds := newFakeCredentialsProvider()
	creds.set("store-a", server.URL, "ck_a", "cs_a")
	conn := newTestConnector(t, server, creds, fastGuardConfig())

	entity, err := conn.FetchProduct(context.Background(), "store-a", "PARENT-XS")
	if err != nil {
		t.Fatalf("FetchProduct: %v", err)
	}
	if entity.Name != "Parent" {
		t.Errorf("got %+v, want the parent product", entity)
	}
}
