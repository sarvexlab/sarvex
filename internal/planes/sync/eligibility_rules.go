// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"log/slog"
)

// EligibilityWeights holds configurable scoring weights for Google UCP eligibility.
type EligibilityWeights struct {
	Price              int `bson:"price_weight"`
	Title              int `bson:"title_weight"`
	Description        int `bson:"description_weight"`
	Images             int `bson:"images_weight"`
	Inventory          int `bson:"inventory_weight"`
	RequiredAttributes int `bson:"required_attributes_weight"`
}

// DefaultEligibilityWeights returns the V1 MVP scoring rubric.
func DefaultEligibilityWeights() EligibilityWeights {
	return EligibilityWeights{
		Price:              50,
		Title:              15,
		Description:        10,
		Images:             10,
		Inventory:          10,
		RequiredAttributes: 5,
	}
}

// ConfigStore queries global configuration from MongoDB.
type ConfigStore interface {
	// GetEligibilityWeights returns the current scoring weights.
	GetEligibilityWeights(ctx context.Context) (*EligibilityWeights, error)
}

// EligibilityChecker evaluates products against Google UCP eligibility requirements.
type EligibilityChecker struct {
	weights EligibilityWeights
	log     *slog.Logger
}

// NewEligibilityChecker creates a checker with the given weights.
func NewEligibilityChecker(weights EligibilityWeights, log *slog.Logger) *EligibilityChecker {
	return &EligibilityChecker{
		weights: weights,
		log:     log,
	}
}

// ScoringResult holds the output of eligibility checking.
type ScoringResult struct {
	Score          int
	BlocksCheckout bool
	Findings       []*Finding
}

// CheckProduct evaluates a product against eligibility rules and returns findings and score.
func (ec *EligibilityChecker) CheckProduct(product *CatalogProduct) *ScoringResult {
	result := &ScoringResult{
		Score:    100,
		Findings: make([]*Finding, 0),
	}

	// 1. Check price (critical)
	if product.PriceMinor <= 0 {
		result.Score -= ec.weights.Price
		result.BlocksCheckout = true
		finding := NewFinding(
			"missing_or_invalid_price",
			"Product has no price or price is zero",
			"critical",
		).
			Field("price").
			RequiredFor("ucp_checkout").
			ActionableBy("merchant_action").
			BlocksCheckout(true).
			Details(map[string]interface{}{
				"current_price": product.PriceMinor,
				"currency":      product.Currency,
			}).
			Build()
		result.Findings = append(result.Findings, finding)
	}

	// 2. Check title
	if product.Title == "" {
		result.Score -= ec.weights.Title
		finding := NewFinding(
			"missing_title",
			"Product has no title",
			"warning",
		).
			Field("title").
			RequiredFor("ucp_checkout").
			ActionableBy("merchant_action").
			Build()
		result.Findings = append(result.Findings, finding)
	}

	// 3. Check description (CatalogProduct.Description exists; not yet scored)
	// Deferred to a future scoring pass.

	// 4. Check images (CatalogProduct.Images exists; not yet scored)
	// Defer to Phase 2+ when images are tracked

	// 5. Check inventory consistency
	if !product.InStock && product.Quantity != nil && *product.Quantity > 0 {
		// Inconsistency: marked out of stock but quantity > 0
		result.Score -= ec.weights.Inventory
		finding := NewFinding(
			"inventory_inconsistency",
			"Product marked out of stock but has positive quantity",
			"warning",
		).
			Field("inventory").
			RequiredFor("ucp_checkout").
			ActionableBy("merchant_action").
			Details(map[string]interface{}{
				"in_stock": product.InStock,
				"quantity": *product.Quantity,
			}).
			Build()
		result.Findings = append(result.Findings, finding)
	}

	// 6. Check required mapped attributes (color is considered required for apparel)
	// For MVP, just check if color was successfully mapped
	if _, hasColor := product.MappedFields["color"]; !hasColor {
		// Not necessarily critical, but penalize
		perAttrPenalty := ec.weights.RequiredAttributes
		if result.Score-perAttrPenalty >= 0 {
			result.Score -= perAttrPenalty
		}
		finding := NewFinding(
			"missing_mapped_attribute",
			"Required product attribute (color) is missing or unmapped",
			"info",
		).
			Field("color").
			RequiredFor("ucp_checkout").
			ActionableBy("merchant_action").
			Build()
		result.Findings = append(result.Findings, finding)
	}

	// Clamp score to [0, 100]
	if result.Score < 0 {
		result.Score = 0
	}
	if result.Score > 100 {
		result.Score = 100
	}

	return result
}
