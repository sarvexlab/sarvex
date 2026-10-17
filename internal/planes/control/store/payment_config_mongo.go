// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"fmt"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// mongoPaymentConfigRepo implements PaymentConfigRepository against MongoDB.
type mongoPaymentConfigRepo struct {
	coll *mongo.Collection
}

// NewMongoPaymentConfigRepository creates a repository for control_payment_configs collection.
func NewMongoPaymentConfigRepository(db *mongo.Database) (PaymentConfigRepository, error) {
	coll := db.Collection("control_payment_configs")

	// Create indexes: merchant_id for tenant isolation
	indexModel := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "merchant_id", Value: 1}},
		},
	}
	_, err := coll.Indexes().CreateMany(context.Background(), indexModel)
	if err != nil {
		return nil, fmt.Errorf("create indexes: %w", err)
	}

	return &mongoPaymentConfigRepo{coll: coll}, nil
}

// Get retrieves the payment config for a store.
func (r *mongoPaymentConfigRepo) Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (*PaymentConfigModel, error) {
	filter := bson.M{
		"_id":         string(storeID),
		"merchant_id": string(merchantID),
	}

	var model PaymentConfigModel
	err := r.coll.FindOne(ctx, filter).Decode(&model)
	if err == mongo.ErrNoDocuments {
		return nil, ErrPaymentConfigNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find payment config: %w", err)
	}

	return &model, nil
}

// Upsert creates or replaces the payment config.
func (r *mongoPaymentConfigRepo) Upsert(ctx context.Context, m *PaymentConfigModel) error {
	filter := bson.M{
		"_id":         m.ID,
		"merchant_id": m.MerchantID,
	}

	opts := options.UpdateOne().SetUpsert(true)
	_, err := r.coll.UpdateOne(ctx, filter, bson.M{"$set": m}, opts)
	if err != nil {
		return fmt.Errorf("upsert payment config: %w", err)
	}

	return nil
}

// Delete removes the payment config.
func (r *mongoPaymentConfigRepo) Delete(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) error {
	filter := bson.M{
		"_id":         string(storeID),
		"merchant_id": string(merchantID),
	}

	result, err := r.coll.DeleteOne(ctx, filter)
	if err != nil {
		return fmt.Errorf("delete payment config: %w", err)
	}

	if result.DeletedCount == 0 {
		return ErrPaymentConfigNotFound
	}

	return nil
}
