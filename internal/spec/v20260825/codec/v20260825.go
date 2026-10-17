// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package codec is the wireadapt codec for UCP 2026-08-25. Relative to the
// canonical 2026-04-08 model, the differences that touch checkout, cart and
// order are:
//
//   - buyer.consent: four booleans become a map keyed by reverse-DNS purpose,
//     each carrying its source, a description and optional segments;
//   - fulfillment destinations gain an explicit "type" discriminator
//     ("shipping_address" / "business_location", the latter being how a pickup
//     location is presented);
//   - a fulfillment option's description is a Description object, not a string.
//
// Everything else 2026-08-25 added to these resources (actions, policies,
// quantity units, ...) is optional and additive, and V1 has no source for it,
// so it is left absent rather than fabricated.
//
// Encoded responses are decoded back through the generation's own typed schema
// before they are returned, so a translation that drifts from the wire contract
// fails loudly here instead of reaching an agent.
package codec

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/types"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wireadapt"
	v20260825 "github.com/sarvexlab/sarvex/internal/spec/v20260825"
	consent25 "github.com/sarvexlab/sarvex/internal/spec/v20260825/extensions/buyerconsent"
)

// Codec is the 2026-08-25 codec.
type Codec struct{}

// New returns the 2026-08-25 codec.
func New() Codec { return Codec{} }

// Version implements wireadapt.Codec.
func (Codec) Version() types.Version { return v20260825.Release }

// DecodeRequest implements wireadapt.Codec.
func (Codec) DecodeRequest(body []byte) ([]byte, wireadapt.RequestExtras, error) {
	return decodeRequest(body)
}

// EncodeResponse implements wireadapt.Codec.
func (Codec) EncodeResponse(r wireadapt.Resource, canonical []byte, consentWire func() json.RawMessage) ([]byte, error) {
	return encodeResponse(r, canonical, consentWire)
}

func decodeRequest(body []byte) ([]byte, wireadapt.RequestExtras, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		// No body: nothing to translate, and the handler's bind decides whether
		// an empty request is acceptable.
		return body, wireadapt.RequestExtras{}, nil
	}
	m, err := decodeObject(body)
	if err != nil {
		return nil, wireadapt.RequestExtras{}, err
	}
	var extras wireadapt.RequestExtras

	if buyer, ok := m["buyer"].(map[string]any); ok {
		if wire, ok := buyer["consent"]; ok && wire != nil {
			wireJSON, err := json.Marshal(wire)
			if err != nil {
				return nil, extras, err
			}
			legacy, err := consentToCanonical(wire)
			if err != nil {
				return nil, extras, err
			}
			extras.BuyerConsent = wireJSON
			buyer["consent"] = legacy
		}
	}

	out, err := json.Marshal(m)
	return out, extras, err
}

func encodeResponse(r wireadapt.Resource, raw []byte, consentWire func() json.RawMessage) ([]byte, error) {
	m, err := decodeObject(raw)
	if err != nil {
		return nil, fmt.Errorf("wireadapt: canonical %s is not a JSON object: %w", r, err)
	}

	if r == wireadapt.Checkout || r == wireadapt.Cart {
		if f, ok := m["fulfillment"].(map[string]any); ok {
			fixFulfillment(f)
		}
	}
	if r == wireadapt.Checkout {
		if buyer, ok := m["buyer"].(map[string]any); ok {
			if c, ok := buyer["consent"].(map[string]any); ok {
				var sent json.RawMessage
				if consentWire != nil {
					sent = consentWire()
				}
				wire, err := consentToV20260825(c, sent)
				if err != nil {
					return nil, err
				}
				buyer["consent"] = wire
			}
		}
	}

	out, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	if out, err = wireadapt.WithEnvelope(out, v20260825.Release, r); err != nil {
		return nil, err
	}
	if err := conform(r, out); err != nil {
		return nil, err
	}
	return out, nil
}

// conform decodes an encoded response through the generation's typed
// schema, proving a 2026-08-25 client could read it. The result is discarded:
// the encoded map, not the typed round-trip, is what is sent, so nothing an
// unmodelled extension added is lost.
func conform(r wireadapt.Resource, body []byte) error {
	var target any
	switch r {
	case wireadapt.Checkout:
		target = &consent25.CheckoutWithBuyerConsent{}
	case wireadapt.Cart:
		target = &v20260825.Cart{}
	case wireadapt.Order:
		target = &v20260825.Order{}
	default:
		return fmt.Errorf("wireadapt: unknown resource %q", r)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("wireadapt: encoded %s does not conform to 2026-08-25: %w", r, err)
	}
	return nil
}

// fixFulfillment rewrites the fulfillment object's shapes that
// changed in 2026-08-25.
func fixFulfillment(f map[string]any) {
	methods, _ := f["methods"].([]any)
	for _, mi := range methods {
		method, _ := mi.(map[string]any)
		if method == nil {
			continue
		}
		dests, _ := method["destinations"].([]any)
		for _, di := range dests {
			dest, _ := di.(map[string]any)
			if dest == nil {
				continue
			}
			if _, has := dest["type"]; has {
				continue
			}
			// 2026-04-08 told the two apart structurally: a pickup location
			// always carries a name, a shipping destination never does.
			if _, named := dest["name"]; named {
				dest["type"] = string(v20260825.FulfillmentDestinationTypeBusinessLocation)
			} else {
				dest["type"] = string(v20260825.FulfillmentDestinationTypeShippingAddress)
			}
		}
		groups, _ := method["groups"].([]any)
		for _, gi := range groups {
			group, _ := gi.(map[string]any)
			options, _ := group["options"].([]any)
			for _, oi := range options {
				opt, _ := oi.(map[string]any)
				if opt == nil {
					continue
				}
				if s, ok := opt["description"].(string); ok {
					opt["description"] = map[string]any{"plain": s}
				}
			}
		}
	}
}

func decodeObject(b []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber() // keep integer amounts exact
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, fmt.Errorf("null body")
	}
	return m, nil
}
