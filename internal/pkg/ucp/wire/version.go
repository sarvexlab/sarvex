// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package wire defines the generation-neutral UCP profile constructs: the
// profile document published at /.well-known/ucp (by businesses) or at an
// arbitrary HTTPS URL (by platforms), the inner protocol object, and what it
// carries — services, capabilities, payment handlers and signing keys.
//
// These shapes are the same in every spec generation Sarvex serves (checked
// against each release's capability, service, payment_handler and ucp
// schemas); where a generation differs — today only the name of the profile's
// key set, see Profile — that generation's internal/spec/vYYYYMMDD/wire
// package holds its own document form and converts to and from this one.
//
// It is distinct from the definitions catalogue
// (internal/pkg/ucp/catalogue/v1), which is Sarvex's internal *metadata*
// registry of capabilities (name, V1 status, description, scoring rubric) — not
// a serialization shape — and from the crypto JWK in package ucpsig, which
// carries the private scalar and EC key behaviour.
//
// See https://ucp.dev/latest/specification/overview/#profile-structure
package wire

import (
	"errors"
	"fmt"
	"time"
)

// Version is a UCP date-based version in YYYY-MM-DD form. Comparison is
// lexical on the canonical string, which is equivalent to chronological
// ordering for valid values.
//
// See https://ucp.dev/latest/specification/overview/#version-format
type Version string

const versionLayout = "2006-01-02"

// ParseVersion validates a date-based version. Empty values are rejected.
func ParseVersion(s string) (Version, error) {
	if s == "" {
		return "", errors.New("wire: empty version")
	}
	if _, err := time.Parse(versionLayout, s); err != nil {
		return "", fmt.Errorf("wire: invalid version %q: %w", s, err)
	}
	return Version(s), nil
}

// Compare returns -1, 0, or 1 if v is older, equal, or newer than other.
// Assumes both are valid YYYY-MM-DD; lexical compare is sufficient.
func (v Version) Compare(other Version) int {
	switch {
	case v < other:
		return -1
	case v > other:
		return 1
	default:
		return 0
	}
}

// String implements fmt.Stringer.
func (v Version) String() string { return string(v) }
