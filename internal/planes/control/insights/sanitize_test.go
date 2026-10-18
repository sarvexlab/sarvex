// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package insights

import "testing"

// TestSanitizeIdentifier_RejectsFreeText is A6/R12: a string that looks
// like free text (contains whitespace — the one thing no real product id,
// capability id, or payment handler name ever contains) must never
// survive this gate.
func TestSanitizeIdentifier_RejectsFreeText(t *testing.T) {
	cases := []string{
		"red running shoes size 10 under $50",
		"best noise cancelling headphones for travel",
		"  leading and trailing space token  ",
		"tab\tseparated\ttoken",
		"newline\nin\nvalue",
	}
	for _, raw := range cases {
		if _, ok := sanitizeIdentifier(raw); ok {
			t.Errorf("sanitizeIdentifier(%q) should have rejected free text, but accepted it", raw)
		}
	}
}

// TestSanitizeIdentifier_RejectsOverlong guards against a raw query string
// that happens to contain no whitespace (unlikely, but not impossible)
// from persisting via an unbounded-length identifier.
func TestSanitizeIdentifier_RejectsOverlong(t *testing.T) {
	long := make([]byte, maxIdentifierLen+1)
	for i := range long {
		long[i] = 'a'
	}
	if _, ok := sanitizeIdentifier(string(long)); ok {
		t.Error("sanitizeIdentifier should reject an identifier longer than maxIdentifierLen")
	}
}

// TestSanitizeIdentifier_AcceptsRealIdentifiers is the control: genuine
// product ids, reverse-domain capability names, and payment handler names
// must pass through unchanged.
func TestSanitizeIdentifier_AcceptsRealIdentifiers(t *testing.T) {
	cases := []string{"sku-12345", "dev.ucp.shopping.checkout", "dev.ucp.payments.paypal", "SKU_ABC-99"}
	for _, raw := range cases {
		got, ok := sanitizeIdentifier(raw)
		if !ok {
			t.Errorf("sanitizeIdentifier(%q) should have been accepted", raw)
		}
		if got != raw {
			t.Errorf("sanitizeIdentifier(%q) = %q, want unchanged", raw, got)
		}
	}
}

// TestSanitizeIdentifiers_DropsOnlyBadEntries verifies the slice helper
// filters rather than fails the whole batch on one bad entry.
func TestSanitizeIdentifiers_DropsOnlyBadEntries(t *testing.T) {
	got := sanitizeIdentifiers([]string{"sku-1", "free text with spaces", "sku-2", ""})
	want := []string{"sku-1", "sku-2"}
	if len(got) != len(want) {
		t.Fatalf("sanitizeIdentifiers() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sanitizeIdentifiers() = %v, want %v", got, want)
		}
	}
}
