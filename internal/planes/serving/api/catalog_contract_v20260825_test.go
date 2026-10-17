// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/negotiation"
	"github.com/sarvexlab/sarvex/internal/planes/serving"
	v20260408 "github.com/sarvexlab/sarvex/internal/spec/v20260408"
	v20260825 "github.com/sarvexlab/sarvex/internal/spec/v20260825"
)

// CatalogSearch/CatalogLookup render in the negotiated protocol version
// (through that generation's codec — version.go) rather than always rendering
// 2026-04-08 — the catalog half of specall.Default().Servable() actually growing to
// include 2026-08-25. These tests set the negotiation result directly on
// the Echo context (as the real WithNegotiation middleware would after a
// platform negotiates up to 2026-08-25 — see the negotiation package's
// TestMiddleware_CheckoutAndCatalogNegotiateIndependently for the full
// middleware-level proof) and assert the response actually decodes as
// v20260825's shape, not v20260408's.

func TestCatalogSearch_NegotiatedV20260825_ReturnsV20260825ShapedResponse(t *testing.T) {
	const storeID = "store-v2-search"
	cache := &stubServingCache{
		products: map[string]*serving.CacheProduct{
			"SKU-1": {SKU: "SKU-1", Title: "Widget", Price: serving.Money{AmountMinor: 1200, Currency: "USD"}, Stock: serving.StockLevel{InStock: true}},
		},
	}
	h := newTestHandler(nil, &stubConnector{}, cache, storeID)

	e := echo.New()
	req := newCatalogSearchRequest(t, v20260408.CatalogSearchRequest{})
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	negotiation.SetStoreProfile(c, &stubStoreProvider{storeID: storeID})
	negotiation.SetNegotiationResult(c, &negotiation.NegotiationResult{Version: "2026-08-25"})

	if err := h.CatalogSearch(c); err != nil {
		t.Fatalf("CatalogSearch returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp v20260825.CatalogSearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding as v20260825.CatalogSearchResponse: %v", err)
	}
	if len(resp.Products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(resp.Products))
	}
	got := resp.Products[0]
	if got.ID != "SKU-1" || got.Title != "Widget" {
		t.Fatalf("unexpected product: %+v", got)
	}
	if len(got.Variants) != 1 || got.Variants[0].Availability == nil || got.Variants[0].Availability.Status != v20260825.AvailabilityStatusInStock {
		t.Fatalf("expected in-stock v20260825 availability, got %+v", got.Variants)
	}
}

func TestCatalogSearch_NoNegotiation_StillReturns2026_04_08Shape(t *testing.T) {
	// Absent negotiation (no UCP-Agent header — e.g. an internal/simulator
	// call), the response must be unchanged from before this file existed:
	// always 2026-04-08.
	const storeID = "store-v1-default"
	cache := &stubServingCache{
		products: map[string]*serving.CacheProduct{"SKU-1": {SKU: "SKU-1"}},
	}
	h := newTestHandler(nil, &stubConnector{}, cache, storeID)

	e := echo.New()
	req := newCatalogSearchRequest(t, v20260408.CatalogSearchRequest{})
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	negotiation.SetStoreProfile(c, &stubStoreProvider{storeID: storeID})
	// No SetNegotiationResult call.

	if err := h.CatalogSearch(c); err != nil {
		t.Fatalf("CatalogSearch returned error: %v", err)
	}

	var resp v20260408.CatalogSearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding as v20260408.CatalogSearchResponse: %v", err)
	}
	if len(resp.Products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(resp.Products))
	}
}

func TestFilterByPriceRange(t *testing.T) {
	items := []*serving.CacheProduct{
		{SKU: "CHEAP", Price: serving.Money{AmountMinor: 500}},
		{SKU: "MID", Price: serving.Money{AmountMinor: 1500}},
		{SKU: "DEAR", Price: serving.Money{AmountMinor: 5000}},
	}
	minAmount := int64(1000)
	maxAmount := int64(2000)

	got := filterByPriceRange(items, &minAmount, &maxAmount)
	if len(got) != 1 || got[0].SKU != "MID" {
		t.Fatalf("min+max filter: got %+v, want only MID", got)
	}

	got = filterByPriceRange(items, &minAmount, nil)
	if len(got) != 2 {
		t.Fatalf("min-only filter: got %d items, want 2 (MID, DEAR)", len(got))
	}

	got = filterByPriceRange(items, nil, &maxAmount)
	if len(got) != 2 {
		t.Fatalf("max-only filter: got %d items, want 2 (CHEAP, MID)", len(got))
	}

	got = filterByPriceRange(items, nil, nil)
	if len(got) != 3 {
		t.Fatalf("no bounds: got %d items, want all 3 unchanged", len(got))
	}
}

func TestCatalogLookup_NegotiatedV20260825_ReturnsV20260825ShapedResponse(t *testing.T) {
	const storeID = "store-v2-lookup"
	cache := &stubServingCache{
		products: map[string]*serving.CacheProduct{
			"SKU-1": {SKU: "SKU-1", Title: "Widget", Stock: serving.StockLevel{InStock: false}},
		},
	}
	h := newTestHandler(nil, &stubConnector{}, cache, storeID)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/catalog/SKU-1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("itemId")
	c.SetParamValues("SKU-1")
	negotiation.SetStoreProfile(c, &stubStoreProvider{storeID: storeID})
	negotiation.SetNegotiationResult(c, &negotiation.NegotiationResult{Version: "2026-08-25"})

	if err := h.CatalogLookup(c); err != nil {
		t.Fatalf("CatalogLookup returned error: %v", err)
	}

	var product v20260825.Product
	if err := json.Unmarshal(rec.Body.Bytes(), &product); err != nil {
		t.Fatalf("decoding as v20260825.Product: %v", err)
	}
	if product.ID != "SKU-1" {
		t.Fatalf("unexpected product id %q", product.ID)
	}
	if len(product.Variants) != 1 || product.Variants[0].Availability == nil || product.Variants[0].Availability.Status != v20260825.AvailabilityStatusOutOfStock {
		t.Fatalf("expected out-of-stock v20260825 availability, got %+v", product.Variants)
	}
}
