// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package journey

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// mongoJourneyRepo implements Repository against the control_journey collection.
type mongoJourneyRepo struct {
	col *mongo.Collection
}

// NewMongoJourneyRepository creates a Repository backed by MongoDB.
func NewMongoJourneyRepository(db *mongo.Database) (Repository, error) {
	col := db.Collection("control_journey")
	// _id is store_id (unique); merchant_id is indexed so Get can enforce
	// tenant isolation as part of the query rather than a post-hoc check.
	_, err := col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "merchant_id", Value: 1}},
	})
	if err != nil {
		return nil, fmt.Errorf("create merchant_id index: %w", err)
	}
	return &mongoJourneyRepo{col: col}, nil
}

func (r *mongoJourneyRepo) Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (*JourneyModel, error) {
	var m JourneyModel
	filter := bson.M{"_id": string(storeID), "merchant_id": string(merchantID)}
	err := r.col.FindOne(ctx, filter).Decode(&m)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get journey %s: %w", storeID, err)
	}
	return &m, nil
}

func (r *mongoJourneyRepo) Upsert(ctx context.Context, m *JourneyModel) error {
	opts := options.UpdateOne().SetUpsert(true)
	_, err := r.col.UpdateOne(ctx,
		bson.M{"_id": m.ID},
		bson.M{"$set": m},
		opts,
	)
	if err != nil {
		return fmt.Errorf("upsert journey %s: %w", m.ID, err)
	}
	return nil
}
