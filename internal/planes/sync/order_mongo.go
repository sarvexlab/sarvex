// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ordersCollection holds one document per order, keyed by order_id.
const ordersCollection = "sync_orders"

// MongoOrderRepository is a MongoDB-backed OrderRepository.
type MongoOrderRepository struct {
	db *mongo.Database
}

// NewMongoOrderRepository creates a MongoDB-backed order repository, with indexes:
// - unique on checkout_session_id (redelivered order_intent dedup)
// - compound on (merchant_id, idempotency_key) for idempotency enforcement
// - compound on (store_id, placed_at, status) for replay/audit queries
// - TTL on placed_at for 90-day audit log retention
func NewMongoOrderRepository(db *mongo.Database) (*MongoOrderRepository, error) {
	coll := db.Collection(ordersCollection)

	const ninetyDaysSeconds = 90 * 24 * 60 * 60
	_, err := coll.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{Keys: bson.D{{Key: "checkout_session_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "idempotency_key", Value: 1}}},
		{Keys: bson.D{{Key: "store_id", Value: 1}, {Key: "placed_at", Value: -1}, {Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "placed_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(int32(ninetyDaysSeconds))},
	})
	if err != nil {
		return nil, fmt.Errorf("create order indexes: %w", err)
	}

	return &MongoOrderRepository{db: db}, nil
}

// Create inserts a new order.
func (r *MongoOrderRepository) Create(ctx context.Context, order *Order) error {
	_, err := r.db.Collection(ordersCollection).InsertOne(ctx, order)
	if mongo.IsDuplicateKeyError(err) {
		return ErrOrderExists
	}
	if err != nil {
		return fmt.Errorf("create order: %w", err)
	}
	return nil
}

// GetByCheckoutSession looks up an order by its originating checkout session.
func (r *MongoOrderRepository) GetByCheckoutSession(ctx context.Context, checkoutSessionID string) (*Order, error) {
	var order Order
	err := r.db.Collection(ordersCollection).FindOne(ctx, bson.M{"checkout_session_id": checkoutSessionID}).Decode(&order)
	if err == mongo.ErrNoDocuments {
		return nil, ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get order by checkout session: %w", err)
	}
	return &order, nil
}

var _ OrderRepository = (*MongoOrderRepository)(nil)
