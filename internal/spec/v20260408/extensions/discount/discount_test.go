// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package discount

import (
	"encoding/json"
	"testing"

	v20260408 "github.com/sarvexlabs/sarvex.cp/internal/spec/v20260408"
)

func TestCartWithDiscountRoundTrip(t *testing.T) {
	data := []byte(`{
		"id": "cart_1",
		"line_items": [{"item": {"id": "sku_1"}, "quantity": 1}],
		"currency": "USD",
		"totals": [],
		"discounts": {
			"codes": ["SUMMER20"],
			"applied": [
				{
					"code": "SUMMER20",
					"title": "Summer Sale 20% Off",
					"amount": 200,
					"method": "across",
					"priority": 1,
					"allocations": [{"path": "$.line_items[0]", "amount": 200}]
				}
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
	applied := c.Discounts.Applied[0]
	if applied.Method != v20260408.AllocationMethodAcross {
		t.Errorf("Method = %q, want %q", applied.Method, v20260408.AllocationMethodAcross)
	}
	if applied.Amount != 200 {
		t.Errorf("Amount = %d, want 200", applied.Amount)
	}

	body, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var back CartWithDiscount
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("re-Unmarshal() error = %v", err)
	}
	if back.Discounts == nil || len(back.Discounts.Applied) != 1 {
		t.Fatalf("round trip lost discounts: %+v", back)
	}
}

func TestCheckoutWithDiscountRoundTrip(t *testing.T) {
	data := []byte(`{
		"id": "chk_1",
		"line_items": [],
		"currency": "USD",
		"totals": [],
		"links": [],
		"discounts": {"codes": ["WELCOME10"]}
	}`)
	var c CheckoutWithDiscount
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if c.ID != "chk_1" {
		t.Errorf("Checkout.ID = %q, want %q", c.ID, "chk_1")
	}
	if c.Discounts == nil || len(c.Discounts.Codes) != 1 || c.Discounts.Codes[0] != "WELCOME10" {
		t.Fatalf("Discounts = %+v", c.Discounts)
	}
}
