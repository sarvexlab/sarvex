// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	specall "github.com/sarvexlabs/sarvex.cp/internal/spec/all"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/golden"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/negotiation"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/serving"
	v20260408 "github.com/sarvexlabs/sarvex.cp/internal/spec/v20260408"
)

// notNegotiated is the golden directory for requests that carried no
// UCP-Agent and so were never negotiated.
const notNegotiated = "not-negotiated"

// TestCatalogGoldens freezes the catalog search and lookup responses in every
// servable generation (and for an un-negotiated request), so adding a
// generation cannot change what an existing one renders.
func TestCatalogGoldens(t *testing.T) {
	versions := []string{notNegotiated}
	for _, v := range specall.Default().Servable() {
		versions = append(versions, string(v))
	}
	for _, version := range versions {
		t.Run(version, func(t *testing.T) {
			const storeID = "store-golden"
			qty := 7
			cache := &stubServingCache{products: map[string]*serving.CacheProduct{
				"SKU-MUG": {
					SKU: "SKU-MUG", Title: "Mug", Description: "A ceramic mug.",
					Price:      serving.Money{AmountMinor: 1500, Currency: "USD"},
					Stock:      serving.StockLevel{InStock: true, Quantity: &qty},
					Attributes: map[string]string{"color": "blue"},
				},
				"SKU-CAP": {
					SKU: "SKU-CAP", Title: "Cap", Description: "A cotton cap.",
					Price: serving.Money{AmountMinor: 2500, Currency: "USD"},
					Stock: serving.StockLevel{InStock: false},
				},
			}}
			h := newTestHandler(nil, &stubConnector{}, cache, storeID)

			lo, hi := v20260408.Amount(1000), v20260408.Amount(2000)
			search := newCatalogSearchRequest(t, v20260408.CatalogSearchRequest{
				Filters: &v20260408.SearchFilters{Price: &v20260408.PriceFilter{Min: &lo, Max: &hi}},
			})
			golden.Assert(t, version+"/catalog_search", runCatalog(t, h, search, version, nil, h.CatalogSearch), nil)

			lookup := httptest.NewRequest(http.MethodGet, "/catalog/SKU-MUG", nil)
			golden.Assert(t, version+"/catalog_lookup",
				runCatalog(t, h, lookup, version, map[string]string{"itemId": "SKU-MUG"}, h.CatalogLookup), nil)
		})
	}
}

func runCatalog(t *testing.T, h *servingHandler, req *http.Request, version string, params map[string]string, handler func(echo.Context) error) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	e := echo.New()
	e.GET("/:a/:b/:c", func(echo.Context) error { return nil })
	c := e.NewContext(req, rec)
	negotiation.SetStoreProfile(c, &stubStoreProvider{storeID: "store-golden"})
	if version != notNegotiated {
		negotiation.SetNegotiationResult(c, &negotiation.NegotiationResult{Version: version})
	}
	var names, values []string
	for k, v := range params {
		names, values = append(names, k), append(values, v)
	}
	c.SetParamNames(names...)
	c.SetParamValues(values...)
	if err := handler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	return mustObj(t, rec)
}
