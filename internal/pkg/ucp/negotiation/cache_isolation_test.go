// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package negotiation

import (
	"testing"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/testtenancy"
)

// TestCacheKey_TenantMatrixNoCollision is the "cache-key collision on
// ucpnego composite keys" deliverable: across a configurable N-tenant x
// M-store/merchant matrix (default 5x3, see internal/testtenancy) all
// negotiating the *same* platform profile URI (the realistic case — many
// stores integrate with the same handful of agent platforms), CacheKey must
// produce a distinct key for every (merchant, store) pair. Before merchantID
// was folded into the key, two different merchants' stores sharing a
// store_id would have collided here; this also folds merchantID in so a
// hypothetical store_id collision (see the forced-collision test below)
// can't cross a merchant boundary via the cache alone.
func TestCacheKey_TenantMatrixNoCollision(t *testing.T) {
	cfg := testtenancy.LoadConfig()
	tenants := testtenancy.Matrix(cfg)
	const sharedProfileURI = "https://agent.example.com/profile"

	seen := make(map[string]string) // key -> "merchantID/storeID" that produced it, for a readable failure
	for _, tn := range tenants {
		for _, storeID := range tn.StoreIDs {
			key := CacheKey(tn.MerchantID, storeID, sharedProfileURI)
			if owner, exists := seen[key]; exists {
				t.Fatalf("CacheKey collision: %s/%s and %s produced the same key %q", tn.MerchantID, storeID, owner, key)
			}
			seen[key] = tn.MerchantID + "/" + storeID
		}
	}
}

// TestNegotiationCache_ForcedStoreIDCollisionNoBleed forces the one
// scenario CacheKey can't defend against on its own — two different
// merchants whose store_id happens to be identical (store_id is expected to
// be a globally-unique UUID minted from control_stores._id, MongoDB's own
// primary key, so this should be structurally impossible in production; see
// docs/TENANCY-PATTERNS.md's compound-key rule and CLAUDE.md architecture
// invariant #4) — and confirms the cache still doesn't bleed one merchant's
// NegotiationResult (active capabilities, payment handlers) into another's,
// because merchantID is now folded into the composite key alongside the
// colliding store_id.
func TestNegotiationCache_ForcedStoreIDCollisionNoBleed(t *testing.T) {
	cache := NewMemoryCache()
	const collidedStoreID = "store-forced-collision"
	const profileURI = "https://agent.example.com/profile"

	merchantAResult := &NegotiationResult{Version: "2026-04-08"}
	merchantBResult := &NegotiationResult{Version: "2024-01-01"}

	cache.SetNegotiation(CacheKey("merch-A", collidedStoreID, profileURI), merchantAResult, time.Minute)
	cache.SetNegotiation(CacheKey("merch-B", collidedStoreID, profileURI), merchantBResult, time.Minute)

	gotA, ok := cache.GetNegotiation(CacheKey("merch-A", collidedStoreID, profileURI))
	if !ok {
		t.Fatal("expected a cache hit for merchant A")
	}
	if gotA.Version != merchantAResult.Version {
		t.Fatalf("merchant A got back version %q, want %q (bled from merchant B)", gotA.Version, merchantAResult.Version)
	}

	gotB, ok := cache.GetNegotiation(CacheKey("merch-B", collidedStoreID, profileURI))
	if !ok {
		t.Fatal("expected a cache hit for merchant B")
	}
	if gotB.Version != merchantBResult.Version {
		t.Fatalf("merchant B got back version %q, want %q (bled from merchant A)", gotB.Version, merchantBResult.Version)
	}
}
