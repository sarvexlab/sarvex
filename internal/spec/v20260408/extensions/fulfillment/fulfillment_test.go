// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package fulfillment

import (
	"encoding/json"
	"testing"
)

func TestCheckoutWithFulfillmentRoundTrip(t *testing.T) {
	data := []byte(`{
		"id": "chk_1",
		"line_items": [],
		"currency": "USD",
		"totals": [],
		"links": [],
		"fulfillment": {
			"methods": [{"id": "fm_1", "type": "shipping", "line_item_ids": ["li_1"]}]
		}
	}`)
	var c CheckoutWithFulfillment
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if c.ID != "chk_1" {
		t.Errorf("Checkout.ID = %q, want %q (base Checkout must be composed, not duplicated)", c.ID, "chk_1")
	}
	if c.Fulfillment == nil || len(c.Fulfillment.Methods) != 1 {
		t.Fatalf("Fulfillment = %+v", c.Fulfillment)
	}
}

func TestPlatformCapabilityRoundTrip(t *testing.T) {
	data := []byte(`{
		"version": "2026-04-08",
		"spec": "https://ucp.dev/spec",
		"schema": "https://ucp.dev/schema",
		"config": {"supports_multi_group": true}
	}`)
	var c PlatformCapability
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if c.Version != "2026-04-08" {
		t.Errorf("Capability.Version = %q, want %q (base Capability must be composed, not duplicated)", c.Version, "2026-04-08")
	}
	if c.Config == nil || !c.Config.SupportsMultiGroup {
		t.Fatalf("Config = %+v, want SupportsMultiGroup=true", c.Config)
	}

	// Regression check for the Go embedding trap: the typed Config must
	// actually appear on the wire, not be silently dropped by a promoted
	// Capability.MarshalJSON.
	body, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	cfg, ok := m["config"].(map[string]any)
	if !ok {
		t.Fatalf(`marshaled output missing "config" object, got %v`, m["config"])
	}
	if cfg["supports_multi_group"] != true {
		t.Errorf("marshaled config.supports_multi_group = %v, want true", cfg["supports_multi_group"])
	}
}

func TestBusinessCapabilityRoundTrip(t *testing.T) {
	data := []byte(`{
		"version": "2026-04-08",
		"config": {
			"allows_multi_destination": {"shipping": true, "pickup": false},
			"allows_method_combinations": [["shipping", "pickup"]]
		}
	}`)
	var c BusinessCapability
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if c.Config == nil || c.Config.AllowsMultiDestination == nil || !c.Config.AllowsMultiDestination.Shipping {
		t.Fatalf("Config = %+v", c.Config)
	}

	body, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["config"]; !ok {
		t.Errorf(`marshaled output missing "config", got %v`, m)
	}
}
