// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"testing"
)

// TestStorage_MultiStoreIsolation verifies data from different stores don't mix.
func TestStorage_MultiStoreIsolation(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Insert products for 3 different stores
	stores := []string{"store-a", "store-b", "store-c"}
	for _, store := range stores {
		product := newTestProduct(store, "merch-a", "SKU-001")
		product.Title = "Product for " + store

		if _, err := env.catalogCol.InsertOne(ctx, product); err != nil {
			t.Fatalf("failed to insert for %s: %v", store, err)
		}
	}

	// Query each store separately
	for _, store := range stores {
		cursor, err := env.catalogCol.Find(ctx, map[string]interface{}{"store_id": store})
		if err != nil {
			t.Fatalf("failed to query %s: %v", store, err)
		}
		defer cursor.Close(ctx)

		var products []map[string]interface{}
		if err = cursor.All(ctx, &products); err != nil {
			t.Fatalf("failed to decode: %v", err)
		}

		if len(products) != 1 {
			t.Errorf("expected 1 product for %s, got %d", store, len(products))
		}
	}
}

// TestStorage_QueryByName verifies querying by product name.
func TestStorage_QueryByName(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Insert products with different names
	product1 := newTestProduct("store-a", "merch-a", "SKU-NAME-001")
	product1.Title = "iPhone 15"

	product2 := newTestProduct("store-a", "merch-a", "SKU-NAME-002")
	product2.Title = "iPad Pro"

	if _, err := env.catalogCol.InsertMany(ctx, []interface{}{product1, product2}); err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	// Query by title
	cursor, err := env.catalogCol.Find(ctx, map[string]interface{}{"title": "iPhone 15"})
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer cursor.Close(ctx)

	var results []map[string]interface{}
	if err = cursor.All(ctx, &results); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	} else if results[0]["sku"] != "SKU-NAME-001" {
		t.Errorf("expected SKU-NAME-001, got %v", results[0]["sku"])
	}
}
