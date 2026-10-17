// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package woocommerce_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
	ucptype "github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/resilience"
)

// consentEchoServer stands in for a WooCommerce store that persists order
// meta_data: it stores whatever meta_data a write sends and returns it on the
// order, exactly as WooCommerce does.
func consentEchoServer(t *testing.T, sent *[]map[string]any) *httptest.Server {
	t.Helper()
	var stored []any
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if md, ok := body["meta_data"].([]any); ok {
				stored = md
				*sent = append(*sent, body)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 7, "status": "pending", "line_items": []any{},
			"billing":   map[string]any{"email": "b@example.com"},
			"shipping":  map[string]any{},
			"meta_data": stored,
			"total":     "0.00", "subtotal": "0.00", "shipping_total": "0.00",
			"tax_total": "0.00", "discount_total": "0.00",
		})
	}))
}

func TestConnector_BuyerConsent_RoundTrip(t *testing.T) {
	var sent []map[string]any
	server := consentEchoServer(t, &sent)
	defer server.Close()
	conn := newTestConnector(t, server, nil, resilience.Config{})

	yes, no := true, false
	create, err := conn.CheckoutCreate(context.Background(), "s", &ucptype.CreateCheckoutRequest{
		Buyer: &model.BuyerWithConsent{
			Buyer:   model.Buyer{Email: "b@example.com"},
			Consent: &model.Consent{Marketing: &yes, SaleOfData: &no},
		},
	})
	if err != nil {
		t.Fatalf("CheckoutCreate: %v", err)
	}
	if create.Buyer == nil || create.Buyer.Consent == nil {
		t.Fatalf("consent not returned on create: %+v", create.Buyer)
	}
	c := create.Buyer.Consent
	if c.Marketing == nil || !*c.Marketing || c.SaleOfData == nil || *c.SaleOfData || c.Analytics != nil {
		t.Fatalf("consent not preserved (want marketing=true, sale_of_data=false, analytics unset): %+v", c)
	}

	// GET reads it back from the merchant, not from any local state.
	got, err := conn.CheckoutGet(context.Background(), "s", "7")
	if err != nil || got.Buyer == nil || got.Buyer.Consent == nil || got.Buyer.Consent.Marketing == nil || !*got.Buyer.Consent.Marketing {
		t.Fatalf("consent not persisted at merchant: %+v err=%v", got, err)
	}

	// Buyer is PUT-replace: an update without consent clears it.
	upd, err := conn.CheckoutUpdate(context.Background(), "s", "7", &ucptype.UpdateCheckoutRequest{
		Buyer: &model.BuyerWithConsent{Buyer: model.Buyer{Email: "b@example.com"}},
	})
	if err != nil {
		t.Fatalf("CheckoutUpdate: %v", err)
	}
	if upd.Buyer == nil || upd.Buyer.Consent != nil {
		t.Fatalf("consent should be cleared by a buyer update without consent: %+v", upd.Buyer)
	}
}

func TestConnector_BuyerConsent_AbsentBuyerLeavesConsentUntouched(t *testing.T) {
	var sent []map[string]any
	server := consentEchoServer(t, &sent)
	defer server.Close()
	conn := newTestConnector(t, server, nil, resilience.Config{})

	if _, err := conn.CheckoutUpdate(context.Background(), "s", "7", &ucptype.UpdateCheckoutRequest{}); err != nil {
		t.Fatalf("CheckoutUpdate: %v", err)
	}
	if len(sent) != 0 {
		t.Fatalf("no buyer sent, yet meta_data was written: %v", sent)
	}
}
