// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// code performs the request and returns its status, closing the body.
func (s *servingTestServer) code(t *testing.T, method, path string, body any) int {
	t.Helper()
	resp := s.do(t, method, path, body)
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// into performs the request, decodes the body into dst and returns the status.
func (s *servingTestServer) into(t *testing.T, method, path string, body, dst any) int {
	t.Helper()
	resp := s.do(t, method, path, body)
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp.StatusCode
}

const cartsPath = checkoutBasePath + "/carts"

var cartBody = map[string]any{
	"line_items": []map[string]any{{"item": map[string]any{"id": "SKU-1"}, "quantity": 2}},
}

func TestHTTP_CartCreate_201_OpaqueID(t *testing.T) {
	ts := newServingTestServer(t)
	ts.connector.cartCreateResp = &base.CartResponse{ID: "4242", Currency: "USD"}

	var out base.CartResponse
	if got := ts.into(t, http.MethodPost, cartsPath, cartBody, &out); got != http.StatusCreated {
		t.Fatalf("status = %d, want 201", got)
	}
	if out.ID == "" || out.ID == "4242" {
		t.Fatalf("id = %q, want an opaque Sarvex id", out.ID)
	}
	if out.Currency != "USD" {
		t.Errorf("currency = %q", out.Currency)
	}
	if got := ts.connector.cartCreateReq.LineItems; len(got) != 1 || got[0].Item.ID != "SKU-1" || got[0].Quantity != 2 {
		t.Errorf("connector saw %+v", got)
	}
}

func TestHTTP_CartCreate_NoLineItems_400(t *testing.T) {
	ts := newServingTestServer(t)
	if got := ts.code(t, http.MethodPost, cartsPath, map[string]any{"line_items": []any{}}); got != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", got)
	}
}

func TestHTTP_CartCreate_Rejection_422(t *testing.T) {
	ts := newServingTestServer(t)
	ts.connector.cartCreateErr = &base.CheckoutRejectedError{Code: "out_of_stock", Detail: "no stock"}
	if got := ts.code(t, http.MethodPost, cartsPath, cartBody); got != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", got)
	}
}

func TestHTTP_CartGetAndUpdate(t *testing.T) {
	ts := newServingTestServer(t)
	ts.connector.cartCreateResp = &base.CartResponse{ID: "4242", Currency: "USD"}
	ts.connector.cartUpdateResp = &base.CartResponse{ID: "4242", Currency: "USD"}

	var created base.CartResponse
	ts.into(t, http.MethodPost, cartsPath, cartBody, &created)

	var got base.CartResponse
	if code := ts.into(t, http.MethodGet, cartsPath+"/"+created.ID, nil, &got); code != http.StatusOK {
		t.Fatalf("GET status = %d", code)
	}
	if got.ID != created.ID {
		t.Errorf("GET id = %q, want %q", got.ID, created.ID)
	}

	upd := ts.code(t, http.MethodPut, cartsPath+"/"+created.ID, map[string]any{
		"line_items": []map[string]any{{"item": map[string]any{"id": "SKU-2"}, "quantity": 1}},
	})
	if upd != http.StatusOK {
		t.Fatalf("PUT status = %d", upd)
	}
	if ts.connector.cartUpdateID != "4242" {
		t.Errorf("connector updated %q, want backend cart 4242", ts.connector.cartUpdateID)
	}

	for name, code := range map[string]int{
		"GET unknown": ts.code(t, http.MethodGet, cartsPath+"/nope", nil),
		"GET backend": ts.code(t, http.MethodGet, cartsPath+"/4242", nil),
		"PUT unknown": ts.code(t, http.MethodPut, cartsPath+"/nope", cartBody),
	} {
		if code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", name, code)
		}
	}
}

func TestHTTP_CheckoutCreate_FromCart_SeedsFromCart(t *testing.T) {
	ts := newServingTestServer(t)
	ts.connector.cartCreateResp = &base.CartResponse{ID: "4242"}
	ts.connector.createResp = &base.CheckoutResponse{ID: "backend-1", Status: base.CheckoutStatusIncomplete}

	var cart base.CartResponse
	ts.into(t, http.MethodPost, cartsPath, map[string]any{
		"line_items": []map[string]any{{"item": map[string]any{"id": "SKU-1"}, "quantity": 3}},
		"buyer":      map[string]any{"email": "b@example.com"},
		"discounts":  map[string]any{"codes": []string{"SPRING20"}},
	}, &cart)

	if got := ts.code(t, http.MethodPost, checkoutBasePath+"/checkout-sessions", map[string]any{"cart_id": cart.ID}); got != http.StatusCreated {
		t.Fatalf("checkout from cart: status = %d, want 201", got)
	}
	req := ts.connector.createReq
	if len(req.LineItems) != 1 || req.LineItems[0].Item.ID != "SKU-1" || req.LineItems[0].Quantity != 3 {
		t.Errorf("checkout line items = %+v, want the cart's", req.LineItems)
	}
	if req.Buyer == nil || req.Buyer.Email != "b@example.com" {
		t.Errorf("checkout buyer = %+v, want the cart's", req.Buyer)
	}
	if req.Discounts == nil || len(req.Discounts.Codes) != 1 || req.Discounts.Codes[0] != "SPRING20" {
		t.Errorf("checkout discounts = %+v, want the cart's", req.Discounts)
	}

	// Request-stated line items win over the cart's.
	ts.connector.createReq = nil
	ts.code(t, http.MethodPost, checkoutBasePath+"/checkout-sessions", map[string]any{
		"cart_id":    cart.ID,
		"line_items": []map[string]any{{"item": map[string]any{"id": "SKU-9"}, "quantity": 1}},
	})
	if got := ts.connector.createReq.LineItems; len(got) != 1 || got[0].Item.ID != "SKU-9" {
		t.Errorf("explicit line items must win over the cart's, got %+v", got)
	}
}

func TestHTTP_CheckoutCreate_UnknownCart_404(t *testing.T) {
	ts := newServingTestServer(t)
	if got := ts.code(t, http.MethodPost, checkoutBasePath+"/checkout-sessions", map[string]any{"cart_id": "nope"}); got != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", got)
	}
}
