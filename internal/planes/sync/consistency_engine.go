// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"fmt"
	"log/slog"
)

// ConsistencyIssue identifies a product data divergence or anomaly.
type ConsistencyIssue struct {
	Field       string // "price", "inventory", etc.
	Type        string // "divergence", "anomaly", "timeout"
	Description string
	Severity    string // "critical", "warning", "info"
}

// ConsistencyEngine reports where a product contradicts itself.
//
// "Inconsistent" means sources disagree, not that a value changed. An edit the
// merchant made — a sale price, a restock — is a legitimate change, so this
// engine does not compare a product with an earlier copy of itself (it used to,
// but callers have already written the new value by the time it runs, so it was
// comparing the product with itself; and had it worked it would have blocked
// checkout on any price change over 10%). Disagreement between sources is
// detected where both sources are in hand:
//
//   - the reconciliation sweep (sweep_handler.go): the mirror against the live
//     store, raised as a discrepancy task;
//   - the control plane's monitoring consistency engine: the live store against
//     the served cache and the feed.
//
// What the product alone can show is checked here: a tracked stock count that
// contradicts its in-stock flag.
type ConsistencyEngine struct {
	log *slog.Logger
}

// NewConsistencyEngine creates a consistency engine.
func NewConsistencyEngine(log *slog.Logger) *ConsistencyEngine {
	if log == nil {
		log = slog.Default()
	}
	return &ConsistencyEngine{log: log}
}

// CheckConsistency returns the contradictions within the product itself.
func (ce *ConsistencyEngine) CheckConsistency(_ context.Context, product *CatalogProduct) ([]ConsistencyIssue, error) {
	issues := ce.checkInventoryConsistency(product)
	if len(issues) > 0 {
		ce.log.Warn("consistency issues detected",
			"sku", product.SKU,
			"store_id", product.StoreID,
			"issue_count", len(issues),
		)
	}
	return issues, nil
}

// checkInventoryConsistency flags an in-stock flag that contradicts a tracked
// quantity (in stock with a count of 0 or less, or out of stock with a positive
// count). A nil quantity is a store that does not track stock (WooCommerce
// manage_stock off): in stock with no count is its normal state, not a
// contradiction — the same reading isMissing("inventory") takes.
func (ce *ConsistencyEngine) checkInventoryConsistency(current *CatalogProduct) []ConsistencyIssue {
	issues := make([]ConsistencyIssue, 0)

	if current.InStock && current.Quantity != nil && *current.Quantity <= 0 {
		issues = append(issues, ConsistencyIssue{
			Field:       "inventory",
			Type:        "anomaly",
			Description: "Marked in stock but quantity is 0",
			Severity:    "warning",
		})
	}
	if !current.InStock && current.Quantity != nil && *current.Quantity > 0 {
		issues = append(issues, ConsistencyIssue{
			Field:       "inventory",
			Type:        "anomaly",
			Description: fmt.Sprintf("Marked out of stock but quantity is %d", *current.Quantity),
			Severity:    "warning",
		})
	}

	return issues
}

// MarkConsistencyIssuesOnProduct adds consistency issues as findings on the product.
// This is called by the scorer to integrate consistency checks into the final score.
func (ce *ConsistencyEngine) MarkConsistencyIssuesOnProduct(product *CatalogProduct, issues []ConsistencyIssue) {
	for _, issue := range issues {
		finding := NewFinding(
			fmt.Sprintf("consistency_%s_%s", issue.Field, issue.Type),
			issue.Description,
			issue.Severity,
		).
			Field(issue.Field).
			RequiredFor("consistency_check").
			BlocksCheckout(issue.Severity == "critical").
			Build()

		product.Findings = append(product.Findings, finding)
	}
}
