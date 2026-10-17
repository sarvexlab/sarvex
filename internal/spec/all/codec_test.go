// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package all_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/types"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wireadapt"
	"github.com/sarvexlab/sarvex/internal/spec"
	"github.com/sarvexlab/sarvex/internal/spec/all"
	v20260408 "github.com/sarvexlab/sarvex/internal/spec/v20260408/release"
)

var reg = all.New().Codecs()

const (
	v0408 types.Version = "2026-04-08"
	v0825 types.Version = "2026-08-25"
)

func ptr[T any](v T) *T { return &v }

// checkoutWithPickupAndShipping is a canonical checkout as a connector builds
// it: a shipping method with a postal destination, a pickup method with a
// retail location, a described option, and buyer consent.
func checkoutWithPickupAndShipping() *base.CheckoutResponse {
	return &base.CheckoutResponse{
		ID:       "co_1",
		Status:   base.CheckoutStatusIncomplete,
		Currency: "USD",
		LineItems: []model.LineItem{{
			ID: "li_1", Quantity: 1,
			Item:   model.Item{ID: "sku", Title: "Mug", Price: ptr(model.Amount(1000))},
			Totals: model.Totals{{Type: model.TotalTypeSubtotal, Amount: 1000}, {Type: model.TotalTypeTotal, Amount: 1000}},
		}},
		Totals: model.Totals{{Type: model.TotalTypeSubtotal, Amount: 1000}, {Type: model.TotalTypeTotal, Amount: 1000}},
		Buyer: &model.BuyerWithConsent{
			Buyer:   model.Buyer{Email: "b@example.com"},
			Consent: &model.Consent{Marketing: ptr(true), SaleOfData: ptr(false)},
		},
		Fulfillment: &model.Fulfillment{Methods: []model.FulfillmentMethod{
			{
				ID: "method_1", Type: model.FulfillmentMethodTypeShipping, LineItemIDs: []string{"li_1"},
				Destinations: []model.FulfillmentDestination{{Shipping: &model.ShippingDestination{
					ID: "dest_1", PostalAddress: model.PostalAddress{StreetAddress: "1 Main", AddressCountry: "US"},
				}}},
				Groups: []model.FulfillmentGroup{{ID: "g1", LineItemIDs: []string{"li_1"}, Options: []model.FulfillmentOption{{
					ID: "flat_rate", Title: "Standard", Description: "3-5 days",
					Totals: model.Totals{{Type: model.TotalTypeTotal, Amount: 500}},
				}}}},
			},
			{
				ID: "method_pickup", Type: model.FulfillmentMethodTypePickup, LineItemIDs: []string{"li_1"},
				Destinations: []model.FulfillmentDestination{{Retail: &model.RetailLocation{
					ID: "pickup_store", Name: "Local pickup", Address: &model.PostalAddress{StreetAddress: "2 Store St"},
				}}},
				Groups: []model.FulfillmentGroup{{ID: "g2", LineItemIDs: []string{"li_1"}, Options: []model.FulfillmentOption{{
					ID: "local_pickup", Title: "Local pickup",
					Totals: model.Totals{{Type: model.TotalTypeTotal, Amount: 0}},
				}}}},
			},
		}},
	}
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, b)
	}
	return m
}

// 2026-04-08's wire form is the canonical body plus the generation's envelope.
func TestEncode_V20260408_IsTheCanonicalBodyPlusItsEnvelope(t *testing.T) {
	c := checkoutWithPickupAndShipping()
	canonical, _ := json.Marshal(c)
	got, err := reg.EncodeResponse(v0408, wireadapt.Checkout, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := decode(t, got)
	delete(body, "ucp")
	if g, w := mustJSON(t, body), mustJSON(t, decode(t, canonical)); g != w {
		t.Fatalf("2026-04-08 body differs from the canonical one\n got %s\nwant %s", g, w)
	}
}

// Every generation's response declares that generation in its envelope, for
// every capability the resource carries, on every resource. (A 2026-08-25
// response used to declare 2026-04-08: the connectors stamped the envelope.)
func TestEncode_EnvelopeDeclaresTheRenderingGeneration(t *testing.T) {
	canonical := map[wireadapt.Resource]any{
		wireadapt.Checkout: checkoutWithPickupAndShipping(),
		wireadapt.Cart:     &base.CartResponse{ID: "cart_1", Currency: "USD"},
		wireadapt.Order:    map[string]any{"id": "ord_1", "checkout_id": "co_1", "permalink_url": "https://shop.example/o/1", "line_items": []any{}, "fulfillment": map[string]any{}, "totals": []any{}, "currency": "USD"},
	}
	for _, v := range reg.Versions() {
		for r, c := range canonical {
			body, err := reg.EncodeResponse(v, r, c, nil)
			if err != nil {
				t.Fatalf("%s %s: %v", v, r, err)
			}
			var env struct {
				Version      string                         `json:"version"`
				Capabilities map[string][]map[string]string `json:"capabilities"`
			}
			if err := json.Unmarshal(mustRaw(t, decode(t, body)["ucp"]), &env); err != nil {
				t.Fatalf("%s %s: envelope: %v", v, r, err)
			}
			if env.Version != string(v) {
				t.Errorf("%s %s: envelope version %q", v, r, env.Version)
			}
			if len(env.Capabilities) != len(r.Capabilities()) {
				t.Errorf("%s %s: capabilities %v, want %v", v, r, env.Capabilities, r.Capabilities())
			}
			for name, entries := range env.Capabilities {
				if len(entries) != 1 || entries[0]["version"] != string(v) {
					t.Errorf("%s %s: %s declared as %v", v, r, name, entries)
				}
			}
		}
	}
}

func mustRaw(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestEncode_V20260825_Checkout(t *testing.T) {
	body, err := reg.EncodeResponse(v0825, wireadapt.Checkout, checkoutWithPickupAndShipping(), nil)
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	m := decode(t, body)

	shipDest := digObj(t, m, "fulfillment", "methods", 0, "destinations", 0)
	pickDest := digObj(t, m, "fulfillment", "methods", 1, "destinations", 0)
	if shipDest["type"] != "shipping_address" {
		t.Errorf("shipping destination type = %v, want shipping_address", shipDest["type"])
	}
	if pickDest["type"] != "business_location" || pickDest["id"] != "pickup_store" || pickDest["name"] != "Local pickup" {
		t.Errorf("pickup destination = %v, want a business_location with its id and name", pickDest)
	}

	opt := digObj(t, m, "fulfillment", "methods", 0, "groups", 0, "options", 0)
	if d, ok := opt["description"].(map[string]any); !ok || d["plain"] != "3-5 days" {
		t.Errorf("option description = %v, want {plain: ...} (a Description object in 2026-08-25)", opt["description"])
	}

	consent := digObj(t, m, "buyer", "consent")
	mk := digObj(t, consent, "dev.ucp.consent.marketing")
	sale := digObj(t, consent, "dev.ucp.consent.sale_or_sharing")
	if mk["granted"] != true || sale["granted"] != false {
		t.Errorf("consent = %v, want marketing granted and sale_or_sharing denied", consent)
	}
	for k := range consent {
		e := digObj(t, consent, k)
		if e["source"] == "" || e["source"] == nil || e["description"] == "" || e["description"] == nil {
			t.Errorf("consent purpose %s lacks the source/description a 2026-08-25 response requires: %v", k, e)
		}
	}
	if _, legacy := consent["marketing"]; legacy {
		t.Error("the 2026-04-08 boolean field leaked into a 2026-08-25 response")
	}
}

func TestEncode_V20260825_ConsentKeepsWhatThePlatformSentWhenItAgrees(t *testing.T) {
	sent := json.RawMessage(`{
		"dev.ucp.consent.marketing": {"granted": true, "source": "platform", "description": "Weekly offers",
			"segments": {"dev.ucp.consent.marketing.email": {"granted": true, "source": "platform"}}},
		"com.acme.loyalty": {"granted": true}
	}`)
	body, err := reg.EncodeResponse(v0825, wireadapt.Checkout, checkoutWithPickupAndShipping(), func() json.RawMessage { return sent })
	if err != nil {
		t.Fatal(err)
	}
	consent := digObj(t, decode(t, body), "buyer", "consent")

	mk := digObj(t, consent, "dev.ucp.consent.marketing")
	if mk["description"] != "Weekly offers" || mk["segments"] == nil {
		t.Errorf("the platform's own description and segments were lost: %v", mk)
	}
	vendor := digObj(t, consent, "com.acme.loyalty")
	if vendor["source"] != "platform" || vendor["description"] == "" {
		t.Errorf("a vendor purpose must be echoed with the required fields filled in: %v", vendor)
	}

	// If the merchant answered differently than the platform stated, the
	// merchant's value wins and the platform's stale detail is not echoed.
	disagree := json.RawMessage(`{"dev.ucp.consent.marketing": {"granted": false, "description": "stale"}}`)
	body, err = reg.EncodeResponse(v0825, wireadapt.Checkout, checkoutWithPickupAndShipping(), func() json.RawMessage { return disagree })
	if err != nil {
		t.Fatal(err)
	}
	mk = digObj(t, decode(t, body), "buyer", "consent", "dev.ucp.consent.marketing")
	if mk["granted"] != true || mk["description"] == "stale" {
		t.Errorf("merchant value must win over a disagreeing platform value: %v", mk)
	}
}

func TestEncode_V20260825_CartAndOrder(t *testing.T) {
	cart := &base.CartResponse{
		ID: "cart_1", Currency: "USD",
		LineItems: []model.LineItem{{ID: "li", Quantity: 1, Item: model.Item{ID: "sku", Title: "Mug"},
			Totals: model.Totals{{Type: model.TotalTypeTotal, Amount: 1000}}}},
		Totals: model.Totals{{Type: model.TotalTypeSubtotal, Amount: 1000}, {Type: model.TotalTypeTotal, Amount: 1000}},
	}
	if _, err := reg.EncodeResponse(v0825, wireadapt.Cart, cart, nil); err != nil {
		t.Errorf("cart: %v", err)
	}

	order := &model.Order{
		ID: "o1", CheckoutID: "co_1", PermalinkURL: "https://s.example/o/1", Currency: "USD",
		LineItems:   []model.OrderLineItem{{ID: "li", Item: model.Item{ID: "sku", Title: "Mug"}, Quantity: model.OrderLineItemQuantity{Total: 1}}},
		Fulfillment: &model.OrderFulfillment{},
		Totals:      model.Totals{{Type: model.TotalTypeTotal, Amount: 1000}},
	}
	if _, err := reg.EncodeResponse(v0825, wireadapt.Order, order, nil); err != nil {
		t.Errorf("order: %v", err)
	}
}

func TestDecodeRequest_V20260408_IsUntouched(t *testing.T) {
	in := []byte(`{"buyer":{"consent":{"marketing":true}}}`)
	out, extras, err := reg.DecodeRequest(v0408, in)
	if err != nil || string(out) != string(in) || extras.BuyerConsent != nil {
		t.Fatalf("2026-04-08 request must pass through: %s %+v %v", out, extras, err)
	}
}

func TestDecodeRequest_V20260825_ConsentBecomesCanonicalBooleans(t *testing.T) {
	in := []byte(`{"line_items":[],"buyer":{"email":"b@example.com","consent":{
		"dev.ucp.consent.marketing":{"granted":true,"source":"platform","description":"d"},
		"dev.ucp.consent.analytics":{"granted":false},
		"dev.ucp.consent.sale_or_sharing":{"granted":false},
		"com.acme.loyalty":{"granted":true}}}}`)
	out, extras, err := reg.DecodeRequest(v0825, in)
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}

	var req struct {
		Buyer model.BuyerWithConsent `json:"buyer"`
	}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("canonical body must bind to the canonical request: %v\n%s", err, out)
	}
	c := req.Buyer.Consent
	if c == nil || c.Marketing == nil || !*c.Marketing || c.Analytics == nil || *c.Analytics || c.SaleOfData == nil || *c.SaleOfData {
		t.Fatalf("canonical consent = %+v, want marketing=true analytics=false sale_of_data=false", c)
	}
	if c.Preferences != nil {
		t.Error("a purpose the platform did not state must stay unstated, not false")
	}
	if !strings.Contains(string(extras.BuyerConsent), "com.acme.loyalty") {
		t.Errorf("the original consent, vendor purposes included, must be kept for echoing: %s", extras.BuyerConsent)
	}
}

func TestDecodeRequest_V20260825_RejectsMalformedConsent(t *testing.T) {
	for name, body := range map[string]string{
		"not a map":          `{"buyer":{"consent":true}}`,
		"purpose not object": `{"buyer":{"consent":{"dev.ucp.consent.marketing":true}}}`,
		"missing granted":    `{"buyer":{"consent":{"dev.ucp.consent.marketing":{"source":"platform"}}}}`,
	} {
		if _, _, err := reg.DecodeRequest(v0825, []byte(body)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestDecodeRequest_V20260825_NoBuyerNoConsentIsFine(t *testing.T) {
	in := []byte(`{"line_items":[{"item":{"id":"sku"},"quantity":1}]}`)
	out, extras, err := reg.DecodeRequest(v0825, in)
	if err != nil || extras.BuyerConsent != nil {
		t.Fatalf("err=%v extras=%+v", err, extras)
	}
	if !strings.Contains(string(out), `"line_items"`) {
		t.Errorf("body mangled: %s", out)
	}
}

// dig walks a decoded JSON value by object keys (string) and array indices
// (int), failing the test if the shape is not as expected.
func dig(t *testing.T, v any, path ...any) any {
	t.Helper()
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				t.Fatalf("want an object at %q, got %T", k, v)
			}
			v = m[k]
		case int:
			a, ok := v.([]any)
			if !ok || k >= len(a) {
				t.Fatalf("want an array with index %d, got %T", k, v)
			}
			v = a[k]
		default:
			t.Fatalf("bad path element %v", p)
		}
	}
	return v
}

func digObj(t *testing.T, v any, path ...any) map[string]any {
	t.Helper()
	m, ok := dig(t, v, path...).(map[string]any)
	if !ok {
		t.Fatalf("want an object at %v", path)
	}
	return m
}

func TestRegistry_UnknownVersionIsAnError(t *testing.T) {
	if _, err := reg.EncodeResponse("1999-01-01", wireadapt.Checkout, map[string]any{}, nil); err == nil {
		t.Error("encoding for an unregistered version must fail, not fall back to another generation")
	}
	if _, _, err := reg.DecodeRequest("1999-01-01", []byte(`{}`)); err == nil {
		t.Error("decoding for an unregistered version must fail")
	}
}

func TestRegistry_DuplicateVersionPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("registering one generation twice must panic")
		}
	}()
	spec.NewRegistry(v20260408.New(), v20260408.New())
}

// Every served generation reads its own catalog search_request into the same
// query, and renders a page and a product in its own shape: the product's one
// variant carries the SKU, price and availability, and the page carries the
// cursor.
func TestCatalog_EveryGeneration(t *testing.T) {
	body := []byte(`{"query":"mug","pagination":{"cursor":"c1","limit":5},"filters":{"categories":["kitchen"],"price":{"min":1000,"max":2000}}}`)
	p := model.CatalogProduct{SKU: "SKU-1", Title: "Mug", Description: "A mug", PriceMinor: 1500, Currency: "USD", InStock: true}

	for _, v := range reg.Versions() {
		t.Run(string(v), func(t *testing.T) {
			q, err := reg.DecodeCatalogSearch(v, body)
			if err != nil {
				t.Fatal(err)
			}
			if q.Query != "mug" || q.Cursor != "c1" || q.Limit != 5 || *q.PriceMin != 1000 || *q.PriceMax != 2000 {
				t.Errorf("query = %+v", q)
			}
			if _, err := reg.DecodeCatalogSearch(v, []byte(`{"query":`)); err == nil {
				t.Error("a malformed search_request decoded")
			}

			out, err := reg.EncodeCatalogSearch(v, model.CatalogPage{Products: []model.CatalogProduct{p}, Cursor: "c2", HasMore: true})
			if err != nil {
				t.Fatal(err)
			}
			var page struct {
				Products   []json.RawMessage `json:"products"`
				Pagination struct {
					Cursor      string `json:"cursor"`
					HasNextPage bool   `json:"has_next_page"`
				} `json:"pagination"`
			}
			mustDecode(t, out, &page)
			if len(page.Products) != 1 || page.Pagination.Cursor != "c2" || !page.Pagination.HasNextPage {
				t.Errorf("page = %s", out)
			}

			one, err := reg.EncodeCatalogProduct(v, p)
			if err != nil {
				t.Fatal(err)
			}
			if string(one) != string(page.Products[0]) {
				t.Errorf("lookup and search render the product differently:\n%s\n%s", one, page.Products[0])
			}
			var prod struct {
				ID       string `json:"id"`
				Variants []struct {
					SKU   string `json:"sku"`
					Price struct {
						Amount int64 `json:"amount"`
					} `json:"price"`
					Availability struct {
						Available bool   `json:"available"`
						Status    string `json:"status"`
					} `json:"availability"`
				} `json:"variants"`
			}
			mustDecode(t, one, &prod)
			if prod.ID != "SKU-1" || len(prod.Variants) != 1 || prod.Variants[0].SKU != "SKU-1" ||
				prod.Variants[0].Price.Amount != 1500 || !prod.Variants[0].Availability.Available ||
				prod.Variants[0].Availability.Status != "in_stock" {
				t.Errorf("product = %s", one)
			}
		})
	}
}

func mustDecode(t *testing.T, b []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
}
