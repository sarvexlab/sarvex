// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// Catalogue is the root of the UCP definitions catalogue.
// It is versioned, immutable, and pinned to a specific UCP spec version.
type Catalogue struct {
	Version         string                `json:"version"`      // Catalogue version (e.g., "2026-06-15")
	SpecVersion     string                `json:"spec_version"` // UCP spec version this catalogue conforms to
	SpecURL         string                `json:"spec_url"`     // PLACEHOLDER: Authority URL for the spec (pending real spec hosting at ucp.dev)
	BuiltAt         time.Time             `json:"built_at"`     // When the catalogue was generated
	Services        []wire.Service        `json:"services"`
	Capabilities    []wire.Capability     `json:"capabilities"`
	PaymentHandlers []wire.PaymentHandler `json:"payment_handlers"`
	ScoringRubric   *ScoringRubric        `json:"scoring_rubric"`

	// RequiredBusinessFacts is the Google-required set of merchant/store business
	// facts (architecture §3 Domain 1 / §7: "Capture of merchant business facts
	// Google requires downstream"). Loadable content, not hardcoded.
	RequiredBusinessFacts []RequiredBusinessFact `json:"required_business_facts"`

	// AttributeFields is the target field model for Domain 2a's map (resolve)
	// stage: the UCP/Google product attributes a normalised product's raw
	// attributes are resolved onto by the ontology's rule and learned tiers.
	AttributeFields []AttributeField `json:"attribute_fields"`
}

// BusinessFactLevel identifies the tenancy level (architecture §2a: merchant →
// store) a required business fact is captured and read at.
type BusinessFactLevel string

const (
	BusinessFactLevelMerchant BusinessFactLevel = "merchant"
	BusinessFactLevelStore    BusinessFactLevel = "store"
)

// RequiredBusinessFact declares one Google-required business fact: the tenancy
// level it is captured at and the field name it is stored/read under. This is
// loadable content (sarvex-v1-architecture.md A10.6: "spec version is data, not code") — the
// business-facts completeness predicate reads this list rather than hardcoding
// which fields are required.
type RequiredBusinessFact struct {
	ID          string            `json:"id"`          // Stable identifier for this required fact
	Level       BusinessFactLevel `json:"level"`       // merchant or store
	Field       string            `json:"field"`       // Field name as persisted/read at that level
	Description string            `json:"description"` // Why Google requires it
}

// ScoringRubric is the scoring rules and thresholds, versioned with the catalogue.
type ScoringRubric struct {
	Version      string            `json:"version"`  // Rubric version (usually matches catalogue version)
	SpecURL      string            `json:"spec_url"` // PLACEHOLDER: Authority spec URL for conformance rules (pending real spec hosting)
	Thresholds   Thresholds        `json:"thresholds"`
	Rules        []ScoringRule     `json:"rules"`
	Remediations []RemediationRule `json:"remediations"`

	// DimensionWeights are the score stage's composition weights: how the
	// four dimensions (completeness, consistency, attribute_quality,
	// delivery_data) combine into the overall 0-100 readiness score. Keyed by
	// dimension name; values are relative weights (need not sum to 100 — the
	// composition normalises by their total). Loadable content, not
	// hardcoded in the score stage.
	DimensionWeights map[string]int `json:"dimension_weights"`
}

// Thresholds define merchant-level gates for catalog readiness. The only gate is
// zero blockers: a product is agent-ready when no issue blocks checkout,
// whatever its score. (There is deliberately no score floor.)
type Thresholds struct {
	ZeroBlockers bool `json:"zero_blockers"` // Checklist: any blocker rule fails the product
}

// ScoringRule is a single rule in the scoring rubric.
type ScoringRule struct {
	ID             string `json:"id"`                       // Rule ID
	Dimension      string `json:"dimension"`                // completeness, attribute_quality, delivery_data, consistency
	Field          string `json:"field"`                    // Product field name
	Condition      string `json:"condition"`                // missing, free_text, non_standard_enum, inconsistent
	BlocksCheckout bool   `json:"blocks_checkout"`          // Does this rule block checkout?
	Weight         int    `json:"weight"`                   // 0-100 relative weight
	SpecClause     string `json:"spec_clause"`              // Spec URL/clause this rule enforces
	RemediationID  string `json:"remediation_id,omitempty"` // Linked remediation rule

	// Surface scopes the rule to one agent surface, like AttributeField.Surface:
	// empty = core (every store); otherwise the rule is evaluated, and counted
	// in its dimension's weight, only for stores enrolled on that surface.
	Surface string `json:"surface,omitempty"`
}

// RemediationRule is how to fix an issue identified by a ScoringRule.
type RemediationRule struct {
	ID            string         `json:"id"`               // Remediation ID
	ScoringRuleID string         `json:"scoring_rule_id"`  // Linked scoring rule
	Field         string         `json:"field"`            // Product field to remediate
	Action        string         `json:"action"`           // normalize, validate, default
	Value         string         `json:"value"`            // The corrected or default value
	Params        map[string]any `json:"params,omitempty"` // Action-specific parameters
	Description   string         `json:"description"`      // Brief description of the remediation
}
