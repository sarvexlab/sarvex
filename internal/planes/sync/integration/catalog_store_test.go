// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/sarvexlab/sarvex/internal/planes/sync"
)

// TestCatalogStore_UpsertListGet_StoreScoped exercises the real write/read
// seam (sync.CatalogStore, not raw Mongo documents): upsert a small set of
// products across two stores, then list/get them back, verifying each store
// only ever sees its own products.
func TestCatalogStore_UpsertListGet_StoreScoped(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	store, err := sync.NewMongoDBCatalogStore(env.db)
	if err != nil {
		t.Fatalf("new catalog store: %v", err)
	}
	ctx := context.Background()

	storeA := []*sync.CatalogProduct{
		newTestProduct("store-a", "merch-a", "SKU-A1"),
		newTestProduct("store-a", "merch-a", "SKU-A2"),
	}
	storeA[0].Title = "A1"
	storeA[1].Title = "A2"
	storeB := newTestProduct("store-b", "merch-b", "SKU-B1")
	storeB.Title = "B1"

	for _, p := range append(storeA, storeB) {
		if err := store.UpsertProduct(ctx, p); err != nil {
			t.Fatalf("upsert %s: %v", p.SKU, err)
		}
	}

	// Get each product back individually.
	got, err := store.GetProduct(ctx, "store-a", "SKU-A1")
	if err != nil {
		t.Fatalf("get SKU-A1: %v", err)
	}
	if got.Title != "A1" {
		t.Errorf("expected title A1, got %q", got.Title)
	}

	// List returns only the given store's products.
	listA, _, err := store.ListByStore(ctx, "store-a", 10, "")
	if err != nil {
		t.Fatalf("list store-a: %v", err)
	}
	if len(listA) != 2 {
		t.Fatalf("expected 2 products for store-a, got %d", len(listA))
	}
	for _, p := range listA {
		if p.StoreID != "store-a" {
			t.Errorf("store-a list returned a product from store %q", p.StoreID)
		}
	}

	listB, _, err := store.ListByStore(ctx, "store-b", 10, "")
	if err != nil {
		t.Fatalf("list store-b: %v", err)
	}
	if len(listB) != 1 || listB[0].SKU != "SKU-B1" {
		t.Fatalf("expected exactly SKU-B1 for store-b, got %+v", listB)
	}

	// store-a can never read store-b's product via Get.
	if _, err := store.GetProduct(ctx, "store-a", "SKU-B1"); err != sync.ErrProductNotFound {
		t.Errorf("expected ErrProductNotFound crossing store scope, got %v", err)
	}

	// Delete removes only that product.
	if err := store.DeleteProduct(ctx, "store-a", "SKU-A1"); err != nil {
		t.Fatalf("delete SKU-A1: %v", err)
	}
	if _, err := store.GetProduct(ctx, "store-a", "SKU-A1"); err != nil && err != sync.ErrProductNotFound {
		t.Errorf("expected SKU-A1 deleted, got %v", err)
	}
	if _, err := store.GetProduct(ctx, "store-a", "SKU-A2"); err != nil {
		t.Errorf("expected SKU-A2 untouched, got %v", err)
	}
}

// TestCatalogStore_SoftDeleteThenReAdd pins the two halves of the soft-delete
// contract against real Mongo, where the in-memory fake used by the unit tests
// cannot reproduce it (the fake hard-deletes).
//
// A deleted product must be invisible to every read, and re-upserting the same
// SKU must bring it back: deleted_at is `omitempty`, so an upsert's $set never
// carried the field and the tombstone survived. A SKU that was ever removed and
// later restocked stayed missing from the catalog, the feed and the serving
// cache, with nothing logged.
func TestCatalogStore_SoftDeleteThenReAdd(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	store, err := sync.NewMongoDBCatalogStore(env.db)
	if err != nil {
		t.Fatalf("new catalog store: %v", err)
	}
	ctx := context.Background()

	p := newTestProduct("store-sd", "merch-sd", "SKU-SD")
	if err := store.UpsertProduct(ctx, p); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := store.DeleteProduct(ctx, "store-sd", "SKU-SD"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := store.GetProduct(ctx, "store-sd", "SKU-SD"); !errors.Is(err, sync.ErrProductNotFound) {
		t.Errorf("expected a deleted product to be invisible to GetProduct, got err=%v", err)
	}
	listed, _, err := store.ListByStore(ctx, "store-sd", 10, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 0 {
		t.Errorf("expected a deleted product to be excluded from ListByStore, got %d", len(listed))
	}

	// Re-add: the merchant restocks the same SKU.
	if err := store.UpsertProduct(ctx, newTestProduct("store-sd", "merch-sd", "SKU-SD")); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}

	if _, err := store.GetProduct(ctx, "store-sd", "SKU-SD"); err != nil {
		t.Errorf("expected a re-added product to be visible again, got err=%v", err)
	}
	listed, _, err = store.ListByStore(ctx, "store-sd", 10, "")
	if err != nil {
		t.Fatalf("list after re-add: %v", err)
	}
	if len(listed) != 1 {
		t.Errorf("expected the re-added product back in ListByStore, got %d", len(listed))
	}
}
