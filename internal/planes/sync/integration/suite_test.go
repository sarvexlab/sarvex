// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

// Package integration contains integration tests for the sync plane event processing pipeline.
//
// Tests verify catalog storage and retrieval with real MongoDB, using the
// sync.CatalogProduct type to represent normalized catalog records.
//
// Run with:
//
//	MONGO_URI="mongodb://admin:password@localhost:27017/?directConnection=true" \
//	  go test -tags=integration ./internal/planes/sync/integration/ -v
package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/platform/woocommerce"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongoopts "go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wire"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/sync"
)

// testEnv holds shared test environment state.
type testEnv struct {
	mongoClient *mongo.Client
	db          *mongo.Database
	catalogCol  *mongo.Collection
	redisClient *redis.Client
	redisPrefix string
}

// setup initializes MongoDB (and, for tests that need it, Redis) for tests.
func setup(t *testing.T) *testEnv {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://admin:password@localhost:27017/?directConnection=true"
	}

	// Connect to MongoDB
	mongoClient, err := mongo.Connect(mongoopts.Client().ApplyURI(mongoURI))
	if err != nil {
		t.Fatalf("failed to connect to MongoDB: %v", err)
	}

	// Verify connection
	if err := mongoClient.Ping(ctx, nil); err != nil {
		t.Fatalf("failed to ping MongoDB: %v", err)
	}

	// Get database with random suffix to avoid conflicts
	dbName := fmt.Sprintf("sarvex_sync_int_%d", time.Now().UnixNano())
	db := mongoClient.Database(dbName)

	// Connect to Redis, matching the REDIS_ADDR convention used by the
	// control/serving integration suites. Only tests that need a real
	// distributed Lease (the reconciliation sweeper) use this; other tests
	// in this package ignore it.
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Fatalf("failed to ping Redis: %v", err)
	}

	return &testEnv{
		mongoClient: mongoClient,
		db:          db,
		catalogCol:  db.Collection("sync_catalog_products"),
		redisClient: redisClient,
		redisPrefix: fmt.Sprintf("sync_int_%d:", time.Now().UnixNano()),
	}
}

// cleanup tears down the test environment.
func (e *testEnv) cleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := e.db.Drop(ctx); err != nil {
		t.Logf("warning: failed to drop test database: %v", err)
	}

	if err := e.mongoClient.Disconnect(ctx); err != nil {
		t.Logf("warning: failed to disconnect: %v", err)
	}

	if e.redisClient != nil {
		if err := e.redisClient.Close(); err != nil {
			t.Logf("warning: failed to close redis client: %v", err)
		}
	}
}

// newTestProduct helper creates a CatalogProduct for testing.
func newTestProduct(storeID, merchantID, sku string) *sync.CatalogProduct {
	return &sync.CatalogProduct{
		ID:            fmt.Sprintf("%s:%s:%s", merchantID, storeID, sku),
		StoreID:       storeID,
		MerchantID:    merchantID,
		SKU:           sku,
		Title:         "Test Product",
		PriceMinor:    2999,
		Currency:      "USD",
		InStock:       true,
		Quantity:      intPtr(100),
		RawAttributes: map[string]string{},
		MappedFields:  map[string]string{},
		UpdatedAt:     time.Now().UTC(),
	}
}

// intPtr helper returns a pointer to an int.
func intPtr(i int) *int {
	return &i
}

// stubStoreProvider is a minimal ucp.StoreProvider for tests that only need
// registry.Get(...) to resolve to a given (platform, storeID) key.
type stubStoreProvider struct {
	storeID    string
	merchantID string
}

func (s *stubStoreProvider) StoreID() string                                   { return s.storeID }
func (s *stubStoreProvider) Version() string                                   { return "2024-01-01" }
func (s *stubStoreProvider) Services() map[string][]wire.Service               { return nil }
func (s *stubStoreProvider) Capabilities() map[string][]wire.Capability        { return nil }
func (s *stubStoreProvider) PaymentHandlers() map[string][]wire.PaymentHandler { return nil }
func (s *stubStoreProvider) SigningKeys() []wire.JWK                           { return nil }
func (s *stubStoreProvider) StorefrontURL() string                             { return "" }
func (s *stubStoreProvider) Configuration() ucp.Configuration {
	return ucp.Configuration{
		MerchantID: s.merchantID,
		StoreID:    s.storeID,
		Platform:   woocommerce.PlatformName,
	}
}

// newTestRegistry builds a *base.Registry with connector pre-injected (via
// Registry.Set, bypassing factories) for (storeID, merchantID) plus the
// ucp.StoreProviderByStoreIDFunc that resolves it, for driving
// sync.NewIngestionPipeline/sync.NewSweepHandler directly in these tests.
func newTestRegistry(storeID, merchantID string, connector base.Connector) (*base.Registry, ucp.StoreProviderByStoreIDFunc) {
	noCreds := base.CredentialsFunc(func(_ context.Context, _ string) (map[string]any, error) {
		return nil, errors.New("no credentials provider configured for this test")
	})
	reg := base.New(noCreds, nil)
	reg.Set(woocommerce.PlatformName, storeID, connector)

	provider := func(mID, sid string) (ucp.StoreProvider, error) {
		return &stubStoreProvider{storeID: sid, merchantID: merchantID}, nil
	}
	return reg, provider
}
