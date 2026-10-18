// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sim

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// simStoresCollection holds one document per simulated store, keyed by
// store_id. Separate from sync_catalog_products (the real sync-plane catalog
// store) — this collection is the *merchant's* backend, the thing the real
// connector pulls from, not Sarvex's own ingested copy.
const simStoresCollection = "sim_stores"

type mongoStoreRepository struct {
	coll *mongo.Collection
}

// NewMongoStoreRepository creates a Mongo-backed StoreRepository against the
// sim_stores collection.
func NewMongoStoreRepository(db *mongo.Database) StoreRepository {
	return &mongoStoreRepository{coll: db.Collection(simStoresCollection)}
}

func (r *mongoStoreRepository) Get(ctx context.Context, storeID string) (*Store, error) {
	var s Store
	err := r.coll.FindOne(ctx, bson.M{"_id": storeID}).Decode(&s)
	if err == mongo.ErrNoDocuments {
		return nil, ErrStoreNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sim: get store %s: %w", storeID, err)
	}
	return &s, nil
}

func (r *mongoStoreRepository) Upsert(ctx context.Context, s *Store) error {
	opts := options.Replace().SetUpsert(true)
	if _, err := r.coll.ReplaceOne(ctx, bson.M{"_id": s.StoreID}, s, opts); err != nil {
		return fmt.Errorf("sim: upsert store %s: %w", s.StoreID, err)
	}
	return nil
}

func (r *mongoStoreRepository) List(ctx context.Context) ([]*Store, error) {
	cursor, err := r.coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("sim: list stores: %w", err)
	}
	defer cursor.Close(ctx)

	var stores []*Store
	if err := cursor.All(ctx, &stores); err != nil {
		return nil, fmt.Errorf("sim: decode stores: %w", err)
	}
	return stores, nil
}
