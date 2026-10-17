// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"testing"
)

// TestCatalog_SearchReturnsProducts tests catalog search functionality.
func TestCatalog_SearchReturnsProducts(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Insert test products into MongoDB catalog
	products := []interface{}{
		map[string]interface{}{
			"_id":         "store-a:SKU-001",
			"store_id":    storeA,
			"sku":         "SKU-001",
			"name":        "iPhone 15",
			"in_stock":    true,
			"price_minor": 99900,
		},
		map[string]interface{}{
			"_id":         "store-a:SKU-002",
			"store_id":    storeA,
			"sku":         "SKU-002",
			"name":        "iPad Pro",
			"in_stock":    true,
			"price_minor": 149900,
		},
	}

	if _, err := env.catalogCol.InsertMany(ctx, products); err != nil {
		t.Fatalf("failed to insert products: %v", err)
	}

	// Search catalog
	cursor, err := env.catalogCol.Find(ctx, map[string]interface{}{"store_id": storeA})
	if err != nil {
		t.Fatalf("failed to search: %v", err)
	}
	defer cursor.Close(ctx)

	var results []map[string]interface{}
	if err = cursor.All(ctx, &results); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 products, got %d", len(results))
	}
}

// TestCatalog_LookupBySkuFindsProduct tests SKU-based lookup.
func TestCatalog_LookupBySkuFindsProduct(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Insert product
	product := map[string]interface{}{
		"_id":         "store-a:SKU-LOOKUP",
		"store_id":    storeA,
		"sku":         "SKU-LOOKUP",
		"name":        "Test Product",
		"in_stock":    true,
		"price_minor": 2999,
	}

	if _, err := env.catalogCol.InsertOne(ctx, product); err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	// Lookup by SKU
	var result map[string]interface{}
	err := env.catalogCol.FindOne(ctx, map[string]interface{}{
		"store_id": storeA,
		"sku":      "SKU-LOOKUP",
	}).Decode(&result)

	if err != nil {
		t.Fatalf("lookup failed: %v", err)
	}

	if result["name"] != "Test Product" {
		t.Errorf("expected name 'Test Product', got %v", result["name"])
	}
}

// TestCatalog_AvailabilityFiltering filters by stock status.
func TestCatalog_AvailabilityFiltering(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Insert products with different stock status
	products := []interface{}{
		map[string]interface{}{
			"_id":      "store-a:AVAIL-001",
			"store_id": storeA,
			"sku":      "AVAIL-001",
			"in_stock": true,
		},
		map[string]interface{}{
			"_id":      "store-a:AVAIL-002",
			"store_id": storeA,
			"sku":      "AVAIL-002",
			"in_stock": false,
		},
	}

	if _, err := env.catalogCol.InsertMany(ctx, products); err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	// Query in-stock products
	cursor, err := env.catalogCol.Find(ctx, map[string]interface{}{
		"store_id": storeA,
		"in_stock": true,
	})
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer cursor.Close(ctx)

	var inStock []map[string]interface{}
	if err = cursor.All(ctx, &inStock); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if len(inStock) != 1 {
		t.Errorf("expected 1 in-stock product, got %d", len(inStock))
	}
}

// TestCatalog_PriceFiltering filters by price range.
func TestCatalog_PriceFiltering(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Insert products with different prices
	products := []interface{}{
		map[string]interface{}{
			"_id":         "store-a:PRICE-001",
			"store_id":    storeA,
			"sku":         "PRICE-001",
			"price_minor": 1000, // $10
		},
		map[string]interface{}{
			"_id":         "store-a:PRICE-002",
			"store_id":    storeA,
			"sku":         "PRICE-002",
			"price_minor": 5000, // $50
		},
	}

	if _, err := env.catalogCol.InsertMany(ctx, products); err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	// Query products under $30
	cursor, err := env.catalogCol.Find(ctx, map[string]interface{}{
		"store_id": storeA,
		"price_minor": map[string]interface{}{
			"$lt": 3000, // < $30
		},
	})
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer cursor.Close(ctx)

	var results []map[string]interface{}
	if err = cursor.All(ctx, &results); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 product under $30, got %d", len(results))
	}
}

// TestCatalog_TenantIsolation verifies cross-store queries are isolated.
func TestCatalog_TenantIsolation(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Insert products for different stores
	products := []interface{}{
		map[string]interface{}{
			"_id":      "store-a:TENANT-001",
			"store_id": storeA,
			"sku":      "TENANT-001",
		},
		map[string]interface{}{
			"_id":      "store-b:TENANT-002",
			"store_id": storeB,
			"sku":      "TENANT-002",
		},
	}

	if _, err := env.catalogCol.InsertMany(ctx, products); err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	// Query store-a only
	cursor, err := env.catalogCol.Find(ctx, map[string]interface{}{"store_id": storeA})
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer cursor.Close(ctx)

	var results []map[string]interface{}
	if err = cursor.All(ctx, &results); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 product for store-a, got %d", len(results))
	}
}
