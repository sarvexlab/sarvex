// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package ucpsig

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// DigestHeaderValue returns the value for the Content-Digest header (RFC 9530)
// using sha-256 over the raw bytes of body. UCP mandates sha-256.
func DigestHeaderValue(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha-256=:" + base64.StdEncoding.EncodeToString(sum[:]) + ":"
}

// VerifyDigest returns nil if the given Content-Digest header value matches
// the sha-256 of body. The header may contain multiple algorithms; only
// sha-256 is checked, per the UCP profile.
func VerifyDigest(header string, body []byte) error {
	want := DigestHeaderValue(body)
	// Fast path: single sha-256 entry.
	if header == want {
		return nil
	}
	// Tolerate other algorithms appearing alongside sha-256.
	for _, part := range splitDigestEntries(header) {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(strings.ToLower(part), "sha-256=") {
			continue
		}
		if part == strings.TrimPrefix(want, "") {
			return nil
		}
		// Compare base64 payload only, in case spacing differs.
		expected := want[len("sha-256=:") : len(want)-1]
		got := strings.TrimPrefix(part, "sha-256=")
		got = strings.TrimPrefix(got, ":")
		got = strings.TrimSuffix(got, ":")
		if subtleEqualString(got, expected) {
			return nil
		}
	}
	return ErrDigestMismatch.withCause(fmt.Errorf("expected %s", want))
}

// splitDigestEntries splits an RFC 9530 dictionary on commas not inside the
// base64 ":...:" byte-sequence wrappers.
func splitDigestEntries(s string) []string {
	var out []string
	var cur bytes.Buffer
	inBytes := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ':' && !inBytes:
			inBytes = true
			cur.WriteByte(c)
		case c == ':' && inBytes:
			inBytes = false
			cur.WriteByte(c)
		case c == ',' && !inBytes:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func subtleEqualString(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
