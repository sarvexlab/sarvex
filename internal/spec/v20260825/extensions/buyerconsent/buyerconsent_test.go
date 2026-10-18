// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package buyerconsent

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestBuyerWithConsentRoundTrip exercises the breaking change from
// v20260408: Consent is now a reverse-DNS-keyed map of ConsentPurpose
// objects (each carrying granted/source/description/segments), not a
// fixed set of nullable booleans.
func TestBuyerWithConsentRoundTrip(t *testing.T) {
	data := []byte(`{
		"first_name": "Jane",
		"email": "jane@example.com",
		"loyalty_tier": "gold",
		"consent": {
			"dev.ucp.consent.analytics": {
				"granted": true,
				"source": "platform",
				"description": "Usage analytics"
			},
			"dev.ucp.consent.marketing": {
				"granted": false,
				"source": "business",
				"description": "Promotional communications across all channels",
				"segments": {
					"dev.ucp.consent.marketing.email": {
						"granted": true,
						"source": "platform",
						"description": "Promotional emails"
					}
				}
			}
		}
	}`)
	var first BuyerWithConsent
	if err := json.Unmarshal(data, &first); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if first.FirstName != "Jane" {
		t.Errorf("FirstName = %q, want %q (base Buyer must be composed, not duplicated)", first.FirstName, "Jane")
	}
	if first.Extra["loyalty_tier"] != "gold" {
		t.Errorf("Extra[loyalty_tier] = %v, want gold", first.Extra["loyalty_tier"])
	}
	analytics, ok := first.Consent[PurposeAnalytics]
	if !ok || !analytics.Granted || analytics.Source != ConsentSourcePlatform {
		t.Fatalf("Consent[analytics] = %+v", analytics)
	}
	marketing, ok := first.Consent[PurposeMarketing]
	if !ok || marketing.Granted || marketing.Source != ConsentSourceBusiness {
		t.Fatalf("Consent[marketing] = %+v", marketing)
	}
	emailSegment, ok := marketing.Segments[SegmentMarketingEmail]
	if !ok || !emailSegment.Granted {
		t.Fatalf("Consent[marketing].Segments[email] = %+v", emailSegment)
	}

	// Regression check for the Go embedding trap: Consent (and Extra) must
	// actually appear on the wire, not be silently dropped by a promoted
	// Buyer.MarshalJSON.
	body, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["consent"]; !ok {
		t.Errorf(`marshaled output missing "consent", got %v`, m)
	}
	if m["loyalty_tier"] != "gold" {
		t.Errorf(`marshaled output missing extra "loyalty_tier", got %v`, m)
	}

	var second BuyerWithConsent
	if err := json.Unmarshal(body, &second); err != nil {
		t.Fatalf("re-Unmarshal() error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("round trip mismatch:\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func TestConsentSourceKnown(t *testing.T) {
	if !ConsentSourceBusiness.Known() {
		t.Error("ConsentSourceBusiness.Known() = false, want true")
	}
	if ConsentSource("bogus").Known() {
		t.Error(`ConsentSource("bogus").Known() = true, want false`)
	}
}

func TestCheckoutWithBuyerConsentRoundTrip(t *testing.T) {
	data := []byte(`{
		"id": "chk_1",
		"line_items": [],
		"currency": "USD",
		"totals": [],
		"links": [],
		"buyer": {
			"first_name": "Jane",
			"consent": {
				"dev.ucp.consent.sale_or_sharing": {"granted": false, "source": "business", "description": "Do not sell my data"}
			}
		}
	}`)
	var c CheckoutWithBuyerConsent
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if c.ID != "chk_1" {
		t.Errorf("Checkout.ID = %q, want %q", c.ID, "chk_1")
	}
	if c.Buyer == nil || c.Buyer.FirstName != "Jane" {
		t.Fatalf("Buyer = %+v", c.Buyer)
	}
	sale, ok := c.Buyer.Consent[PurposeSaleOrSharing]
	if !ok || sale.Granted {
		t.Errorf("Buyer.Consent[sale_or_sharing] = %+v", sale)
	}
}
