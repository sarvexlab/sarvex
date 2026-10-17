// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package buyerconsent

import (
	"encoding/json"
	"reflect"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func TestBuyerWithConsentRoundTrip(t *testing.T) {
	data := []byte(`{
		"first_name": "Jane",
		"email": "jane@example.com",
		"loyalty_tier": "gold",
		"consent": {"analytics": true, "marketing": false}
	}`)
	var first BuyerWithConsent
	if err := json.Unmarshal(data, &first); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if first.FirstName != "Jane" {
		t.Errorf("FirstName = %q, want %q (base Buyer must be composed, not duplicated)", first.FirstName, "Jane")
	}
	if first.Extra["loyalty_tier"] != "gold" {
		t.Errorf("Extra[loyalty_tier] = %v, want gold", first.Extra["loyalty_tier"])
	}
	if first.Consent == nil || first.Consent.Analytics == nil || !*first.Consent.Analytics {
		t.Fatalf("Consent.Analytics = %v, want pointer to true", first.Consent)
	}
	if first.Consent.Marketing == nil || *first.Consent.Marketing {
		t.Fatalf("Consent.Marketing = %v, want pointer to false", first.Consent.Marketing)
	}

	// Regression check for the Go embedding trap: Consent (and Extra) must
	// actually appear on the wire, not be silently dropped by a promoted
	// Buyer.MarshalJSON.
	body, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["consent"]; !ok {
		t.Errorf(`marshaled output missing "consent", got %v`, m)
	}
	if m["loyalty_tier"] != "gold" {
		t.Errorf(`marshaled output missing extra "loyalty_tier", got %v`, m)
	}

	var second BuyerWithConsent
	if err := json.Unmarshal(body, &second); err != nil {
		t.Fatalf("re-Unmarshal() error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("round trip mismatch:\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func TestConsentDistinguishesUnsetFromFalse(t *testing.T) {
	var c Consent
	if err := json.Unmarshal([]byte(`{"marketing": false}`), &c); err != nil {
		t.Fatal(err)
	}
	if c.Marketing == nil || *c.Marketing != false {
		t.Errorf("Marketing = %v, want pointer to false", c.Marketing)
	}
	if c.Analytics != nil {
		t.Errorf("Analytics = %v, want nil (unset, not false)", c.Analytics)
	}
}

func TestCheckoutWithBuyerConsentRoundTrip(t *testing.T) {
	data := []byte(`{
		"id": "chk_1",
		"line_items": [],
		"currency": "USD",
		"totals": [],
		"links": [],
		"buyer": {"first_name": "Jane", "consent": {"sale_of_data": false}}
	}`)
	var c CheckoutWithBuyerConsent
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if c.ID != "chk_1" {
		t.Errorf("Checkout.ID = %q, want %q", c.ID, "chk_1")
	}
	if c.Buyer == nil || c.Buyer.FirstName != "Jane" {
		t.Fatalf("Buyer = %+v", c.Buyer)
	}
	if c.Buyer.Consent == nil || c.Buyer.Consent.SaleOfData == nil || *c.Buyer.Consent.SaleOfData {
		t.Errorf("Buyer.Consent.SaleOfData = %v, want pointer to false", c.Buyer.Consent)
	}
}
