// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package wire is the UCP 2026-04-08 profile document: the generation-neutral
// constructs of internal/pkg/ucp/wire, with the profile's public keys published
// as "signing_keys" (renamed "keys" in 2026-08-25).
//
// See https://ucp.dev/2026-04-08/specification/overview/#profile-structure
package wire

import (
	ucpwire "github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// Profile is a 2026-04-08 profile document.
type Profile struct {
	UCP         ucpwire.UCP   `json:"ucp"`
	SigningKeys []ucpwire.JWK `json:"signing_keys,omitempty"`
}

// FromCanonical writes p in this generation's form.
func FromCanonical(p *ucpwire.Profile) Profile {
	return Profile{UCP: p.UCP, SigningKeys: p.Keys}
}

// Canonical reads this document into the generation-neutral form.
func (p Profile) Canonical() *ucpwire.Profile {
	return &ucpwire.Profile{UCP: p.UCP, Keys: p.SigningKeys}
}
