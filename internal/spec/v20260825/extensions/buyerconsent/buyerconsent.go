// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package buyerconsent implements the UCP Buyer Consent extension
// (dev.ucp.shopping.buyer_consent): buyer consent tracking for privacy
// compliance via the Checkout buyer object.
//
// Breaking change from v20260408: that version's Consent was a fixed set
// of four nullable booleans (Analytics, Preferences, Marketing,
// SaleOfData). This spec version replaces it with Consent, a map keyed
// by reverse-DNS purpose identifier, whose values (ConsentPurpose) carry
// not just the granted boolean but its Source (business default vs.
// platform-captured buyer decision), a required human-readable
// Description, optional Links, and optional finer-grained Segments
// (ConsentSegment) scoping the decision to specific channels, vendors,
// or programs within that purpose. See DISCREPANCIES.md.
package buyerconsent

import (
	"encoding/json"
	"fmt"

	v20260825 "github.com/sarvexlab/sarvex/internal/spec/v20260825"
)

// Well-known reverse-DNS purpose identifiers for Consent's keys, per the
// spec's buyer_consent.json description.
const (
	PurposeMarketing     v20260825.ReverseDomainName = "dev.ucp.consent.marketing"
	PurposeAnalytics     v20260825.ReverseDomainName = "dev.ucp.consent.analytics"
	PurposePreferences   v20260825.ReverseDomainName = "dev.ucp.consent.preferences"
	PurposeSaleOrSharing v20260825.ReverseDomainName = "dev.ucp.consent.sale_or_sharing"
)

// Well-known reverse-DNS segment identifiers scoped to PurposeMarketing,
// per the spec's buyer_consent.json description.
const (
	SegmentMarketingEmail v20260825.ReverseDomainName = "dev.ucp.consent.marketing.email"
	SegmentMarketingSMS   v20260825.ReverseDomainName = "dev.ucp.consent.marketing.sms"
)

// ConsentSource identifies the party that asserted a ConsentPurpose or
// ConsentSegment's current Granted value.
type ConsentSource string

const (
	// ConsentSourceBusiness: the value reflects the business's default policy.
	ConsentSourceBusiness ConsentSource = "business"
	// ConsentSourcePlatform: the value reflects an explicit buyer decision
	// captured by the platform.
	ConsentSourcePlatform ConsentSource = "platform"
)

var knownConsentSources = map[ConsentSource]struct{}{
	ConsentSourceBusiness: {},
	ConsentSourcePlatform: {},
}

// Known reports whether s is one of the two standardized consent sources.
func (s ConsentSource) Known() bool { _, ok := knownConsentSources[s]; return ok }

// ConsentSegment is a buyer's consent decision for a specific refinement
// of a parent ConsentPurpose (e.g. email marketing under the marketing
// purpose). Overrides the parent's Granted value for this scope.
// Segments do not nest further.
type ConsentSegment struct {
	// Granted overrides the parent purpose's Granted value for this
	// specific scope.
	Granted bool `json:"granted"`
	// Source identifies the party that asserted Granted for this segment.
	Source ConsentSource `json:"source"`
	// Description is a human-readable description of what the buyer is
	// consenting to within this segment. Required in responses.
	Description string `json:"description,omitempty"`
	// Links are optional segment-specific links (e.g. channel terms).
	Links []v20260825.Link `json:"links,omitempty"`
}

// ConsentPurpose is a buyer's consent decision for a purpose (e.g.
// marketing, analytics): the current binary state, its Source, a
// required human-readable Description, and optional Links and Segments
// scoping the decision to specific channels, vendors, or programs.
type ConsentPurpose struct {
	// Granted reports whether consent has been granted for this purpose.
	Granted bool `json:"granted"`
	// Source identifies the party that asserted Granted.
	Source ConsentSource `json:"source"`
	// Description is a human-readable description of what the buyer is
	// consenting to. Required in responses.
	Description string `json:"description,omitempty"`
	// Links are optional links providing context (e.g. privacy policy).
	Links []v20260825.Link `json:"links,omitempty"`
	// Segments are optional refinements scoping this purpose to specific
	// channels, vendors, or programs, keyed by reverse-DNS identifier.
	Segments map[v20260825.ReverseDomainName]ConsentSegment `json:"segments,omitempty"`
}

// Consent holds per-purpose consent decisions, keyed by reverse-DNS
// purpose identifier (see the Purpose* constants for the well-known
// ones). Businesses and vendors may define additional purposes under
// their own reverse-DNS namespace.
type Consent map[v20260825.ReverseDomainName]ConsentPurpose

// BuyerWithConsent is Buyer extended with per-purpose consent tracking.
// Composes the base Buyer type rather than duplicating its fields.
type BuyerWithConsent struct {
	v20260825.Buyer
	Consent Consent `json:"consent,omitempty"`
}

// UnmarshalJSON populates the embedded Buyer (including its Extra
// passthrough) and Consent.
func (b *BuyerWithConsent) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &b.Buyer); err != nil {
		return err
	}
	var c struct {
		Consent Consent `json:"consent"`
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return fmt.Errorf("ucp/buyerconsent: buyer with consent: %w", err)
	}
	b.Consent = c.Consent
	return nil
}

// MarshalJSON layers Consent on top of the embedded Buyer's fields.
// Defined explicitly so Buyer.MarshalJSON is not promoted verbatim, which
// would otherwise silently drop Consent.
func (b BuyerWithConsent) MarshalJSON() ([]byte, error) {
	m, err := b.FieldMap()
	if err != nil {
		return nil, err
	}
	if b.Consent != nil {
		body, err := json.Marshal(b.Consent)
		if err != nil {
			return nil, err
		}
		m["consent"] = body
	}
	return json.Marshal(m)
}

// CheckoutWithBuyerConsent is Checkout extended with consent tracking via
// the buyer object. Composes the base Checkout type rather than
// duplicating its fields; Buyer shadows Checkout.Buyer with the
// consent-aware variant.
type CheckoutWithBuyerConsent struct {
	v20260825.Checkout
	Buyer *BuyerWithConsent `json:"buyer,omitempty"`
}
