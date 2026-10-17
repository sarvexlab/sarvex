// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package types holds the UCP version type every generation shares. Which
// generations exist, and what each serves, is the spec registry's
// (internal/spec, assembled in internal/spec/all) — never a list here.
package types

// Version identifies a UCP spec release, as it appears in the "version"
// field of UCP response metadata and discovery profiles (YYYY-MM-DD).
// Comparison is lexical on the canonical string, which is equivalent to
// chronological ordering for valid date-based values.
type Version string

// String returns the wire representation of v.
func (v Version) String() string {
	return string(v)
}

// Compare returns -1, 0, or 1 if v is older, equal, or newer than other.
// Assumes both are valid YYYY-MM-DD values; lexical compare is sufficient.
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
