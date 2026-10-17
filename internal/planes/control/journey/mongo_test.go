// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package journey_test

import (
	"context"
	"testing"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/journey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func setupTestMongoDB(t *testing.T) *mongo.Database {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://admin:password@localhost:27017/?directConnection=true"))
	require.NoError(t, err)

	// Use a test database that gets cleaned up.
	db := client.Database("sarvex_journey_test")

	// Clean up before test.
	_ = db.Drop(ctx)

	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})

	return db
}

func TestMongoJourneyRepository_Get_NotFound(t *testing.T) {
	t.Skip("requires MongoDB running locally")

	db := setupTestMongoDB(t)
	repo, err := journey.NewMongoJourneyRepository(db)
	require.NoError(t, err)
	ctx := context.Background()

	_, err = repo.Get(ctx, "merchant1", "nonexistent")
	assert.ErrorIs(t, err, journey.ErrNotFound)
}

func TestMongoJourneyRepository_Upsert_Get(t *testing.T) {
	t.Skip("requires MongoDB running locally")

	db := setupTestMongoDB(t)
	repo, err := journey.NewMongoJourneyRepository(db)
	require.NoError(t, err)
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	// Upsert a new document.
	now := time.Now().UTC()
	m := &journey.JourneyModel{
		ID:             string(id),
		MerchantID:     string(id),
		State:          "connected",
		EnteredStateAt: now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	err = repo.Upsert(ctx, m)
	require.NoError(t, err)

	// Retrieve it.
	retrieved, err := repo.Get(ctx, id, domain.StoreID(id))
	require.NoError(t, err)

	assert.Equal(t, m.ID, retrieved.ID)
	assert.Equal(t, m.State, retrieved.State)
	assert.Equal(t, m.EnteredStateAt.Unix(), retrieved.EnteredStateAt.Unix())
}

func TestMongoJourneyRepository_Upsert_Idempotent(t *testing.T) {
	t.Skip("requires MongoDB running locally")

	db := setupTestMongoDB(t)
	repo, err := journey.NewMongoJourneyRepository(db)
	require.NoError(t, err)
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	now := time.Now().UTC()
	m1 := &journey.JourneyModel{
		ID:             string(id),
		MerchantID:     string(id),
		State:          "connected",
		EnteredStateAt: now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	err = repo.Upsert(ctx, m1)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	m2 := &journey.JourneyModel{
		ID:             string(id),
		MerchantID:     string(id),
		State:          "synced",
		EnteredStateAt: time.Now().UTC(),
		CreatedAt:      now,
		UpdatedAt:      time.Now().UTC(),
	}

	err = repo.Upsert(ctx, m2)
	require.NoError(t, err)

	// Retrieve and verify it was updated.
	retrieved, err := repo.Get(ctx, id, domain.StoreID(id))
	require.NoError(t, err)
	assert.Equal(t, "synced", retrieved.State)
}
