// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package catalog_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/planes/control/catalog"
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
	db := client.Database("catalog_test")
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return db
}

// ── ReadinessRepository ───────────────────────────────────────────────────────

func TestMongoReadinessRepo_GetNotFound(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := catalog.NewMongoReadinessRepository(db)
	require.NoError(t, err)

	_, err = repo.Get(context.Background(), "merch-missing", "store-1")
	assert.ErrorIs(t, err, catalog.ErrNotFound)
}

// ── ProductRepository ─────────────────────────────────────────────────────────

func TestMongoProductRepo_GetNotFound(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := catalog.NewMongoProductRepository(db)
	require.NoError(t, err)

	_, err = repo.Get(context.Background(), "merch-1", "store-1", "sku-missing")
	assert.ErrorIs(t, err, catalog.ErrNotFound)
}

func TestMongoProductRepo_ListEmpty(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := catalog.NewMongoProductRepository(db)
	require.NoError(t, err)

	products, hasMore, err := repo.List(context.Background(), "merch-1", "store-1", "", 20, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, products)
	assert.False(t, hasMore)
}

// ── RemediationRepository ─────────────────────────────────────────────────────

func TestMongoRemediationRepo_ListEmpty(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := catalog.NewMongoRemediationRepository(db)
	require.NoError(t, err)

	rems, hasMore, err := repo.List(context.Background(), "merch-1", "store-1", "", 20, nil)
	require.NoError(t, err)
	assert.Empty(t, rems)
	assert.False(t, hasMore)
}

func TestMongoRemediationRepo_GetByIDsNotFound(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := catalog.NewMongoRemediationRepository(db)
	require.NoError(t, err)

	_, err = repo.GetByIDs(context.Background(), "merch-1", "store-1", []string{"r-missing"})
	assert.ErrorIs(t, err, catalog.ErrNotFound)
}

func TestMongoRemediationRepo_ListUnappliedBySKU(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := catalog.NewMongoRemediationRepository(db)
	require.NoError(t, err)
	ctx := context.Background()

	seedRemediation(t, db, &catalog.RemediationModel{
		ID: "r-1", MerchantID: "merch-1", StoreID: "store-1", SKU: "sku-1",
		Field: "color", FromValue: "Rot", ToValue: "Red", Type: "rule", Trust: "high",
		Applied: false, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	seedRemediation(t, db, &catalog.RemediationModel{
		ID: "r-2", MerchantID: "merch-1", StoreID: "store-1", SKU: "sku-1",
		Field: "size", FromValue: "Groß", ToValue: "Large", Type: "rule", Trust: "high",
		Applied: true, WriteBackID: "wb-1", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	seedRemediation(t, db, &catalog.RemediationModel{
		ID: "r-3", MerchantID: "merch-1", StoreID: "store-1", SKU: "sku-2",
		Field: "color", FromValue: "Blau", ToValue: "Blue", Type: "rule", Trust: "high",
		Applied: false, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})

	rems, err := repo.ListUnappliedBySKU(ctx, "merch-1", "store-1", "sku-1")
	require.NoError(t, err)
	require.Len(t, rems, 1)
	assert.Equal(t, "r-1", rems[0].ID)
}

func TestMongoRemediationRepo_ListUnappliedBySKU_Empty(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := catalog.NewMongoRemediationRepository(db)
	require.NoError(t, err)

	rems, err := repo.ListUnappliedBySKU(context.Background(), "merch-1", "store-1", "sku-missing")
	require.NoError(t, err)
	assert.Empty(t, rems)
}

// ── WriteBackRepository ───────────────────────────────────────────────────────

func TestMongoWriteBackRepo_GetNotFound(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := catalog.NewMongoWriteBackRepository(db)
	require.NoError(t, err)

	_, err = repo.Get(context.Background(), "merch-1", "store-1", "wb-missing")
	assert.ErrorIs(t, err, catalog.ErrWriteBackNotFound)
}

func TestMongoWriteBackRepo_InsertAndGet(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := catalog.NewMongoWriteBackRepository(db)
	require.NoError(t, err)

	m := &catalog.WriteBackModel{
		ID:             "wb-1",
		MerchantID:     "merch-1",
		StoreID:        "store-1",
		RemediationIDs: []string{"r-1", "r-2"},
		Reverted:       false,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	require.NoError(t, repo.Insert(context.Background(), m))

	got, err := repo.Get(context.Background(), "merch-1", "store-1", "wb-1")
	require.NoError(t, err)
	assert.Equal(t, "wb-1", got.ID)
	assert.Equal(t, "merch-1", got.MerchantID)
	assert.ElementsMatch(t, []string{"r-1", "r-2"}, got.RemediationIDs)
	assert.False(t, got.Reverted)
}

func TestMongoWriteBackRepo_MarkReverted(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := catalog.NewMongoWriteBackRepository(db)
	require.NoError(t, err)

	m := &catalog.WriteBackModel{
		ID:         "wb-1",
		MerchantID: "merch-1",
		StoreID:    "store-1",
		Reverted:   false,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	require.NoError(t, repo.Insert(context.Background(), m))
	require.NoError(t, repo.MarkReverted(context.Background(), "merch-1", "store-1", "wb-1"))

	got, err := repo.Get(context.Background(), "merch-1", "store-1", "wb-1")
	require.NoError(t, err)
	assert.True(t, got.Reverted)
}

func TestMongoWriteBackRepo_GetWrongMerchant(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := catalog.NewMongoWriteBackRepository(db)
	require.NoError(t, err)

	m := &catalog.WriteBackModel{
		ID:         "wb-1",
		MerchantID: "merch-other",
		StoreID:    "store-1",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	require.NoError(t, repo.Insert(context.Background(), m))

	_, err = repo.Get(context.Background(), domain.MerchantID("merch-1"), "store-1", "wb-1")
	assert.ErrorIs(t, err, catalog.ErrWriteBackNotFound)
}

// ── AuditJobRepository ────────────────────────────────────────────────────────

func TestMongoAuditJobRepo_Insert(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := catalog.NewMongoAuditJobRepository(db)
	require.NoError(t, err)

	m := &catalog.AuditJobModel{
		ID:         "job-1",
		MerchantID: "merch-1",
		Status:     string(domain.JobQueued),
		CreatedAt:  time.Now().UTC(),
	}
	require.NoError(t, repo.Insert(context.Background(), m))
}

// seedRemediation stands in for the sync plane, the collection's only writer:
// the control plane's repository is read-only.
func seedRemediation(t *testing.T, db *mongo.Database, m *catalog.RemediationModel) {
	t.Helper()
	_, err := db.Collection("sync_catalog_remediations").InsertOne(context.Background(), m)
	require.NoError(t, err)
}
