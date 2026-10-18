// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package domain

import "github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"

// StoreBusinessProfile holds merchant-facing store identity and business facts.
// This is control-plane data owned by the merchant during onboarding.
// Per sarvex-v1-architecture.md A10.4: tenant isolation at store_id level, merchant owns this data.
type StoreBusinessProfile struct {
	StoreID         string // Unique store identifier
	MerchantID      string // Merchant owner (denormalized for queries)
	LegalName       string // Store's legal business name
	SupportEmail    string // Support contact for the store
	StorefrontURL   string // Store's public domain/storefront URL
	ReturnPolicyURL string // URL of this store's return policy
}

// StoreConfiguration holds technical configuration for a store.
// This includes which UCP capabilities are enabled and shipping/fulfillment setup.
type StoreConfiguration struct {
	StoreID            string                       // Unique store identifier
	MerchantID         string                       // Merchant owner (denormalized for queries)
	FulfillmentRegions []string                     // Store-specific fulfillment regions
	ShippingZones      map[string]ShippingZone      // Shipping zones keyed by zone ID
	Capabilities       map[string][]wire.Capability // Capabilities enabled by service
}
