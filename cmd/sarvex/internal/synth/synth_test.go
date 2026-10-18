// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package synth

import (
	"net/mail"
	"reflect"
	"strings"
	"testing"
)

// Determinism is the whole contract: the conformance suite reproduces a
// failing run from its seed alone, which only works if the same seed
// rebuilds the identical fixture.
func TestSameNameProducesIdenticalFixture(t *testing.T) {
	a, b := New("Acme Widgets", 0), New("Acme Widgets", 0)
	if a.Seed() != b.Seed() {
		t.Fatalf("seeds differ: %d vs %d", a.Seed(), b.Seed())
	}

	ma, mb := a.Merchant("Acme Widgets"), b.Merchant("Acme Widgets")
	if !reflect.DeepEqual(ma, mb) {
		t.Errorf("merchants differ:\n a = %+v\n b = %+v", ma, mb)
	}

	sa, sb := a.Store(ma, 0, "woocommerce"), b.Store(mb, 0, "woocommerce")
	if !reflect.DeepEqual(sa, sb) {
		t.Errorf("stores differ:\n a = %+v\n b = %+v", sa, sb)
	}
}

func TestDifferentNamesProduceDifferentSeeds(t *testing.T) {
	if New("Acme Widgets", 0).Seed() == New("Beta Goods", 0).Seed() {
		t.Error("distinct merchant names collided on the same seed")
	}
}

func TestExplicitSeedOverridesName(t *testing.T) {
	g := New("Acme Widgets", 42)
	if g.Seed() != 42 {
		t.Errorf("Seed() = %d, want 42", g.Seed())
	}
}

// The control plane validates support_email with mail.ParseAddress and
// return_policy_url with url.ParseRequestURI, rejecting anything that does
// not parse. Generating a value the server will reject turns a fixture bug
// into a confusing 400 mid-onboarding.
func TestGeneratedContactDetailsPassServerValidation(t *testing.T) {
	g := New("Acme Widgets & Co.", 0)
	m := g.Merchant("Acme Widgets & Co.")

	for _, addr := range []string{m.OwnerEmail, m.SupportEmail} {
		if _, err := mail.ParseAddress(addr); err != nil {
			t.Errorf("mail.ParseAddress(%q) = %v, want nil", addr, err)
		}
	}

	s := g.Store(m, 0, "woocommerce")
	if _, err := mail.ParseAddress(s.SupportEmail); err != nil {
		t.Errorf("store support email %q does not parse: %v", s.SupportEmail, err)
	}
	if !strings.HasPrefix(s.ReturnPolicyURL, "https://") {
		t.Errorf("return policy URL %q is not an https URL", s.ReturnPolicyURL)
	}
}

// Fictitious fixtures must not look deliverable: RFC 2606 reserves
// example.com and the 555-01xx block is the reserved fictitious-number
// range. Getting this wrong means generated accounts point at addresses
// and numbers that could belong to someone.
func TestFixturesUseReservedDomainsAndNumbers(t *testing.T) {
	g := New("Acme Widgets", 0)
	m := g.Merchant("Acme Widgets")

	for _, addr := range []string{m.OwnerEmail, m.SupportEmail} {
		if !strings.HasSuffix(addr, ".example.com") {
			t.Errorf("%q is not under the reserved example.com domain", addr)
		}
	}
	if !strings.HasPrefix(m.SupportPhone, "+1-555-01") {
		t.Errorf("support phone %q is outside the reserved 555-01xx range", m.SupportPhone)
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Acme Widgets", "acme-widgets"},
		{"Acme Widgets & Co.", "acme-widgets-co"},
		{"  Spaced  Out  ", "spaced-out"},
		{"!!!", "merchant"},
		{"", "merchant"},
	}
	for _, tt := range tests {
		if got := Slugify(tt.in); got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// A merchant's stores must have distinct display names, or their SKUs
// collide and the fixture stops being a useful multi-store test.
func TestStoresAreDistinct(t *testing.T) {
	g := New("Acme Widgets", 0)
	m := g.Merchant("Acme Widgets")

	seen := map[string]bool{}
	for i := range 3 {
		name := g.Store(m, i, "woocommerce").DisplayName
		if seen[name] {
			t.Errorf("store %d reused display name %q", i, name)
		}
		seen[name] = true
	}
}
