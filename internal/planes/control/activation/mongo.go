// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package activation

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// mongoRepo implements Repository against the control_store_activation
// collection.
type mongoRepo struct {
	col *mongo.Collection
}

// NewMongoRepository creates a Repository backed by MongoDB.
func NewMongoRepository(db *mongo.Database) (Repository, error) {
	r := &mongoRepo{col: db.Collection("control_store_activation")}
	if err := r.createIndexes(context.Background()); err != nil {
		return nil, fmt.Errorf("control_store_activation indexes: %w", err)
	}
	return r, nil
}

func (r *mongoRepo) createIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "_id", Value: 1}}},
		{Keys: bson.D{{Key: "ucp_state", Value: 1}}},
	})
	return err
}

func (r *mongoRepo) Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (*ActivationModel, error) {
	filter := bson.M{"_id": string(storeID), "merchant_id": string(merchantID)}

	var m ActivationModel
	err := r.col.FindOne(ctx, filter).Decode(&m)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get activation for store %s: %w", storeID, err)
	}
	return &m, nil
}

func (r *mongoRepo) Upsert(ctx context.Context, m *ActivationModel) error {
	filter := bson.M{"_id": m.ID, "merchant_id": m.MerchantID}
	_, err := r.col.ReplaceOne(ctx, filter, m, options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("upsert activation for store %s: %w", m.ID, err)
	}
	return nil
}

func (r *mongoRepo) ListByState(ctx context.Context, state domain.UCPState) ([]*ActivationModel, error) {
	cursor, err := r.col.Find(ctx, bson.M{"ucp_state": string(state)})
	if err != nil {
		return nil, fmt.Errorf("list activation by state %s: %w", state, err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var models []*ActivationModel
	if err := cursor.All(ctx, &models); err != nil {
		return nil, fmt.Errorf("decode activation by state %s: %w", state, err)
	}
	return models, nil
}

func (r *mongoRepo) Delete(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) error {
	filter := bson.M{"_id": string(storeID), "merchant_id": string(merchantID)}
	if _, err := r.col.DeleteOne(ctx, filter); err != nil {
		return fmt.Errorf("delete activation for store %s: %w", storeID, err)
	}
	return nil
}
