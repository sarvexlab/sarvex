// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package billing

import "testing"

func TestMockStripeClient_CreatePortalSessionIncludesConfiguration(t *testing.T) {
	c := newMockStripeClient()

	url, err := c.CreatePortalSession("cus_123", "https://example.com/return", "bpc_123")
	if err != nil {
		t.Fatalf("CreatePortalSession: %v", err)
	}
	if url == "" {
		t.Fatal("expected non-empty url")
	}
}

func TestMockStripeClient_EnsurePortalConfigurationCreatesWhenNoExistingID(t *testing.T) {
	c := newMockStripeClient()

	id, err := c.EnsurePortalConfiguration("", PortalConfigParams{
		DefaultReturnURL: "https://example.com/billing",
		SwitchableProducts: []PortalProduct{
			{ProductID: "prod_concierge", PriceID: "price_concierge"},
		},
	})
	if err != nil {
		t.Fatalf("EnsurePortalConfiguration: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty configuration id")
	}

	configs := c.PortalConfigurations()
	if len(configs) != 1 {
		t.Fatalf("expected 1 recorded configuration, got %d", len(configs))
	}
	if _, ok := configs[id]; !ok {
		t.Fatalf("expected configuration recorded under id %s", id)
	}
}

func TestMockStripeClient_EnsurePortalConfigurationUpdatesExisting(t *testing.T) {
	c := newMockStripeClient()

	id, err := c.EnsurePortalConfiguration("bpc_existing", PortalConfigParams{})
	if err != nil {
		t.Fatalf("EnsurePortalConfiguration: %v", err)
	}
	if id != "bpc_existing" {
		t.Fatalf("expected existing id to be preserved, got %s", id)
	}
}

func TestMockStripeClient_GetPriceProductID(t *testing.T) {
	c := newMockStripeClient()

	productID, err := c.GetPriceProductID("price_123")
	if err != nil {
		t.Fatalf("GetPriceProductID: %v", err)
	}
	if productID == "" {
		t.Fatal("expected non-empty product id")
	}
}
