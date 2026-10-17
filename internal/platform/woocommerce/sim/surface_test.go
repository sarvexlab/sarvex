// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sim

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/simstore"
)

// newTestHandler serves the WooCommerce surface alone, over repo, the way the
// simulator's server registers it.
func newTestHandler(repo simstore.StoreRepository) http.Handler {
	mux := http.NewServeMux()
	Surface{}.Register(mux, simstore.Env{Stores: repo, BaseURL: "http://localhost:4106", Log: slog.Default()})
	return mux
}

func basicAuthHeader(key, secret string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(key+":"+secret))
}

func TestHTTP_RequiresAuth(t *testing.T) {
	repo := simstore.NewMemoryRepository()
	store := simstore.NewStore("store-1")
	store.Seed(3, false)
	repo.Upsert(context.Background(), store)

	ts := httptest.NewServer(newTestHandler(repo))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/stores/store-1/wp-json/wc/v3/products")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 without auth, got %d", resp.StatusCode)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/stores/store-1/wp-json/wc/v3/products", nil)
	req.Header.Set("Authorization", basicAuthHeader("wrong", "creds"))
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 with wrong creds, got %d", resp2.StatusCode)
	}
}

func TestHTTP_ListAndGetAndUpdateProduct(t *testing.T) {
	repo := simstore.NewMemoryRepository()
	store := simstore.NewStore("store-1")
	store.Seed(3, false)
	repo.Upsert(context.Background(), store)

	ts := httptest.NewServer(newTestHandler(repo))
	defer ts.Close()

	authedGet := func(path string) *http.Response {
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
		req.Header.Set("Authorization", basicAuthHeader(ConsumerKey("store-1"), ConsumerSecret("store-1")))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// List all products.
	resp := authedGet("/stores/store-1/wp-json/wc/v3/products")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var products []wcProduct
	if err := json.NewDecoder(resp.Body).Decode(&products); err != nil {
		t.Fatal(err)
	}
	if len(products) != 3 {
		t.Fatalf("want 3 products, got %d", len(products))
	}

	// FetchProduct-style lookup by sku (base.Connector.lookupBySKU's exact query shape).
	sku := products[0].SKU
	respSKU := authedGet("/stores/store-1/wp-json/wc/v3/products?sku=" + sku)
	defer respSKU.Body.Close()
	var bySKU []wcProduct
	if err := json.NewDecoder(respSKU.Body).Decode(&bySKU); err != nil {
		t.Fatal(err)
	}
	if len(bySKU) != 1 || bySKU[0].SKU != sku {
		t.Fatalf("sku lookup: want exactly one match for %s, got %+v", sku, bySKU)
	}

	// Paging (FetchCatalog's exact query shape).
	respPage := authedGet("/stores/store-1/wp-json/wc/v3/products?per_page=2&page=1")
	defer respPage.Body.Close()
	var page1 []wcProduct
	json.NewDecoder(respPage.Body).Decode(&page1)
	if len(page1) != 2 {
		t.Fatalf("want page size 2, got %d", len(page1))
	}

	// Get by numeric id.
	id := products[0].ID
	respGet := authedGet("/stores/store-1/wp-json/wc/v3/products/" + strconv.Itoa(id))
	defer respGet.Body.Close()
	if respGet.StatusCode != http.StatusOK {
		t.Fatalf("want 200 getting product by id, got %d", respGet.StatusCode)
	}

	// Update (remediation write-back path).
	body, _ := json.Marshal(map[string]any{"name": "Fixed Name", "regular_price": "9.99"})
	req, _ := http.NewRequest("PUT", ts.URL+"/stores/store-1/wp-json/wc/v3/products/"+strconv.Itoa(id), bytes.NewReader(body))
	req.Header.Set("Authorization", basicAuthHeader(ConsumerKey("store-1"), ConsumerSecret("store-1")))
	req.Header.Set("Content-Type", "application/json")
	respPut, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer respPut.Body.Close()
	if respPut.StatusCode != http.StatusOK {
		t.Fatalf("want 200 on update, got %d", respPut.StatusCode)
	}
	var updated wcProduct
	json.NewDecoder(respPut.Body).Decode(&updated)
	if updated.Name != "Fixed Name" {
		t.Errorf("want updated name, got %q", updated.Name)
	}
	if updated.Price != "9.99" {
		t.Errorf("want updated price 9.99, got %q", updated.Price)
	}

	// Persisted, not just returned in the response.
	reloaded, _ := repo.Get(context.Background(), "store-1")
	if reloaded.Products[sku] == nil {
		t.Fatal("expected sku still present after update")
	}
}

func TestHTTP_UnseededStoreReturns404NotPanic(t *testing.T) {
	repo := simstore.NewMemoryRepository()
	ts := httptest.NewServer(newTestHandler(repo))
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/stores/never-seeded/wp-json/wc/v3/products", nil)
	req.Header.Set("Authorization", basicAuthHeader(ConsumerKey("never-seeded"), ConsumerSecret("never-seeded")))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 for an unseeded store, got %d", resp.StatusCode)
	}
}

// TestHTTP_OrderLifecycle drives the /orders surface the way the real
// woocommerce.Connector does across a checkout session: POST (create,
// "pending"), GET (checkout get / poll), PUT with a shipping method
// (checkout update), and PUT with status=processing (checkout complete).
func TestHTTP_OrderLifecycle(t *testing.T) {
	repo := simstore.NewMemoryRepository()
	store := simstore.NewStore("store-1")
	store.Seed(2, false)
	repo.Upsert(context.Background(), store)

	ts := httptest.NewServer(newTestHandler(repo))
	defer ts.Close()

	auth := basicAuthHeader(ConsumerKey("store-1"), ConsumerSecret("store-1"))
	doReq := func(method, path string, body []byte) *http.Response {
		var r *bytes.Reader
		if body != nil {
			r = bytes.NewReader(body)
		} else {
			r = bytes.NewReader(nil)
		}
		req, _ := http.NewRequest(method, ts.URL+path, r)
		req.Header.Set("Authorization", auth)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	var products []wcProduct
	respList := doReq("GET", "/stores/store-1/wp-json/wc/v3/products", nil)
	json.NewDecoder(respList.Body).Decode(&products)
	respList.Body.Close()
	if len(products) != 2 {
		t.Fatalf("want 2 seeded products, got %d", len(products))
	}
	productID := products[0].ID

	// Create — matches CheckoutCreate/CartCreate's request shape.
	createBody, _ := json.Marshal(map[string]any{
		"status": "pending",
		"line_items": []map[string]any{
			{"product_id": productID, "quantity": 2},
		},
	})
	respCreate := doReq("POST", "/stores/store-1/wp-json/wc/v3/orders", createBody)
	defer respCreate.Body.Close()
	if respCreate.StatusCode != http.StatusCreated {
		t.Fatalf("want 201 creating order, got %d", respCreate.StatusCode)
	}
	var created wcOrder
	if err := json.NewDecoder(respCreate.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Status != "pending" {
		t.Errorf("want status pending, got %q", created.Status)
	}
	if len(created.LineItems) != 1 || created.LineItems[0].Quantity != 2 {
		t.Fatalf("unexpected line items: %+v", created.LineItems)
	}
	if created.Total == "" || created.Total == "0.00" {
		t.Errorf("want a non-zero computed total, got %q", created.Total)
	}

	// Get — matches CheckoutGet/idempotent-replay.
	respGet := doReq("GET", "/stores/store-1/wp-json/wc/v3/orders/"+strconv.Itoa(created.ID), nil)
	defer respGet.Body.Close()
	if respGet.StatusCode != http.StatusOK {
		t.Fatalf("want 200 getting order, got %d", respGet.StatusCode)
	}

	// Update — matches CheckoutUpdate's shipping selection; total must now
	// include the flat simulated shipping rate.
	updateBody, _ := json.Marshal(map[string]any{
		"shipping": map[string]any{"first_name": "Jane", "last_name": "Doe", "city": "Springfield"},
		"shipping_lines": []map[string]any{
			{"method_id": "flat_rate"},
		},
	})
	respUpdate := doReq("PUT", "/stores/store-1/wp-json/wc/v3/orders/"+strconv.Itoa(created.ID), updateBody)
	defer respUpdate.Body.Close()
	if respUpdate.StatusCode != http.StatusOK {
		t.Fatalf("want 200 updating order, got %d", respUpdate.StatusCode)
	}
	var updated wcOrder
	json.NewDecoder(respUpdate.Body).Decode(&updated)
	if updated.ShippingAddress.City != "Springfield" {
		t.Errorf("want shipping address persisted, got %+v", updated.ShippingAddress)
	}
	if len(updated.ShippingLines) != 1 || updated.ShippingLines[0].MethodID != "flat_rate" {
		t.Fatalf("want shipping line persisted, got %+v", updated.ShippingLines)
	}

	// Complete — matches CheckoutComplete: status -> processing.
	completeBody, _ := json.Marshal(map[string]any{"status": "processing"})
	respComplete := doReq("PUT", "/stores/store-1/wp-json/wc/v3/orders/"+strconv.Itoa(created.ID), completeBody)
	defer respComplete.Body.Close()
	if respComplete.StatusCode != http.StatusOK {
		t.Fatalf("want 200 completing order, got %d", respComplete.StatusCode)
	}
	var completed wcOrder
	json.NewDecoder(respComplete.Body).Decode(&completed)
	if completed.Status != "processing" {
		t.Errorf("want status processing after complete, got %q", completed.Status)
	}

	// Persisted, not just returned in the response.
	reloaded, _ := repo.Get(context.Background(), "store-1")
	if reloaded.Orders[strconv.Itoa(created.ID)] == nil {
		t.Fatal("expected order still present after update")
	}
	if reloaded.Orders[strconv.Itoa(created.ID)].Status != "processing" {
		t.Errorf("want persisted status processing, got %q", reloaded.Orders[strconv.Itoa(created.ID)].Status)
	}
}

func TestHTTP_CreateOrder_UnknownProduct_400(t *testing.T) {
	repo := simstore.NewMemoryRepository()
	store := simstore.NewStore("store-1")
	store.Seed(1, false)
	repo.Upsert(context.Background(), store)

	ts := httptest.NewServer(newTestHandler(repo))
	defer ts.Close()

	body, _ := json.Marshal(map[string]any{
		"line_items": []map[string]any{{"product_id": 999999, "quantity": 1}},
	})
	req, _ := http.NewRequest("POST", ts.URL+"/stores/store-1/wp-json/wc/v3/orders", bytes.NewReader(body))
	req.Header.Set("Authorization", basicAuthHeader(ConsumerKey("store-1"), ConsumerSecret("store-1")))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for unknown product_id, got %d", resp.StatusCode)
	}
}
