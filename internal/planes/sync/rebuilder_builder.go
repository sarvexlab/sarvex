// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	servingpkg "github.com/sarvexlab/sarvex/internal/pkg/serving"
)

// ProductSnapshot holds the products and metadata from the catalog store.
// This is a lightweight intermediate structure - serving plane adds profile/shipping.
type ProductSnapshot struct {
	StoreID    string
	BuiltAt    time.Time
	SourceSync time.Time
	Products   map[string]*servingpkg.CacheProduct
}

// SnapshotBuilder generates product snapshots from catalog state.
// Sync plane responsibility: fetch and transform products.
// Serving plane responsibility: add profile, shipping, and build final CacheSnapshot.
type SnapshotBuilder interface {
	// BuildProductSnapshot fetches products from the catalog store and returns them.
	// If skus is empty, builds for all products (full rebuild).
	// If skus is non-empty, builds for those products only (partial rebuild).
	BuildProductSnapshot(ctx context.Context, storeID string, skus []string) (*ProductSnapshot, error)
}

// MongoSnapshotBuilder implements SnapshotBuilder backed by the MongoDB catalog store.
type MongoSnapshotBuilder struct {
	db          *mongo.Database
	transformer ProductTransformer
	log         *slog.Logger
}

// NewMongoSnapshotBuilder creates a snapshot builder.
func NewMongoSnapshotBuilder(db *mongo.Database, transformer ProductTransformer, log *slog.Logger) *MongoSnapshotBuilder {
	return &MongoSnapshotBuilder{
		db:          db,
		transformer: transformer,
		log:         log,
	}
}

// BuildProductSnapshot fetches products from the catalog store and returns them.
// This is sync-plane-only responsibility: transform products from catalog store.
// The serving plane adds profile, shipping config, and builds the final CacheSnapshot.
func (msb *MongoSnapshotBuilder) BuildProductSnapshot(ctx context.Context, storeID string, skus []string) (*ProductSnapshot, error) {
	coll := msb.db.Collection(catalogProductsCollection)
	now := time.Now().UTC()

	// Build query: fetch products for the store, optionally filtered by SKU.
	// Catalog data is partitioned by store_id, never merchant_id.
	filter := bson.M{
		"store_id":   storeID,
		"deleted_at": bson.M{"$exists": false}, // Exclude soft-deleted
	}

	if len(skus) > 0 {
		// Partial rebuild: fetch only specified SKUs
		filter["sku"] = bson.M{"$in": skus}
	}

	// Fetch all products matching the filter.
	cursor, err := coll.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("fetch products: %w", err)
	}
	defer cursor.Close(ctx)

	// Decode and transform products.
	products := make(map[string]*servingpkg.CacheProduct)
	var sourceSync time.Time // Track the latest product update time

	if err = cursor.All(ctx, &[]CatalogProduct{}); err != nil {
		// Retry with proper decoding
	}

	// Redo with proper iteration
	cursor, err = coll.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("fetch products (retry): %w", err)
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var catalogProduct CatalogProduct
		if err := cursor.Decode(&catalogProduct); err != nil {
			msb.log.Warn("failed to decode product, skipping", "error", err)
			continue
		}

		// Transform to serving format.
		cacheProduct, err := msb.transformer.Transform(&catalogProduct)
		if err != nil {
			msb.log.Warn("failed to transform product, skipping", "sku", catalogProduct.SKU, "error", err)
			continue
		}

		products[catalogProduct.SKU] = cacheProduct

		// Update sourceSync to the latest product timestamp.
		if catalogProduct.UpdatedAt.After(sourceSync) {
			sourceSync = catalogProduct.UpdatedAt
		}
	}

	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor error: %w", err)
	}

	// Return product snapshot (sync plane responsibility only).
	snapshot := &ProductSnapshot{
		StoreID:    storeID,
		BuiltAt:    now,
		SourceSync: sourceSync,
		Products:   products,
	}

	msb.log.Info("product snapshot built",
		"store_id", storeID,
		"product_count", len(products),
		"partial", len(skus) > 0,
		"source_sync", sourceSync,
	)

	return snapshot, nil
}
