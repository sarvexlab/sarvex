// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package buyerconsent implements the UCP Buyer Consent extension
// (dev.ucp.shopping.buyer_consent): buyer consent tracking for privacy
// compliance via the Checkout buyer object.
package buyerconsent

import (
	"encoding/json"
	"fmt"

	v20260408 "github.com/sarvexlab/sarvex/internal/spec/v20260408"
)

// Consent holds user consent states for data processing. Pointers
// distinguish "not stated" from an explicit grant or denial — consent
// semantics depend on that distinction.
type Consent struct {
	Analytics   *bool `json:"analytics,omitempty"`
	Preferences *bool `json:"preferences,omitempty"`
	Marketing   *bool `json:"marketing,omitempty"`
	// SaleOfData is consent for selling data to third parties (CCPA).
	SaleOfData *bool `json:"sale_of_data,omitempty"`
}

// BuyerWithConsent is Buyer extended with consent tracking. Composes the
// base Buyer type rather than duplicating its fields.
type BuyerWithConsent struct {
	v20260408.Buyer
	Consent *Consent `json:"consent,omitempty"`
}

// UnmarshalJSON populates the embedded Buyer (including its Extra
// passthrough) and Consent.
func (b *BuyerWithConsent) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &b.Buyer); err != nil {
		return err
	}
	var c struct {
		Consent *Consent `json:"consent"`
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
	m, err := b.Buyer.FieldMap()
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
	v20260408.Checkout
	Buyer *BuyerWithConsent `json:"buyer,omitempty"`
}
