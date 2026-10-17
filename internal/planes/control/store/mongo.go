// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var ErrNotFound = errors.New("store not found")

// mongoStoreRepo implements StoreRepository against MongoDB.
type mongoStoreRepo struct {
	coll *mongo.Collection
}

// GetByD implements [StoreRepository].
func (r *mongoStoreRepo) GetByD(ctx context.Context, storeID domain.StoreID) (*StoreModel, error) {
	filter := bson.M{
		"_id": string(storeID),
	}

	var model StoreModel
	err := r.coll.FindOne(ctx, filter).Decode(&model)
	if err == mongo.ErrNoDocuments {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find store: %w", err)
	}

	return &model, nil
}

// NewMongoStoreRepository creates a repository for control_stores collection.
// Creates indexes and returns an error if index creation fails.
func NewMongoStoreRepository(db *mongo.Database, createIndexes bool) (StoreRepository, error) {
	coll := db.Collection("control_stores")

	if createIndexes {
		// Create indexes: merchant_id for list queries, compound for cursor pagination.
		indexModel := []mongo.IndexModel{
			{
				Keys: bson.D{{Key: "merchant_id", Value: 1}},
			},
			{
				Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "_id", Value: 1}},
			},
		}
		_, err := coll.Indexes().CreateMany(context.Background(), indexModel)
		if err != nil {
			return nil, fmt.Errorf("create indexes: %w", err)
		}
	}

	return &mongoStoreRepo{coll: coll}, nil
}

// Get returns the store record, enforcing tenant isolation.
func (r *mongoStoreRepo) Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (*StoreModel, error) {
	filter := bson.M{
		"_id":         string(storeID),
		"merchant_id": string(merchantID),
	}

	var model StoreModel
	err := r.coll.FindOne(ctx, filter).Decode(&model)
	if err == mongo.ErrNoDocuments {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find store: %w", err)
	}

	return &model, nil
}

// ListByMerchant returns stores for a merchant with cursor-based pagination.
func (r *mongoStoreRepo) ListByMerchant(ctx context.Context, merchantID domain.MerchantID, afterID string, limit int) ([]*StoreModel, bool, error) {
	// Clamp limit to reasonable bounds.
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	filter := bson.M{"merchant_id": string(merchantID)}
	if afterID != "" {
		filter["_id"] = bson.M{"$gt": afterID}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: 1}}).
		SetLimit(int64(limit + 1)) // Fetch one extra to detect hasMore

	cursor, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, false, fmt.Errorf("list stores: %w", err)
	}
	defer cursor.Close(ctx)

	models := make([]*StoreModel, 0, limit)
	if err = cursor.All(ctx, &models); err != nil {
		return nil, false, fmt.Errorf("decode stores: %w", err)
	}

	hasMore := len(models) > limit
	if hasMore {
		models = models[:limit] // Trim to exactly limit
	}

	return models, hasMore, nil
}

// ListAll returns stores across every merchant, cursor-paginated by _id.
func (r *mongoStoreRepo) ListAll(ctx context.Context, afterID string, limit int) ([]*StoreModel, bool, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	filter := bson.M{}
	if afterID != "" {
		filter["_id"] = bson.M{"$gt": afterID}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: 1}}).
		SetLimit(int64(limit + 1))

	cursor, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, false, fmt.Errorf("list all stores: %w", err)
	}
	defer cursor.Close(ctx)

	models := make([]*StoreModel, 0, limit)
	if err = cursor.All(ctx, &models); err != nil {
		return nil, false, fmt.Errorf("decode stores: %w", err)
	}

	hasMore := len(models) > limit
	if hasMore {
		models = models[:limit]
	}

	return models, hasMore, nil
}

// Upsert creates or replaces the store document.
func (r *mongoStoreRepo) Upsert(ctx context.Context, m *StoreModel) error {
	filter := bson.M{
		"_id":         m.ID,
		"merchant_id": m.MerchantID,
	}

	opts := options.UpdateOne().SetUpsert(true)
	_, err := r.coll.UpdateOne(ctx, filter, bson.M{"$set": m}, opts)
	if err != nil {
		return fmt.Errorf("upsert store: %w", err)
	}

	return nil
}

// Delete removes the store record.
func (r *mongoStoreRepo) Delete(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) error {
	filter := bson.M{
		"_id":         string(storeID),
		"merchant_id": string(merchantID),
	}

	result, err := r.coll.DeleteOne(ctx, filter)
	if err != nil {
		return fmt.Errorf("delete store: %w", err)
	}

	if result.DeletedCount == 0 {
		return ErrNotFound
	}

	return nil
}
