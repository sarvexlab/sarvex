// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v20260408

import "testing"

func TestFulfillmentDestinationShipping(t *testing.T) {
	data := []byte(`{
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

func TestFulfillmentDestinationRetail(t *testing.T) {
	data := []byte(`{
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

func TestFulfillmentMethodWithDestinationsRoundTrip(t *testing.T) {
	data := []byte(`{
		"id": "fm_1",
		"type": "shipping",
		"line_item_ids": ["li_1", "li_2"],
		"destinations": [
			{"id": "d1", "street_address": "1 Infinite Loop", "address_country": "US"},
			{"id": "loc_2", "name": "Mall Kiosk"}
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
