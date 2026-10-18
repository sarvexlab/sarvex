// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package ap2

import (
	"encoding/json"
	"reflect"
	"testing"

	v20260408 "github.com/sarvexlab/sarvex/internal/spec/v20260408"
)

func TestMerchantAuthorizationValidate(t *testing.T) {
	valid := MerchantAuthorization("eyJhbGciOiJFUzI1NiJ9..c2lnbmF0dXJl")
	if err := valid.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
	invalid := MerchantAuthorization("not-a-jws")
	if err := invalid.Validate(); err == nil {
		t.Error("Validate() error = nil, want error")
	}
}

func TestCheckoutMandateValidate(t *testing.T) {
	valid := CheckoutMandate("eyJhbGciOiJFUzI1NiJ9.eyJzdWIiOiIxMjMifQ.c2ln~ZGlzY2xvc3VyZQ")
	if err := valid.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
	invalid := CheckoutMandate("not a jwt")
	if err := invalid.Validate(); err == nil {
		t.Error("Validate() error = nil, want error")
	}
}

func TestErrorCodeKnown(t *testing.T) {
	if !ErrorCodeMandateExpired.Known() {
		t.Error("ErrorCodeMandateExpired.Known() = false, want true")
	}
	if ErrorCode("bogus").Known() {
		t.Error(`ErrorCode("bogus").Known() = true, want false`)
	}
}

func TestCheckoutWithAP2MandateRoundTrip(t *testing.T) {
	data := []byte(`{
		"id": "chk_1",
		"line_items": [{"item": {"id": "sku_1"}, "quantity": 1}],
		"status": "completed",
		"currency": "USD",
		"totals": [{"type": "subtotal", "amount": 1000}, {"type": "total", "amount": 1000}],
		"links": [],
		"ap2": {
			"merchant_authorization": "eyJhbGciOiJFUzI1NiJ9..c2lnbmF0dXJl",
			"checkout_mandate": "eyJhbGciOiJFUzI1NiJ9.eyJzdWIiOiIxMjMifQ.c2ln~ZGlzY2xvc3VyZQ"
		}
	}`)
	var first CheckoutWithAP2Mandate
	if err := json.Unmarshal(data, &first); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if first.AP2 == nil {
		t.Fatal("AP2 = nil, want non-nil")
	}
	if first.AP2.MerchantAuthorization == "" || first.AP2.CheckoutMandate == "" {
		t.Errorf("AP2 = %+v, expected both fields set", first.AP2)
	}
	if first.ID != "chk_1" {
		t.Errorf("Checkout.ID = %q, want %q (base Checkout must be composed, not duplicated)", first.ID, "chk_1")
	}

	body, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var second CheckoutWithAP2Mandate
	if err := json.Unmarshal(body, &second); err != nil {
		t.Fatalf("re-Unmarshal() error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("round trip mismatch:\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func TestCheckoutWithAP2MandateComposesBaseCheckout(t *testing.T) {
	// The extension type must be usable anywhere a base Checkout's fields
	// are needed, without redeclaring them.
	var c CheckoutWithAP2Mandate
	c.Checkout = v20260408.Checkout{ID: "chk_2", Currency: "EUR"}
	if c.ID != "chk_2" || c.Currency != "EUR" {
		t.Errorf("composed Checkout fields not accessible: %+v", c.Checkout)
	}
}
