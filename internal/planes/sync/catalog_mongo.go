// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongoopts "go.mongodb.org/mongo-driver/v2/mongo/options"
)

// catalogProductsCollection is the catalog store's collection name. It is
// shared wire identity with control/catalog.NewMongoProductRepository, which
// reads this same collection read-only (architecture §4.3 "one writer").
const catalogProductsCollection = "sync_catalog_products"

// ErrMissingTenantKey is returned when a product is upserted without the
// identity fields the storage key is derived from.
var ErrMissingTenantKey = errors.New("sync: product missing merchant_id, store_id, or sku")

// MongoDBCatalogStore is a MongoDB-backed catalog store (one writer per store).
type MongoDBCatalogStore struct {
	db *mongo.Database
}

// NewMongoDBCatalogStore creates a MongoDB-backed catalog store.
func NewMongoDBCatalogStore(db *mongo.Database) (*MongoDBCatalogStore, error) {
	coll := db.Collection(catalogProductsCollection)

	indexModels := []mongo.IndexModel{
		// Composite primary key: merchant_id:store_id:sku (matches
		// control/catalog.ProductModel's _id scheme).
		{
			Keys: bson.D{{Key: "_id", Value: 1}},
			// Options: mongoopts.Index().SetUnique(true),
		},
		// Unique (store_id, sku): the write seam's own tenancy contract —
		// store_id determines merchant_id 1:1, so this alone enforces
		// "one product per (store, sku)" without requiring merchant_id on
		// every call.
		{
			Keys: bson.D{{Key: "store_id", Value: 1}, {Key: "sku", Value: 1}},
			// Options: mongoopts.Index().SetUnique(true),
		},
		// Latest update per store (for freshness watermark)
		{
			Keys: bson.D{
				{Key: "store_id", Value: 1},
				{Key: "updated_at", Value: -1},
			},
		},
		// Query by merchant (for backward compat, cross-store listing)
		{
			Keys: bson.D{{Key: "merchant_id", Value: 1}},
		},
	}

	opts := mongoopts.CreateIndexes()
	_, err := coll.Indexes().CreateMany(context.Background(), indexModels, opts)

	if err != nil {
		return nil, fmt.Errorf("create catalog indexes: %w", err)
	}

	return &MongoDBCatalogStore{db: db}, nil
}

// docID derives the storage key "<merchant_id>:<store_id>:<sku>".
func docID(merchantID, storeID, sku string) string {
	return fmt.Sprintf("%s:%s:%s", merchantID, storeID, sku)
}

// UpsertProduct stores or updates a product atomically. The document id is
// always (re)computed from product.MerchantID/StoreID/SKU, never trusted
// from a caller-supplied product.ID.
func (mcs *MongoDBCatalogStore) UpsertProduct(ctx context.Context, product *CatalogProduct) error {
	if product.MerchantID == "" || product.StoreID == "" || product.SKU == "" {
		return ErrMissingTenantKey
	}
	product.ID = docID(product.MerchantID, product.StoreID, product.SKU)

	coll := mcs.db.Collection(catalogProductsCollection)

	opts := mongoopts.UpdateOne().SetUpsert(true)
	// $unset clears any soft-delete tombstone: an upsert means the store has
	// this SKU now, so a record deleted earlier and re-added has to come back.
	// It cannot be left to $set — DeletedAt is `omitempty` and is nil on every
	// write path (only DeleteProduct sets deleted_at, and it writes the field
	// directly), so $set alone silently preserved the tombstone and the
	// re-added product stayed invisible to every read that filters on it.
	_, err := coll.UpdateOne(ctx, bson.M{"_id": product.ID}, bson.M{
		"$set":   product,
		"$unset": bson.M{"deleted_at": ""},
	}, opts)
	if err != nil {
		return fmt.Errorf("upsert product: %w", err)
	}

	return nil
}

// DeleteProduct marks a product as deleted (soft delete via flag or hard delete).
// For now, using soft delete (mark updated_at but preserve record for auditing).
func (mcs *MongoDBCatalogStore) DeleteProduct(ctx context.Context, storeID, sku string) error {
	coll := mcs.db.Collection(catalogProductsCollection)

	// Soft delete: update a "deleted_at" field
	result, err := coll.UpdateOne(ctx, bson.M{"store_id": storeID, "sku": sku}, bson.M{
		"$set": bson.M{
			"updated_at": time.Now().UTC(),
			"deleted_at": time.Now().UTC(),
		},
	})
	if err != nil {
		return fmt.Errorf("delete product: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrProductNotFound
	}

	return nil
}

// GetProduct retrieves a product by (store_id, sku).
func (mcs *MongoDBCatalogStore) GetProduct(ctx context.Context, storeID, sku string) (*CatalogProduct, error) {
	coll := mcs.db.Collection(catalogProductsCollection)

	// Soft-deleted products are excluded, matching ListByStore, LatestUpdateTime
	// and the serving-cache rebuilder. Without this GetProduct was the one read
	// that still returned tombstones: a product removed from the merchant's
	// store came back as live, the reconciliation sweep could never observe its
	// own correction, and ingestion's existingOrBlank merged a fresh pull onto
	// the tombstone instead of starting clean.
	var product CatalogProduct
	err := coll.FindOne(ctx, bson.M{
		"store_id":   storeID,
		"sku":        sku,
		"deleted_at": bson.M{"$exists": false},
	}).Decode(&product)
	if err == mongo.ErrNoDocuments {
		return nil, ErrProductNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get product: %w", err)
	}

	return &product, nil
}

// ListByStore returns all products for a store (paginated).
func (mcs *MongoDBCatalogStore) ListByStore(ctx context.Context, storeID string, limit int, cursor string) ([]*CatalogProduct, *string, error) {
	coll := mcs.db.Collection(catalogProductsCollection)

	// Build filter: store_id = storeID, exclude soft-deleted
	filter := bson.M{
		"store_id":   storeID,
		"deleted_at": bson.M{"$exists": false},
	}

	// If cursor provided, fetch after it (alphabetical SKU order)
	if cursor != "" {
		filter["sku"] = bson.M{"$gt": cursor}
	}

	// Fetch limit+1 to determine if there are more
	opts := mongoopts.Find().SetLimit(int64(limit + 1)).SetSort(bson.M{"sku": 1})
	cursor_iter, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("list products: %w", err)
	}
	defer cursor_iter.Close(ctx)

	products := make([]*CatalogProduct, 0)
	if err = cursor_iter.All(ctx, &products); err != nil {
		return nil, nil, fmt.Errorf("decode products: %w", err)
	}

	// Check if there are more results
	var nextCursor *string
	if len(products) > limit {
		// Trim to limit size and set cursor to last SKU
		products = products[:limit]
		lastSKU := products[limit-1].SKU
		nextCursor = &lastSKU
	}

	return products, nextCursor, nil
}

// ListStoreIDs returns the distinct store_ids present in the catalog store.
func (mcs *MongoDBCatalogStore) ListStoreIDs(ctx context.Context) ([]string, error) {
	coll := mcs.db.Collection(catalogProductsCollection)

	var storeIDs []string
	if err := coll.Distinct(ctx, "store_id", bson.M{}).Decode(&storeIDs); err != nil {
		return nil, fmt.Errorf("list store ids: %w", err)
	}
	return storeIDs, nil
}

// LatestUpdateTime returns the most recent product update timestamp for a store.
func (mcs *MongoDBCatalogStore) LatestUpdateTime(ctx context.Context, storeID string) (time.Time, error) {
	coll := mcs.db.Collection(catalogProductsCollection)

	filter := bson.M{
		"store_id":   storeID,
		"deleted_at": bson.M{"$exists": false},
	}

	opts := mongoopts.FindOne().SetSort(bson.M{"updated_at": -1})
	var product CatalogProduct
	err := coll.FindOne(ctx, filter, opts).Decode(&product)
	if err == mongo.ErrNoDocuments {
		// No products yet; return zero time (or current time as fallback)
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("get latest update time: %w", err)
	}

	return product.UpdatedAt, nil
}
