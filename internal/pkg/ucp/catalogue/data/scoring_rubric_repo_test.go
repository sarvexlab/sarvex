// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package data

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	v1cat "github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/catalogue/v1"
)

func setupTestDB(t *testing.T) *mongo.Database {
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		uri = "mongodb://admin:password@localhost:27017/?directConnection=true" //nolint:gosec // G101: local docker-compose credentials, not a secret
	}

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Skipf("skipping MongoDB tests: cannot connect to %s: %v", uri, err)
	}

	// Verify the connection by pinging
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx, nil); err != nil {
		t.Skipf("skipping MongoDB tests: ping failed: %v", err)
	}

	t.Cleanup(func() {
		_ = client.Disconnect(context.Background())
	})

	// Clean up test database
	db := client.Database("test_catalogue_rubric")
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
	})

	return db
}

func setupTestRepo(t *testing.T) *ScoringRubricRepo {
	db := setupTestDB(t)
	repo, err := NewScoringRubricRepo(db)
	if err != nil {
		t.Skipf("skipping test: cannot create repository: %v", err)
	}
	return repo
}

func TestUpsertScoringRubric(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()

	// Create a test rubric
	rubric := &v1cat.ScoringRubric{
		Version: "2026-06-15",
		SpecURL: "https://ucp.dev/published/2026-06-15/specification/conformance/",
		Thresholds: v1cat.Thresholds{
			ZeroBlockers: true,
		},
		Rules: []v1cat.ScoringRule{
			{
				ID:             "rule_sku_missing",
				Dimension:      "completeness",
				Field:          "sku",
				Condition:      "missing",
				BlocksCheckout: true,
				Weight:         100,
				SpecClause:     "https://ucp.dev/published/2026-06-15/specification/conformance/#sku-required",
				RemediationID:  "rem_sku_required",
			},
		},
		Remediations: []v1cat.RemediationRule{
			{
				ID:            "rem_sku_required",
				ScoringRuleID: "rule_sku_missing",
				Field:         "sku",
				Action:        "error",
				Value:         "",
				Description:   "Product SKU is required; cannot auto-remediate",
			},
		},
	}

	// Upsert
	err := repo.UpsertScoringRubric(ctx, rubric)
	require.NoError(t, err)

	// Verify it was stored
	retrieved, err := repo.GetScoringRubric(ctx, "2026-06-15")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	assert.Equal(t, rubric.Version, retrieved.Version)
	assert.Equal(t, rubric.SpecURL, retrieved.SpecURL)
	assert.Len(t, retrieved.Rules, 1)
	assert.Len(t, retrieved.Remediations, 1)
}

func TestGetScoringRubricNotFound(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()

	// Fetch non-existent rubric
	retrieved, err := repo.GetScoringRubric(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, retrieved)
}

func TestGetLatestScoringRubric(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()

	// Insert two rubrics
	rubric1 := &v1cat.ScoringRubric{
		Version:      "2026-06-01",
		SpecURL:      "https://ucp.dev/published/2026-06-01/specification/conformance/",
		Rules:        []v1cat.ScoringRule{},
		Remediations: []v1cat.RemediationRule{},
	}
	rubric2 := &v1cat.ScoringRubric{
		Version:      "2026-06-15",
		SpecURL:      "https://ucp.dev/published/2026-06-15/specification/conformance/",
		Rules:        []v1cat.ScoringRule{},
		Remediations: []v1cat.RemediationRule{},
	}

	err := repo.UpsertScoringRubric(ctx, rubric1)
	require.NoError(t, err)

	err = repo.UpsertScoringRubric(ctx, rubric2)
	require.NoError(t, err)

	// Get latest
	latest, err := repo.GetLatestScoringRubric(ctx)
	require.NoError(t, err)
	require.NotNil(t, latest)

	// Should be rubric2 (inserted last)
	assert.Equal(t, "2026-06-15", latest.Version)
}

func TestUpsertScoringRubricWithMultipleRules(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()

	rubric := &v1cat.ScoringRubric{
		Version: "2026-06-15",
		SpecURL: "https://ucp.dev/published/2026-06-15/specification/conformance/",
		Thresholds: v1cat.Thresholds{
			ZeroBlockers: true,
		},
		Rules: []v1cat.ScoringRule{
			{
				ID:             "rule_sku_missing",
				Dimension:      "completeness",
				Field:          "sku",
				Condition:      "missing",
				BlocksCheckout: true,
				Weight:         100,
				SpecClause:     "https://ucp.dev/published/2026-06-15/specification/conformance/#sku-required",
				RemediationID:  "rem_sku_required",
			},
			{
				ID:             "rule_title_missing",
				Dimension:      "completeness",
				Field:          "title",
				Condition:      "missing",
				BlocksCheckout: true,
				Weight:         100,
				SpecClause:     "https://ucp.dev/published/2026-06-15/specification/conformance/#title-required",
				RemediationID:  "rem_title_required",
			},
		},
		Remediations: []v1cat.RemediationRule{
			{
				ID:            "rem_sku_required",
				ScoringRuleID: "rule_sku_missing",
				Field:         "sku",
				Action:        "error",
				Value:         "",
				Description:   "Product SKU is required",
			},
			{
				ID:            "rem_title_required",
				ScoringRuleID: "rule_title_missing",
				Field:         "title",
				Action:        "error",
				Value:         "",
				Description:   "Product title is required",
			},
		},
	}

	err := repo.UpsertScoringRubric(ctx, rubric)
	require.NoError(t, err)

	retrieved, err := repo.GetScoringRubric(ctx, "2026-06-15")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	assert.Len(t, retrieved.Rules, 2)
	assert.Len(t, retrieved.Remediations, 2)

	// Verify rules are correct
	ruleIDs := make(map[string]bool)
	for _, rule := range retrieved.Rules {
		ruleIDs[rule.ID] = true
	}
	assert.True(t, ruleIDs["rule_sku_missing"])
	assert.True(t, ruleIDs["rule_title_missing"])
}

func TestConvertRubricToDocAndBack(t *testing.T) {
	rubric := &v1cat.ScoringRubric{
		Version: "2026-06-15",
		SpecURL: "https://ucp.dev/published/2026-06-15/specification/conformance/",
		Thresholds: v1cat.Thresholds{
			ZeroBlockers: true,
		},
		Rules: []v1cat.ScoringRule{
			{
				ID:             "rule_test",
				Dimension:      "completeness",
				Field:          "sku",
				Condition:      "missing",
				BlocksCheckout: true,
				Weight:         100,
				SpecClause:     "https://example.com/sku",
				RemediationID:  "rem_test",
			},
		},
		Remediations: []v1cat.RemediationRule{
			{
				ID:            "rem_test",
				ScoringRuleID: "rule_test",
				Field:         "sku",
				Action:        "error",
				Value:         "",
				Params: map[string]interface{}{
					"param1": "value1",
				},
				Description: "Test remediation",
			},
		},
	}

	// Convert to doc
	doc := convertRubricToDoc(rubric)

	// Convert back
	converted := convertDocToRubric(doc)

	// Verify
	assert.Equal(t, rubric.Version, converted.Version)
	assert.Equal(t, rubric.SpecURL, converted.SpecURL)
	assert.Equal(t, rubric.Thresholds.ZeroBlockers, converted.Thresholds.ZeroBlockers)
	assert.Len(t, converted.Rules, 1)
	assert.Len(t, converted.Remediations, 1)

	assert.Equal(t, rubric.Rules[0].ID, converted.Rules[0].ID)
	assert.Equal(t, rubric.Rules[0].Dimension, converted.Rules[0].Dimension)
	assert.Equal(t, rubric.Remediations[0].ID, converted.Remediations[0].ID)
	assert.Equal(t, rubric.Remediations[0].Params, converted.Remediations[0].Params)
}
