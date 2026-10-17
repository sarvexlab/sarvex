// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"context"
	"fmt"
	"time"

	"github.com/samber/lo"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// HealthRepository is the read/write interface to HealthStatement storage,
// scoped per store, one document per (store, kind) — always the latest
// check. monitoring.MonitoringLoop is its one writer (semantic probe runs);
// OperatorService.GetHealth is the reader.
type HealthRepository interface {
	// Save upserts the latest statement for (storeID, statement.Kind).
	Save(ctx context.Context, storeID string, statement domain.HealthStatement) error
	// GetByStore returns every kind currently recorded for storeID (no
	// fixed count — only kinds that have ever been checked are present).
	GetByStore(ctx context.Context, storeID string) ([]domain.HealthStatement, error)
}

// healthStatementModel is the BSON-mapped storage document, one per
// (store_id, kind). _id is "storeID:kind" so Save is a plain upsert.
type healthStatementModel struct {
	ID        string    `bson:"_id"`
	StoreID   string    `bson:"store_id"`
	Kind      string    `bson:"kind"`
	OK        bool      `bson:"ok"`
	Message   string    `bson:"message"`
	CheckedAt time.Time `bson:"checked_at"`
}

type mongoHealthRepo struct {
	col *mongo.Collection
}

// NewMongoHealthRepository creates a HealthRepository backed by MongoDB.
func NewMongoHealthRepository(db *mongo.Database) (HealthRepository, error) {
	r := &mongoHealthRepo{col: db.Collection("control_health_statements")}
	if err := r.createIndexes(context.Background()); err != nil {
		return nil, fmt.Errorf("health_statements indexes: %w", err)
	}
	return r, nil
}

func (r *mongoHealthRepo) createIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "store_id", Value: 1}},
	})
	return err
}

func (r *mongoHealthRepo) Save(ctx context.Context, storeID string, statement domain.HealthStatement) error {
	doc := healthStatementModel{
		ID:        storeID + ":" + string(statement.Kind),
		StoreID:   storeID,
		Kind:      string(statement.Kind),
		OK:        statement.OK,
		Message:   statement.Message,
		CheckedAt: statement.CheckedAt,
	}
	_, err := r.col.ReplaceOne(ctx, bson.M{"_id": doc.ID}, doc, options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("save health statement %s: %w", doc.ID, err)
	}
	return nil
}

func (r *mongoHealthRepo) GetByStore(ctx context.Context, storeID string) ([]domain.HealthStatement, error) {
	cursor, err := r.col.Find(ctx, bson.M{"store_id": storeID})
	if err != nil {
		return nil, fmt.Errorf("list health statements for store %s: %w", storeID, err)
	}
	defer cursor.Close(ctx)

	var docs []healthStatementModel
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode health statements for store %s: %w", storeID, err)
	}

	statements := lo.Map(docs, func(d healthStatementModel, _ int) domain.HealthStatement {
		return domain.HealthStatement{
			Kind:      domain.HealthStatementKind(d.Kind),
			OK:        d.OK,
			Message:   d.Message,
			CheckedAt: d.CheckedAt,
		}
	})
	return statements, nil
}
