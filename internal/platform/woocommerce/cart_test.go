// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package woocommerce_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
	ucptype "github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/resilience"
)

func TestConnector_CartCreate_ForwardsBuyerAndDiscounts_ReturnsCurrencyAndApplied(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 9, "status": "pending", "line_items": []any{},
			"billing":      map[string]any{"email": "b@example.com"},
			"shipping":     map[string]any{},
			"coupon_lines": []any{map[string]any{"code": "SPRING20", "discount": "2.00"}},
			"total":        "8.00", "subtotal": "10.00", "shipping_total": "0.00",
			"tax_total": "0.00", "discount_total": "2.00",
		})
	}))
	defer server.Close()
	conn := newTestConnector(t, server, nil, resilience.Config{})

	resp, err := conn.CartCreate(context.Background(), "s", &ucptype.CreateCartRequest{
		Buyer:     &model.Buyer{Email: "b@example.com"},
		Discounts: &model.DiscountsObject{Codes: []string{"SPRING20"}},
	})
	if err != nil {
		t.Fatalf("CartCreate: %v", err)
	}

	if resp.Currency != "USD" {
		t.Errorf("currency = %q, want USD (required on a cart response)", resp.Currency)
	}
	if lines, _ := body["coupon_lines"].([]any); len(lines) != 1 {
		t.Errorf("discount codes not forwarded to the store: %v", body["coupon_lines"])
	}
	if billing, _ := body["billing"].(map[string]any); billing["email"] != "b@example.com" {
		t.Errorf("buyer not forwarded: %v", body["billing"])
	}
	if resp.Discounts == nil || len(resp.Discounts.Applied) != 1 || resp.Discounts.Applied[0].Code != "SPRING20" {
		t.Errorf("applied discounts not reported: %+v", resp.Discounts)
	}
}

// Creating an order is not idempotent at the store: a transparent retry after
// a lost response would leave a second pending order behind.
func TestConnector_CartCreate_IsNotRetried(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, `{"message":"unavailable"}`, http.StatusServiceUnavailable)
	}))
	defer server.Close()
	conn := newTestConnector(t, server, nil, resilience.Config{})

	if _, err := conn.CartCreate(context.Background(), "s", &ucptype.CreateCartRequest{}); err == nil {
		t.Fatal("expected an error from a 503")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("CartCreate hit the store %d times, want exactly 1", got)
	}
}

func TestConnector_CartCreate_ClientErrorIsABusinessRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":"woocommerce_rest_cannot_create_order","message":"Product is out of stock"}`, http.StatusBadRequest)
	}))
	defer server.Close()
	conn := newTestConnector(t, server, nil, resilience.Config{})

	_, err := conn.CartCreate(context.Background(), "s", &ucptype.CreateCartRequest{})
	var rej *ucptype.CheckoutRejectedError
	if !errors.As(err, &rej) {
		t.Fatalf("err = %v, want a CheckoutRejectedError (the buyer's to fix, not a store outage)", err)
	}
}
