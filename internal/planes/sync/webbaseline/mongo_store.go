// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package webbaseline

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type mongoStore struct {
	baselines *mongo.Collection
}

// NewMongoStore creates a Store backed by the sync_web_baseline_daily
// collection.
func NewMongoStore(db *mongo.Database) (Store, error) {
	s := &mongoStore{baselines: db.Collection("sync_web_baseline_daily")}
	if _, err := s.baselines.Indexes().CreateOne(context.Background(),
		mongo.IndexModel{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "store_id", Value: 1}, {Key: "date", Value: 1}}},
	); err != nil {
		return nil, fmt.Errorf("sync_web_baseline_daily index: %w", err)
	}
	return s, nil
}

func (s *mongoStore) Upsert(ctx context.Context, day BaselineDay) error {
	if _, err := s.baselines.ReplaceOne(ctx, bson.M{"_id": day.ID}, day, options.Replace().SetUpsert(true)); err != nil {
		return fmt.Errorf("upsert baseline day %s: %w", day.ID, err)
	}
	return nil
}

// mongoStoreLister reads control_stores directly — the same collection
// internal/planes/control/store.NewMongoStoreRepository writes — without
// importing that control-plane package (this is a sync-plane worker; the
// plane-isolation invariant forbids importing control-plane code, but
// reading a shared Mongo collection by name across planes is the
// established pattern, e.g. control's surface.NewMongoSyncedArtifactReader
// reading the sync-owned sync_surface_artifacts collection).
type mongoStoreLister struct {
	stores *mongo.Collection
}

// NewMongoStoreLister creates a StoreLister over control_stores.
func NewMongoStoreLister(db *mongo.Database) StoreLister {
	return &mongoStoreLister{stores: db.Collection("control_stores")}
}

type storeRefDoc struct {
	ID         string `bson:"_id"`
	MerchantID string `bson:"merchant_id"`
	Platform   string `bson:"platform"`
}

func (l *mongoStoreLister) ListAllStores(ctx context.Context, afterID string, limit int) ([]StoreRef, bool, error) {
	if limit < 1 {
		limit = 50
	}
	filter := bson.M{}
	if afterID != "" {
		filter["_id"] = bson.M{"$gt": afterID}
	}
	cur, err := l.stores.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(limit+1)))
	if err != nil {
		return nil, false, fmt.Errorf("list all stores: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()

	var docs []storeRefDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, false, fmt.Errorf("decode stores: %w", err)
	}
	hasMore := len(docs) > limit
	if hasMore {
		docs = docs[:limit]
	}
	refs := make([]StoreRef, len(docs))
	for i, d := range docs {
		refs[i] = StoreRef{MerchantID: d.MerchantID, StoreID: d.ID, Platform: d.Platform}
	}
	return refs, hasMore, nil
}
