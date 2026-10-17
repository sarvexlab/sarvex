// Copyright 2026 Sarvex Labs
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

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue/data"
)

func setupTestDBForSeeder(t *testing.T) *mongo.Database {
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		uri = "mongodb://admin:password@localhost:27017/?directConnection=true" //nolint:gosec // G101: local docker-compose credentials, not a secret
	}

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Skipf("skipping MongoDB tests: cannot connect to %s: %v", uri, err)
	}

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx, nil); err != nil {
		t.Skipf("skipping MongoDB tests: ping failed: %v", err)
	}

	t.Cleanup(func() {
		_ = client.Disconnect(context.Background())
	})

	db := client.Database("test_catalogue_seeder")
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
	})

	return db
}

func setupTestSeeder(t *testing.T) (*ScoringRubricSeeder, *mongo.Database) {
	db := setupTestDBForSeeder(t)
	log := slog.Default()
	seeder, err := NewScoringRubricSeeder(db, log)
	if err != nil {
		t.Skipf("skipping test: cannot create seeder: %v", err)
	}
	return seeder, db
}

func TestSeedRubric(t *testing.T) {
	seeder, db := setupTestSeeder(t)
	ctx := context.Background()

	// Seed rubric from embedded JSON
	err := seeder.SeedRubric(ctx)
	require.NoError(t, err)

	// Verify it was seeded by fetching via repo
	repo, err := data.NewScoringRubricRepo(db)
	require.NoError(t, err)

	rubric, err := repo.GetScoringRubric(ctx, "2026-06-15")
	require.NoError(t, err)
	require.NotNil(t, rubric)

	// Verify content from actual catalogue
	assert.Equal(t, "2026-06-15", rubric.Version)
	assert.True(t, rubric.Thresholds.ZeroBlockers)
	assert.Greater(t, len(rubric.Rules), 0)
	assert.Greater(t, len(rubric.Remediations), 0)
}

func TestSeedRubricIdempotent(t *testing.T) {
	seeder, db := setupTestSeeder(t)
	ctx := context.Background()

	// Seed once
	err := seeder.SeedRubric(ctx)
	require.NoError(t, err)

	// Seed again (should be idempotent - upsert on same version)
	err = seeder.SeedRubric(ctx)
	require.NoError(t, err)

	// Verify only one version exists
	repo, err := data.NewScoringRubricRepo(db)
	require.NoError(t, err)

	rubric1, err := repo.GetScoringRubric(ctx, "2026-06-15")
	require.NoError(t, err)
	require.NotNil(t, rubric1)

	// Verify it still has the same content
	assert.Equal(t, "2026-06-15", rubric1.Version)
	assert.Greater(t, len(rubric1.Rules), 0)
}

func TestSeedRubricContainsExpectedRules(t *testing.T) {
	seeder, db := setupTestSeeder(t)
	ctx := context.Background()

	// Seed from embedded JSON catalogue
	err := seeder.SeedRubric(ctx)
	require.NoError(t, err)

	// Verify it was stored correctly
	repo, err := data.NewScoringRubricRepo(db)
	require.NoError(t, err)

	rubric, err := repo.GetScoringRubric(ctx, "2026-06-15")
	require.NoError(t, err)
	require.NotNil(t, rubric)

	// Build map of rule IDs for verification
	ruleIDs := make(map[string]bool)
	for _, rule := range rubric.Rules {
		ruleIDs[rule.ID] = true
	}

	// Verify expected rules exist (from actual catalogue)
	assert.True(t, ruleIDs["rule_sku_missing"], "rule_sku_missing should exist")
	assert.True(t, ruleIDs["rule_title_missing"], "rule_title_missing should exist")
	assert.True(t, ruleIDs["rule_price_missing"], "rule_price_missing should exist")
	assert.True(t, ruleIDs["rule_images_missing"], "rule_images_missing should exist")
	assert.True(t, ruleIDs["rule_currency_missing"], "rule_currency_missing should exist")

	// Verify expected remediations exist
	remediationIDs := make(map[string]bool)
	for _, rem := range rubric.Remediations {
		remediationIDs[rem.ID] = true
	}
	assert.True(t, remediationIDs["rem_sku_required"], "rem_sku_required should exist")
	assert.True(t, remediationIDs["rem_title_required"], "rem_title_required should exist")
}
