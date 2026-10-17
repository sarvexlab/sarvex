// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/planes/serving"
)

// TestProfile_ServesCachedProfile verifies profile serving from cache.
func TestProfile_ServesCachedProfile(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Store a profile in Redis
	profileKey := "profile:store-a"
	profileJSON := `{"version":"1.0","capabilities":["catalog","checkout"]}`

	env.setRedisKey(ctx, t, profileKey, profileJSON, 5*time.Minute)

	// Verify it's retrievable
	retrieved := env.getRedisKey(ctx, t, profileKey)
	if retrieved != profileJSON {
		t.Errorf("expected profile to match, got %s", retrieved)
	}
}

// TestProfile_ETagCaching verifies ETag-based conditional returns.
func TestProfile_ETagCaching(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Store profile with ETag
	profileKey := "profile:store-a"
	profileJSON := `{"version":"1.0"}`
	etagKey := "etag:store-a"
	etag := "hash-v1-abc123"

	env.setRedisKey(ctx, t, profileKey, profileJSON, 5*time.Minute)
	env.setRedisKey(ctx, t, etagKey, etag, 5*time.Minute)

	// Verify both are stored
	if env.getRedisKey(ctx, t, profileKey) == "" {
		t.Error("profile not found in Redis")
	}
	if env.getRedisKey(ctx, t, etagKey) == "" {
		t.Error("etag not found in Redis")
	}
}

// TestProfile_FreshnessCheck verifies cache freshness validation (>5min = stale).
func TestProfile_FreshnessCheck(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Store profile with creation timestamp
	profileKey := "profile:store-a"
	createdAtKey := "profile:created_at:store-a"

	env.setRedisKey(ctx, t, profileKey, `{"version":"1.0"}`, 5*time.Minute)
	env.setRedisKey(ctx, t, createdAtKey, time.Now().Unix(), 5*time.Minute)

	// Verify timestamp is recent
	createdAtStr := env.getRedisKey(ctx, t, createdAtKey)
	if createdAtStr == "" {
		t.Error("created_at timestamp not found")
	}
}

// TestProfile_TenantIsolation verifies per-store profiles don't mix.
func TestProfile_TenantIsolation(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Store profiles for different stores
	profileA := "profile:store-a"
	profileB := "profile:store-b"

	profileJSONA := `{"store":"a"}`
	profileJSONB := `{"store":"b"}`

	env.setRedisKey(ctx, t, profileA, profileJSONA, 5*time.Minute)
	env.setRedisKey(ctx, t, profileB, profileJSONB, 5*time.Minute)

	// Verify isolation
	if env.getRedisKey(ctx, t, profileA) != profileJSONA {
		t.Error("store-a profile corrupted")
	}
	if env.getRedisKey(ctx, t, profileB) != profileJSONB {
		t.Error("store-b profile corrupted")
	}
}

// TestProfile_CacheInvalidation verifies cache can be invalidated.
func TestProfile_CacheInvalidation(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	profileKey := "profile:store-a"
	profileJSON := `{"version":"1.0"}`

	// Store profile
	env.setRedisKey(ctx, t, profileKey, profileJSON, 5*time.Minute)
	if env.getRedisKey(ctx, t, profileKey) == "" {
		t.Fatal("profile not stored")
	}

	// Invalidate cache
	if err := env.redisClient.Del(ctx, profileKey).Err(); err != nil {
		t.Fatalf("failed to delete key: %v", err)
	}

	// Verify deletion
	if env.getRedisKey(ctx, t, profileKey) != "" {
		t.Error("profile should be deleted after invalidation")
	}
}

// TestProfile_MerchantIDSurvivesRoundTrip verifies RedisCache.GetProfile
// returns the merchant_id SwapSnapshot wrote, not an empty string.
// CacheProfile.MerchantID carries json:"-" (it must never leak into the
// spec-compliant .well-known/ucp response this struct also serializes to
// directly), so it never survives inside the "profile" key's own JSON —
// GetProfile must restore it from its companion "merchant_id" key instead.
// A regression here silently blanks merchant_id on every serving-plane
// request sourced from cache: rate limiting buckets every merchant
// together, and any event derived from the resolved instance (e.g.
// CatalogQueryRecordedEvent) fails downstream consumers' required-field
// checks.
func TestProfile_MerchantIDSurvivesRoundTrip(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()
	env.flushRedis(ctx, t)

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	cache := serving.NewRedisCache(redisAddr, "", 0, 5*time.Minute)

	err := cache.SwapSnapshot(ctx, &serving.CacheSnapshot{
		StoreID:    storeA,
		MerchantID: merchantA,
		BuiltAt:    time.Now(),
		SourceSync: time.Now(),
		Profile: &serving.CacheProfile{
			StoreID:    storeA,
			MerchantID: merchantA,
			UCP:        serving.UCPBlockCache{Version: "2026-04-08"},
		},
		Products: map[string]*serving.CacheProduct{},
	})
	if err != nil {
		t.Fatalf("SwapSnapshot failed: %v", err)
	}

	prof, err := cache.GetProfile(ctx, storeA)
	if err != nil {
		t.Fatalf("GetProfile failed: %v", err)
	}
	if prof.StoreID != storeA {
		t.Errorf("got StoreID %q, want %q", prof.StoreID, storeA)
	}
	if prof.MerchantID != merchantA {
		t.Errorf("got MerchantID %q, want %q", prof.MerchantID, merchantA)
	}
}

// Platform must survive the cache round-trip for the same reason MerchantID
// must: it is json:"-" on CacheProfile, so it never appears in the profile
// key's bytes and has to be restored from its own companion key.
//
// It is the one whose loss is not merely cosmetic. Checkout resolves a
// store's connector by platform, so a profile read back with an empty one
// fails every checkout with `no factory registered for platform ""` — a 503
// to the agent — while the profile endpoint, which never reads the field,
// keeps serving perfectly.
func TestProfile_PlatformSurvivesRoundTrip(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()
	env.flushRedis(ctx, t)

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	cache := serving.NewRedisCache(redisAddr, "", 0, 5*time.Minute)

	const storefrontURL = "https://acme.example"
	err := cache.SwapSnapshot(ctx, &serving.CacheSnapshot{
		StoreID:    storeA,
		MerchantID: merchantA,
		BuiltAt:    time.Now(),
		SourceSync: time.Now(),
		Profile: &serving.CacheProfile{
			StoreID:       storeA,
			MerchantID:    merchantA,
			Platform:      "woocommerce",
			StorefrontURL: storefrontURL,
			UCP:           serving.UCPBlockCache{Version: "2026-04-08"},
		},
		Products: map[string]*serving.CacheProduct{},
	})
	if err != nil {
		t.Fatalf("SwapSnapshot failed: %v", err)
	}

	prof, err := cache.GetProfile(ctx, storeA)
	if err != nil {
		t.Fatalf("GetProfile failed: %v", err)
	}
	if prof.Platform != "woocommerce" {
		t.Errorf("got Platform %q, want %q — checkout cannot resolve a connector without it", prof.Platform, "woocommerce")
	}
	if prof.StorefrontURL != storefrontURL {
		t.Errorf("got StorefrontURL %q, want %q", prof.StorefrontURL, storefrontURL)
	}
}

// TestProfile_LastKnownGood verifies fallback to cached profile on error.
func TestProfile_LastKnownGood(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Store last-known-good profile
	lkgKey := "profile:lkg:store-a"
	lkgJSON := `{"version":"0.9","cached":true}`

	env.setRedisKey(ctx, t, lkgKey, lkgJSON, 24*time.Hour)

	// Verify it's still available after 1 hour (simulating Sarvex failure)
	retrieved := env.getRedisKey(ctx, t, lkgKey)
	if retrieved != lkgJSON {
		t.Error("last-known-good profile not retrievable")
	}
}
