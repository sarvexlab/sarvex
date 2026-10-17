// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/commerce/model"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/negotiation"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/serving"
)

// versionHarness drives the checkout, cart and order handlers at a chosen
// negotiated UCP generation, without the network negotiation itself.
type versionHarness struct {
	t        *testing.T
	h        *servingHandler
	conn     *stubConnector
	sessions serving.SessionStore
}

const versionStoreID = "store-version"

func newVersionHarness(t *testing.T) *versionHarness {
	t.Helper()
	sessions := serving.NewMemorySessionStore(time.Minute)
	t.Cleanup(sessions.Close)
	conn := &stubConnector{}
	h := newTestHandler(sessions, conn, nil, versionStoreID)
	h.cart = serving.NewCartService(serving.NewMemoryCartStore(time.Hour), h.deps.Registry, slog.Default())
	h.order = serving.NewOrderService(sessions)
	return &versionHarness{t: t, h: h, conn: conn, sessions: sessions}
}

// call runs handler as the negotiated generation `version` ("" = not negotiated).
func (v *versionHarness) call(handler func(echo.Context) error, version, method string, body any, params map[string]string) *httptest.ResponseRecorder {
	v.t.Helper()
	var r *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	} else {
		r = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, "/x", r)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e := echo.New()
	// Echo sizes its path-param storage from the routes registered on it, and
	// silently drops values beyond that, so give it a route with params.
	e.GET("/:a/:b/:c", func(echo.Context) error { return nil })
	c := e.NewContext(req, rec)
	negotiation.SetStoreProfile(c, &stubStoreProvider{storeID: versionStoreID})
	if version != "" {
		negotiation.SetNegotiationResult(c, &negotiation.NegotiationResult{Version: version})
	}
	var names, values []string
	for k, val := range params {
		names, values = append(names, k), append(values, val)
	}
	c.SetParamNames(names...)
	c.SetParamValues(values...)
	if err := handler(c); err != nil {
		var he *echo.HTTPError
		if errors.As(err, &he) {
			rec.Code = he.Code
		} else {
			v.t.Fatalf("handler error: %v", err)
		}
	}
	return rec
}

func mustObj(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("response is not a JSON object: %v\n%s", err, rec.Body.String())
	}
	return m
}

func obj(t *testing.T, m map[string]any, path ...string) map[string]any {
	t.Helper()
	cur := m
	for _, k := range path {
		next, ok := cur[k].(map[string]any)
		if !ok {
			t.Fatalf("no object at %v in %v", path, m)
		}
		cur = next
	}
	return cur
}

func canonicalCheckoutResponse(withConsent bool) *base.CheckoutResponse {
	yes := true
	resp := &base.CheckoutResponse{
		ID: "backend-1", Status: base.CheckoutStatusIncomplete, Currency: "USD",
		LineItems: []model.LineItem{{ID: "li_1", Quantity: 1, Item: model.Item{ID: "SKU-1", Title: "Mug"},
			Totals: model.Totals{{Type: model.TotalTypeTotal, Amount: 1000}}}},
		Totals: model.Totals{{Type: model.TotalTypeSubtotal, Amount: 1000}, {Type: model.TotalTypeTotal, Amount: 1000}},
		Fulfillment: &model.Fulfillment{Methods: []model.FulfillmentMethod{{
			ID: "method_pickup", Type: model.FulfillmentMethodTypePickup, LineItemIDs: []string{"li_1"},
			Destinations: []model.FulfillmentDestination{{Retail: &model.RetailLocation{ID: "pickup_store", Name: "Local pickup"}}},
		}}},
	}
	if withConsent {
		resp.Buyer = buyerWithConsent(&yes)
	}
	return resp
}

const consent25Body = `{"dev.ucp.consent.marketing":{"granted":true,"source":"platform","description":"Weekly offers"},"com.acme.loyalty":{"granted":true}}`

func checkoutBody25() map[string]any {
	var consent any
	_ = json.Unmarshal([]byte(consent25Body), &consent)
	return map[string]any{
		"line_items": []map[string]any{{"item": map[string]any{"id": "SKU-1"}, "quantity": 1}},
		"buyer":      map[string]any{"email": "b@example.com", "consent": consent},
		"fulfillment": map[string]any{"methods": []map[string]any{{
			"type": "pickup", "selected_destination_id": "pickup_store",
			"destinations": []map[string]any{{"id": "pickup_store", "type": "business_location", "name": "Local pickup"}},
			"groups":       []map[string]any{{"id": "g", "selected_option_id": "local_pickup"}},
		}}},
	}
}

func TestCheckout_V20260825_RequestAndResponseUseTheNegotiatedGeneration(t *testing.T) {
	v := newVersionHarness(t)
	v.conn.createResp = canonicalCheckoutResponse(true)

	rec := v.call(v.h.CheckoutCreate, "2026-08-25", http.MethodPost, checkoutBody25(), nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	// The connector saw the canonical request: booleans, and a pickup choice.
	req := v.conn.createReq
	if req.Buyer == nil || req.Buyer.Consent == nil || req.Buyer.Consent.Marketing == nil || !*req.Buyer.Consent.Marketing {
		t.Fatalf("connector buyer consent = %+v, want marketing=true as a canonical boolean", req.Buyer)
	}
	if req.Fulfillment == nil || req.Fulfillment.MethodType != "pickup" || req.Fulfillment.SelectedDestinationID != "pickup_store" {
		t.Errorf("connector fulfillment = %+v, want a pickup selection", req.Fulfillment)
	}

	// The response is in 2026-08-25's shape, echoing what the platform sent.
	m := mustObj(t, rec)
	dest := digObj(t, m, "fulfillment", "methods", 0, "destinations", 0)
	if dest["type"] != "business_location" {
		t.Errorf("destination = %v, want a business_location", dest)
	}
	consent := obj(t, m, "buyer", "consent")
	if obj(t, consent, "dev.ucp.consent.marketing")["description"] != "Weekly offers" {
		t.Errorf("the platform's consent wording was not echoed: %v", consent)
	}
	if _, ok := consent["com.acme.loyalty"]; !ok {
		t.Errorf("vendor consent purpose was dropped: %v", consent)
	}
	if _, legacy := consent["marketing"]; legacy {
		t.Errorf("2026-04-08 consent booleans leaked into a 2026-08-25 response: %v", consent)
	}

	// A GET negotiated at 2026-08-25 echoes the same detail from the session.
	id, _ := m["id"].(string)
	get := v.call(v.h.CheckoutGet, "2026-08-25", http.MethodGet, nil, map[string]string{"checkoutId": id})
	if get.Code != http.StatusOK {
		t.Fatalf("get status = %d: %s", get.Code, get.Body.String())
	}
	if obj(t, mustObj(t, get), "buyer", "consent", "dev.ucp.consent.marketing")["description"] != "Weekly offers" {
		t.Errorf("GET lost the platform's consent wording: %s", get.Body.String())
	}
}

func TestCheckout_NotNegotiated_StaysOn20260408(t *testing.T) {
	v := newVersionHarness(t)
	v.conn.createResp = canonicalCheckoutResponse(true)

	body := map[string]any{
		"line_items": []map[string]any{{"item": map[string]any{"id": "SKU-1"}, "quantity": 1}},
		"buyer":      map[string]any{"email": "b@example.com", "consent": map[string]any{"marketing": true}},
	}
	rec := v.call(v.h.CheckoutCreate, "", http.MethodPost, body, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	consent := obj(t, mustObj(t, rec), "buyer", "consent")
	if consent["marketing"] != true {
		t.Errorf("2026-04-08 consent = %v, want the boolean form", consent)
	}
	if _, wrong := consent["dev.ucp.consent.marketing"]; wrong {
		t.Errorf("a non-negotiated request got 2026-08-25's consent shape: %v", consent)
	}
	dest := digObj(t, mustObj(t, rec), "fulfillment", "methods", 0, "destinations", 0)
	if _, has := dest["type"]; has {
		t.Errorf("a 2026-04-08 destination has no type discriminator, got %v", dest)
	}
}

func TestCheckout_V20260825_RejectsMalformedConsent(t *testing.T) {
	v := newVersionHarness(t)
	body := checkoutBody25()
	body["buyer"] = map[string]any{"consent": map[string]any{"dev.ucp.consent.marketing": true}}
	if rec := v.call(v.h.CheckoutCreate, "2026-08-25", http.MethodPost, body, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a consent purpose that is not an object", rec.Code)
	}
}

func TestCart_V20260825(t *testing.T) {
	v := newVersionHarness(t)
	v.conn.cartCreateResp = &base.CartResponse{ID: "4242", Currency: "USD",
		LineItems: []model.LineItem{{ID: "li", Quantity: 1, Item: model.Item{ID: "SKU-1", Title: "Mug"},
			Totals: model.Totals{{Type: model.TotalTypeTotal, Amount: 1000}}}},
		Totals: model.Totals{{Type: model.TotalTypeTotal, Amount: 1000}}}

	body := map[string]any{"line_items": []map[string]any{{"item": map[string]any{"id": "SKU-1"}, "quantity": 1}}}
	rec := v.call(v.h.CartCreate, "2026-08-25", http.MethodPost, body, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("cart create at 2026-08-25: status = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" || created.ID == "4242" {
		t.Fatalf("cart id = %q, want an opaque Sarvex id", created.ID)
	}
	if got := v.call(v.h.CartGet, "2026-08-25", http.MethodGet, nil, map[string]string{"cartId": created.ID}); got.Code != http.StatusOK {
		t.Errorf("cart get at 2026-08-25: status = %d", got.Code)
	}
}

func TestOrder_V20260825(t *testing.T) {
	v := newVersionHarness(t)
	sess := &serving.CheckoutSession{
		SessionID: "sess-1", StoreID: versionStoreID, MerchantID: versionStoreID,
		BackendCheckoutID: "b1", State: serving.SessionStateCompleted, OrderID: "order-1",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
		LastResponse: &base.CheckoutResponse{
			ID: "sess-1", Status: base.CheckoutStatusCompleted, Currency: "USD",
			LineItems: []model.LineItem{{ID: "li_1", Item: model.Item{ID: "SKU-1", Title: "Mug"}, Quantity: 1}},
			Order:     &model.OrderConfirmation{ID: "order-1", PermalinkURL: "https://s.example/o/1"},
		},
	}
	if err := v.sessions.Create(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	for _, ver := range []string{"", "2026-04-08", "2026-08-25"} {
		rec := v.call(v.h.OrderGet, ver, http.MethodGet, nil, map[string]string{"orderId": "order-1"})
		if rec.Code != http.StatusOK {
			t.Errorf("order get (version %q): status = %d: %s", ver, rec.Code, rec.Body.String())
		}
		if mustObj(t, rec)["id"] != "order-1" {
			t.Errorf("order get (version %q): wrong order: %s", ver, rec.Body.String())
		}
	}
}

func TestPaymentCredentialsNeverReachTheSessionStore(t *testing.T) {
	v := newVersionHarness(t)
	v.conn.createResp = canonicalCheckoutResponse(false)

	body := map[string]any{
		"line_items": []map[string]any{{"item": map[string]any{"id": "SKU-1"}, "quantity": 1}},
		"payment": map[string]any{"instruments": []map[string]any{
			{"id": "i1", "handler_id": "com.google.pay", "type": "card", "selected": true,
				"credential": map[string]any{"type": "pan", "number": "4111111111111111", "cvc": "123"}},
			{"id": "i2", "handler_id": "com.google.pay", "type": "card",
				"credential": map[string]any{"type": "card", "number": "4242424242424242"}},
		}},
	}
	rec := v.call(v.h.CheckoutCreate, "2026-08-25", http.MethodPost, body, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	var id struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &id)
	sess, err := v.sessions.Get(context.Background(), versionStoreID, id.ID)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	stored, _ := json.Marshal(sess)
	for _, raw := range []string{"4111111111111111", "4242424242424242", `"cvc"`} {
		if bytes.Contains(stored, []byte(raw)) {
			t.Errorf("raw credential %q was persisted in the session store: %s", raw, stored)
		}
	}
	for _, sent := range []*base.CreateCheckoutRequest{v.conn.createReq} {
		fwd, _ := json.Marshal(sent)
		if bytes.Contains(fwd, []byte("4111111111111111")) || bytes.Contains(fwd, []byte("4242424242424242")) {
			t.Errorf("raw credential was forwarded to the connector: %s", fwd)
		}
	}
}

func buyerWithConsent(marketing *bool) *model.BuyerWithConsent {
	return &model.BuyerWithConsent{
		Buyer:   model.Buyer{Email: "b@example.com"},
		Consent: &model.Consent{Marketing: marketing},
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
