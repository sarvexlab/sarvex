// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package insights

import "strings"

// maxIdentifierLen bounds a product id / capability id / payment handler
// name this package will persist. Real identifiers (SKUs, reverse-domain
// capability names, handler names) are short tokens; anything longer is
// far more likely to be free text that leaked into a field it doesn't
// belong in than a genuine identifier.
const maxIdentifierLen = 200

// sanitizeIdentifier is the single write-time gate (R12) every string this
// package persists as a "requested thing" (product id, capability id,
// payment handler name, platform profile URI host) passes through before
// it reaches a Mongo document. It enforces two invariants: no whitespace
// (a real identifier is one token; free text is not) and a bounded length.
// A value that fails either check is rejected outright — never truncated
// and stored anyway, which could still leak a prefix of raw query text.
//
// This is deliberately the ONLY place in the package that decides whether
// a string is safe to persist — every consumer.go handler routes
// candidate identifiers through this function before they reach store.go,
// so there is exactly one gate to audit and test (privacy_test.go proves
// a raw free-text string cannot survive it).
func sanitizeIdentifier(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	if len(trimmed) > maxIdentifierLen {
		return "", false
	}
	if strings.ContainsAny(trimmed, " \t\n\r") {
		return "", false
	}
	return trimmed, true
}

// sanitizeIdentifiers filters a slice through sanitizeIdentifier, dropping
// (never truncating) anything that fails.
func sanitizeIdentifiers(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		if v, ok := sanitizeIdentifier(r); ok {
			out = append(out, v)
		}
	}
	return out
}
