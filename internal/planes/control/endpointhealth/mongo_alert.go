// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"context"
	"fmt"
	"time"

	"github.com/samber/lo"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// alertModel is the BSON-mapped storage document for one Alert.
type alertModel struct {
	ID             string     `bson:"_id"`
	MerchantID     string     `bson:"merchant_id"`
	StoreID        string     `bson:"store_id"`
	CheckType      string     `bson:"check_type"`
	Severity       string     `bson:"severity"`
	Status         string     `bson:"status"`
	Detail         string     `bson:"detail"`
	OpenedAt       time.Time  `bson:"opened_at"`
	AcknowledgedAt *time.Time `bson:"acknowledged_at,omitempty"`
	AcknowledgedBy string     `bson:"acknowledged_by,omitempty"`
	ResolvedAt     *time.Time `bson:"resolved_at,omitempty"`
	AutoResolved   bool       `bson:"auto_resolved"`
}

type mongoAlertRepo struct {
	col *mongo.Collection
}

// NewMongoAlertRepository creates an AlertRepository backed by MongoDB.
func NewMongoAlertRepository(db *mongo.Database) (AlertRepository, error) {
	r := &mongoAlertRepo{col: db.Collection("control_endpoint_health_alerts")}
	ctx := context.Background()
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "opened_at", Value: -1}}},
		{Keys: bson.D{{Key: "store_id", Value: 1}, {Key: "check_type", Value: 1}, {Key: "status", Value: 1}}},
	})
	if err != nil {
		return nil, fmt.Errorf("endpoint health alerts indexes: %w", err)
	}
	return r, nil
}

func (r *mongoAlertRepo) Insert(ctx context.Context, a *Alert) error {
	_, err := r.col.InsertOne(ctx, toAlertModel(a))
	if err != nil {
		return fmt.Errorf("insert alert %s: %w", a.ID, err)
	}
	return nil
}

func (r *mongoAlertRepo) GetOpenByCheck(ctx context.Context, storeID string, checkType CheckType) (*Alert, error) {
	var m alertModel
	err := r.col.FindOne(ctx, bson.M{
		"store_id":   storeID,
		"check_type": string(checkType),
		"status":     bson.M{"$in": bson.A{string(AlertOpen), string(AlertAcknowledged)}},
	}).Decode(&m)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get open alert for %s/%s: %w", storeID, checkType, err)
	}
	return fromAlertModel(m), nil
}

func (r *mongoAlertRepo) Get(ctx context.Context, merchantID, alertID string) (*Alert, error) {
	var m alertModel
	err := r.col.FindOne(ctx, bson.M{"_id": alertID, "merchant_id": merchantID}).Decode(&m)
	if err == mongo.ErrNoDocuments {
		return nil, ErrAlertNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get alert %s: %w", alertID, err)
	}
	return fromAlertModel(m), nil
}

func (r *mongoAlertRepo) Update(ctx context.Context, a *Alert) error {
	m := toAlertModel(a)
	res, err := r.col.ReplaceOne(ctx, bson.M{"_id": m.ID, "merchant_id": m.MerchantID}, m)
	if err != nil {
		return fmt.Errorf("update alert %s: %w", a.ID, err)
	}
	if res.MatchedCount == 0 {
		return ErrAlertNotFound
	}
	return nil
}

func (r *mongoAlertRepo) List(ctx context.Context, merchantID string, filter AlertListFilter, cursor string, limit int) ([]*Alert, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	q := bson.M{"merchant_id": merchantID}
	if filter.Status != "" {
		q["status"] = string(filter.Status)
	}
	if filter.Severity != "" {
		q["severity"] = string(filter.Severity)
	}
	if filter.StoreID != "" {
		q["store_id"] = filter.StoreID
	}
	if cursor != "" {
		q["_id"] = bson.M{"$gt": cursor}
	}

	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(limit) + 1)
	c, err := r.col.Find(ctx, q, opts)
	if err != nil {
		return nil, false, fmt.Errorf("list alerts: %w", err)
	}
	defer c.Close(ctx)

	var docs []alertModel
	if err := c.All(ctx, &docs); err != nil {
		return nil, false, fmt.Errorf("decode alerts: %w", err)
	}

	hasMore := len(docs) > limit
	if hasMore {
		docs = docs[:limit]
	}
	return lo.Map(docs, func(m alertModel, _ int) *Alert { return fromAlertModel(m) }), hasMore, nil
}

func toAlertModel(a *Alert) alertModel {
	return alertModel{
		ID:             a.ID,
		MerchantID:     a.MerchantID,
		StoreID:        a.StoreID,
		CheckType:      string(a.CheckType),
		Severity:       string(a.Severity),
		Status:         string(a.Status),
		Detail:         a.Detail,
		OpenedAt:       a.OpenedAt,
		AcknowledgedAt: a.AcknowledgedAt,
		AcknowledgedBy: a.AcknowledgedBy,
		ResolvedAt:     a.ResolvedAt,
		AutoResolved:   a.AutoResolved,
	}
}

func fromAlertModel(m alertModel) *Alert {
	return &Alert{
		ID:             m.ID,
		MerchantID:     m.MerchantID,
		StoreID:        m.StoreID,
		CheckType:      CheckType(m.CheckType),
		Severity:       Severity(m.Severity),
		Status:         AlertStatus(m.Status),
		Detail:         m.Detail,
		OpenedAt:       m.OpenedAt,
		AcknowledgedAt: m.AcknowledgedAt,
		AcknowledgedBy: m.AcknowledgedBy,
		ResolvedAt:     m.ResolvedAt,
		AutoResolved:   m.AutoResolved,
	}
}
