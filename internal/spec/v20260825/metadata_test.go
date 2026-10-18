// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package v20260825

import (
	"encoding/json"
	"testing"
)

func TestCapabilityExtendsSingleString(t *testing.T) {
	data := []byte(`{"version": "2026-08-25", "extends": "dev.ucp.shopping.checkout"}`)
	c := roundTrip[Capability](t, data)
	if len(c.Extends) != 1 || c.Extends[0] != "dev.ucp.shopping.checkout" {
		t.Errorf("Extends = %v, want [dev.ucp.shopping.checkout]", c.Extends)
	}
	body, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["extends"].(string); !ok {
		t.Errorf(`marshaled "extends" should be a bare string for a single parent, got %#v`, m["extends"])
	}
}

func TestCapabilityExtendsArray(t *testing.T) {
	data := []byte(`{"version": "2026-08-25", "extends": ["dev.ucp.shopping.cart", "dev.ucp.shopping.checkout"]}`)
	c := roundTrip[Capability](t, data)
	if len(c.Extends) != 2 {
		t.Fatalf("len(Extends) = %d, want 2", len(c.Extends))
	}
	body, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["extends"].([]any); !ok {
		t.Errorf(`marshaled "extends" should be an array for multiple parents, got %#v`, m["extends"])
	}
}

func TestCapabilityNoExtends(t *testing.T) {
	data := []byte(`{"version": "2026-08-25", "spec": "https://ucp.dev/spec", "schema": "https://ucp.dev/schema"}`)
	c := roundTrip[Capability](t, data)
	if len(c.Extends) != 0 {
		t.Errorf("Extends = %v, want empty for a root capability", c.Extends)
	}
	if err := c.ValidateDiscovery(); err != nil {
		t.Errorf("ValidateDiscovery() error = %v, want nil", err)
	}
	if err := c.ValidateBusinessDiscovery(); err != nil {
		t.Errorf("ValidateBusinessDiscovery() error = %v, want nil", err)
	}
}

func TestCapabilityValidateDiscoveryRequiresSpecAndSchema(t *testing.T) {
	c := Capability{Entity: Entity{Version: "2026-08-25"}}
	if err := c.ValidateDiscovery(); err == nil {
		t.Error("ValidateDiscovery() error = nil, want error for missing spec/schema")
	}
}

// TestCapabilityValidateBusinessDiscoveryRequiresSchema exercises the
// breaking change from v20260408: business_schema now requires "schema"
// too (capability.json's business_schema def gained a "required":
// ["schema"] clause — see DISCREPANCIES.md).
func TestCapabilityValidateBusinessDiscoveryRequiresSchema(t *testing.T) {
	c := Capability{Entity: Entity{Version: "2026-08-25"}}
	if err := c.ValidateBusinessDiscovery(); err == nil {
		t.Error("ValidateBusinessDiscovery() error = nil, want error for missing schema")
	}
	c.Schema = "https://ucp.dev/schema"
	if err := c.ValidateBusinessDiscovery(); err != nil {
		t.Errorf("ValidateBusinessDiscovery() error = %v, want nil once schema is set", err)
	}
}

// TestPlatformDiscoveryProfileKeysRoundTrip exercises the breaking change
// from v20260408: the wire field for publishing signing keys is "keys",
// not "signing_keys".
func TestPlatformDiscoveryProfileKeysRoundTrip(t *testing.T) {
	data := []byte(`{
		"ucp": {
			"version": "2026-08-25",
			"services": {"dev.ucp.shopping.checkout": [{"version": "2026-08-25", "spec": "https://x/spec", "schema": "https://x/schema", "transport": "rest", "endpoint": "https://merchant.example/api"}]},
			"capabilities": {"dev.ucp.shopping.checkout": [{"version": "2026-08-25", "spec": "https://x/spec", "schema": "https://x/schema"}]},
			"payment_handlers": {"com.stripe.card": [{"version": "2026-08-25", "spec": "https://x/spec", "schema": "https://x/schema", "id": "stripe_card_1"}]}
		},
		"keys": [{"kid": "key-1", "kty": "EC", "crv": "P-256", "x": "abc", "y": "def", "use": "sig", "alg": "ES256"}]
	}`)
	profile := roundTrip[PlatformDiscoveryProfile](t, data)
	if err := profile.UCP.Validate(); err != nil {
		t.Errorf("UCP.Validate() error = %v, want nil", err)
	}
	if len(profile.Keys) != 1 || profile.Keys[0].Kid != "key-1" {
		t.Errorf("Keys = %+v", profile.Keys)
	}

	// Regression check: the wire key must actually be "keys", not the
	// v20260408 "signing_keys".
	body, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["keys"]; !ok {
		t.Errorf(`marshaled output missing "keys", got %v`, m)
	}
	if _, ok := m["signing_keys"]; ok {
		t.Errorf(`marshaled output has stale "signing_keys" field, got %v`, m)
	}
}

func TestBusinessDiscoveryProfileRoundTrip(t *testing.T) {
	data := []byte(`{
		"ucp": {
			"version": "2026-08-25",
			"services": {"dev.ucp.shopping.checkout": [{"version": "2026-08-25", "transport": "rest", "endpoint": "https://merchant.example/api"}]},
			"payment_handlers": {"com.stripe.card": [{"version": "2026-08-25", "id": "stripe_card_1"}]},
			"supported_versions": {"2026-04-08": "https://merchant.example/.well-known/ucp/2026-04-08"}
		}
	}`)
	profile := roundTrip[BusinessDiscoveryProfile](t, data)
	if err := profile.UCP.Validate(); err != nil {
		t.Errorf("UCP.Validate() error = %v, want nil", err)
	}
	if profile.UCP.SupportedVersions["2026-04-08"] == "" {
		t.Errorf("SupportedVersions = %v", profile.UCP.SupportedVersions)
	}
}

func TestErrorResponseMetadataValidate(t *testing.T) {
	m := ErrorResponseMetadata{UCPMetadataBase: UCPMetadataBase{Version: "2026-08-25", Status: UCPStatusSuccess}}
	if err := m.Validate(); err == nil {
		t.Error("Validate() error = nil, want error for status != error")
	}
	m.Status = UCPStatusError
	if err := m.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestServiceValidateDiscovery(t *testing.T) {
	tests := []struct {
		name    string
		svc     Service
		wantErr bool
	}{
		{"a2a needs only spec", Service{Entity: Entity{Version: "2026-08-25", Spec: "https://x"}, Transport: ServiceTransportA2A}, false},
		{"rest needs schema too", Service{Entity: Entity{Version: "2026-08-25", Spec: "https://x"}, Transport: ServiceTransportREST}, true},
		{"rest with schema passes", Service{Entity: Entity{Version: "2026-08-25", Spec: "https://x", Schema: "https://y"}, Transport: ServiceTransportREST}, false},
		{"missing spec always fails", Service{Entity: Entity{Version: "2026-08-25"}, Transport: ServiceTransportA2A}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.svc.ValidateDiscovery()
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDiscovery() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestUCPMetadataBaseMapOrderRoundTrip(t *testing.T) {
	data := []byte(`{
		"version": "2026-08-25",
		"map_order": {"capabilities": ["dev.ucp.shopping.checkout", "dev.ucp.shopping.cart"]}
	}`)
	m := roundTrip[UCPMetadataBase](t, data)
	if len(m.MapOrder["capabilities"]) != 2 {
		t.Errorf("MapOrder = %+v", m.MapOrder)
	}
}
