// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package codec

import (
	"encoding/json"
	"fmt"

	v20260825 "github.com/sarvexlab/sarvex/internal/spec/v20260825"
	consent25 "github.com/sarvexlab/sarvex/internal/spec/v20260825/extensions/buyerconsent"
)

// consentPurposes maps the four purposes 2026-04-08's buyer consent has (its
// fixed boolean fields) to their 2026-08-25 reverse-DNS purpose keys, with the
// description a response must carry for each.
var consentPurposes = []struct {
	legacy      string
	purpose     v20260825.ReverseDomainName
	description string
}{
	{"analytics", consent25.PurposeAnalytics, "Use of the buyer's data to measure and improve the service."},
	{"preferences", consent25.PurposePreferences, "Remembering the buyer's preferences."},
	{"marketing", consent25.PurposeMarketing, "Marketing communications to the buyer."},
	{"sale_of_data", consent25.PurposeSaleOrSharing, "Sale or sharing of the buyer's data with third parties."},
}

// consentToCanonical converts a 2026-08-25 consent map to the canonical
// booleans. A purpose the canonical model has no field for is not lost here —
// the caller keeps the original for echoing (RequestExtras.BuyerConsent) —
// but it cannot be persisted at a merchant that only models four booleans.
func consentToCanonical(wire any) (map[string]any, error) {
	purposes, ok := wire.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("wireadapt: buyer.consent must be an object keyed by purpose")
	}
	out := map[string]any{}
	for _, p := range consentPurposes {
		entry, present := purposes[string(p.purpose)]
		if !present {
			continue
		}
		obj, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("wireadapt: buyer.consent[%s] must be an object", p.purpose)
		}
		granted, ok := obj["granted"].(bool)
		if !ok {
			return nil, fmt.Errorf("wireadapt: buyer.consent[%s].granted is required and must be a boolean", p.purpose)
		}
		out[p.legacy] = granted
	}
	return out, nil
}

// consentToV20260825 renders canonical consent booleans as a 2026-08-25
// consent map. wire is the request's original consent, if any: where it
// agrees with the canonical value it is echoed as sent, so segments, links
// and the platform's own description survive; where it disagrees (the merchant
// answered differently) the canonical value wins. Purposes only the wire
// carried (vendor purposes, standard ones the merchant did not persist) are
// echoed too, since the platform stated them and the response is the
// business's record of what it received.
func consentToV20260825(canonical map[string]any, wire json.RawMessage) (map[string]any, error) {
	var original map[string]any
	if len(wire) > 0 {
		if err := json.Unmarshal(wire, &original); err != nil {
			original = nil
		}
	}

	out := map[string]any{}
	for _, p := range consentPurposes {
		granted, ok := canonical[p.legacy].(bool)
		if !ok {
			continue
		}
		entry := map[string]any{
			"granted":     granted,
			"source":      string(consent25.ConsentSourcePlatform),
			"description": p.description,
		}
		if orig, ok := original[string(p.purpose)].(map[string]any); ok {
			if og, _ := orig["granted"].(bool); og == granted {
				entry = withRequiredFields(orig, p.description)
			}
		}
		out[string(p.purpose)] = entry
	}
	for key, val := range original {
		if _, done := out[key]; done {
			continue
		}
		if obj, ok := val.(map[string]any); ok {
			out[key] = withRequiredFields(obj, "Consent stated by the platform.")
		}
	}
	return out, nil
}

// withRequiredFields fills in what a 2026-08-25 response must carry on a
// consent purpose (source and description) when a request omitted it.
func withRequiredFields(entry map[string]any, defaultDescription string) map[string]any {
	out := make(map[string]any, len(entry)+2)
	for k, v := range entry {
		out[k] = v
	}
	if s, _ := out["source"].(string); s == "" {
		out["source"] = string(consent25.ConsentSourcePlatform)
	}
	if s, _ := out["description"].(string); s == "" {
		out["description"] = defaultDescription
	}
	return out
}
