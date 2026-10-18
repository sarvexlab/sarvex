// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package negotiation

import (
	"testing"

	specall "github.com/sarvexlab/sarvex/internal/spec/all"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/types"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

func TestSelectVersion_SingleServable(t *testing.T) {
	servable := []wire.Version{"2026-04-08"}

	got, ok := SelectVersion(servable, "2026-04-08")
	if !ok || got != "2026-04-08" {
		t.Fatalf("SelectVersion() = %q, %v; want 2026-04-08, true", got, ok)
	}
}

func TestSelectVersion_PicksHighestMutual(t *testing.T) {
	// Business supports three generations; the platform declares the
	// newest one, so the highest mutual version must be selected — not
	// just the first or oldest entry in servable.
	servable := []wire.Version{"2026-04-08", "2026-06-15", "2026-08-25"}

	got, ok := SelectVersion(servable, "2026-08-25")
	if !ok || got != "2026-08-25" {
		t.Fatalf("SelectVersion() = %q, %v; want 2026-08-25, true", got, ok)
	}
}

func TestSelectVersion_PicksHighestMutual_PlatformBehindNewestServable(t *testing.T) {
	// The platform only declares the middle generation: business must not
	// select its own newest (2026-08-25, which the platform never said it
	// understands) — it must fall back to the highest servable version the
	// platform actually asked for.
	servable := []wire.Version{"2026-04-08", "2026-06-15", "2026-08-25"}

	got, ok := SelectVersion(servable, "2026-06-15")
	if !ok || got != "2026-06-15" {
		t.Fatalf("SelectVersion() = %q, %v; want 2026-06-15, true", got, ok)
	}
}

func TestSelectVersion_NoMutualVersion(t *testing.T) {
	// Every servable version is newer than what the platform declared.
	servable := []wire.Version{"2026-04-08", "2026-08-25"}

	_, ok := SelectVersion(servable, "2026-01-01")
	if ok {
		t.Fatal("SelectVersion() ok = true, want false (no mutual version)")
	}
}

func TestSelectVersion_EmptyServable(t *testing.T) {
	_, ok := SelectVersion(nil, "2026-04-08")
	if ok {
		t.Fatal("SelectVersion() ok = true, want false for empty servable set")
	}
}

func TestSelectVersion_OrderIndependent(t *testing.T) {
	servable := []wire.Version{"2026-08-25", "2026-04-08"} // deliberately unsorted

	got, ok := SelectVersion(servable, "2026-08-25")
	if !ok || got != "2026-08-25" {
		t.Fatalf("SelectVersion() = %q, %v; want 2026-08-25, true", got, ok)
	}
}

// TestServableVersions_AreTheCatalogGenerations proves the production
// candidate set negotiation offers comes from the spec registry — the
// generations that serve the catalog — converted to this package's
// wire.Version, not an independently maintained copy that could drift.
func TestServableVersions_AreTheCatalogGenerations(t *testing.T) {
	assertVersions(t, ServableVersions(), specall.Default().Serving("dev.ucp.shopping.catalog"))
}

// TestCheckoutServableVersions_AreTheCheckoutGenerations is the checkout
// counterpart.
func TestCheckoutServableVersions_AreTheCheckoutGenerations(t *testing.T) {
	assertVersions(t, CheckoutServableVersions(), specall.Default().Serving("dev.ucp.shopping.checkout"))
}

func assertVersions(t *testing.T, got []wire.Version, want []types.Version) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, v := range want {
		if got[i] != wire.Version(v.String()) {
			t.Fatalf("[%d] = %q, want %q", i, got[i], v)
		}
	}
}

// TestCheckoutGenerationsServeTheCatalog guards the relationship that makes the
// split safe: checkout may trail the catalog while a new generation's checkout
// support is built, but a generation an agent can check out in must also serve
// it a catalog — and today every generation serves both.
func TestCheckoutGenerationsServeTheCatalog(t *testing.T) {
	catalog := map[types.Version]bool{}
	for _, v := range specall.Default().Serving("dev.ucp.shopping.catalog") {
		catalog[v] = true
	}
	checkout := specall.Default().Serving("dev.ucp.shopping.checkout")
	for _, v := range checkout {
		if !catalog[v] {
			t.Errorf("%s serves checkout but not the catalog", v)
		}
	}
	if len(checkout) != len(catalog) {
		t.Errorf("checkout generations %v, catalog generations %v: an agent negotiating one of the catalog's could not check out", checkout, catalog)
	}
}
