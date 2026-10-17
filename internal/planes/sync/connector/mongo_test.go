// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package connector

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/platform/woocommerce"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/planes/sync/domain"
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

func TestMongoConnectorRepository_Upsert(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := NewMongoConnectorRepository(db)
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

	// Verify update. The sync plane reads by connector id, not by
	// (merchant, connector): its HMAC auth is connector-scoped, so
	// GetByConnector is the only read it has — Get is the control plane's
	// shape, which this test was a stale copy of.
	retrieved, err := repo.GetByConnector(context.Background(), "conn-1")
	require.NoError(t, err)
	assert.Equal(t, domain.ConnectorStatusHealthy, retrieved.Status)
	assert.Equal(t, "https://updated.com", retrieved.SiteURL)
}
