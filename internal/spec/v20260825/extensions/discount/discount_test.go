// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package discount

import (
	"encoding/json"
	"testing"

	v20260825 "github.com/sarvexlab/sarvex/internal/spec/v20260825"
)

func TestCartWithDiscountRoundTrip(t *testing.T) {
	data := []byte(`{
		"id": "cart_1",
		"line_items": [],
		"currency": "USD",
		"totals": [],
		"discounts": {
			"codes": ["SUMMER20"],
			"applied": [
				{"code": "SUMMER20", "title": "Summer Sale 20% Off", "amount": 500, "method": "across", "priority": 1}
			]
		}
	}`)
	var c CartWithDiscount
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if c.ID != "cart_1" {
		t.Errorf("Cart.ID = %q, want %q (base Cart must be composed, not duplicated)", c.ID, "cart_1")
	}
	if c.Discounts == nil || len(c.Discounts.Applied) != 1 {
		t.Fatalf("Discounts = %+v", c.Discounts)
	}
	if c.Discounts.Applied[0].Method != v20260825.AllocationMethodAcross {
		t.Errorf("Method = %q, want %q", c.Discounts.Applied[0].Method, v20260825.AllocationMethodAcross)
	}
}

func TestCheckoutWithDiscountEligibilityRoundTrip(t *testing.T) {
	data := []byte(`{
		"id": "chk_1",
		"line_items": [],
		"currency": "USD",
		"totals": [],
		"links": [],
		"discounts": {
			"applied": [
				{"title": "Gold member discount", "amount": 300, "automatic": true, "provisional": true, "eligibility": "com.example.loyalty_gold"}
			]
		}
	}`)
	var c CheckoutWithDiscount
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if c.Discounts == nil || len(c.Discounts.Applied) != 1 {
		t.Fatalf("Discounts = %+v", c.Discounts)
	}
	applied := c.Discounts.Applied[0]
	if !applied.Automatic || !applied.Provisional || applied.Eligibility != "com.example.loyalty_gold" {
		t.Errorf("Applied[0] = %+v", applied)
	}
}
