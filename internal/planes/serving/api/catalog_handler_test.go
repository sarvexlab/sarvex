// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/negotiation"
	"github.com/sarvexlab/sarvex/internal/planes/serving"
	v20260408 "github.com/sarvexlab/sarvex/internal/spec/v20260408"
)

// newCatalogSearchRequest builds a POST /catalog/search request with a JSON
// body — the reconciled ucp.dev transport (search_catalog is POST-only,
// see catalog_contract.go's doc comment), not GET+query params.
func newCatalogSearchRequest(t *testing.T, body v20260408.CatalogSearchRequest) *http.Request {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/catalog/search", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// waitForCatalogQueryEvent polls the stub publisher until a
// CatalogQueryRecordedEvent appears or timeout elapses — catalog query
// events are published fire-and-forget off the request goroutine
// (EmitCatalogQueryRecorded), same async pattern as the checkout lifecycle
// contract and telemetry.
func waitForCatalogQueryEvent(t *testing.T, events *stubEventPublisher, timeout time.Duration) *serving.CatalogQueryRecordedEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		events.mu.Lock()
		for _, ev := range events.published {
			if qev, ok := ev.(*serving.CatalogQueryRecordedEvent); ok {
				events.mu.Unlock()
				return qev
			}
		}
		events.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for CatalogQueryRecordedEvent to be published")
			return nil
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestCatalogSearch_EmitsQueryRecordedEvent(t *testing.T) {
	const storeID = "store-cat-1"
	cache := &stubServingCache{
		products: map[string]*serving.CacheProduct{
			"SKU-1": {SKU: "SKU-1"},
			"SKU-2": {SKU: "SKU-2"},
		},
	}
	h := newTestHandler(nil, &stubConnector{}, cache, storeID)
	events := h.deps.EventPublisher.(*stubEventPublisher)

	e := echo.New()
	req := newCatalogSearchRequest(t, v20260408.CatalogSearchRequest{Query: "some secret buyer phrase"})
	req.Header.Set("UCP-Agent", `profile="https://gemini.google.com/profile.json"`)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	negotiation.SetStoreProfile(c, &stubStoreProvider{storeID: storeID})

	if err := h.CatalogSearch(c); err != nil {
		t.Fatalf("CatalogSearch returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp v20260408.CatalogSearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(resp.Products) != 2 {
		t.Fatalf("expected 2 products in the spec-shaped response, got %d", len(resp.Products))
	}

	ev := waitForCatalogQueryEvent(t, events, time.Second)
	if ev.Kind != serving.CatalogQuerySearch {
		t.Errorf("expected kind=search, got %q", ev.Kind)
	}
	if ev.StoreID_ != storeID {
		t.Errorf("expected store_id %q, got %q", storeID, ev.StoreID_)
	}
	if ev.MerchantID_ != storeID {
		t.Errorf("expected merchant_id %q, got %q", storeID, ev.MerchantID_)
	}
	if ev.Surface != serving.SurfaceGoogleGemini {
		t.Errorf("expected surface derived from UCP-Agent host, got %q", ev.Surface)
	}
	if len(ev.RequestedProductIDs) != 2 {
		t.Fatalf("expected 2 matched product ids, got %v", ev.RequestedProductIDs)
	}

	// R12: never persist the raw free-text query string — assert it can't
	// appear anywhere in the published payload, not just that no field is
	// named for it.
	if strings.Contains(string(ev.Payload()), "secret") {
		t.Fatalf("raw query text leaked into event payload: %s", ev.Payload())
	}
}

func TestCatalogSearch_AppliesPriceFilterButAcceptsCategoriesWithoutApplying(t *testing.T) {
	const storeID = "store-cat-price"
	cache := &stubServingCache{
		products: map[string]*serving.CacheProduct{
			"SKU-CHEAP": {SKU: "SKU-CHEAP", Price: serving.Money{AmountMinor: 500, Currency: "USD"}},
			"SKU-MID":   {SKU: "SKU-MID", Price: serving.Money{AmountMinor: 1500, Currency: "USD"}},
			"SKU-DEAR":  {SKU: "SKU-DEAR", Price: serving.Money{AmountMinor: 5000, Currency: "USD"}},
		},
	}
	h := newTestHandler(nil, &stubConnector{}, cache, storeID)

	minPrice := v20260408.Amount(1000)
	maxPrice := v20260408.Amount(2000)
	e := echo.New()
	req := newCatalogSearchRequest(t, v20260408.CatalogSearchRequest{
		Filters: &v20260408.SearchFilters{
			Price:      &v20260408.PriceFilter{Min: &minPrice, Max: &maxPrice},
			Categories: []string{"shoes"}, // accepted on the wire, not applied — see catalog_contract.go
		},
	})
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	negotiation.SetStoreProfile(c, &stubStoreProvider{storeID: storeID})

	if err := h.CatalogSearch(c); err != nil {
		t.Fatalf("CatalogSearch returned error: %v", err)
	}

	var resp v20260408.CatalogSearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(resp.Products) != 1 || resp.Products[0].ID != "SKU-MID" {
		t.Fatalf("expected only the mid-priced product to survive the price filter, got %+v", resp.Products)
	}
}

func TestCatalogLookup_EmitsQueryRecordedEvent_OnHitAndMiss(t *testing.T) {
	const storeID = "store-cat-2"
	cache := &stubServingCache{
		products: map[string]*serving.CacheProduct{
			"SKU-1": {SKU: "SKU-1"},
		},
	}
	h := newTestHandler(nil, &stubConnector{}, cache, storeID)
	events := h.deps.EventPublisher.(*stubEventPublisher)

	e := echo.New()

	// Hit.
	req := httptest.NewRequest(http.MethodGet, "/catalog/SKU-1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("itemId")
	c.SetParamValues("SKU-1")
	negotiation.SetStoreProfile(c, &stubStoreProvider{storeID: storeID})

	if err := h.CatalogLookup(c); err != nil {
		t.Fatalf("CatalogLookup returned error: %v", err)
	}
	ev := waitForCatalogQueryEvent(t, events, time.Second)
	if ev.Kind != serving.CatalogQueryLookup {
		t.Errorf("expected kind=lookup, got %q", ev.Kind)
	}
	if len(ev.RequestedProductIDs) != 1 || ev.RequestedProductIDs[0] != "SKU-1" {
		t.Errorf("expected requested id SKU-1, got %v", ev.RequestedProductIDs)
	}

	// Miss — still recorded (an honest "agent asked for X" signal, not a
	// fabricated one), with the requested id and no matched product.
	events.mu.Lock()
	events.published = nil
	events.mu.Unlock()

	req2 := httptest.NewRequest(http.MethodGet, "/catalog/does-not-exist", nil)
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req2, rec2)
	c2.SetParamNames("itemId")
	c2.SetParamValues("does-not-exist")
	negotiation.SetStoreProfile(c2, &stubStoreProvider{storeID: storeID})

	err := h.CatalogLookup(c2)
	if err == nil {
		t.Fatal("expected not-found error")
	}
	ev2 := waitForCatalogQueryEvent(t, events, time.Second)
	if len(ev2.RequestedProductIDs) != 1 || ev2.RequestedProductIDs[0] != "does-not-exist" {
		t.Errorf("expected requested id recorded even on a miss, got %v", ev2.RequestedProductIDs)
	}
}
