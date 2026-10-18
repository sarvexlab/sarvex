// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package shopify_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
	ucptype "github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/platform/frozen/shopify"
)

// newTestConnector creates a connector pointing at a mock Shopify server.
// The mock server URL replaces the real Shopify domain for test isolation.
func newTestConnector(t *testing.T, mockServer *httptest.Server) *shopify.Connector {
	t.Helper()

	// Strip https:// prefix — MerchantConfig expects a host only.
	host := strings.TrimPrefix(mockServer.URL, "https://")

	cfg := shopify.MerchantConfig{
		MerchantID:            "test_merchant",
		ShopDomain:            host,
		StorefrontAccessToken: "test-storefront-token",
		AdminAccessToken:      "test-admin-token",
		WebhookSecret:         "test-webhook-secret-32bytes-padded",
		ShopPayEnabled:        true,
		ShopID:                "123456789",
		StoreCurrency:         "USD",
		StoreBaseURL:          "https://test-store.example.com",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	conn, err := shopify.NewConnector(cfg, logger, shopify.WithHTTPClient(mockServer.Client()))
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	return conn
}

// ─── Catalog ──────────────────────────────────────────────────────────────────

func TestConnector_CatalogSearch(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertStorefrontToken(t, r)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"products": map[string]any{
					"edges": []map[string]any{
						{
							"cursor": "cursor1",
							"node": map[string]any{
								"id":               "gid://shopify/Product/1",
								"title":            "Blue T-Shirt",
								"description":      "A nice shirt",
								"availableForSale": true,
								"priceRange": map[string]any{
									"minVariantPrice": map[string]any{
										"amount": "29.99", "currencyCode": "USD",
									},
								},
								"variants": map[string]any{"edges": []any{}},
							},
						},
					},
					"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
				},
			},
		})
	}))
	defer server.Close()

	conn := newTestConnector(t, server)
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
	if item.PriceRange.Min.Currency != "USD" {
		t.Errorf("currency: %s", item.PriceRange.Min.Currency)
	}
}

// ─── Cart ─────────────────────────────────────────────────────────────────────

func TestConnector_CartCreate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertStorefrontToken(t, r)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"cartCreate": map[string]any{
					"cart": map[string]any{
						"id":          "gid://shopify/Cart/abc123",
						"checkoutUrl": "https://test-store.example.com/cart/abc123/checkout",
						"lines": map[string]any{
							"edges": []map[string]any{
								{
									"node": map[string]any{
										"id":       "gid://shopify/CartLine/1",
										"quantity": 2,
										"merchandise": map[string]any{
											"id":    "gid://shopify/ProductVariant/456",
											"title": "Blue / M",
											"price": map[string]any{"amount": "29.99", "currencyCode": "USD"},
											"product": map[string]any{
												"id":    "gid://shopify/Product/1",
												"title": "Blue T-Shirt",
											},
										},
										"cost": map[string]any{
											"subtotalAmount": map[string]any{"amount": "59.98", "currencyCode": "USD"},
											"totalAmount":    map[string]any{"amount": "59.98", "currencyCode": "USD"},
										},
									},
								},
							},
						},
						"cost": map[string]any{
							"subtotalAmount": map[string]any{"amount": "59.98", "currencyCode": "USD"},
							"totalTaxAmount": map[string]any{"amount": "5.99", "currencyCode": "USD"},
							"totalAmount":    map[string]any{"amount": "65.97", "currencyCode": "USD"},
						},
						"buyerIdentity": map[string]any{"email": "buyer@example.com"},
					},
					"userErrors": []any{},
				},
			},
		})
	}))
	defer server.Close()

	conn := newTestConnector(t, server)
	resp, err := conn.CartCreate(context.Background(), "test_instance", &ucptype.CreateCartRequest{
		LineItems: []ucptype.LineItemCreateRequest{
			{Item: ucptype.ItemRef{ID: "gid://shopify/ProductVariant/456"}, Quantity: 2},
		},
		Buyer: &model.Buyer{Email: "buyer@example.com"},
	})
	if err != nil {
		t.Fatalf("CartCreate: %v", err)
	}

	if resp.ID != "gid://shopify/Cart/abc123" {
		t.Errorf("cart ID: %s", resp.ID)
	}
	if len(resp.LineItems) != 1 {
		t.Fatalf("line items: %d", len(resp.LineItems))
	}
	if resp.LineItems[0].Quantity != 2 {
		t.Errorf("quantity: %d", resp.LineItems[0].Quantity)
	}

	// Verify totals are in minor units.
	totalsByType := make(map[string]int64)
	for _, total := range resp.Totals {
		totalsByType[string(total.Type)] = int64(total.Amount)
	}
	if totalsByType["subtotal"] != 5998 {
		t.Errorf("subtotal: %d, want 5998", totalsByType["subtotal"])
	}
	if totalsByType["total"] != 6597 {
		t.Errorf("total: %d, want 6597", totalsByType["total"])
	}
}

// ─── Checkout ─────────────────────────────────────────────────────────────────

func TestConnector_CheckoutCreate_Incomplete(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertStorefrontToken(t, r)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"checkoutCreate": map[string]any{
					"checkout": map[string]any{
						"id":               "gid://shopify/Checkout/xyz789",
						"webUrl":           "https://test-store.example.com/checkouts/xyz789",
						"email":            "",
						"currencyCode":     "USD",
						"requiresShipping": true,
						"subtotalPriceV2":  map[string]any{"amount": "50.00", "currencyCode": "USD"},
						"totalPriceV2":     map[string]any{"amount": "50.00", "currencyCode": "USD"},
						"lineItems": map[string]any{
							"edges": []map[string]any{
								{
									"node": map[string]any{
										"id":       "li_1",
										"title":    "Test Product",
										"quantity": 1,
										"variant": map[string]any{
											"id":      "gid://shopify/ProductVariant/789",
											"title":   "Default",
											"priceV2": map[string]any{"amount": "50.00", "currencyCode": "USD"},
											"product": map[string]any{"id": "p1", "title": "Test Product"},
										},
										"discountAllocations": []any{},
									},
								},
							},
						},
					},
					"checkoutUserErrors": []any{},
				},
			},
		})
	}))
	defer server.Close()

	conn := newTestConnector(t, server)
	resp, err := conn.CheckoutCreate(context.Background(), "test_instance", &ucptype.CreateCheckoutRequest{
		LineItems: []ucptype.LineItemCreateRequest{
			{Item: ucptype.ItemRef{ID: "gid://shopify/ProductVariant/789"}, Quantity: 1},
		},
	})
	if err != nil {
		t.Fatalf("CheckoutCreate: %v", err)
	}

	if resp.Status != ucptype.CheckoutStatusIncomplete {
		t.Errorf("status: %s, want incomplete", resp.Status)
	}

	// Must surface the missing email error for the platform to resolve.
	foundEmailError := false
	for _, msg := range resp.Messages {
		if errMsg, ok := msg.AsError(); ok && errMsg.Path == "$.buyer.email" && errMsg.Severity == model.MessageSeverityRecoverable {
			foundEmailError = true
		}
	}
	if !foundEmailError {
		t.Error("expected recoverable email error in messages")
	}
}

func TestConnector_CheckoutCreate_OutOfStock(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"checkoutCreate": map[string]any{
					"checkout": nil,
					"checkoutUserErrors": []map[string]any{
						{
							"code":    "NOT_ENOUGH_IN_STOCK",
							"field":   []string{},
							"message": "This product is not available.",
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	conn := newTestConnector(t, server)
	resp, err := conn.CheckoutCreate(context.Background(), "test_instance", &ucptype.CreateCheckoutRequest{
		LineItems: []ucptype.LineItemCreateRequest{
			{Item: ucptype.ItemRef{ID: "gid://shopify/ProductVariant/999"}, Quantity: 1},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Out of stock maps to recoverable per UCP spec standard errors.
	if len(resp.Messages) == 0 {
		t.Fatal("expected error messages for out of stock")
	}
	errMsg, ok := resp.Messages[0].AsError()
	if !ok || errMsg.Code != "out_of_stock" {
		t.Errorf("code: want out_of_stock, got %v", resp.Messages[0])
	}
}

// ─── Store-API pull (not supported) ────────────────────────────────────────────

func TestConnector_FetchProduct_NotSupported(t *testing.T) {
	conn := newTestConnector(t, httptest.NewTLSServer(http.NotFoundHandler()))

	if _, err := conn.FetchProduct(context.Background(), "test_instance", "sku-1"); err == nil {
		t.Fatal("expected not-supported error")
	}
}

// ─── Config validation ────────────────────────────────────────────────────────

func TestNewConnector_MissingRequiredFields(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cases := []struct {
		name string
		cfg  shopify.MerchantConfig
	}{
		{"missing merchant_id", shopify.MerchantConfig{
			ShopDomain: "x.myshopify.com", StorefrontAccessToken: "t", AdminAccessToken: "t",
		}},
		{"missing shop_domain", shopify.MerchantConfig{
			MerchantID: "m1", StorefrontAccessToken: "t", AdminAccessToken: "t",
		}},
		{"missing storefront_access_token", shopify.MerchantConfig{
			MerchantID: "m1", ShopDomain: "x.myshopify.com", AdminAccessToken: "t",
		}},
		{"missing admin_access_token", shopify.MerchantConfig{
			MerchantID: "m1", ShopDomain: "x.myshopify.com", StorefrontAccessToken: "t",
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := shopify.NewConnector(tc.cfg, logger)
			if err == nil {
				t.Error("expected validation error, got nil")
			}
		})
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func assertStorefrontToken(t *testing.T, r *http.Request) {
	t.Helper()
	if r.Header.Get("X-Shopify-Storefront-Access-Token") == "" {
		t.Error("X-Shopify-Storefront-Access-Token header is missing")
	}
}
