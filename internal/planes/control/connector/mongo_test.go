// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package connector

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/platform/woocommerce"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

func connectTestMongo(t *testing.T) *mongo.Database {
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		t.Skip("MONGO_URI not set")
	}

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)

	db := client.Database("control_test")
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})

	return db
}

func TestMongoConnectorRepository_Get(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := NewMongoConnectorRepository(db, true)
	require.NoError(t, err)

	merchantID := domain.MerchantID("merchant-123")

	// Get non-existent connector
	_, err = repo.Get(context.Background(), merchantID, "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)

	// Upsert a connector
	model := &ConnectorModel{
		ID:         "conn-1",
		MerchantID: string(merchantID),
		Platform:   woocommerce.PlatformName,
		Status:     domain.ConnectorStatusHealthy,
		SyncModel:  domain.SyncModelRealtime,
		SiteURL:    "https://example.com",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	err = repo.Upsert(context.Background(), model)
	require.NoError(t, err)

	// Get the connector
	retrieved, err := repo.Get(context.Background(), merchantID, "conn-1")
	require.NoError(t, err)
	assert.Equal(t, model.ID, retrieved.ID)
	assert.Equal(t, model.Platform, retrieved.Platform)
	assert.Equal(t, model.SiteURL, retrieved.SiteURL)

	// Tenant isolation: different merchant should not find it
	_, err = repo.Get(context.Background(), "other-merchant", "conn-1")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMongoConnectorRepository_List(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := NewMongoConnectorRepository(db, true)
	require.NoError(t, err)

	merchantID := domain.MerchantID("merchant-123")

	// Insert 3 connectors
	for i := 1; i <= 3; i++ {
		model := &ConnectorModel{
			ID:         "conn-" + string(rune('0'+i)),
			MerchantID: string(merchantID),
			Platform:   woocommerce.PlatformName,
			Status:     domain.ConnectorStatusHealthy,
			SyncModel:  domain.SyncModelRealtime,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		err := repo.Upsert(context.Background(), model)
		require.NoError(t, err)
	}

	// List first page
	models, hasMore, err := repo.List(context.Background(), merchantID, "", 2)
	require.NoError(t, err)
	assert.Len(t, models, 2)
	assert.True(t, hasMore)

	// List next page
	models, hasMore, err = repo.List(context.Background(), merchantID, models[1].ID, 2)
	require.NoError(t, err)
	assert.Len(t, models, 1)
	assert.False(t, hasMore)

	// List for different merchant returns empty
	models, hasMore, err = repo.List(context.Background(), "other-merchant", "", 10)
	require.NoError(t, err)
	assert.Len(t, models, 0)
	assert.False(t, hasMore)
}

func TestMongoConnectorRepository_Delete(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := NewMongoConnectorRepository(db, true)
	require.NoError(t, err)

	merchantID := domain.MerchantID("merchant-123")
	model := &ConnectorModel{
		ID:         "conn-1",
		MerchantID: string(merchantID),
		Platform:   woocommerce.PlatformName,
		Status:     domain.ConnectorStatusHealthy,
		SyncModel:  domain.SyncModelRealtime,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	err = repo.Upsert(context.Background(), model)
	require.NoError(t, err)

	// Delete the connector
	err = repo.Delete(context.Background(), merchantID, "conn-1")
	require.NoError(t, err)

	// Verify it's gone
	_, err = repo.Get(context.Background(), merchantID, "conn-1")
	assert.ErrorIs(t, err, ErrNotFound)

	// Delete non-existent returns ErrNotFound
	err = repo.Delete(context.Background(), merchantID, "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMongoConnectorRepository_Upsert(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := NewMongoConnectorRepository(db, true)
	require.NoError(t, err)

	merchantID := domain.MerchantID("merchant-123")
	model := &ConnectorModel{
		ID:         "conn-1",
		MerchantID: string(merchantID),
		Platform:   woocommerce.PlatformName,
		Status:     domain.ConnectorStatusPairing,
		SyncModel:  domain.SyncModelRealtime,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	// Insert
	err = repo.Upsert(context.Background(), model)
	require.NoError(t, err)

	// Update
	model.Status = domain.ConnectorStatusHealthy
	model.SiteURL = "https://updated.com"
	err = repo.Upsert(context.Background(), model)
	require.NoError(t, err)

	// Verify update
	retrieved, err := repo.Get(context.Background(), merchantID, "conn-1")
	require.NoError(t, err)
	assert.Equal(t, domain.ConnectorStatusHealthy, retrieved.Status)
	assert.Equal(t, "https://updated.com", retrieved.SiteURL)
}
