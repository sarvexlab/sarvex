// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	servingpkg "github.com/sarvexlab/sarvex/internal/pkg/serving"
	"github.com/sarvexlab/sarvex/internal/planes/serving"
	"github.com/sarvexlab/sarvex/internal/testtenancy"
)

// newTestRedisCache builds a real serving.RedisCache against the same Redis
// instance the suite's testEnv already connected to (REDIS_ADDR, same
// default as suite_test.go's setup), so these tests exercise the actual
// production cache code (internal/planes/serving/cache_redis.go), not a
// hand-rolled raw-key check like session_test.go's shallower
// TestSession_TenantIsolation.
func newTestRedisCache() *serving.RedisCache {
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	return serving.NewRedisCache(redisAddr, "", 0, 5*time.Minute)
}

// TestTenantIsolation_ServingCache_Matrix extends TestCatalog_TenantIsolation/
// TestSession_TenantIsolation's 2-store precedent to a configurable
// N-tenant x M-store/merchant matrix (default 5x3, see internal/testtenancy),
// exercising the real SwapSnapshot/GetProduct/GetProfile/Watermark path:
// every store's cache entry must only ever surface that store's own data.
//
// This cache is intentionally keyed by store_id alone, not a
// merchant_id+store_id compound (architecture invariant #4: catalog/serving
// data is fenced by store_id) — store_id is control_stores._id, MongoDB's
// own primary key, so a collision across two different merchants'
// documents is structurally impossible, not just statistically unlikely.
// This test's job is confirming that store_id-only scoping still holds
// cleanly at N-tenant scale, not exercising a merchant_id filter that
// doesn't apply at this layer.
func TestTenantIsolation_ServingCache_Matrix(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	cache := newTestRedisCache()
	ctx := context.Background()

	cfg := testtenancy.LoadConfig()
	tenants := testtenancy.Matrix(cfg)

	for i, tn := range tenants {
		for j, storeID := range tn.StoreIDs {
			sku := fmt.Sprintf("SKU-%d-%d", i, j)
			snapshot := &serving.CacheSnapshot{
				StoreID:    storeID,
				MerchantID: tn.MerchantID,
				BuiltAt:    time.Now().UTC(),
				SourceSync: time.Now().UTC(),
				Profile: &servingpkg.CacheProfile{
					StoreID:       storeID,
					MerchantID:    tn.MerchantID,
					Platform:      "woocommerce",
					StorefrontURL: fmt.Sprintf("https://%s.example.com/", storeID),
				},
				Products: map[string]*servingpkg.CacheProduct{
					sku: {SKU: sku, Title: storeID},
				},
			}
			if err := cache.SwapSnapshot(ctx, snapshot); err != nil {
				t.Fatalf("swap snapshot for %s: %v", storeID, err)
			}
		}
	}

	for i, tn := range tenants {
		for j, storeID := range tn.StoreIDs {
			t.Run(fmt.Sprintf("tenant_%d_store_%d", i, j), func(t *testing.T) {
				sku := fmt.Sprintf("SKU-%d-%d", i, j)

				profile, err := cache.GetProfile(ctx, storeID)
				if err != nil {
					t.Fatalf("get profile for %s: %v", storeID, err)
				}
				if profile.MerchantID != tn.MerchantID {
					t.Fatalf("store %s's cached profile resolved to merchant %q, want %q", storeID, profile.MerchantID, tn.MerchantID)
				}

				prod, err := cache.GetProduct(ctx, storeID, sku)
				if err != nil {
					t.Fatalf("get own product for %s: %v", storeID, err)
				}
				if prod.Title != storeID {
					t.Fatalf("store %s got back a product titled %q (wrong store's data)", storeID, prod.Title)
				}

				// Searching this store never surfaces a neighboring store's SKU.
				result, err := cache.Search(ctx, storeID, "", 100, "")
				if err != nil {
					t.Fatalf("search %s: %v", storeID, err)
				}
				if len(result.Items) != 1 || result.Items[0].SKU != sku {
					t.Fatalf("store %s's search returned %+v, want exactly [%s]", storeID, result.Items, sku)
				}

				// This store's id never resolves a neighboring store's product —
				// the cyclic successor covers every (i,j) pair across the run.
				nextI, nextJ := (i+1)%len(tenants), j
				if nextI == i {
					nextJ = (j + 1) % len(tn.StoreIDs)
				}
				otherSKU := fmt.Sprintf("SKU-%d-%d", nextI, nextJ)
				if otherSKU == sku {
					return
				}
				if _, err := cache.GetProduct(ctx, storeID, otherSKU); err != serving.ErrNotFound {
					t.Errorf("expected ErrNotFound reading another store's SKU under %s, got %v", storeID, err)
				}
			})
		}
	}
}
