// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// AuditLog stores detailed mutation history for every order operation.
// Used to detect double-charges, replay failed checkouts, and audit trails.
type AuditLog struct {
	MutationID      string                 `bson:"_id" json:"mutation_id"`
	MerchantID      string                 `bson:"merchant_id" json:"merchant_id"`
	OrderID         string                 `bson:"order_id" json:"order_id"`
	IdempotencyKey  string                 `bson:"idempotency_key" json:"idempotency_key"`
	AttemptedAt     time.Time              `bson:"attempted_at" json:"attempted_at"`
	CompletedAt     *time.Time             `bson:"completed_at,omitempty" json:"completed_at,omitempty"`
	HandlerResponse map[string]interface{} `bson:"handler_response" json:"handler_response"`
	LocalStatus     string                 `bson:"local_status" json:"local_status"` // "capturing", "paid", "failed", etc.
	ErrorMessage    string                 `bson:"error_message,omitempty" json:"error_message,omitempty"`
	RetryCount      int                    `bson:"retry_count" json:"retry_count"`
	MutationType    string                 `bson:"mutation_type" json:"mutation_type"` // "create", "capture", "refund", "void"
}

// OrderAuditRepository persists and queries order mutation audit logs.
type OrderAuditRepository interface {
	// RecordMutationAttempt records a single mutation attempt (create, capture, refund, void).
	RecordMutationAttempt(ctx context.Context, log *AuditLog) error
	// GetMutationsByKey returns all mutations for a given idempotency key.
	// Used for "replay by key" diagnostics and double-charge detection.
	GetMutationsByKey(ctx context.Context, merchantID, idempotencyKey string) ([]*AuditLog, error)
	// GetMutationsByOrder returns all mutations for a given order ID.
	// Used for order audit trail inspection.
	GetMutationsByOrder(ctx context.Context, orderID string) ([]*AuditLog, error)
}

const auditLogsCollection = "sync_order_mutations"

// MongoOrderAuditRepository is a MongoDB-backed OrderAuditRepository.
type MongoOrderAuditRepository struct {
	db *mongo.Database
}

// NewMongoOrderAuditRepository creates a MongoDB-backed audit repository with TTL index.
func NewMongoOrderAuditRepository(db *mongo.Database) (*MongoOrderAuditRepository, error) {
	coll := db.Collection(auditLogsCollection)

	const ninetyDaysSeconds = 90 * 24 * 60 * 60
	_, err := coll.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "idempotency_key", Value: 1}}},
		{Keys: bson.D{{Key: "order_id", Value: 1}}},
		{Keys: bson.D{{Key: "attempted_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(int32(ninetyDaysSeconds))},
	})
	if err != nil {
		return nil, fmt.Errorf("create audit log indexes: %w", err)
	}

	return &MongoOrderAuditRepository{db: db}, nil
}

// RecordMutationAttempt stores a mutation attempt in the audit log.
func (r *MongoOrderAuditRepository) RecordMutationAttempt(ctx context.Context, log *AuditLog) error {
	_, err := r.db.Collection(auditLogsCollection).InsertOne(ctx, log)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}

// GetMutationsByKey retrieves all mutations for a given idempotency key.
// Useful for replaying a failed checkout: send the same key, get all prior attempts.
func (r *MongoOrderAuditRepository) GetMutationsByKey(ctx context.Context, merchantID, idempotencyKey string) ([]*AuditLog, error) {
	cursor, err := r.db.Collection(auditLogsCollection).Find(ctx, bson.M{
		"merchant_id":     merchantID,
		"idempotency_key": idempotencyKey,
	})
	if err != nil {
		return nil, fmt.Errorf("find mutations by key: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var logs []*AuditLog
	if err := cursor.All(ctx, &logs); err != nil {
		return nil, fmt.Errorf("decode mutations by key: %w", err)
	}
	return logs, nil
}

// GetMutationsByOrder retrieves all mutations for a given order ID.
// Useful for examining an order's complete mutation history.
func (r *MongoOrderAuditRepository) GetMutationsByOrder(ctx context.Context, orderID string) ([]*AuditLog, error) {
	cursor, err := r.db.Collection(auditLogsCollection).Find(ctx, bson.M{
		"order_id": orderID,
	})
	if err != nil {
		return nil, fmt.Errorf("find mutations by order: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var logs []*AuditLog
	if err := cursor.All(ctx, &logs); err != nil {
		return nil, fmt.Errorf("decode mutations by order: %w", err)
	}
	return logs, nil
}

var _ OrderAuditRepository = (*MongoOrderAuditRepository)(nil)
