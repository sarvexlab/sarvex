// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package surface

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// mongoRepo implements Repository against the control_store_surfaces
// collection.
type mongoRepo struct {
	col *mongo.Collection
}

// NewMongoRepository creates a Repository backed by MongoDB.
func NewMongoRepository(db *mongo.Database) (Repository, error) {
	r := &mongoRepo{col: db.Collection("control_store_surfaces")}
	if err := r.createIndexes(context.Background()); err != nil {
		return nil, fmt.Errorf("control_store_surfaces indexes: %w", err)
	}
	return r, nil
}

func (r *mongoRepo) createIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "merchant_id", Value: 1},
				{Key: "store_id", Value: 1},
				{Key: "surface_id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "store_id", Value: 1}}},
		// Backs the worker-side "which stores are enrolled in X" read.
		{Keys: bson.D{{Key: "surface_id", Value: 1}, {Key: "state", Value: 1}}},
	})
	return err
}

func (r *mongoRepo) Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) (*EnrollmentModel, error) {
	filter := bson.M{
		"merchant_id": string(merchantID),
		"store_id":    string(storeID),
		"surface_id":  surfaceID,
	}

	var m EnrollmentModel
	err := r.col.FindOne(ctx, filter).Decode(&m)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get enrollment %s/%s: %w", storeID, surfaceID, err)
	}
	return &m, nil
}

func (r *mongoRepo) ListByStore(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) ([]*EnrollmentModel, error) {
	filter := bson.M{"merchant_id": string(merchantID), "store_id": string(storeID)}
	opts := options.Find().SetSort(bson.D{{Key: "surface_id", Value: 1}})

	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("list enrollments for store %s: %w", storeID, err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var models []*EnrollmentModel
	if err := cursor.All(ctx, &models); err != nil {
		return nil, fmt.Errorf("decode enrollments for store %s: %w", storeID, err)
	}
	return models, nil
}

func (r *mongoRepo) Upsert(ctx context.Context, m *EnrollmentModel) error {
	filter := bson.M{
		"merchant_id": m.MerchantID,
		"store_id":    m.StoreID,
		"surface_id":  m.SurfaceID,
	}
	_, err := r.col.ReplaceOne(ctx, filter, m, options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("upsert enrollment %s/%s: %w", m.StoreID, m.SurfaceID, err)
	}
	return nil
}

func (r *mongoRepo) Delete(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) error {
	filter := bson.M{
		"merchant_id": string(merchantID),
		"store_id":    string(storeID),
		"surface_id":  surfaceID,
	}
	if _, err := r.col.DeleteOne(ctx, filter); err != nil {
		return fmt.Errorf("delete enrollment %s/%s: %w", storeID, surfaceID, err)
	}
	return nil
}

func (r *mongoRepo) ListStoresBySurfaceState(ctx context.Context, surfaceID string, state domain.SurfaceState) ([]string, error) {
	filter := bson.M{"surface_id": surfaceID, "state": string(state)}
	opts := options.Find().SetProjection(bson.M{"store_id": 1})

	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("list stores on surface %s: %w", surfaceID, err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var rows []struct {
		StoreID string `bson:"store_id"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("decode stores on surface %s: %w", surfaceID, err)
	}

	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.StoreID)
	}
	return out, nil
}
