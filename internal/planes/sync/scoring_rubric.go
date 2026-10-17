// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/catalogue"
	v1cat "github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/catalogue/v1"
)

// ScoringRule represents a single rule in the scoring rubric.
type ScoringRule struct {
	ID             string `bson:"id"`
	Dimension      string `bson:"dimension"` // completeness, attribute_quality, delivery_data, consistency
	Field          string `bson:"field"`
	Condition      string `bson:"condition"` // missing, free_text, non_standard_enum, inconsistent
	BlocksCheckout bool   `bson:"blocks_checkout"`
	Weight         int    `bson:"weight"` // 0-100
	RemediationID  string `bson:"remediation_id"`

	// Surface scopes the rule to one agent surface; empty = core (every
	// store). See ScoringRubric.ForSurfaces.
	Surface string `bson:"surface,omitempty"`
}

// RemediationRule represents how to fix an issue.
type RemediationRule struct {
	ID            string         `bson:"id"`
	ScoringRuleID string         `bson:"scoring_rule_id"`
	Field         string         `bson:"field"`
	Action        string         `bson:"action"` // normalize, validate, default, error
	Value         string         `bson:"value"`  // the corrected/default value
	Params        map[string]any `bson:"params,omitempty"`
	Description   string         `bson:"description"`
}

// ScoringRubric holds the versioned scoring rubric and remediation rules.
// Loaded from the UCP definitions catalogue, not hardcoded.
type ScoringRubric struct {
	Version      string            `bson:"version"`
	LoadedAt     time.Time         `bson:"loaded_at"`
	ScoringRules []ScoringRule     `bson:"scoring_rules"`
	Remediations []RemediationRule `bson:"remediations"`
	Thresholds   CatalogThresholds `bson:"thresholds"`

	// CatalogueVersion is the UCP definitions catalogue version this rubric
	// was loaded from — every ReadinessResult is pinned to this value
	// (CLAUDE.md §2.6: "spec version is data, not code").
	CatalogueVersion string `bson:"catalogue_version"`

	// DimensionWeights are the overall-score composition weights, keyed by
	// dimension name. Catalogue content, not hardcoded in the score stage.
	DimensionWeights map[string]int `bson:"dimension_weights"`
}

// ForSurfaces returns the rubric a store on the given surfaces is scored
// against: every core rule, plus the rules scoped to a surface in the set.
// Filtering the rules (rather than skipping them at evaluation) is what keeps
// a rule that does not apply out of its dimension's total weight too, so a
// store is never marked down for — or graded on — a surface it does not use.
// The receiver is not modified.
func (r *ScoringRubric) ForSurfaces(applicable map[string]bool) *ScoringRubric {
	out := *r
	out.ScoringRules = make([]ScoringRule, 0, len(r.ScoringRules))
	for _, rule := range r.ScoringRules {
		if rule.Surface == "" || applicable[rule.Surface] {
			out.ScoringRules = append(out.ScoringRules, rule)
		}
	}
	return &out
}

// FindScoringRule returns the scoring rule with the given id, or nil.
func (r *ScoringRubric) FindScoringRule(id string) *ScoringRule {
	i := slices.IndexFunc(r.ScoringRules, func(rule ScoringRule) bool { return rule.ID == id })
	if i < 0 {
		return nil
	}
	return &r.ScoringRules[i]
}

// FindRemediation returns the remediation rule with the given id, or nil if
// none matches. Every trigger that turns a scoring rule into a
// merchant-facing message (audit, live rescore, sweep correction) needs the
// same lookup, so it lives here once rather than being re-implemented at
// each call site.
func (r *ScoringRubric) FindRemediation(id string) *RemediationRule {
	i := slices.IndexFunc(r.Remediations, func(rem RemediationRule) bool { return rem.ID == id })
	if i < 0 {
		return nil
	}
	return &r.Remediations[i]
}

// CatalogThresholds defines merchant-level gates.
type CatalogThresholds struct {
	ZeroBlockers bool `bson:"zero_blockers"` // Any blocker rule fails the product
}

// LoadScoringRubric loads the scoring rubric from the UCP definitions catalogue.
// The catalogue is the single source of truth; this function converts the catalogue
// rubric to the internal ScoringRubric format.
func LoadScoringRubric(ctx context.Context, log *slog.Logger) (*ScoringRubric, error) {
	if log == nil {
		log = slog.Default()
	}

	// Load the catalogue (embedded JSON)
	cat, err := catalogue.LoadCatalogue(ctx, log)
	if err != nil {
		return nil, fmt.Errorf("load catalogue: %w", err)
	}

	// Extract the scoring rubric from the catalogue
	catRubric := cat.ScoringRubric()
	if catRubric == nil {
		return nil, fmt.Errorf("catalogue missing scoring_rubric")
	}

	// Convert catalogue rubric to internal format
	rubric := &ScoringRubric{
		Version:      catRubric.Version,
		LoadedAt:     time.Now(),
		ScoringRules: convertCatalogueRules(catRubric.Rules),
		Remediations: convertCatalogueRemediations(catRubric.Remediations),
		Thresholds: CatalogThresholds{
			ZeroBlockers: catRubric.Thresholds.ZeroBlockers,
		},
		CatalogueVersion: cat.Version(),
		DimensionWeights: catRubric.DimensionWeights,
	}

	log.Info("loaded scoring rubric from catalogue",
		"version", rubric.Version,
		"rules", len(rubric.ScoringRules),
		"remediations", len(rubric.Remediations),
	)

	return rubric, nil
}

// convertCatalogueRules converts catalogue scoring rules to internal format.
func convertCatalogueRules(catRules []v1cat.ScoringRule) []ScoringRule {
	rules := make([]ScoringRule, len(catRules))
	for i, cr := range catRules {
		rules[i] = ScoringRule{
			ID:             cr.ID,
			Dimension:      cr.Dimension,
			Field:          cr.Field,
			Condition:      cr.Condition,
			BlocksCheckout: cr.BlocksCheckout,
			Weight:         cr.Weight,
			RemediationID:  cr.RemediationID,
			Surface:        cr.Surface,
		}
	}
	return rules
}

// convertCatalogueRemediations converts catalogue remediations to internal format.
func convertCatalogueRemediations(catRems []v1cat.RemediationRule) []RemediationRule {
	rems := make([]RemediationRule, len(catRems))
	for i, cr := range catRems {
		rems[i] = RemediationRule{
			ID:            cr.ID,
			ScoringRuleID: cr.ScoringRuleID,
			Field:         cr.Field,
			Action:        cr.Action,
			Value:         cr.Value,
			Params:        cr.Params,
			Description:   cr.Description,
		}
	}
	return rems
}
