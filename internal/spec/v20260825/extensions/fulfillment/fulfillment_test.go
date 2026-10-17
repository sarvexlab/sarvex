// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package fulfillment

import (
	"encoding/json"
	"reflect"
	"testing"
)

// roundTrip unmarshals data into a fresh T, marshals it back out, and
// re-unmarshals that output into a second T, asserting the two are
// deep-equal.
func roundTrip[T any](t *testing.T, data []byte) T {
	t.Helper()
	var first T
	if err := json.Unmarshal(data, &first); err != nil {
		t.Fatalf("unmarshal: %v\ndata: %s", err, data)
	}
	out, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var second T
	if err := json.Unmarshal(out, &second); err != nil {
		t.Fatalf("re-unmarshal: %v\ndata: %s", err, out)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("round trip mismatch:\nfirst:  %#v\nsecond: %#v\nmarshaled: %s", first, second, out)
	}
	return first
}

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
		"version": "2026-08-25",
		"spec": "https://ucp.dev/spec",
		"schema": "https://ucp.dev/schema",
		"config": {"supports_multi_group": true}
	}`)
	var c PlatformCapability
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if c.Version != "2026-08-25" {
		t.Errorf("Capability.Version = %q, want %q (base Capability must be composed, not duplicated)", c.Version, "2026-08-25")
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

// TestBusinessCapabilityRoundTrip exercises the breaking change from
// v20260408: multi_destination is now a list of {method, ...} entries,
// and the "allows_" field-name prefix is gone.
func TestBusinessCapabilityRoundTrip(t *testing.T) {
	data := []byte(`{
		"version": "2026-08-25",
		"config": {
			"multi_destination": [{"method": "shipping"}],
			"method_combinations": [["shipping", "pickup"]]
		}
	}`)
	var c BusinessCapability
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if c.Config == nil || len(c.Config.MultiDestination) != 1 || c.Config.MultiDestination[0].Method != "shipping" {
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

// TestCatalogFulfillmentRoundTrip exercises the new-in-this-spec-version
// catalog-level fulfillment preview attached to a Variant.
func TestCatalogFulfillmentRoundTrip(t *testing.T) {
	data := []byte(`{
		"methods": [
			{
				"type": "pickup",
				"description": {"plain": "Ready in 1 hour"},
				"availability": {"available": true, "status": "in_stock"},
				"location": "loc_downtown",
				"options": [{"id": "opt_1", "title": "Curbside"}]
			}
		]
	}`)
	f := roundTrip[CatalogFulfillment](t, data)
	if len(f.Methods) != 1 {
		t.Fatalf("Methods = %+v", f.Methods)
	}
	m := f.Methods[0]
	if m.Type != "pickup" || m.Location != "loc_downtown" {
		t.Errorf("Methods[0] = %+v", m)
	}
	if m.Availability == nil || !m.Availability.Available {
		t.Errorf("Availability = %+v", m.Availability)
	}
	if len(m.Options) != 1 || m.Options[0].Title != "Curbside" {
		t.Errorf("Options = %+v", m.Options)
	}
}

func TestFulfillmentVariantComposesBaseVariant(t *testing.T) {
	data := []byte(`{
		"id": "var_1",
		"title": "Widget",
		"description": {"plain": "A widget."},
		"price": {"amount": 1000, "currency": "USD"},
		"fulfillment": {"methods": [{"type": "shipping"}]}
	}`)
	var v FulfillmentVariant
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if v.ID != "var_1" {
		t.Errorf("Variant.ID = %q, want %q (base Variant must be composed, not duplicated)", v.ID, "var_1")
	}
	if v.Fulfillment == nil || len(v.Fulfillment.Methods) != 1 {
		t.Fatalf("Fulfillment = %+v", v.Fulfillment)
	}
}

func TestSearchFiltersWithFulfillmentRoundTrip(t *testing.T) {
	data := []byte(`{
		"categories": ["shoes"],
		"fulfills_to": {"address_country": "US", "postal_code": "94043"},
		"methods": ["pickup"]
	}`)
	var f SearchFiltersWithFulfillment
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(f.Categories) != 1 || f.Categories[0] != "shoes" {
		t.Errorf("Categories = %v (base SearchFilters must be composed, not duplicated)", f.Categories)
	}
	if f.FulfillsTo == nil || f.FulfillsTo.AddressCountry != "US" {
		t.Fatalf("FulfillsTo = %+v", f.FulfillsTo)
	}
	if len(f.Methods) != 1 || f.Methods[0] != "pickup" {
		t.Errorf("Methods = %v", f.Methods)
	}

	// Regression check for the Go embedding trap.
	body, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["fulfills_to"]; !ok {
		t.Errorf(`marshaled output missing "fulfills_to", got %v`, m)
	}
	if _, ok := m["categories"]; !ok {
		t.Errorf(`marshaled output missing "categories" (base field dropped), got %v`, m)
	}
}
