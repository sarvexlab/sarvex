// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"log/slog"
)

// Scorer transforms a raw product into a mapped, scored product.
type Scorer interface {
	// Score maps a product's raw attributes and computes readiness against Google eligibility.
	// Idempotent per merchant & SKU; safe to call multiple times.
	Score(ctx context.Context, merchantID string, product *CatalogProduct) (*ScoringResult, error)
}

// ProductScorer implements Scorer using ontology rules and eligibility checks.
type ProductScorer struct {
	ontology    OntologyStore
	configStore ConfigStore
	checker     *EligibilityChecker
	log         *slog.Logger
}

// NewProductScorer creates a scorer with the given dependencies.
func NewProductScorer(ontology OntologyStore, configStore ConfigStore, log *slog.Logger) (*ProductScorer, error) {
	// Load eligibility weights from config
	ctx := context.Background()
	weights, err := configStore.GetEligibilityWeights(ctx)
	if err != nil {
		log.Warn("failed to load eligibility weights, using defaults", "error", err)
		weights = &EligibilityWeights{
			Price:              50,
			Title:              15,
			Description:        10,
			Images:             10,
			Inventory:          10,
			RequiredAttributes: 5,
		}
	}

	checker := NewEligibilityChecker(*weights, log)

	return &ProductScorer{
		ontology:    ontology,
		configStore: configStore,
		checker:     checker,
		log:         log,
	}, nil
}

// Score maps raw attributes and scores the product against Google eligibility.
func (ps *ProductScorer) Score(ctx context.Context, merchantID string, product *CatalogProduct) (*ScoringResult, error) {
	if product.MappedFields == nil {
		product.MappedFields = make(map[string]string)
	}

	// 1. Apply ontology mappings: raw attributes → standard fields
	rules, err := ps.ontology.GetRulesByPlatform(ctx, "*")
	if err != nil {
		ps.log.Warn("failed to load ontology rules, continuing without mapping", "error", err)
		// Non-fatal: score without mapping
	}

	for _, rule := range rules {
		// Check if raw attribute exists
		if rawValue, ok := product.RawAttributes[rule.SourceAttribute]; ok {
			// Apply transformation
			mapped, err := ps.ontology.ApplyRule(rule, rawValue)
			if err != nil {
				ps.log.Debug("rule transform failed, skipping", "rule_id", rule.ID, "error", err)
				continue
			}
			// Store mapped value
			product.MappedFields[rule.TargetField] = mapped
			ps.log.Debug("applied ontology rule", "source", rule.SourceAttribute, "target", rule.TargetField)
		}
	}

	// 2. Run eligibility checks and calculate score
	scoringResult := ps.checker.CheckProduct(product)

	ps.log.Info("product scored",
		"merchant_id", merchantID,
		"sku", product.SKU,
		"score", scoringResult.Score,
		"blocks_checkout", scoringResult.BlocksCheckout,
		"findings", len(scoringResult.Findings),
	)

	return scoringResult, nil
}
