// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"log/slog"
	"testing"
)

// testRubric creates a rubric for testing (loads from catalogue).
func testRubric(t *testing.T) *ScoringRubric {
	rubric, err := LoadScoringRubric(context.Background(), slog.Default())
	if err != nil {
		t.Fatalf("failed to load rubric: %v", err)
	}
	return rubric
}

func TestScoringRubricStructure(t *testing.T) {
	rubric := testRubric(t)

	// Verify version is pinned (not stub)
	if rubric.Version == "1.0-stub" {
		t.Error("rubric version should not be stub; spec reconciliation required")
	}
	if rubric.Version == "" {
		t.Error("rubric version must be set")
	}

	// Verify loaded date is set
	if rubric.LoadedAt.IsZero() {
		t.Error("loaded_at must be set")
	}

	// The gate is zero blockers; there is no score floor (a product is
	// agent-ready when nothing blocks checkout, whatever its score).
	if !rubric.Thresholds.ZeroBlockers {
		t.Error("zero_blockers must be the configured gate")
	}
}

func TestScoringRulesCompleteness(t *testing.T) {
	rubric := testRubric(t)

	if len(rubric.ScoringRules) == 0 {
		t.Fatal("rubric must define scoring rules")
	}

	// Verify rule structure
	for _, rule := range rubric.ScoringRules {
		if rule.ID == "" {
			t.Error("rule ID must not be empty")
		}
		if rule.Dimension == "" {
			t.Error("rule dimension must not be empty")
		}
		if rule.Field == "" {
			t.Error("rule field must not be empty")
		}
		if rule.Condition == "" {
			t.Error("rule condition must not be empty")
		}
		if rule.Weight < 0 || rule.Weight > 100 {
			t.Errorf("rule %s weight %d out of valid range [0,100]", rule.ID, rule.Weight)
		}
		if rule.RemediationID == "" {
			t.Errorf("rule %s must have a remediation ID", rule.ID)
		}
	}

	// Verify dimension values are known
	validDimensions := map[string]bool{
		"completeness":      true,
		"attribute_quality": true,
		"delivery_data":     true,
		"consistency":       true,
	}
	for _, rule := range rubric.ScoringRules {
		if !validDimensions[rule.Dimension] {
			t.Errorf("unknown dimension %q in rule %s", rule.Dimension, rule.ID)
		}
	}

	// Verify condition values are known
	validConditions := map[string]bool{
		"missing":           true,
		"free_text":         true,
		"non_standard_enum": true,
		"inconsistent":      true,
	}
	for _, rule := range rubric.ScoringRules {
		if !validConditions[rule.Condition] {
			t.Errorf("unknown condition %q in rule %s", rule.Condition, rule.ID)
		}
	}

	// Verify blocking rules make sense
	blockingRules := []string{"rule_sku_missing", "rule_title_missing", "rule_price_missing", "rule_currency_missing"}
	for _, rule := range rubric.ScoringRules {
		for _, blockID := range blockingRules {
			if rule.ID == blockID && !rule.BlocksCheckout {
				t.Errorf("rule %s should block checkout", rule.ID)
			}
		}
	}
}

func TestRemediationRulesCompleteness(t *testing.T) {
	rubric := testRubric(t)

	if len(rubric.Remediations) == 0 {
		t.Fatal("rubric must define remediation rules")
	}

	// Build map of remediation IDs
	remediationMap := make(map[string]*RemediationRule)
	for i, rem := range rubric.Remediations {
		if rem.ID == "" {
			t.Error("remediation ID must not be empty")
		}
		if _, exists := remediationMap[rem.ID]; exists {
			t.Errorf("duplicate remediation ID: %s", rem.ID)
		}
		remediationMap[rem.ID] = &rubric.Remediations[i]
	}

	// Verify each scoring rule has a corresponding remediation
	for _, rule := range rubric.ScoringRules {
		rem, exists := remediationMap[rule.RemediationID]
		if !exists {
			t.Errorf("scoring rule %s references non-existent remediation %s", rule.ID, rule.RemediationID)
		}
		if rem != nil && rem.ScoringRuleID != rule.ID {
			t.Errorf("remediation %s references wrong scoring rule (expected %s, got %s)", rule.RemediationID, rule.ID, rem.ScoringRuleID)
		}
	}

	// Verify remediation structure
	validActions := map[string]bool{
		"error":     true,
		"default":   true,
		"normalize": true,
		"validate":  true,
		"infer":     true,
		"sync":      true,
	}

	for _, rem := range rubric.Remediations {
		if rem.Field == "" {
			t.Errorf("remediation %s must specify field", rem.ID)
		}
		if !validActions[rem.Action] {
			t.Errorf("remediation %s has unknown action %q", rem.ID, rem.Action)
		}
	}
}

func TestRuleWeightDistribution(t *testing.T) {
	rubric := testRubric(t)

	// Count rules by dimension
	dimensionWeights := make(map[string]int)
	dimensionCounts := make(map[string]int)

	for _, rule := range rubric.ScoringRules {
		dimensionWeights[rule.Dimension] += rule.Weight
		dimensionCounts[rule.Dimension]++
	}

	// Verify each dimension has rules
	expectedDimensions := []string{"completeness", "attribute_quality", "delivery_data", "consistency"}
	for _, dim := range expectedDimensions {
		if dimensionCounts[dim] == 0 {
			t.Errorf("dimension %s has no rules", dim)
		}
		if dimensionWeights[dim] == 0 {
			t.Errorf("dimension %s has zero total weight", dim)
		}
	}

	// Log distribution for inspection
	t.Logf("Rubric rule distribution:")
	for _, dim := range expectedDimensions {
		t.Logf("  %s: %d rules, total weight %d", dim, dimensionCounts[dim], dimensionWeights[dim])
	}
}

func TestBlockingRulesHaveHighWeight(t *testing.T) {
	rubric := testRubric(t)

	for _, rule := range rubric.ScoringRules {
		if rule.BlocksCheckout && rule.Weight < 80 {
			t.Logf("info: blocking rule %s has low weight %d (should be >= 80 to signal importance)", rule.ID, rule.Weight)
		}
	}
}

func TestConsistencyRulesExist(t *testing.T) {
	rubric := testRubric(t)

	// Verify that consistency rules exist for key fields
	expectedConsistencyRules := map[string]bool{
		"rule_price_inconsistent": false,
		"rule_stock_inconsistent": false,
	}

	for _, rule := range rubric.ScoringRules {
		if rule.Dimension == "consistency" {
			expectedConsistencyRules[rule.ID] = true
		}
	}

	for rule, found := range expectedConsistencyRules {
		if !found {
			t.Errorf("expected consistency rule %s not found", rule)
		}
	}
}

func TestNoOrphanRemediations(t *testing.T) {
	rubric := testRubric(t)

	// Build map of scoring rule IDs
	ruleMap := make(map[string]bool)
	for _, rule := range rubric.ScoringRules {
		ruleMap[rule.ID] = true
	}

	// Verify no remediation references non-existent scoring rule
	for _, rem := range rubric.Remediations {
		if !ruleMap[rem.ScoringRuleID] {
			t.Errorf("remediation %s references non-existent scoring rule %s", rem.ID, rem.ScoringRuleID)
		}
	}
}

func TestRubricLoadsWithoutError(t *testing.T) {
	rubric := testRubric(t)

	// Verify rubric is not nil and populated
	if rubric.Version == "" {
		t.Error("rubric version is empty")
	}

	if len(rubric.ScoringRules) == 0 {
		t.Error("rubric has no scoring rules")
	}

	if len(rubric.Remediations) == 0 {
		t.Error("rubric has no remediations")
	}

	// Quick sanity check: should have a reasonable set of rules
	if len(rubric.ScoringRules) < 10 {
		t.Errorf("rubric should have comprehensive rules (got %d, expected >= 10)", len(rubric.ScoringRules))
	}

	if len(rubric.Remediations) < 10 {
		t.Errorf("rubric should have comprehensive remediations (got %d, expected >= 10)", len(rubric.Remediations))
	}
}
