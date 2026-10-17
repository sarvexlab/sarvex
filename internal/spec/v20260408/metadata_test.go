// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v20260408

import (
	"encoding/json"
	"testing"
)

func TestCapabilityExtendsSingleString(t *testing.T) {
	data := []byte(`{"version": "2026-04-08", "extends": "dev.ucp.shopping.checkout"}`)
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
	data := []byte(`{"version": "2026-04-08", "extends": ["dev.ucp.shopping.cart", "dev.ucp.shopping.checkout"]}`)
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
	data := []byte(`{"version": "2026-04-08", "spec": "https://ucp.dev/spec", "schema": "https://ucp.dev/schema"}`)
	c := roundTrip[Capability](t, data)
	if len(c.Extends) != 0 {
		t.Errorf("Extends = %v, want empty for a root capability", c.Extends)
	}
	if err := c.ValidateDiscovery(); err != nil {
		t.Errorf("ValidateDiscovery() error = %v, want nil", err)
	}
}

func TestCapabilityValidateDiscoveryRequiresSpecAndSchema(t *testing.T) {
	c := Capability{Entity: Entity{Version: "2026-04-08"}}
	if err := c.ValidateDiscovery(); err == nil {
		t.Error("ValidateDiscovery() error = nil, want error for missing spec/schema")
	}
}

func TestPlatformDiscoveryProfileRoundTrip(t *testing.T) {
	data := []byte(`{
		"ucp": {
			"version": "2026-04-08",
			"services": {"dev.ucp.shopping.checkout": [{"version": "2026-04-08", "spec": "https://x/spec", "schema": "https://x/schema", "transport": "rest", "endpoint": "https://merchant.example/api"}]},
			"capabilities": {"dev.ucp.shopping.checkout": [{"version": "2026-04-08", "spec": "https://x/spec", "schema": "https://x/schema"}]},
			"payment_handlers": {"com.stripe.card": [{"version": "2026-04-08", "spec": "https://x/spec", "schema": "https://x/schema", "id": "stripe_card_1"}]}
		},
		"signing_keys": [{"kid": "key-1", "kty": "EC", "crv": "P-256", "x": "abc", "y": "def", "use": "sig", "alg": "ES256"}]
	}`)
	profile := roundTrip[PlatformDiscoveryProfile](t, data)
	if err := profile.UCP.Validate(); err != nil {
		t.Errorf("UCP.Validate() error = %v, want nil", err)
	}
	if len(profile.SigningKeys) != 1 || profile.SigningKeys[0].Kid != "key-1" {
		t.Errorf("SigningKeys = %+v", profile.SigningKeys)
	}
}

func TestBusinessDiscoveryProfileRoundTrip(t *testing.T) {
	data := []byte(`{
		"ucp": {
			"version": "2026-04-08",
			"services": {"dev.ucp.shopping.checkout": [{"version": "2026-04-08", "transport": "rest", "endpoint": "https://merchant.example/api"}]},
			"payment_handlers": {"com.stripe.card": [{"version": "2026-04-08", "id": "stripe_card_1"}]},
			"supported_versions": {"2026-01-11": "https://merchant.example/.well-known/ucp/2026-01-11"}
		}
	}`)
	profile := roundTrip[BusinessDiscoveryProfile](t, data)
	if err := profile.UCP.Validate(); err != nil {
		t.Errorf("UCP.Validate() error = %v, want nil", err)
	}
	if profile.UCP.SupportedVersions["2026-01-11"] == "" {
		t.Errorf("SupportedVersions = %v", profile.UCP.SupportedVersions)
	}
}

func TestErrorResponseMetadataValidate(t *testing.T) {
	m := ErrorResponseMetadata{UCPMetadataBase: UCPMetadataBase{Version: "2026-04-08", Status: UCPStatusSuccess}}
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
		{"a2a needs only spec", Service{Entity: Entity{Version: "2026-04-08", Spec: "https://x"}, Transport: ServiceTransportA2A}, false},
		{"rest needs schema too", Service{Entity: Entity{Version: "2026-04-08", Spec: "https://x"}, Transport: ServiceTransportREST}, true},
		{"rest with schema passes", Service{Entity: Entity{Version: "2026-04-08", Spec: "https://x", Schema: "https://y"}, Transport: ServiceTransportREST}, false},
		{"missing spec always fails", Service{Entity: Entity{Version: "2026-04-08"}, Transport: ServiceTransportA2A}, true},
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
