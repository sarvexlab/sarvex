// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package catalogue

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestLoadCatalogue(t *testing.T) {
	log := slog.Default()
	cat, err := LoadCatalogue(context.Background(), log)
	require.NoError(t, err)
	require.NotNil(t, cat)

	// Catalogue version comes from backend registry
	assert.NotEmpty(t, cat.Version())
	assert.NotEmpty(t, cat.SpecVersion())
	assert.Equal(t, cat.Version(), cat.SpecVersion())
}

func TestCatalogueServices(t *testing.T) {
	log := slog.Default()
	cat, err := LoadCatalogue(context.Background(), log)
	require.NoError(t, err)

	services := cat.Services()
	assert.Greater(t, len(services), 0)

	// Verify each service has required fields
	for _, svc := range services {
		assert.NotEmpty(t, svc.ID)
		assert.NotEmpty(t, svc.Version)
	}
}

func TestCatalogueCapabilities(t *testing.T) {
	log := slog.Default()
	cat, err := LoadCatalogue(context.Background(), log)
	require.NoError(t, err)

	allCaps := cat.AllCapabilities()
	// Capabilities come from backend registry, verify structure
	for _, cap := range allCaps {
		assert.NotEmpty(t, cap.ID)
	}
}

func TestPaymentHandlers(t *testing.T) {
	log := slog.Default()
	cat, err := LoadCatalogue(context.Background(), log)
	require.NoError(t, err)

	handlers := cat.PaymentHandlers()
	// Payment handlers come from backend registry
	for _, h := range handlers {
		assert.NotEmpty(t, h.ID)
		assert.NotEmpty(t, h.Version)
	}
}

func TestScoringRubric(t *testing.T) {
	log := slog.Default()
	cat, err := LoadCatalogue(context.Background(), log)
	require.NoError(t, err)

	rubric := cat.ScoringRubric()
	assert.NotNil(t, rubric)
	assert.Equal(t, "2026-06-15", rubric.Version)
	assert.True(t, rubric.Thresholds.ZeroBlockers)

	// Check scoring rules
	assert.Greater(t, len(rubric.Rules), 10)

	// Check blocking rules
	blockingRules := 0
	for _, rule := range rubric.Rules {
		if rule.BlocksCheckout {
			blockingRules++
		}
	}
	assert.Greater(t, blockingRules, 0)

	// Check remediations
	assert.Greater(t, len(rubric.Remediations), 0)
}

func TestScoringRubricDimensions(t *testing.T) {
	log := slog.Default()
	cat, err := LoadCatalogue(context.Background(), log)
	require.NoError(t, err)

	rubric := cat.ScoringRubric()

	// Count rules by dimension
	dimensions := make(map[string]int)
	for _, rule := range rubric.Rules {
		dimensions[rule.Dimension]++
	}

	// Verify all four dimensions are present
	assert.Greater(t, dimensions["completeness"], 0)
	assert.Greater(t, dimensions["attribute_quality"], 0)
	assert.Greater(t, dimensions["delivery_data"], 0)
	assert.Greater(t, dimensions["consistency"], 0)
}

func TestConcurrentAccess(t *testing.T) {
	log := slog.Default()
	cat, err := LoadCatalogue(context.Background(), log)
	require.NoError(t, err)

	// Simulate concurrent reads
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			_ = cat.Version()
			_ = cat.CapabilitiesForV1()
			_ = cat.PaymentHandlers()
			_ = cat.ScoringRubric()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func setupTestDBForLoader(t *testing.T) *mongo.Database {
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		uri = "mongodb://admin:password@localhost:27017/?directConnection=true" //nolint:gosec // G101: local docker-compose credentials, not a secret
	}

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Skipf("skipping MongoDB tests: cannot connect to %s: %v", uri, err)
	}

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Microsecond)
	defer cancel()
	if err := client.Ping(ctx, nil); err != nil {
		t.Skipf("skipping MongoDB tests: ping failed: %v", err)
	}

	t.Cleanup(func() {
		_ = client.Disconnect(context.Background())
	})

	db := client.Database("test_catalogue_loader")
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
	})

	return db
}

func TestLoadCatalogueWithDBFallback(t *testing.T) {
	// Test that loader falls back to JSON when MongoDB is empty
	db := setupTestDBForLoader(t)
	log := slog.Default()

	ctx := context.Background()
	cat, err := LoadCatalogueWithDB(ctx, db, log)
	require.NoError(t, err)
	require.NotNil(t, cat)

	// Should fallback to embedded JSON (MongoDB is empty)
	rubric := cat.ScoringRubric()
	require.NotNil(t, rubric)
	assert.Equal(t, "2026-06-15", rubric.Version)
	assert.Greater(t, len(rubric.Rules), 0)
}

func TestLoadCatalogueWithDBFetchesFromMongo(t *testing.T) {
	// Test that loader fetches from MongoDB when data is available
	db := setupTestDBForLoader(t)
	log := slog.Default()

	ctx := context.Background()

	// Seed the rubric from embedded JSON into MongoDB
	seeder, err := NewScoringRubricSeeder(db, log)
	require.NoError(t, err)

	err = seeder.SeedRubric(ctx)
	require.NoError(t, err)

	// Now load catalogue with DB
	catWithDB, err := LoadCatalogueWithDB(ctx, db, log)
	require.NoError(t, err)
	require.NotNil(t, catWithDB)

	// Should fetch from MongoDB
	rubric := catWithDB.ScoringRubric()
	require.NotNil(t, rubric)
	assert.Equal(t, "2026-06-15", rubric.Version)
	assert.Greater(t, len(rubric.Rules), 0)
}

func TestLoaderStillHasOtherFields(t *testing.T) {
	// Verify that other catalogue fields are still loaded from JSON
	db := setupTestDBForLoader(t)
	log := slog.Default()

	ctx := context.Background()
	cat, err := LoadCatalogueWithDB(ctx, db, log)
	require.NoError(t, err)

	// Verify other fields are still available from embedded JSON
	assert.Equal(t, "2026-06-15", cat.Version())
	assert.Equal(t, 1, len(cat.Services()))
	assert.Equal(t, 5, len(cat.AllCapabilities()))
	assert.Equal(t, 6, len(cat.PaymentHandlers()))
}

// TestCapabilitiesForV1_OnlyFunctional pins the advertised surface: the
// profile must not promise capabilities or extensions with no working
// implementation behind them.
func TestCapabilitiesForV1_OnlyFunctional(t *testing.T) {
	cat, err := LoadCatalogue(context.Background(), slog.Default())
	require.NoError(t, err)

	var ids []string
	for _, c := range cat.CapabilitiesForV1() {
		ids = append(ids, c.ID)
	}
	assert.ElementsMatch(t, []string{
		"dev.ucp.shopping.checkout",
		"dev.ucp.shopping.order",
		"dev.ucp.shopping.catalog",
		"dev.ucp.shopping.cart",
		"dev.ucp.shopping.fulfillment",
		"dev.ucp.shopping.discount",
		"dev.ucp.shopping.buyer_consent",
	}, ids)
}
