// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"testing"
	"time"
)

// TestCatalog_InsertRetrieveProduct tests basic catalog CRUD operations.
func TestCatalog_InsertRetrieveProduct(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Create and insert product
	product := newTestProduct("store-a", "merch-a", "SKU-001")
	_, err := env.catalogCol.InsertOne(ctx, product)
	if err != nil {
		t.Fatalf("failed to insert product: %v", err)
	}

	// Retrieve product by ID
	var retrieved map[string]interface{}
	err = env.catalogCol.FindOne(ctx, map[string]interface{}{"_id": product.ID}).Decode(&retrieved)
	if err != nil {
		t.Fatalf("failed to retrieve product: %v", err)
	}

	// Verify basic fields
	if retrieved["sku"] != "SKU-001" {
		t.Errorf("expected SKU SKU-001, got %v", retrieved["sku"])
	}
	if retrieved["store_id"] != "store-a" {
		t.Errorf("expected store_id store-a, got %v", retrieved["store_id"])
	}
}

// TestCatalog_TenantIsolation verifies per-store product scoping.
func TestCatalog_TenantIsolation(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Insert products for two different stores
	productA := newTestProduct("store-a", "merch-a", "SKU-TENANT-001")
	productB := newTestProduct("store-b", "merch-a", "SKU-TENANT-002")

	if _, err := env.catalogCol.InsertMany(ctx, []interface{}{productA, productB}); err != nil {
		t.Fatalf("failed to insert products: %v", err)
	}

	// Query store-a only
	cursor, err := env.catalogCol.Find(ctx, map[string]interface{}{"store_id": "store-a"})
	if err != nil {
		t.Fatalf("failed to query store-a: %v", err)
	}
	defer cursor.Close(ctx)

	var productsA []map[string]interface{}
	if err = cursor.All(ctx, &productsA); err != nil {
		t.Fatalf("failed to decode products: %v", err)
	}

	if len(productsA) != 1 {
		t.Errorf("expected 1 product for store-a, got %d", len(productsA))
	} else if productsA[0]["sku"] != "SKU-TENANT-001" {
		t.Errorf("expected SKU-TENANT-001, got %v", productsA[0]["sku"])
	}
}

// TestCatalog_UpdateProduct tests atomic product updates.
func TestCatalog_UpdateProduct(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Insert product
	product := newTestProduct("store-a", "merch-a", "SKU-UPDATE-001")
	_, err := env.catalogCol.InsertOne(ctx, product)
	if err != nil {
		t.Fatalf("failed to insert product: %v", err)
	}

	// Update product price and title
	_, err = env.catalogCol.UpdateOne(ctx,
		map[string]interface{}{"_id": product.ID},
		map[string]interface{}{
			"$set": map[string]interface{}{
				"price_minor": int64(4999),
				"title":       "Updated Product",
				"updated_at":  time.Now().UTC(),
			},
		},
	)
	if err != nil {
		t.Fatalf("failed to update product: %v", err)
	}

	// Verify update
	var updated map[string]interface{}
	env.catalogCol.FindOne(ctx, map[string]interface{}{"_id": product.ID}).Decode(&updated)

	if updated["price_minor"] != int64(4999) {
		t.Errorf("expected price_minor 4999, got %v", updated["price_minor"])
	}
	if updated["title"] != "Updated Product" {
		t.Errorf("expected title 'Updated Product', got %v", updated["title"])
	}
}

// TestCatalog_Blocking flags blocking products.
func TestCatalog_Blocking(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Create blocking product (missing price)
	blockingProduct := newTestProduct("store-a", "merch-a", "SKU-BLOCKING-001")
	blockingProduct.PriceMinor = 0 // Missing price
	blockingProduct.BlocksCheckout = true

	_, err := env.catalogCol.InsertOne(ctx, blockingProduct)
	if err != nil {
		t.Fatalf("failed to insert blocking product: %v", err)
	}

	// Query for blocking products
	cursor, err := env.catalogCol.Find(ctx, map[string]interface{}{
		"store_id":           "store-a",
		"has_blocking_issue": true,
	})
	if err != nil {
		t.Fatalf("failed to query blocking products: %v", err)
	}
	defer cursor.Close(ctx)

	var blocking []map[string]interface{}
	if err = cursor.All(ctx, &blocking); err != nil {
		t.Fatalf("failed to decode blocking products: %v", err)
	}

	if len(blocking) != 1 {
		t.Errorf("expected 1 blocking product, got %d", len(blocking))
	}
}

// TestCatalog_ReadinessScore verifies readiness scoring.
func TestCatalog_ReadinessScore(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Create products with different scores
	products := []*map[string]interface{}{
		{
			"_id":                "merch-a:store-a:SKU-SCORE-001",
			"store_id":           "store-a",
			"merchant_id":        "merch-a",
			"sku":                "SKU-SCORE-001",
			"score":              100,
			"has_blocking_issue": false,
			"updated_at":         time.Now().UTC(),
		},
		{
			"_id":                "merch-a:store-a:SKU-SCORE-002",
			"store_id":           "store-a",
			"merchant_id":        "merch-a",
			"sku":                "SKU-SCORE-002",
			"score":              50,
			"has_blocking_issue": true,
			"updated_at":         time.Now().UTC(),
		},
	}

	docs := make([]interface{}, len(products))
	for i, p := range products {
		docs[i] = *p
	}

	if _, err := env.catalogCol.InsertMany(ctx, docs); err != nil {
		t.Fatalf("failed to insert products: %v", err)
	}

	// Query products by readiness score
	cursor, err := env.catalogCol.Find(ctx, map[string]interface{}{
		"store_id": "store-a",
		"score":    map[string]interface{}{"$gte": 100},
	})
	if err != nil {
		t.Fatalf("failed to query by score: %v", err)
	}
	defer cursor.Close(ctx)

	var ready []map[string]interface{}
	if err = cursor.All(ctx, &ready); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if len(ready) != 1 {
		t.Errorf("expected 1 fully ready product, got %d", len(ready))
	}
}

// TestCatalog_Idempotency verifies duplicate inserts are safely handled.
func TestCatalog_Idempotency(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	product := newTestProduct("store-a", "merch-a", "SKU-IDEMP-001")

	// First insert succeeds
	_, err := env.catalogCol.InsertOne(ctx, product)
	if err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	// Second insert fails (unique constraint)
	_, err = env.catalogCol.InsertOne(ctx, product)
	if err == nil {
		t.Error("second insert should fail due to unique index")
	} else {
		t.Logf("second insert correctly rejected: %v", err)
	}

	// Verify only one copy exists
	count, err := env.catalogCol.CountDocuments(ctx, map[string]interface{}{"_id": product.ID})
	if err != nil {
		t.Fatalf("failed to count documents: %v", err)
	}

	if count != 1 {
		t.Errorf("expected exactly 1 document, got %d", count)
	}
}
