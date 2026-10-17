// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v20260825

import (
	"encoding/json"
	"testing"
)

func TestFulfillmentDestinationShippingWithDiscriminator(t *testing.T) {
	data := []byte(`{
		"type": "shipping_address",
		"id": "dest_1",
		"street_address": "1600 Amphitheatre Pkwy",
		"address_locality": "Mountain View",
		"address_region": "CA",
		"address_country": "US",
		"postal_code": "94043"
	}`)
	d := roundTrip[FulfillmentDestination](t, data)
	if d.Shipping == nil {
		t.Fatal("Shipping = nil, want non-nil")
	}
	if d.Retail != nil {
		t.Error("Retail != nil, want nil")
	}
	if d.Shipping.ID != "dest_1" {
		t.Errorf("Shipping.ID = %q, want %q", d.Shipping.ID, "dest_1")
	}
	if d.Shipping.AddressLocality != "Mountain View" {
		t.Errorf("Shipping.AddressLocality = %q, want %q", d.Shipping.AddressLocality, "Mountain View")
	}
}

func TestFulfillmentDestinationBusinessLocationWithDiscriminator(t *testing.T) {
	data := []byte(`{
		"type": "business_location",
		"id": "loc_1",
		"name": "Downtown Store",
		"address": {"street_address": "123 Main St", "address_locality": "Springfield"}
	}`)
	d := roundTrip[FulfillmentDestination](t, data)
	if d.Retail == nil {
		t.Fatal("Retail = nil, want non-nil")
	}
	if d.Shipping != nil {
		t.Error("Shipping != nil, want nil")
	}
	if d.Retail.Name != "Downtown Store" {
		t.Errorf("Retail.Name = %q, want %q", d.Retail.Name, "Downtown Store")
	}
}

// TestFulfillmentDestinationHeuristicFallback exercises the fallback path
// for platform requests that omit the (optional-in-requests) "type"
// discriminator, mirroring v20260408's purely structural dispatch.
func TestFulfillmentDestinationHeuristicFallback(t *testing.T) {
	shipData := []byte(`{"id": "dest_2", "street_address": "1 Infinite Loop", "address_country": "US"}`)
	d := roundTrip[FulfillmentDestination](t, shipData)
	if d.Shipping == nil {
		t.Fatal("Shipping = nil, want non-nil (no type, no name -> shipping heuristic)")
	}

	retailData := []byte(`{"id": "loc_2", "name": "Mall Kiosk"}`)
	d2 := roundTrip[FulfillmentDestination](t, retailData)
	if d2.Retail == nil {
		t.Fatal("Retail = nil, want non-nil (no type, has name -> retail heuristic)")
	}
}

func TestFulfillmentDestinationMarshalInjectsDiscriminator(t *testing.T) {
	d := FulfillmentDestination{Retail: &BusinessLocationDestination{ID: "loc_3", Name: "Kiosk"}}
	body, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if m["type"] != "business_location" {
		t.Errorf(`marshaled "type" = %v, want "business_location"`, m["type"])
	}

	d2 := FulfillmentDestination{Shipping: &ShippingDestination{ID: "dest_3"}}
	body2, err := json.Marshal(d2)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var m2 map[string]any
	if err := json.Unmarshal(body2, &m2); err != nil {
		t.Fatal(err)
	}
	if m2["type"] != "shipping_address" {
		t.Errorf(`marshaled "type" = %v, want "shipping_address"`, m2["type"])
	}
}

func TestFulfillmentMethodWithDestinationsRoundTrip(t *testing.T) {
	data := []byte(`{
		"id": "fm_1",
		"type": "shipping",
		"line_item_ids": ["li_1", "li_2"],
		"destinations": [
			{"type": "shipping_address", "id": "d1", "street_address": "1 Infinite Loop", "address_country": "US"},
			{"type": "business_location", "id": "loc_2", "name": "Mall Kiosk"}
		],
		"selected_destination_id": "d1",
		"groups": [
			{
				"id": "g1",
				"line_item_ids": ["li_1"],
				"options": [{"id": "opt_1", "title": "Standard", "totals": [{"type": "fulfillment", "amount": 500}]}],
				"selected_option_id": "opt_1"
			}
		]
	}`)
	m := roundTrip[FulfillmentMethod](t, data)
	if len(m.Destinations) != 2 {
		t.Fatalf("len(Destinations) = %d, want 2", len(m.Destinations))
	}
	if m.Destinations[0].Shipping == nil {
		t.Error("Destinations[0] expected shipping variant")
	}
	if m.Destinations[1].Retail == nil {
		t.Error("Destinations[1] expected retail variant")
	}
	if m.SelectedDestinationID == nil || *m.SelectedDestinationID != "d1" {
		t.Errorf("SelectedDestinationID = %v, want d1", m.SelectedDestinationID)
	}
}

func TestFulfillmentEventValidate(t *testing.T) {
	tests := []struct {
		name    string
		event   FulfillmentEvent
		wantErr bool
	}{
		{
			name:    "processing needs no tracking",
			event:   FulfillmentEvent{Type: FulfillmentEventTypeProcessing},
			wantErr: false,
		},
		{
			name:    "shipped without tracking fails",
			event:   FulfillmentEvent{Type: FulfillmentEventTypeShipped},
			wantErr: true,
		},
		{
			name: "shipped with tracking passes",
			event: FulfillmentEvent{
				Type:           FulfillmentEventTypeShipped,
				TrackingNumber: "1Z999",
				TrackingURL:    "https://example.com/track/1Z999",
			},
			wantErr: false,
		},
		{
			name: "delivered missing tracking_url fails",
			event: FulfillmentEvent{
				Type:           FulfillmentEventTypeDelivered,
				TrackingNumber: "1Z999",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFulfillmentContainerRoundTrip(t *testing.T) {
	data := []byte(`{
		"methods": [{"id": "fm_1", "type": "pickup", "line_item_ids": ["li_1"]}],
		"available_methods": [
			{"type": "shipping", "line_item_ids": ["li_1"], "fulfillable_on": "now"},
			{"type": "pickup", "line_item_ids": ["li_2"], "fulfillable_on": null}
		]
	}`)
	f := roundTrip[Fulfillment](t, data)
	if len(f.AvailableMethods) != 2 {
		t.Fatalf("len(AvailableMethods) = %d, want 2", len(f.AvailableMethods))
	}
	if f.AvailableMethods[0].FulfillableOn == nil || *f.AvailableMethods[0].FulfillableOn != "now" {
		t.Errorf("AvailableMethods[0].FulfillableOn = %v, want \"now\"", f.AvailableMethods[0].FulfillableOn)
	}
	if f.AvailableMethods[1].FulfillableOn != nil {
		t.Errorf("AvailableMethods[1].FulfillableOn = %v, want nil for explicit JSON null", *f.AvailableMethods[1].FulfillableOn)
	}
}

// TestBusinessFulfillmentConfigMultiDestinationRoundTrip exercises the
// breaking change from v20260408: MultiDestination is now a list of
// {method, ...} entries, not a fixed {shipping, pickup} boolean map, and
// the "allows_" prefix is gone from both field names.
func TestBusinessFulfillmentConfigMultiDestinationRoundTrip(t *testing.T) {
	data := []byte(`{
		"multi_destination": [{"method": "shipping"}, {"method": "pickup"}],
		"method_combinations": [["shipping", "pickup"]]
	}`)
	c := roundTrip[BusinessFulfillmentConfig](t, data)
	if len(c.MultiDestination) != 2 {
		t.Fatalf("len(MultiDestination) = %d, want 2", len(c.MultiDestination))
	}
	if c.MultiDestination[0].Method != FulfillmentMethodTypeShipping {
		t.Errorf("MultiDestination[0].Method = %q, want %q", c.MultiDestination[0].Method, FulfillmentMethodTypeShipping)
	}
	if len(c.MethodCombinations) != 1 || len(c.MethodCombinations[0]) != 2 {
		t.Errorf("MethodCombinations = %+v", c.MethodCombinations)
	}
}

func TestFulfillmentDestinationFilterRoundTrip(t *testing.T) {
	data := []byte(`{"address_country": "US", "address_region": "CA", "custom_hint": true}`)
	f := roundTrip[FulfillmentDestinationFilter](t, data)
	if f.AddressCountry != "US" || f.AddressRegion != "CA" {
		t.Errorf("FulfillmentDestinationFilter = %+v", f)
	}
	if f.Extra["custom_hint"] != true {
		t.Errorf("Extra[custom_hint] = %v, want true", f.Extra["custom_hint"])
	}
}
