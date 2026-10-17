// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package billing_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/planes/control/billing"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

func connectTestMongo(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		t.Skip("MONGO_URI not set; skipping MongoDB integration tests")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	db := client.Database("billing_test")
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return db
}

// ── BillingRepository ─────────────────────────────────────────────────────────

func TestMongoBillingRepo_GetNotFound(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := billing.NewMongoBillingRepository(db)
	require.NoError(t, err)

	_, err = repo.Get(context.Background(), "does-not-exist")
	assert.ErrorIs(t, err, billing.ErrNotFound)
}

func TestMongoBillingRepo_UpsertAndGet(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := billing.NewMongoBillingRepository(db)
	require.NoError(t, err)

	m := &billing.BillingRecordModel{
		MerchantID:           "merch-1",
		StripeCustomerID:     "cus_123",
		StripeSubscriptionID: "sub_456",
		Plan:                 string(domain.PlanSelfServe),
		Status:               string(domain.BillingActive),
		SKUBand:              "500",
		SKUCap:               500,
		SKUCount:             10,
	}
	require.NoError(t, repo.Upsert(context.Background(), m))

	got, err := repo.Get(context.Background(), "merch-1")
	require.NoError(t, err)
	assert.Equal(t, "cus_123", got.StripeCustomerID)
	assert.Equal(t, "sub_456", got.StripeSubscriptionID)
	assert.Equal(t, string(domain.PlanSelfServe), got.Plan)
	assert.Equal(t, string(domain.BillingActive), got.Status)
	assert.Equal(t, "500", got.SKUBand)
	assert.Equal(t, 500, got.SKUCap)
	assert.Equal(t, 10, got.SKUCount)
}

func TestMongoBillingRepo_GetByStripeCustomerID_NotFound(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := billing.NewMongoBillingRepository(db)
	require.NoError(t, err)

	_, err = repo.GetByStripeCustomerID(context.Background(), "cus_does_not_exist")
	assert.ErrorIs(t, err, billing.ErrNotFound)
}

func TestMongoBillingRepo_GetByStripeCustomerID_Found(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := billing.NewMongoBillingRepository(db)
	require.NoError(t, err)

	m := &billing.BillingRecordModel{
		MerchantID:       "merch-1",
		StripeCustomerID: "cus_123",
	}
	require.NoError(t, repo.Upsert(context.Background(), m))

	got, err := repo.GetByStripeCustomerID(context.Background(), "cus_123")
	require.NoError(t, err)
	assert.Equal(t, "merch-1", got.MerchantID)
}

func TestMongoBillingRepo_UpsertUpdatesExisting(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := billing.NewMongoBillingRepository(db)
	require.NoError(t, err)

	m := &billing.BillingRecordModel{
		MerchantID: "merch-1",
		Status:     string(domain.BillingActive),
	}
	require.NoError(t, repo.Upsert(context.Background(), m))

	m.Status = string(domain.BillingPastDue)
	require.NoError(t, repo.Upsert(context.Background(), m))

	got, err := repo.Get(context.Background(), "merch-1")
	require.NoError(t, err)
	assert.Equal(t, string(domain.BillingPastDue), got.Status)
}

// ── NotificationPrefsRepository ───────────────────────────────────────────────

func TestMongoNotificationPrefsRepo_GetNoRecord_ReturnsDefaults(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := billing.NewMongoNotificationPrefsRepository(db)
	require.NoError(t, err)

	m, err := repo.Get(context.Background(), "merch-1")
	require.NoError(t, err)
	assert.Equal(t, "merch-1", m.MerchantID)
	assert.False(t, m.EmailEnabled)
	assert.False(t, m.WeeklyDigest)
	assert.Nil(t, m.AlertEmail)
}

func TestMongoNotificationPrefsRepo_UpsertAndGet(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := billing.NewMongoNotificationPrefsRepository(db)
	require.NoError(t, err)

	email := "alerts@acme.com"
	m := &billing.NotificationPrefsModel{
		MerchantID:   "merch-1",
		EmailEnabled: true,
		WeeklyDigest: true,
		AlertEmail:   &email,
	}
	require.NoError(t, repo.Upsert(context.Background(), m))

	got, err := repo.Get(context.Background(), "merch-1")
	require.NoError(t, err)
	assert.True(t, got.EmailEnabled)
	assert.True(t, got.WeeklyDigest)
	require.NotNil(t, got.AlertEmail)
	assert.Equal(t, "alerts@acme.com", *got.AlertEmail)
}

func TestMongoNotificationPrefsRepo_UpsertOverwrites(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := billing.NewMongoNotificationPrefsRepository(db)
	require.NoError(t, err)

	m := &billing.NotificationPrefsModel{
		MerchantID:   "merch-1",
		EmailEnabled: true,
	}
	require.NoError(t, repo.Upsert(context.Background(), m))

	m.EmailEnabled = false
	m.WeeklyDigest = true
	require.NoError(t, repo.Upsert(context.Background(), m))

	got, err := repo.Get(context.Background(), "merch-1")
	require.NoError(t, err)
	assert.False(t, got.EmailEnabled)
	assert.True(t, got.WeeklyDigest)
}

// ── WebhookEventRepository ────────────────────────────────────────────────────

func TestMongoWebhookEventRepo_IsProcessed_FalseUntilMarked(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := billing.NewMongoWebhookEventRepository(db)
	require.NoError(t, err)

	processed, err := repo.IsProcessed(context.Background(), "evt_1")
	require.NoError(t, err)
	assert.False(t, processed)

	require.NoError(t, repo.MarkProcessed(context.Background(), "evt_1"))

	processed, err = repo.IsProcessed(context.Background(), "evt_1")
	require.NoError(t, err)
	assert.True(t, processed)
}

func TestMongoWebhookEventRepo_MarkProcessed_IsIdempotent(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := billing.NewMongoWebhookEventRepository(db)
	require.NoError(t, err)

	require.NoError(t, repo.MarkProcessed(context.Background(), "evt_1"))
	require.NoError(t, repo.MarkProcessed(context.Background(), "evt_1"), "marking an already-processed event again must not error")
}
