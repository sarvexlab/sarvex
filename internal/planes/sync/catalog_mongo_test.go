// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// connectTestMongo connects to a scratch database for catalog store tests.
// Skips (rather than fails) when MONGO_URI is unset, following the same
// convention as internal/planes/control/catalog/mongo_test.go.
func connectTestMongo(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		t.Skip("MONGO_URI not set; skipping MongoDB catalog store tests")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	db := client.Database("sync_catalog_store_test")
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return db
}

func testProduct(storeID, merchantID, sku, title string) *CatalogProduct {
	return &CatalogProduct{
		StoreID:       storeID,
		MerchantID:    merchantID,
		SKU:           sku,
		Title:         title,
		PriceMinor:    2999,
		Currency:      "USD",
		InStock:       true,
		RawAttributes: map[string]string{},
		MappedFields:  map[string]string{},
		UpdatedAt:     time.Now().UTC(),
	}
}

func TestMongoDBCatalogStore_UpsertThenGet(t *testing.T) {
	db := connectTestMongo(t)
	store, err := NewMongoDBCatalogStore(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	ctx := context.Background()

	product := testProduct("store-a", "merch-a", "SKU-001", "Widget")
	if err := store.UpsertProduct(ctx, product); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := store.GetProduct(ctx, "store-a", "SKU-001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "Widget" || got.SKU != "SKU-001" || got.StoreID != "store-a" {
		t.Errorf("unexpected product returned: %+v", got)
	}
	if got.PriceMinor != 2999 || got.Currency != "USD" {
		t.Errorf("expected money fields to round-trip, got %d %s", got.PriceMinor, got.Currency)
	}
}

func TestMongoDBCatalogStore_UpsertUpdatesInPlace(t *testing.T) {
	db := connectTestMongo(t)
	store, err := NewMongoDBCatalogStore(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	ctx := context.Background()

	product := testProduct("store-a", "merch-a", "SKU-002", "Original Title")
	if err := store.UpsertProduct(ctx, product); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	updated := testProduct("store-a", "merch-a", "SKU-002", "Updated Title")
	updated.PriceMinor = 5999
	if err := store.UpsertProduct(ctx, updated); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := store.GetProduct(ctx, "store-a", "SKU-002")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "Updated Title" || got.PriceMinor != 5999 {
		t.Errorf("expected in-place update, got %+v", got)
	}

	products, _, err := store.ListByStore(ctx, "store-a", 100, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	count := 0
	for _, p := range products {
		if p.SKU == "SKU-002" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 record for SKU-002 after re-upsert, got %d", count)
	}
}

func TestMongoDBCatalogStore_ListIsStoreScoped(t *testing.T) {
	db := connectTestMongo(t)
	store, err := NewMongoDBCatalogStore(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	ctx := context.Background()

	if err := store.UpsertProduct(ctx, testProduct("store-a", "merch-a", "SKU-A1", "A1")); err != nil {
		t.Fatalf("upsert a1: %v", err)
	}
	if err := store.UpsertProduct(ctx, testProduct("store-a", "merch-a", "SKU-A2", "A2")); err != nil {
		t.Fatalf("upsert a2: %v", err)
	}
	if err := store.UpsertProduct(ctx, testProduct("store-b", "merch-b", "SKU-B1", "B1")); err != nil {
		t.Fatalf("upsert b1: %v", err)
	}

	productsA, _, err := store.ListByStore(ctx, "store-a", 100, "")
	if err != nil {
		t.Fatalf("list store-a: %v", err)
	}
	if len(productsA) != 2 {
		t.Fatalf("expected 2 products for store-a, got %d", len(productsA))
	}
	for _, p := range productsA {
		if p.StoreID != "store-a" {
			t.Errorf("store-a list leaked a product from %q", p.StoreID)
		}
	}

	// store-a can't read store-b's product.
	if _, err := store.GetProduct(ctx, "store-a", "SKU-B1"); err != ErrProductNotFound {
		t.Errorf("expected ErrProductNotFound reading store-b's SKU via store-a, got %v", err)
	}

	productsB, _, err := store.ListByStore(ctx, "store-b", 100, "")
	if err != nil {
		t.Fatalf("list store-b: %v", err)
	}
	if len(productsB) != 1 || productsB[0].SKU != "SKU-B1" {
		t.Errorf("expected exactly SKU-B1 for store-b, got %+v", productsB)
	}
}

func TestMongoDBCatalogStore_DeleteRemovesOnlyThatProduct(t *testing.T) {
	db := connectTestMongo(t)
	store, err := NewMongoDBCatalogStore(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	ctx := context.Background()

	if err := store.UpsertProduct(ctx, testProduct("store-a", "merch-a", "SKU-DEL", "Delete Me")); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := store.UpsertProduct(ctx, testProduct("store-a", "merch-a", "SKU-KEEP", "Keep Me")); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if err := store.DeleteProduct(ctx, "store-a", "SKU-DEL"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := store.GetProduct(ctx, "store-a", "SKU-DEL"); err != ErrProductNotFound {
		t.Errorf("expected deleted product to be gone, got %v", err)
	}
	if _, err := store.GetProduct(ctx, "store-a", "SKU-KEEP"); err != nil {
		t.Errorf("expected untouched product to remain, got %v", err)
	}
}
