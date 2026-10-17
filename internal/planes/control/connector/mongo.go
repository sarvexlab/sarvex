// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package connector

import (
	"context"
	"fmt"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// mongoConnectorRepo implements ConnectorRepository against MongoDB.
type mongoConnectorRepo struct {
	coll *mongo.Collection
}

// NewMongoConnectorRepository creates a repository for control_connectors collection.
// Creates indexes and returns an error if index creation fails.
func NewMongoConnectorRepository(db *mongo.Database, createIndexes bool) (ConnectorRepository, error) {
	coll := db.Collection("control_connectors")

	if createIndexes {
		// Create indexes: merchant_id for list queries, compound for cursor pagination, store_id for HMAC auth.
		indexModel := []mongo.IndexModel{
			{
				Keys: bson.D{{Key: "merchant_id", Value: 1}},
			},
			{
				Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "_id", Value: 1}},
			},
			{
				Keys: bson.D{{Key: "store_id", Value: 1}, {Key: "_id", Value: 1}},
			},
		}
		_, err := coll.Indexes().CreateMany(context.Background(), indexModel)
		if err != nil {
			return nil, fmt.Errorf("create indexes: %w", err)
		}
	}

	return &mongoConnectorRepo{coll: coll}, nil
}

// Get returns the connector record, enforcing tenant isolation.
func (r *mongoConnectorRepo) Get(ctx context.Context, merchantID domain.MerchantID, connectorID string) (*ConnectorModel, error) {
	filter := bson.M{
		"_id":         connectorID,
		"merchant_id": string(merchantID),
	}

	var model ConnectorModel
	err := r.coll.FindOne(ctx, filter).Decode(&model)
	if err == mongo.ErrNoDocuments {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find connector: %w", err)
	}

	return &model, nil
}

// GetByConnector returns the connector record by connector_id (used by HMAC auth).
func (r *mongoConnectorRepo) GetByConnector(ctx context.Context, connectorID string) (*ConnectorModel, error) {
	filter := bson.M{
		"_id": connectorID,
	}

	var model ConnectorModel
	err := r.coll.FindOne(ctx, filter).Decode(&model)
	if err == mongo.ErrNoDocuments {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find connector: %w", err)
	}

	return &model, nil
}

// GetByStoreID returns the connector record for a store. Used by the serving and
// sync planes to retrieve encrypted store credentials without traversing a pairing
// code. Queries the store_id index; returns ErrNotFound when absent.
func (r *mongoConnectorRepo) GetByStoreID(ctx context.Context, storeID string) (*ConnectorModel, error) {
	filter := bson.M{"store_id": storeID}

	var model ConnectorModel
	err := r.coll.FindOne(ctx, filter).Decode(&model)
	if err == mongo.ErrNoDocuments {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find connector by store_id: %w", err)
	}

	return &model, nil
}

// GetByPairingCode returns the connector with the given pairing code.
// Used during pairing completion to find the connector from plugin request.
// Works across all merchants/stores (no tenant isolation here - code is one-time-use).
func (r *mongoConnectorRepo) GetByPairingCode(ctx context.Context, pairingCode string) (*ConnectorModel, error) {
	filter := bson.M{
		"status": domain.ConnectorStatusPairing,
	}

	var model ConnectorModel
	err := r.coll.FindOne(ctx, filter).Decode(&model)
	if err == mongo.ErrNoDocuments {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find connector by pairing code: %w", err)
	}

	return &model, nil
}

// List returns connectors for a merchant with cursor-based pagination.
func (r *mongoConnectorRepo) List(ctx context.Context, merchantID domain.MerchantID, afterID string, limit int) ([]*ConnectorModel, bool, error) {
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
		return nil, false, fmt.Errorf("list connectors: %w", err)
	}
	defer cursor.Close(ctx)

	models := make([]*ConnectorModel, 0, limit)
	if err = cursor.All(ctx, &models); err != nil {
		return nil, false, fmt.Errorf("decode connectors: %w", err)
	}

	hasMore := len(models) > limit
	if hasMore {
		models = models[:limit] // Trim to exactly limit
	}

	return models, hasMore, nil
}

// Upsert creates or replaces the connector document.
func (r *mongoConnectorRepo) Upsert(ctx context.Context, m *ConnectorModel) error {
	filter := bson.M{
		"_id":         m.ID,
		"merchant_id": m.MerchantID,
	}

	opts := options.UpdateOne().SetUpsert(true)
	_, err := r.coll.UpdateOne(ctx, filter, bson.M{"$set": m}, opts)
	if err != nil {
		return fmt.Errorf("upsert connector: %w", err)
	}

	return nil
}

// Delete removes the connector record.
func (r *mongoConnectorRepo) Delete(ctx context.Context, merchantID domain.MerchantID, connectorID string) error {
	filter := bson.M{
		"_id":         connectorID,
		"merchant_id": string(merchantID),
	}

	result, err := r.coll.DeleteOne(ctx, filter)
	if err != nil {
		return fmt.Errorf("delete connector: %w", err)
	}

	if result.DeletedCount == 0 {
		return ErrNotFound
	}

	return nil
}
