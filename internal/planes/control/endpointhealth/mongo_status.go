// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// checkStateModel is the BSON-mapped form of CheckState.
type checkStateModel struct {
	State                 string     `bson:"state"`
	LastPassAt            *time.Time `bson:"last_pass_at,omitempty"`
	LastFailAt            *time.Time `bson:"last_fail_at,omitempty"`
	CurrentValue          float64    `bson:"current_value"`
	ConsecutiveBreaches   int        `bson:"consecutive_breaches"`
	ConsecutiveRecoveries int        `bson:"consecutive_recoveries"`
}

// statusModel is the BSON-mapped storage document, one per store (_id =
// store_id).
type statusModel struct {
	ID           string                     `bson:"_id"`
	StoreID      string                     `bson:"store_id"`
	MerchantID   string                     `bson:"merchant_id"`
	Status       string                     `bson:"status"`
	EvaluatedAt  time.Time                  `bson:"evaluated_at"`
	Checks       map[string]checkStateModel `bson:"checks"`
	ActiveAlerts []string                   `bson:"active_alerts"`
}

type mongoStatusRepo struct {
	col *mongo.Collection
}

// NewMongoStatusRepository creates a StatusRepository backed by MongoDB.
func NewMongoStatusRepository(db *mongo.Database) (StatusRepository, error) {
	r := &mongoStatusRepo{col: db.Collection("control_endpoint_health_status")}
	if _, err := r.col.Indexes().CreateOne(context.Background(), mongo.IndexModel{Keys: bson.D{{Key: "merchant_id", Value: 1}}}); err != nil {
		return nil, fmt.Errorf("endpoint health status indexes: %w", err)
	}
	return r, nil
}

func (r *mongoStatusRepo) Get(ctx context.Context, merchantID, storeID string) (*StatusDocument, error) {
	var m statusModel
	err := r.col.FindOne(ctx, bson.M{"_id": storeID, "merchant_id": merchantID}).Decode(&m)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get status document %s: %w", storeID, err)
	}
	return fromStatusModel(m), nil
}

func (r *mongoStatusRepo) Upsert(ctx context.Context, doc *StatusDocument) error {
	m := toStatusModel(doc)
	_, err := r.col.ReplaceOne(ctx, bson.M{"_id": m.ID}, m, options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("upsert status document %s: %w", m.ID, err)
	}
	return nil
}

func toStatusModel(doc *StatusDocument) statusModel {
	checks := make(map[string]checkStateModel, len(doc.Checks))
	for k, v := range doc.Checks {
		checks[string(k)] = checkStateModel{
			State:                 string(v.State),
			LastPassAt:            v.LastPassAt,
			LastFailAt:            v.LastFailAt,
			CurrentValue:          v.CurrentValue,
			ConsecutiveBreaches:   v.ConsecutiveBreaches,
			ConsecutiveRecoveries: v.ConsecutiveRecoveries,
		}
	}
	return statusModel{
		ID:           doc.StoreID,
		StoreID:      doc.StoreID,
		MerchantID:   doc.MerchantID,
		Status:       string(doc.Status),
		EvaluatedAt:  doc.EvaluatedAt,
		Checks:       checks,
		ActiveAlerts: doc.ActiveAlerts,
	}
}

func fromStatusModel(m statusModel) *StatusDocument {
	checks := make(map[CheckType]CheckState, len(m.Checks))
	for k, v := range m.Checks {
		checks[CheckType(k)] = CheckState{
			State:                 Outcome(v.State),
			LastPassAt:            v.LastPassAt,
			LastFailAt:            v.LastFailAt,
			CurrentValue:          v.CurrentValue,
			ConsecutiveBreaches:   v.ConsecutiveBreaches,
			ConsecutiveRecoveries: v.ConsecutiveRecoveries,
		}
	}
	return &StatusDocument{
		StoreID:      m.StoreID,
		MerchantID:   m.MerchantID,
		Status:       Severity(m.Status),
		EvaluatedAt:  m.EvaluatedAt,
		Checks:       checks,
		ActiveAlerts: m.ActiveAlerts,
	}
}
