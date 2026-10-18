// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package v20260825

import "testing"

func checkoutFixture(status string) []byte {
	return []byte(`{
		"ucp": {"version": "2026-08-25"},
		"id": "chk_1",
		"line_items": [
			{"id": "li_1", "item": {"id": "sku_1", "title": "Widget", "price": 1000}, "quantity": 2,
			 "totals": [{"type": "subtotal", "amount": 2000}, {"type": "total", "amount": 2000}]}
		],
		"status": "` + status + `",
		"currency": "USD",
		"totals": [{"type": "subtotal", "amount": 2000}, {"type": "total", "amount": 2000}],
		"links": [{"type": "privacy_policy", "url": "https://example.com/privacy"}],
		"policies": [{"type": "dev.ucp.shopping.policy.return", "description": {"plain": "30-day returns"}}],
		"actions": {"dev.ucp.common.payment.authentication.device_data": [{"id": "act_1"}]}
	}`)
}

func TestCheckoutRoundTrip(t *testing.T) {
	c := roundTrip[Checkout](t, checkoutFixture("incomplete"))
	if c.ID != "chk_1" {
		t.Errorf("ID = %q, want %q", c.ID, "chk_1")
	}
	if c.Status != CheckoutStatusIncomplete {
		t.Errorf("Status = %q, want %q", c.Status, CheckoutStatusIncomplete)
	}
	if len(c.Policies) != 1 {
		t.Errorf("Policies = %+v, want 1 entry", c.Policies)
	}
	if len(c.Actions) != 1 {
		t.Errorf("Actions = %+v, want 1 key", c.Actions)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestCheckoutValidateRequiresContinueURLOnEscalation(t *testing.T) {
	var c Checkout
	if err := unmarshalFixture(t, checkoutFixture("requires_escalation"), &c); err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err == nil {
		t.Error("Validate() error = nil, want error: continue_url required when status is requires_escalation")
	}
	c.ContinueURL = "https://example.com/continue"
	if err := c.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil once continue_url is set", err)
	}
}

func TestCheckoutValidateTotalsInvariant(t *testing.T) {
	c := Checkout{
		ID:       "chk_2",
		Currency: "USD",
		Totals:   Totals{{Type: TotalTypeSubtotal, Amount: 500}}, // missing "total"
	}
	if err := c.Validate(); err == nil {
		t.Error("Validate() error = nil, want totals invariant error")
	}
}

func TestCartRoundTrip(t *testing.T) {
	data := []byte(`{
		"ucp": {"version": "2026-08-25"},
		"id": "cart_1",
		"line_items": [{"item": {"id": "sku_1"}, "quantity": 1}],
		"currency": "USD",
		"totals": [{"type": "subtotal", "amount": 1000}, {"type": "total", "amount": 1000}],
		"policies": [{"type": "dev.ucp.shopping.policy.warranty", "description": {"plain": "1yr warranty"}}]
	}`)
	cart := roundTrip[Cart](t, data)
	if len(cart.Policies) != 1 {
		t.Errorf("Policies = %+v, want 1 entry", cart.Policies)
	}
	if err := cart.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestOrderRoundTrip(t *testing.T) {
	data := []byte(`{
		"ucp": {"version": "2026-08-25"},
		"id": "ord_1",
		"checkout_id": "chk_1",
		"permalink_url": "https://example.com/orders/ord_1",
		"line_items": [
			{"id": "li_1", "item": {"id": "sku_1"}, "quantity": {"total": 1, "fulfilled": 0},
			 "totals": [{"type": "subtotal", "amount": 1000}], "status": "processing"}
		],
		"fulfillment": {
			"expectations": [
				{"id": "exp_1", "line_items": [{"id": "li_1", "quantity": 1}], "method_type": "shipping",
				 "destination": {"address_country": "US"}}
			]
		},
		"currency": "USD",
		"totals": [{"type": "subtotal", "amount": 1000}, {"type": "total", "amount": 1000}],
		"policies": [{"type": "dev.ucp.shopping.policy.return", "description": {"plain": "30-day returns"}}]
	}`)
	order := roundTrip[Order](t, data)
	if err := order.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
	if order.Fulfillment == nil || len(order.Fulfillment.Expectations) != 1 {
		t.Errorf("Fulfillment = %+v, want one expectation", order.Fulfillment)
	}
	if len(order.Policies) != 1 {
		t.Errorf("Policies = %+v, want 1 entry", order.Policies)
	}
}

func TestOrderValidateRequiresFulfillment(t *testing.T) {
	o := Order{ID: "o1", CheckoutID: "c1", PermalinkURL: "https://x", Currency: "USD"}
	if err := o.Validate(); err == nil {
		t.Error("Validate() error = nil, want fulfillment required error")
	}
}

func TestCatalogSearchResponseRoundTrip(t *testing.T) {
	data := []byte(`{
		"ucp": {"version": "2026-08-25"},
		"products": [
			{
				"id": "prod_1", "title": "Widget", "description": {"plain": "A widget."},
				"price_range": {"min": {"amount": 1000, "currency": "USD"}, "max": {"amount": 1000, "currency": "USD"}},
				"variants": [{"id": "var_1", "title": "Default", "description": {"plain": "A widget."}, "price": {"amount": 1000, "currency": "USD"}}]
			}
		],
		"pagination": {"cursor": "abc", "has_next_page": true},
		"policies": [{"type": "dev.ucp.shopping.policy.return", "description": {"plain": "30-day returns"}}]
	}`)
	resp := roundTrip[CatalogSearchResponse](t, data)
	if len(resp.Products) != 1 {
		t.Fatalf("len(Products) = %d, want 1", len(resp.Products))
	}
	if err := resp.Pagination.Validate(); err != nil {
		t.Errorf("Pagination.Validate() error = %v", err)
	}
	if len(resp.Policies) != 1 {
		t.Errorf("Policies = %+v, want 1 entry", resp.Policies)
	}
}

func TestCatalogLookupResponseRoundTrip(t *testing.T) {
	data := []byte(`{
		"ucp": {"version": "2026-08-25"},
		"products": [
			{
				"id": "prod_1", "title": "Widget", "description": {"plain": "A widget."},
				"price_range": {"min": {"amount": 1000, "currency": "USD"}, "max": {"amount": 1000, "currency": "USD"}},
				"variants": [
					{"id": "var_1", "title": "Default", "description": {"plain": "A widget."}, "price": {"amount": 1000, "currency": "USD"},
					 "inputs": [{"id": "var_1", "match": "exact"}]}
				]
			}
		]
	}`)
	resp := roundTrip[CatalogLookupResponse](t, data)
	if len(resp.Products) != 1 || len(resp.Products[0].Variants) != 1 {
		t.Fatalf("unexpected shape: %+v", resp)
	}
	if resp.Products[0].Variants[0].Inputs[0].Match != InputCorrelationMatchExact {
		t.Errorf("Match = %q, want %q", resp.Products[0].Variants[0].Inputs[0].Match, InputCorrelationMatchExact)
	}
}

func TestGetProductResponseRoundTrip(t *testing.T) {
	data := []byte(`{
		"ucp": {"version": "2026-08-25"},
		"product": {
			"id": "prod_1", "title": "Widget", "description": {"plain": "A widget."},
			"price_range": {"min": {"amount": 1000, "currency": "USD"}, "max": {"amount": 1500, "currency": "USD"}},
			"variants": [{"id": "var_1", "title": "Small", "description": {"plain": "A widget."}, "price": {"amount": 1000, "currency": "USD"}}],
			"selected": [{"name": "Size", "label": "Small"}],
			"options": [{"name": "Size", "values": [{"label": "Small", "available": true, "exists": true}]}]
		}
	}`)
	resp := roundTrip[GetProductResponse](t, data)
	if len(resp.Product.Options) != 1 {
		t.Fatalf("len(Options) = %d, want 1", len(resp.Product.Options))
	}
	avail := resp.Product.Options[0].Values[0].Available
	if avail == nil || !*avail {
		t.Errorf("Available = %v, want pointer to true", avail)
	}
}
