// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package wire is the UCP 2026-08-25 profile document: the generation-neutral
// constructs of internal/pkg/ucp/wire, with the profile's public keys published
// as "keys" — a JWK Set (RFC 7517), replacing 2026-04-08's "signing_keys" as
// the sole canonical name (source/schemas/profile.json's
// base.properties.keys in the release/2026-08-25 spec tree). Capability,
// Service, PaymentHandler and Entity keep the prior generation's shape
// (verified against source/schemas/{capability,service,payment_handler,ucp}.json).
//
// See https://ucp.dev/latest/specification/overview/#profile-structure
package wire

import (
	ucpwire "github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wire"
)

// Profile is a 2026-08-25 profile document.
type Profile struct {
	UCP  ucpwire.UCP   `json:"ucp"`
	Keys []ucpwire.JWK `json:"keys,omitempty"`
}

// FromCanonical writes p in this generation's form.
func FromCanonical(p *ucpwire.Profile) Profile {
	return Profile{UCP: p.UCP, Keys: p.Keys}
}

// Canonical reads this document into the generation-neutral form.
func (p Profile) Canonical() *ucpwire.Profile {
	return &ucpwire.Profile{UCP: p.UCP, Keys: p.Keys}
}
