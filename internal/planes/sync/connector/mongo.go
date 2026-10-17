// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package connector

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/sync/domain"
)

// mongoConnectorRepo implements ConnectorRepository against MongoDB.
type mongoConnectorRepo struct {
	coll *mongo.Collection
}

// NewMongoConnectorRepository creates a repository for control_connectors collection.
// Creates indexes and returns an error if index creation fails.
func NewMongoConnectorRepository(db *mongo.Database) (ConnectorRepository, error) {
	coll := db.Collection("control_connectors")
	return &mongoConnectorRepo{coll: coll}, nil
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

// GetByStoreID returns the connector record for a store (read-only; see
// ConnectorRepository.GetByStoreID). Mirrors
// control/connector/mongo.go's method of the same name against the same
// control_connectors collection.
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

// ListPaired returns every healthy (paired) connector, unscoped by merchant.
// See ConnectorRepository.ListPaired — dev-tooling-only.
func (r *mongoConnectorRepo) ListPaired(ctx context.Context) ([]*ConnectorModel, error) {
	filter := bson.M{"status": domain.ConnectorStatusHealthy}

	cursor, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list paired connectors: %w", err)
	}
	defer cursor.Close(ctx)

	var models []*ConnectorModel
	if err := cursor.All(ctx, &models); err != nil {
		return nil, fmt.Errorf("decode paired connectors: %w", err)
	}

	return models, nil
}
