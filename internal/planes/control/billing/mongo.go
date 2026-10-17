// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// mongoBillingRepo implements BillingRepository against the control_billing_records collection.
type mongoBillingRepo struct {
	col *mongo.Collection
}

// NewMongoBillingRepository creates a BillingRepository backed by MongoDB.
func NewMongoBillingRepository(db *mongo.Database) (BillingRepository, error) {
	r := &mongoBillingRepo{col: db.Collection("control_billing_records")}
	// _id is merchant_id — unique by default. stripe_customer_id gets a
	// partial unique index (partial because most documents have it empty,
	// and a plain unique index would collide on that) so the webhook
	// handler's reverse lookup can't cross merchants.
	_, err := r.col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "stripe_customer_id", Value: 1}},
		Options: options.Index().SetUnique(true).
			SetPartialFilterExpression(bson.M{"stripe_customer_id": bson.M{"$exists": true, "$gt": ""}}),
	})
	if err != nil {
		return nil, fmt.Errorf("create billing record indexes: %w", err)
	}
	return r, nil
}

func (r *mongoBillingRepo) Get(ctx context.Context, id domain.MerchantID) (*BillingRecordModel, error) {
	var m BillingRecordModel
	err := r.col.FindOne(ctx, bson.M{"_id": string(id)}).Decode(&m)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get billing record %s: %w", id, err)
	}
	return &m, nil
}

func (r *mongoBillingRepo) GetByStripeCustomerID(ctx context.Context, customerID string) (*BillingRecordModel, error) {
	var m BillingRecordModel
	err := r.col.FindOne(ctx, bson.M{"stripe_customer_id": customerID}).Decode(&m)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get billing record by stripe customer %s: %w", customerID, err)
	}
	return &m, nil
}

func (r *mongoBillingRepo) Upsert(ctx context.Context, m *BillingRecordModel) error {
	opts := options.UpdateOne().SetUpsert(true)
	_, err := r.col.UpdateOne(ctx,
		bson.M{"_id": m.MerchantID},
		bson.M{"$set": m},
		opts,
	)
	if err != nil {
		return fmt.Errorf("upsert billing record %s: %w", m.MerchantID, err)
	}
	return nil
}

// mongoNotificationPrefsRepo implements NotificationPrefsRepository against
// the control_notification_prefs collection.
type mongoNotificationPrefsRepo struct {
	col *mongo.Collection
}

// NewMongoNotificationPrefsRepository creates a NotificationPrefsRepository backed by MongoDB.
func NewMongoNotificationPrefsRepository(db *mongo.Database) (NotificationPrefsRepository, error) {
	r := &mongoNotificationPrefsRepo{col: db.Collection("control_notification_prefs")}
	// _id is merchant_id — unique by default; no extra indexes needed.
	return r, nil
}

func (r *mongoNotificationPrefsRepo) Get(ctx context.Context, id domain.MerchantID) (*NotificationPrefsModel, error) {
	var m NotificationPrefsModel
	err := r.col.FindOne(ctx, bson.M{"_id": string(id)}).Decode(&m)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			// Return zero-value defaults rather than an error — no prefs set is valid.
			return &NotificationPrefsModel{MerchantID: string(id)}, nil
		}
		return nil, fmt.Errorf("get notification prefs %s: %w", id, err)
	}
	return &m, nil
}

func (r *mongoNotificationPrefsRepo) Upsert(ctx context.Context, m *NotificationPrefsModel) error {
	opts := options.UpdateOne().SetUpsert(true)
	_, err := r.col.UpdateOne(ctx,
		bson.M{"_id": m.MerchantID},
		bson.M{"$set": m},
		opts,
	)
	if err != nil {
		return fmt.Errorf("upsert notification prefs %s: %w", m.MerchantID, err)
	}
	return nil
}

// mongoWebhookEventRepo implements WebhookEventRepository against the
// control_billing_webhook_events collection — a dedup ledger, one document
// per processed Stripe event ID.
type mongoWebhookEventRepo struct {
	col *mongo.Collection
}

// NewMongoWebhookEventRepository creates a WebhookEventRepository backed by MongoDB.
func NewMongoWebhookEventRepository(db *mongo.Database) (WebhookEventRepository, error) {
	r := &mongoWebhookEventRepo{col: db.Collection("control_billing_webhook_events")}
	// _id is the Stripe event ID — unique by default; no extra indexes needed.
	return r, nil
}

func (r *mongoWebhookEventRepo) IsProcessed(ctx context.Context, eventID string) (bool, error) {
	err := r.col.FindOne(ctx, bson.M{"_id": eventID}).Err()
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return false, nil
		}
		return false, fmt.Errorf("check webhook event processed %s: %w", eventID, err)
	}
	return true, nil
}

func (r *mongoWebhookEventRepo) MarkProcessed(ctx context.Context, eventID string) error {
	opts := options.UpdateOne().SetUpsert(true)
	_, err := r.col.UpdateOne(ctx,
		bson.M{"_id": eventID},
		bson.M{"$setOnInsert": bson.M{"processed_at": time.Now().UTC()}},
		opts,
	)
	if err != nil {
		return fmt.Errorf("mark webhook event processed %s: %w", eventID, err)
	}
	return nil
}
