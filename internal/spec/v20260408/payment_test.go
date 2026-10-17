// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v20260408

import (
	"encoding/json"
	"testing"
)

func TestPaymentCredentialCard(t *testing.T) {
	data := []byte(`{
		"type": "card",
		"card_number_type": "fpan",
		"number": "4242424242424242",
		"expiry_month": 12,
		"expiry_year": 2030,
		"cvc": "123"
	}`)
	cred := roundTrip[PaymentCredential](t, data)
	if cred.Card == nil {
		t.Fatal("Card = nil, want non-nil")
	}
	if cred.Card.CardNumberType != CardNumberTypeFPAN {
		t.Errorf("CardNumberType = %q, want %q", cred.Card.CardNumberType, CardNumberTypeFPAN)
	}
	if cred.Token != nil || cred.Raw != nil {
		t.Errorf("expected only Card set, got Token=%v Raw=%v", cred.Token, cred.Raw)
	}
}

func TestPaymentCredentialToken(t *testing.T) {
	data := []byte(`{"type": "stripe_token", "token": "tok_abc123"}`)
	cred := roundTrip[PaymentCredential](t, data)
	if cred.Token == nil {
		t.Fatal("Token = nil, want non-nil")
	}
	if cred.Token.Type != "stripe_token" {
		t.Errorf("Type = %q, want %q", cred.Token.Type, "stripe_token")
	}
	if cred.Card != nil || cred.Raw != nil {
		t.Errorf("expected only Token set, got Card=%v Raw=%v", cred.Card, cred.Raw)
	}
}

func TestPaymentCredentialUnknownPreservedRaw(t *testing.T) {
	data := []byte(`{"type": "com.example.custom_credential", "opaque_blob": "abc==", "nested": {"x": 1}}`)
	cred := roundTrip[PaymentCredential](t, data)
	if cred.Raw == nil {
		t.Fatal("Raw = nil, want non-nil for unknown credential type")
	}
	if cred.Card != nil || cred.Token != nil {
		t.Errorf("expected only Raw set, got Card=%v Token=%v", cred.Card, cred.Token)
	}
	var back map[string]any
	if err := json.Unmarshal(cred.Raw, &back); err != nil {
		t.Fatalf("Raw did not contain valid JSON: %v", err)
	}
	if back["opaque_blob"] != "abc==" {
		t.Errorf("Raw lost opaque_blob field: %v", back)
	}
}

func TestSelectedPaymentInstrumentRoundTrip(t *testing.T) {
	data := []byte(`{
		"id": "pi_1",
		"handler_id": "h_1",
		"type": "card",
		"selected": true,
		"custom_field": "preserved"
	}`)
	inst := roundTrip[SelectedPaymentInstrument](t, data)
	if !inst.Selected {
		t.Error("Selected = false, want true")
	}
	if inst.ID != "pi_1" || inst.HandlerID != "h_1" {
		t.Errorf("unexpected base fields: %+v", inst.PaymentInstrument)
	}
	if inst.Extra["custom_field"] != "preserved" {
		t.Errorf("Extra did not preserve custom_field: %v", inst.Extra)
	}
	// Regression check for the Go embedding trap: Selected must actually
	// appear on the wire, not be silently dropped by a promoted
	// PaymentInstrument.MarshalJSON.
	body, err := json.Marshal(inst)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if sel, ok := m["selected"]; !ok || sel != true {
		t.Errorf(`marshaled output missing "selected": true, got %v`, m)
	}
}

func TestCardPaymentInstrumentRoundTrip(t *testing.T) {
	data := []byte(`{
		"id": "pi_2",
		"handler_id": "h_2",
		"type": "card",
		"display": {
			"brand": "visa",
			"last_digits": "4242",
			"expiry_month": 12,
			"expiry_year": 2030
		}
	}`)
	inst := roundTrip[CardPaymentInstrument](t, data)
	if inst.Display == nil {
		t.Fatal("Display = nil, want non-nil")
	}
	if inst.Display.Brand != "visa" {
		t.Errorf("Brand = %q, want %q", inst.Display.Brand, "visa")
	}
	// Regression check for the Go embedding trap: the typed Display must
	// actually appear on the wire.
	body, err := json.Marshal(inst)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	display, ok := m["display"].(map[string]any)
	if !ok {
		t.Fatalf(`marshaled output missing typed "display" object, got %v`, m["display"])
	}
	if display["brand"] != "visa" {
		t.Errorf("marshaled display.brand = %v, want %q", display["brand"], "visa")
	}
}

func TestBindingRoundTrip(t *testing.T) {
	data := []byte(`{"checkout_id": "chk_1", "identity": {"access_token": "tok_xyz"}}`)
	b := roundTrip[Binding](t, data)
	if b.CheckoutID != "chk_1" {
		t.Errorf("CheckoutID = %q, want %q", b.CheckoutID, "chk_1")
	}
	if b.Identity == nil || b.Identity.AccessToken != "tok_xyz" {
		t.Errorf("Identity = %+v, want AccessToken tok_xyz", b.Identity)
	}
}
