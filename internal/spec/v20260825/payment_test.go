// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package v20260825

import (
	"encoding/json"
	"testing"
)

func TestPaymentCredentialCardDeprecated(t *testing.T) {
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
	if cred.PAN != nil || cred.NetworkToken != nil || cred.Token != nil || cred.Raw != nil {
		t.Errorf("expected only Card set, got PAN=%v NetworkToken=%v Token=%v Raw=%v", cred.PAN, cred.NetworkToken, cred.Token, cred.Raw)
	}
}

func TestPaymentCredentialPAN(t *testing.T) {
	data := []byte(`{
		"type": "pan",
		"number": "4242424242424242",
		"expiry_month": 12,
		"expiry_year": 2030,
		"cvc": "123"
	}`)
	cred := roundTrip[PaymentCredential](t, data)
	if cred.PAN == nil {
		t.Fatal("PAN = nil, want non-nil")
	}
	if cred.PAN.Number != "4242424242424242" || cred.PAN.CVC != "123" {
		t.Errorf("PAN = %+v", cred.PAN)
	}
	if cred.Card != nil || cred.NetworkToken != nil || cred.Token != nil || cred.Raw != nil {
		t.Errorf("expected only PAN set, got Card=%v NetworkToken=%v Token=%v Raw=%v", cred.Card, cred.NetworkToken, cred.Token, cred.Raw)
	}
}

func TestPaymentCredentialNetworkToken(t *testing.T) {
	data := []byte(`{
		"type": "network_token",
		"number": "5204240000004242",
		"cryptogram": "gXc5UCLnM6ckD7pjM1TdPA==",
		"eci_value": "07",
		"token_requestor_id": "12345678901"
	}`)
	cred := roundTrip[PaymentCredential](t, data)
	if cred.NetworkToken == nil {
		t.Fatal("NetworkToken = nil, want non-nil")
	}
	if cred.NetworkToken.Cryptogram != "gXc5UCLnM6ckD7pjM1TdPA==" || cred.NetworkToken.TokenRequestorID != "12345678901" {
		t.Errorf("NetworkToken = %+v", cred.NetworkToken)
	}
	if cred.Card != nil || cred.PAN != nil || cred.Token != nil || cred.Raw != nil {
		t.Errorf("expected only NetworkToken set, got Card=%v PAN=%v Token=%v Raw=%v", cred.Card, cred.PAN, cred.Token, cred.Raw)
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
	if cred.Card != nil || cred.PAN != nil || cred.NetworkToken != nil || cred.Raw != nil {
		t.Errorf("expected only Token set, got Card=%v PAN=%v NetworkToken=%v Raw=%v", cred.Card, cred.PAN, cred.NetworkToken, cred.Raw)
	}
}

func TestPaymentCredentialUnknownPreservedRaw(t *testing.T) {
	data := []byte(`{"type": "com.example.custom_credential", "opaque_blob": "abc==", "nested": {"x": 1}}`)
	cred := roundTrip[PaymentCredential](t, data)
	if cred.Raw == nil {
		t.Fatal("Raw = nil, want non-nil for unknown credential type")
	}
	if cred.Card != nil || cred.PAN != nil || cred.NetworkToken != nil || cred.Token != nil {
		t.Errorf("expected only Raw set, got Card=%v PAN=%v NetworkToken=%v Token=%v", cred.Card, cred.PAN, cred.NetworkToken, cred.Token)
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
		"network": "visa",
		"display": {
			"brand": "visa",
			"last_digits": "4242",
			"expiry_month": 12,
			"expiry_year": 2030
		}
	}`)
	inst := roundTrip[CardPaymentInstrument](t, data)
	if inst.Network != "visa" {
		t.Errorf("Network = %q, want %q", inst.Network, "visa")
	}
	if inst.Display == nil {
		t.Fatal("Display = nil, want non-nil")
	}
	if inst.Display.Brand != "visa" {
		t.Errorf("Brand = %q, want %q", inst.Display.Brand, "visa")
	}
	// Regression check for the Go embedding trap: Network and the typed
	// Display must actually appear on the wire.
	body, err := json.Marshal(inst)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if m["network"] != "visa" {
		t.Errorf("marshaled network = %v, want %q", m["network"], "visa")
	}
	display, ok := m["display"].(map[string]any)
	if !ok {
		t.Fatalf(`marshaled output missing typed "display" object, got %v`, m["display"])
	}
	if display["brand"] != "visa" {
		t.Errorf("marshaled display.brand = %v, want %q", display["brand"], "visa")
	}
}

func TestAvailablePaymentInstrumentRoundTrip(t *testing.T) {
	data := []byte(`{
		"type": "card",
		"constraints": {"properties": {"brand": {"enum": ["visa", "mastercard"]}}}
	}`)
	inst := roundTrip[AvailablePaymentInstrument](t, data)
	if inst.Type != "card" {
		t.Errorf("Type = %q, want %q", inst.Type, "card")
	}
	if inst.Constraints == nil {
		t.Fatal("Constraints = nil, want non-nil")
	}
}
