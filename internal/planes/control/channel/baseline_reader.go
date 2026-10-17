// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package channel

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// BaselineConfidence mirrors internal/planes/sync/webbaseline.Confidence's
// string values exactly, redeclared rather than imported — importing a
// sync-plane package from this control-plane package would violate the
// plane-isolation invariant (same rationale as consumer.go's payload
// structs duplicating the serving-plane's event shapes field-for-field
// instead of importing them).
type BaselineConfidence string

const (
	BaselineConnectorReported BaselineConfidence = "connector_reported"
	BaselinePartial           BaselineConfidence = "partial"
	BaselineUnavailable       BaselineConfidence = "unavailable"
)

// BaselineDay is the read-side view of a
// internal/planes/sync/webbaseline.BaselineDay document.
type BaselineDay struct {
	StoreID      string
	Date         string
	Orders       int64
	RevenueMinor int64
	AOVMinor     int64
	Currency     string
	Source       string
	Confidence   BaselineConfidence
}

// BaselineReader reads the sync-plane-owned sync_web_baseline_daily
// collection (R10/R11) — read-only here: a control-plane reader over a
// sync-owned collection.
type BaselineReader interface {
	ListRange(ctx context.Context, merchantID, storeID, from, to string) ([]BaselineDay, error)
}

type mongoBaselineReader struct {
	baselines *mongo.Collection
}

// NewMongoBaselineReader constructs a BaselineReader over
// sync_web_baseline_daily.
func NewMongoBaselineReader(db *mongo.Database) BaselineReader {
	return &mongoBaselineReader{baselines: db.Collection("sync_web_baseline_daily")}
}

type baselineDoc struct {
	MerchantID   string    `bson:"merchant_id"`
	StoreID      string    `bson:"store_id"`
	Date         string    `bson:"date"`
	Orders       int64     `bson:"orders"`
	RevenueMinor int64     `bson:"revenue_minor"`
	AOVMinor     int64     `bson:"aov_minor"`
	Currency     string    `bson:"currency"`
	Source       string    `bson:"source"`
	Confidence   string    `bson:"confidence"`
	SyncedAt     time.Time `bson:"synced_at"`
}

func (r *mongoBaselineReader) ListRange(ctx context.Context, merchantID, storeID, from, to string) ([]BaselineDay, error) {
	q := bson.M{"merchant_id": merchantID, "date": bson.M{"$gte": from, "$lte": to}}
	if storeID != "" {
		q["store_id"] = storeID
	}
	cur, err := r.baselines.Find(ctx, q, options.Find().SetSort(bson.D{{Key: "date", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("list baseline range: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()

	var docs []baselineDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode baseline range: %w", err)
	}
	days := make([]BaselineDay, len(docs))
	for i, d := range docs {
		days[i] = BaselineDay{
			StoreID: d.StoreID, Date: d.Date, Orders: d.Orders, RevenueMinor: d.RevenueMinor,
			AOVMinor: d.AOVMinor, Currency: d.Currency, Source: d.Source, Confidence: BaselineConfidence(d.Confidence),
		}
	}
	return days, nil
}
