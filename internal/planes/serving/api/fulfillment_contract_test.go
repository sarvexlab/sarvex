// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import "testing"

func TestFulfillmentToBase_PickupSelectionByID(t *testing.T) {
	w := &wireFulfillmentRequest{Methods: []wireFulfillmentMethod{{
		Type:                  "pickup",
		SelectedDestinationID: "pickup_store",
		Groups:                []wireFulfillmentGroup{{ID: "group_pickup", SelectedOptionID: "local_pickup"}},
	}}}
	got := w.toBase()
	if got == nil || got.MethodType != "pickup" || got.SelectedDestinationID != "pickup_store" || got.SelectedMethodID != "local_pickup" {
		t.Fatalf("toBase = %+v, want a pickup selection by destination and option id", got)
	}
	if got.SelectedDestination != nil {
		t.Errorf("a pickup selection carries no postal address, got %+v", got.SelectedDestination)
	}

	// Shipping keeps its behaviour and reports no method type.
	ship := &wireFulfillmentRequest{Methods: []wireFulfillmentMethod{{
		Destinations: []wireFulfillmentDestination{{ID: "d1"}},
		Groups:       []wireFulfillmentGroup{{SelectedOptionID: "flat_rate"}},
	}}}
	if b := ship.toBase(); b == nil || b.MethodType != "" || b.SelectedMethodID != "flat_rate" {
		t.Errorf("shipping toBase regressed: %+v", b)
	}
}
