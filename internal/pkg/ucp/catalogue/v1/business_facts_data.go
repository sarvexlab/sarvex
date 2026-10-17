// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v1

// DefaultRequiredBusinessFacts returns the V1 set of Google-required business
// facts and the tenancy level each is captured at. Mirrors the merchant-level
// facts in sarvex-v1-architecture.md §1 ("legal entity, support contact,
// fulfillment regions") and the store-level business profile in §3 Domain 1
// ("legal entity, support contact, return policy reference, fulfillment
// regions") — return policy is store-level: a merchant with multiple stores
// can point agents at a different returns page per storefront.
//
// These are Sarvex-internal business facts, not UCP wire types — the
// "Pinned to UCP spec version 2026-06-15" this comment previously carried
// didn't apply to this data and referenced a UCP release date that
// doesn't exist (see DefaultScoringRubric's doc comment for the same
// stale value elsewhere in this package).
func DefaultRequiredBusinessFacts() []RequiredBusinessFact {
	return []RequiredBusinessFact{
		{
			ID:          "merchant_legal_entity_name",
			Level:       BusinessFactLevelMerchant,
			Field:       "legal_entity_name",
			Description: "Registered legal entity name of the merchant.",
		},
		{
			ID:          "merchant_support_email",
			Level:       BusinessFactLevelMerchant,
			Field:       "support_email",
			Description: "Merchant support contact email.",
		},
		{
			ID:          "merchant_fulfillment_regions",
			Level:       BusinessFactLevelMerchant,
			Field:       "fulfillment_regions",
			Description: "ISO country codes the merchant fulfills orders to.",
		},
		{
			ID:          "store_legal_name",
			Level:       BusinessFactLevelStore,
			Field:       "legal_name",
			Description: "Store's legal business name bound into the UCP profile.",
		},
		{
			ID:          "store_support_email",
			Level:       BusinessFactLevelStore,
			Field:       "support_email",
			Description: "Store-level support contact bound into the UCP profile.",
		},
		{
			ID:          "store_return_policy_url",
			Level:       BusinessFactLevelStore,
			Field:       "return_policy_url",
			Description: "URL of this store's return policy.",
		},
		{
			ID:          "store_fulfillment_regions",
			Level:       BusinessFactLevelStore,
			Field:       "fulfillment_regions",
			Description: "ISO country codes this store fulfills orders to.",
		},
	}
}
