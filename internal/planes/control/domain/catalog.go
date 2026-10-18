// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package domain

import "time"

// Readiness is the merchant-level Agent Readiness Score.
type Readiness struct {
	Overall      int                 `json:"overall"`
	Dimensions   ReadinessDimensions `json:"dimensions"`
	LastAuditAt  time.Time           `json:"last_audit_at"`
	ProductCount int                 `json:"product_count"`
}

// ReadinessDimensions breaks the overall score into four pillars.
type ReadinessDimensions struct {
	Completeness     int `json:"completeness"`
	Consistency      int `json:"consistency"`
	AttributeQuality int `json:"attribute_quality"`
	DeliveryData     int `json:"delivery_data"`
}

// ProductSummary is the list-view row for a product.
type ProductSummary struct {
	SKU              string `json:"sku"`
	Title            string `json:"title"`
	Score            int    `json:"score"`
	HasBlockingIssue bool   `json:"has_blocking_issue"`
	OpenIssueCount   int    `json:"open_issue_count"`
}

// ProductDetail extends ProductSummary with per-dimension scores, issues, and
// attribute mappings.
type ProductDetail struct {
	ProductSummary
	Dimensions        ReadinessDimensions `json:"dimensions"`
	Issues            []ProductIssue      `json:"issues"`
	AttributeMappings []AttributeMapping  `json:"attribute_mappings"`
}

// ProductIssue is a single actionable finding on a product.
type ProductIssue struct {
	IssueID              string       `json:"issue_id"`
	Field                string       `json:"field"`
	Problem              IssueProblem `json:"problem"`
	BlocksCheckout       bool         `json:"blocks_checkout"`
	RevenueImpactMonthly *Money       `json:"revenue_impact_monthly,omitempty"`
	Remediable           RemediableBy `json:"remediable"`
}

// IssueProblem classifies why an attribute fails.
type IssueProblem string

const (
	ProblemMissing         IssueProblem = "missing"
	ProblemFreeText        IssueProblem = "free_text"
	ProblemNonStandardEnum IssueProblem = "non_standard_enum"
	ProblemInconsistent    IssueProblem = "inconsistent"
)

// RemediableBy indicates who or what can fix the issue.
type RemediableBy string

const (
	RemediableByRule   RemediableBy = "rule"
	RemediableByManual RemediableBy = "manual"
)

// AttributeMapping records how a raw merchant attribute resolved onto a
// canonical catalog attribute.
//
// CatalogAttribute is a merchandising attribute (brand, gtin, size...), not a
// field of the UCP protocol — the spec has no brand/mpn/condition/age_group,
// and models gtin as a barcode type and color/size/material as product
// options. It was called UCPField, which claimed a correspondence that does
// not exist.
type AttributeMapping struct {
	SourceAttribute  string         `json:"source_attribute"`
	CatalogAttribute string         `json:"catalog_attribute"`
	Tier             ResolutionTier `json:"tier"`
	Confidence       float64        `json:"confidence"`
	Value            string         `json:"value"`
}

// ResolutionTier records which ontology tier resolved the mapping.
// The LLM tier is explicitly excluded from V1.
type ResolutionTier string

const (
	TierRule       ResolutionTier = "rule"
	TierLearned    ResolutionTier = "learned"
	TierUnresolved ResolutionTier = "unresolved"
)

// Remediation is an available rule-based fix for a product issue.
type Remediation struct {
	RemediationID string           `json:"remediation_id"`
	SKU           string           `json:"sku"`
	Field         string           `json:"field"`
	FromValue     string           `json:"from_value"`
	ToValue       string           `json:"to_value"`
	Type          RemediableBy     `json:"type"`
	Trust         RemediationTrust `json:"trust"`
}

// RemediationTrust indicates confidence in the proposed fix.
type RemediationTrust string

const (
	TrustHigh RemediationTrust = "high"
	TrustLow  RemediationTrust = "low"
)

// ApplyRemediationsRequest is the body for POST /remediations/apply.
type ApplyRemediationsRequest struct {
	RemediationIDs []string `json:"remediation_ids"`
}

// ApplyRemediationsResponse is returned after a batch apply.
type ApplyRemediationsResponse struct {
	Applied      int      `json:"applied"`
	WriteBackIDs []string `json:"write_back_ids"`
}

// RevertResponse is returned after reverting a write-back.
type RevertResponse struct {
	Reverted bool `json:"reverted"`
}
