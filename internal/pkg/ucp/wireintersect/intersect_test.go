// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package wireintersect

import (
	"reflect"
	"testing"
)

// synthCapability is a minimal, deliberately non-wire.Capability type used
// to prove the algorithm is truly generation-agnostic: it only needs
// something satisfying the Capability interface, not any particular
// UCP generation's concrete struct.
type synthCapability struct {
	version string
	extends []string
}

func (c synthCapability) CapVersion() string    { return c.version }
func (c synthCapability) ExtendsList() []string { return c.extends }

func TestCapabilities_NameAndVersionIntersection(t *testing.T) {
	business := map[string][]synthCapability{
		"dev.ucp.shopping.checkout": {{version: "2026-04-08"}, {version: "2026-08-25"}},
		"dev.ucp.shopping.cart":     {{version: "2026-04-08"}},
	}
	platform := map[string][]synthCapability{
		"dev.ucp.shopping.checkout": {{version: "2026-04-08"}},
		// cart absent from platform: must be excluded.
	}

	got := Capabilities(business, platform)

	want := map[string][]synthCapability{
		"dev.ucp.shopping.checkout": {{version: "2026-04-08"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Capabilities() = %+v, want %+v", got, want)
	}
}

func TestCapabilities_PicksHighestMutualVersion(t *testing.T) {
	business := map[string][]synthCapability{
		"dev.ucp.shopping.checkout": {{version: "2026-04-08"}, {version: "2026-08-25"}},
	}
	platform := map[string][]synthCapability{
		"dev.ucp.shopping.checkout": {{version: "2026-04-08"}, {version: "2026-08-25"}},
	}

	got := Capabilities(business, platform)

	if len(got["dev.ucp.shopping.checkout"]) != 1 || got["dev.ucp.shopping.checkout"][0].version != "2026-08-25" {
		t.Errorf("Capabilities() = %+v, want highest mutual version 2026-08-25", got)
	}
}

func TestCapabilities_PrunesOrphanedExtensions(t *testing.T) {
	business := map[string][]synthCapability{
		"dev.ucp.shopping.checkout":                 {{version: "2026-04-08"}},
		"dev.ucp.shopping.buyer_consent":            {{version: "2026-04-08", extends: []string{"dev.ucp.shopping.checkout"}}},
		"dev.ucp.shopping.buyer_consent.sub_extend": {{version: "2026-04-08", extends: []string{"dev.ucp.shopping.buyer_consent"}}},
	}
	platform := map[string][]synthCapability{
		// checkout itself is not mutually supported, so both the
		// extension and its transitive sub-extension must be pruned.
		"dev.ucp.shopping.buyer_consent":            {{version: "2026-04-08"}},
		"dev.ucp.shopping.buyer_consent.sub_extend": {{version: "2026-04-08"}},
	}

	got := Capabilities(business, platform)

	if len(got) != 0 {
		t.Errorf("Capabilities() = %+v, want empty (transitive prune)", got)
	}
}

// Multi-parent extension pruning requires ALL listed parents to survive
// intersection, matching the production negotiation.IntersectCapabilities
// this package was extracted from (negotiation/capability.go's pre-refactor
// orphaned()). Note: internal/pkg/ucp/ucpnego/negotiate (a separate,
// largely-unused reference implementation, not the production path used
// by internal/pkg/ucp/negotiation) implements the opposite rule —
// "at least one parent must remain" — a pre-existing inconsistency
// between the two implementations that predates this refactor and is out
// of scope here; this package intentionally preserves production
// behavior only.
func TestCapabilities_MultiParentExtensionRequiresAllParentsPresent(t *testing.T) {
	business := map[string][]synthCapability{
		"dev.ucp.shopping.checkout": {{version: "2026-04-08"}},
		"dev.ucp.shopping.cart":     {{version: "2026-04-08"}},
		"dev.ucp.shopping.discount": {
			{version: "2026-04-08", extends: []string{"dev.ucp.shopping.checkout", "dev.ucp.shopping.cart"}},
		},
	}
	platform := map[string][]synthCapability{
		"dev.ucp.shopping.checkout": {{version: "2026-04-08"}},
		"dev.ucp.shopping.cart":     {{version: "2026-04-08"}},
		"dev.ucp.shopping.discount": {{version: "2026-04-08"}},
	}

	got := Capabilities(business, platform)
	if _, ok := got["dev.ucp.shopping.discount"]; !ok {
		t.Errorf("Capabilities() = %+v, want dev.ucp.shopping.discount to survive (all parents present)", got)
	}

	// Now drop cart from the platform: discount must be pruned since one
	// of its two parents is no longer in the intersection.
	delete(platform, "dev.ucp.shopping.cart")
	got = Capabilities(business, platform)
	if _, ok := got["dev.ucp.shopping.discount"]; ok {
		t.Errorf("Capabilities() = %+v, want dev.ucp.shopping.discount pruned (one parent missing)", got)
	}
}

func TestPaymentHandlers_IntersectsByName(t *testing.T) {
	type handler struct{ ID string }
	business := map[string][]handler{
		"com.example.stripe": {{ID: "stripe-1"}},
		"com.example.paypal": {{ID: "paypal-1"}},
	}
	platform := map[string][]handler{
		"com.example.stripe": {{ID: "stripe-platform"}}, // unused; intersection is name-only
	}

	got := PaymentHandlers(business, platform)

	want := map[string][]handler{
		"com.example.stripe": {{ID: "stripe-1"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PaymentHandlers() = %+v, want %+v", got, want)
	}
}
