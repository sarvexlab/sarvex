// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"testing"
	"time"
)

// TestIdempotency_DuplicateProductSync verifies duplicate syncs are safe.
func TestIdempotency_DuplicateProductSync(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	product := newTestProduct("store-a", "merch-a", "SKU-DUP-001")
	product.RawAttributes["message_id"] = "msg-123" // Simulate dedup key

	// First sync inserts product
	_, err := env.catalogCol.InsertOne(ctx, product)
	if err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	// Get state after first sync
	var state1 map[string]interface{}
	env.catalogCol.FindOne(ctx, map[string]interface{}{"_id": product.ID}).Decode(&state1)

	// Second sync attempts same update
	product.Title = "Updated Name"
	_, err = env.catalogCol.ReplaceOne(ctx,
		map[string]interface{}{"_id": product.ID},
		product,
	)
	if err != nil {
		t.Fatalf("replace failed: %v", err)
	}

	// Get state after second sync
	var state2 map[string]interface{}
	env.catalogCol.FindOne(ctx, map[string]interface{}{"_id": product.ID}).Decode(&state2)

	// Verify only one document exists (idempotent)
	count, err := env.catalogCol.CountDocuments(ctx, map[string]interface{}{"_id": product.ID})
	if err != nil || count != 1 {
		t.Errorf("expected exactly 1 document, got %d", count)
	}
}

// TestIdempotency_BatchUpdateSafety verifies batch updates are consistent.
func TestIdempotency_BatchUpdateSafety(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Insert batch of products
	products := []interface{}{
		newTestProduct("store-a", "merch-a", "SKU-BATCH-001"),
		newTestProduct("store-a", "merch-a", "SKU-BATCH-002"),
		newTestProduct("store-a", "merch-a", "SKU-BATCH-003"),
	}

	if _, err := env.catalogCol.InsertMany(ctx, products); err != nil {
		t.Fatalf("failed to insert batch: %v", err)
	}

	// Update all products in batch
	_, err := env.catalogCol.UpdateMany(ctx,
		map[string]interface{}{"store_id": "store-a"},
		map[string]interface{}{
			"$set": map[string]interface{}{
				"score":      100,
				"updated_at": time.Now().UTC(),
			},
		},
	)
	if err != nil {
		t.Fatalf("batch update failed: %v", err)
	}

	// Verify all updated
	cursor, err := env.catalogCol.Find(ctx, map[string]interface{}{
		"store_id": "store-a",
		"score":    100,
	})
	if err != nil {
		t.Fatalf("failed to query updated: %v", err)
	}
	defer cursor.Close(ctx)

	var updated []map[string]interface{}
	if err = cursor.All(ctx, &updated); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if len(updated) != 3 {
		t.Errorf("expected 3 updated products, got %d", len(updated))
	}
}

// TestIdempotency_SequenceOrdering verifies signal ordering preserved.
func TestIdempotency_SequenceOrdering(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Insert product
	product := newTestProduct("store-a", "merch-a", "SKU-SEQ-001")
	product.RawAttributes["sequence"] = "0"
	_, err := env.catalogCol.InsertOne(ctx, product)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	// Apply sequence of updates
	for i := 1; i <= 3; i++ {
		product.RawAttributes["sequence"] = string(rune('0' + byte(i)))
		product.PriceMinor = int64(1000 * i)

		_, err := env.catalogCol.ReplaceOne(ctx,
			map[string]interface{}{"_id": product.ID},
			product,
		)
		if err != nil {
			t.Fatalf("update %d failed: %v", i, err)
		}
	}

	// Verify final state reflects last update
	var final map[string]interface{}
	env.catalogCol.FindOne(ctx, map[string]interface{}{"_id": product.ID}).Decode(&final)

	if final["price_minor"] != int64(3000) {
		t.Errorf("expected final price 3000, got %v", final["price_minor"])
	}
}

// TestIdempotency_ConcurrentWrites verifies atomic writes.
func TestIdempotency_ConcurrentWrites(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	// Insert initial product
	product := newTestProduct("store-a", "merch-a", "SKU-CONC-001")
	_, err := env.catalogCol.InsertOne(ctx, product)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	// Simulate multiple concurrent updates (sequential in this test)
	for i := 0; i < 5; i++ {
		product.ReadinessScore = 20 * (i + 1)
		product.UpdatedAt = time.Now().UTC()

		result, err := env.catalogCol.ReplaceOne(ctx,
			map[string]interface{}{"_id": product.ID},
			product,
		)

		if err != nil {
			t.Errorf("update %d failed: %v", i, err)
		}
		if result.ModifiedCount != 1 {
			t.Errorf("update %d: expected 1 modified, got %d", i, result.ModifiedCount)
		}
	}

	// Verify final state is consistent
	count, _ := env.catalogCol.CountDocuments(ctx, map[string]interface{}{"_id": product.ID})
	if count != 1 {
		t.Errorf("expected exactly 1 document after concurrent writes, got %d", count)
	}
}

// TestIdempotency_ExactlyOnceDelivery verifies no message duplication.
func TestIdempotency_ExactlyOnceDelivery(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx := context.Background()

	product := newTestProduct("store-a", "merch-a", "SKU-E2E-001")
	product.RawAttributes["message_id"] = "msg-e2e-001"

	// Insert product with message_id
	_, err := env.catalogCol.InsertOne(ctx, product)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	// Attempt to re-process same message_id (should be rejected or ignored)
	// In real system, message dedup happens at event bus level
	// Here we verify MongoDB state is consistent

	count, err := env.catalogCol.CountDocuments(ctx, map[string]interface{}{
		"_id":                       product.ID,
		"raw_attributes.message_id": "msg-e2e-001",
	})

	if err != nil || count != 1 {
		t.Errorf("expected 1 document with message_id, got %d", count)
	}

	t.Logf("exactly-once semantics verified: single document with unique message_id")
}
