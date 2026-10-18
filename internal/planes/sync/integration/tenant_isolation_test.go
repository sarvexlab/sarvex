// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"

	"github.com/sarvexlab/sarvex/internal/planes/sync"
	"github.com/sarvexlab/sarvex/internal/planes/sync/connector"
	syncdomain "github.com/sarvexlab/sarvex/internal/planes/sync/domain"
	"github.com/sarvexlab/sarvex/internal/platform/woocommerce"
	"github.com/sarvexlab/sarvex/internal/testtenancy"
)

// TestTenantIsolation_CatalogStore_Matrix extends
// TestCatalogStore_UpsertListGet_StoreScoped's 2-store precedent to a
// configurable N-tenant x M-store/merchant matrix (default 5x3, see
// internal/testtenancy): every store's ListByStore/GetProduct must only
// ever surface that store's own products, across the whole matrix, not
// just one neighboring store.
func TestTenantIsolation_CatalogStore_Matrix(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	store, err := sync.NewMongoDBCatalogStore(env.db)
	if err != nil {
		t.Fatalf("new catalog store: %v", err)
	}
	ctx := context.Background()

	cfg := testtenancy.LoadConfig()
	tenants := testtenancy.Matrix(cfg)

	// Seed one product per store, title-tagged with its own store id so a
	// leak is trivially identifiable in a failure message.
	for i, tn := range tenants {
		for j, storeID := range tn.StoreIDs {
			p := newTestProduct(storeID, tn.MerchantID, fmt.Sprintf("SKU-%d-%d", i, j))
			p.Title = storeID
			if err := store.UpsertProduct(ctx, p); err != nil {
				t.Fatalf("upsert product for %s: %v", storeID, err)
			}
		}
	}

	for i, tn := range tenants {
		for j, storeID := range tn.StoreIDs {
			t.Run(fmt.Sprintf("tenant_%d_store_%d", i, j), func(t *testing.T) {
				sku := fmt.Sprintf("SKU-%d-%d", i, j)

				list, _, err := store.ListByStore(ctx, storeID, cfg.NumTenants*cfg.StoresPerTenant+10, "")
				if err != nil {
					t.Fatalf("list %s: %v", storeID, err)
				}
				if len(list) != 1 {
					t.Fatalf("expected exactly 1 product for %s, got %d", storeID, len(list))
				}
				if list[0].StoreID != storeID {
					t.Fatalf("store %s's list leaked a product from store %q", storeID, list[0].StoreID)
				}

				got, err := store.GetProduct(ctx, storeID, sku)
				if err != nil {
					t.Fatalf("get own product for %s: %v", storeID, err)
				}
				if got.Title != storeID {
					t.Fatalf("store %s got back a product titled %q (wrong store's data)", storeID, got.Title)
				}

				// Reading a neighboring store's SKU under this store's id must
				// miss, never silently return the neighbor's product — the
				// cyclic successor covers every (i,j) pair across the full run.
				nextI, nextJ := (i+1)%len(tenants), j
				if nextI == i {
					nextJ = (j + 1) % len(tn.StoreIDs)
				}
				otherSKU := fmt.Sprintf("SKU-%d-%d", nextI, nextJ)
				if otherSKU == sku {
					return
				}
				if _, err := store.GetProduct(ctx, storeID, otherSKU); err != sync.ErrProductNotFound {
					t.Errorf("expected ErrProductNotFound reading another store's SKU under %s, got %v", storeID, err)
				}
			})
		}
	}
}

// TestTenantIsolation_ConnectorRegistry_Matrix extends
// TestAuth_TenantIsolation's connector-isolation assertion (control
// integration suite) to the sync plane's own read of the same
// control_connectors collection (internal/planes/sync/connector —
// sarvex-v1-architecture.md documented cross-plane read: sync reads control_connectors
// read-only). GetByStoreID is intentionally store_id-scoped only (a store
// belongs to exactly one merchant — architecture invariant #4), so this
// test's job is confirming that scoping still holds at N-tenant scale, not
// exercising a merchant_id filter that doesn't apply at this layer.
func TestTenantIsolation_ConnectorRegistry_Matrix(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	repo, err := connector.NewMongoConnectorRepository(env.db)
	if err != nil {
		t.Fatalf("new connector repo: %v", err)
	}
	ctx := context.Background()

	cfg := testtenancy.LoadConfig()
	tenants := testtenancy.Matrix(cfg)

	for i, tn := range tenants {
		for j, storeID := range tn.StoreIDs {
			connectorID := fmt.Sprintf("conn-%d-%d", i, j)
			err := repo.Upsert(ctx, &connector.ConnectorModel{
				ID:         connectorID,
				StoreID:    storeID,
				MerchantID: tn.MerchantID,
				Platform:   woocommerce.PlatformName,
				Status:     syncdomain.ConnectorStatusHealthy,
				SyncModel:  syncdomain.SyncModelRealtime,
				HMACSecret: []byte("tenancy-matrix-secret-32bytes!!"),
			})
			if err != nil {
				t.Fatalf("seed connector for %s: %v", storeID, err)
			}
		}
	}

	for i, tn := range tenants {
		for j, storeID := range tn.StoreIDs {
			t.Run(fmt.Sprintf("tenant_%d_store_%d", i, j), func(t *testing.T) {
				got, err := repo.GetByStoreID(ctx, storeID)
				if err != nil {
					t.Fatalf("get connector for own store %s: %v", storeID, err)
				}
				if got.MerchantID != tn.MerchantID {
					t.Fatalf("store %s's connector resolved to merchant %q, want %q", storeID, got.MerchantID, tn.MerchantID)
				}
				if got.StoreID != storeID {
					t.Fatalf("GetByStoreID(%s) returned a connector for a different store: %q", storeID, got.StoreID)
				}
			})
		}
	}
}
