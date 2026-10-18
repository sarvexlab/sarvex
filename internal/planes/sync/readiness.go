// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"fmt"
	"sort"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
)

// ReadinessDimensions breaks a readiness result into the four rubric
// pillars. Field-for-field mirror of CatalogProduct's own
// Completeness/Consistency/AttributeQuality/DeliveryData fields (and, in
// turn, of control/catalog.ProductModel's).
type ReadinessDimensions struct {
	Completeness     int
	Consistency      int
	AttributeQuality int
	DeliveryData     int
}

// ReadinessResult is Domain 2a stage 3 (score)'s output: never a bare
// number. Overall and each dimension are 0-100, composed from the rubric's
// per-rule weights (dimension scores) and per-dimension weights (overall);
// Issues has one entry per rule the product failed.
type ReadinessResult struct {
	Overall          int
	Dimensions       ReadinessDimensions
	Issues           []ProductIssue
	CatalogueVersion string
}

// EvaluateReadiness runs Domain 2a stage 3 (score): evaluates a mapped
// product against every rule in rubric, folds in the consistency findings
// input for "inconsistent" rules (the consistency dimension is fed by
// findings, never computed from the product alone — detecting consistency
// is the consistency engine's job, an upstream stage this one only
// consumes), and composes the four-dimension ReadinessResult.
//
// Deterministic: identical product + rubric + findings always produce an
// identical result — no clock reads, no randomness, no external calls.
func EvaluateReadiness(product *CatalogProduct, rubric *ScoringRubric, findings []ConsistencyIssue) *ReadinessResult {
	inconsistentFields := make(map[string]bool, len(findings))
	for _, f := range findings {
		inconsistentFields[f.Field] = true
	}

	triggeredWeight := make(map[string]int, 4)
	totalWeight := make(map[string]int, 4)
	var issues []ProductIssue

	for _, rule := range rubric.ScoringRules {
		totalWeight[rule.Dimension] += rule.Weight
		if !ruleFails(product, rule, inconsistentFields) {
			continue
		}
		triggeredWeight[rule.Dimension] += rule.Weight
		issues = append(issues, newProductIssue(rule, rubric))
	}

	dims := ReadinessDimensions{
		Completeness:     dimensionScore(triggeredWeight["completeness"], totalWeight["completeness"]),
		Consistency:      dimensionScore(triggeredWeight["consistency"], totalWeight["consistency"]),
		AttributeQuality: dimensionScore(triggeredWeight["attribute_quality"], totalWeight["attribute_quality"]),
		DeliveryData:     dimensionScore(triggeredWeight["delivery_data"], totalWeight["delivery_data"]),
	}

	sortIssues(issues, rubric)

	return &ReadinessResult{
		Overall:          composeOverall(dims, rubric),
		Dimensions:       dims,
		Issues:           issues,
		CatalogueVersion: rubric.CatalogueVersion,
	}
}

// ruleFails evaluates a single rule's condition against product. The
// "inconsistent" condition is answered from the consistency findings input
// (inconsistentFields), never from the product's own fields — consistency
// detection belongs to the consistency engine (an upstream stage), not the
// score stage. Every other condition reuses the same product-field checks
// ScoreProduct (scoring.go) evaluates.
func ruleFails(product *CatalogProduct, rule ScoringRule, inconsistentFields map[string]bool) bool {
	if rule.Condition == "inconsistent" {
		return inconsistentFields[rule.Field]
	}
	return checkRuleCondition(product, rule)
}

// dimensionScore converts a dimension's triggered rule weight into a 0-100
// score: the percentage of the dimension's total rule weight that did NOT
// trigger. A dimension with no rules scores 100 (nothing to fail).
func dimensionScore(triggeredWeight, totalWeight int) int {
	if totalWeight <= 0 {
		return 100
	}
	score := 100 - (triggeredWeight*100)/totalWeight
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}

// composeOverall combines the four dimensions into a single 0-100 score
// using the rubric's DimensionWeights. Falls back to an equal-weight split
// if the rubric doesn't define composition weights.
func composeOverall(dims ReadinessDimensions, rubric *ScoringRubric) int {
	weights := rubric.DimensionWeights
	if len(weights) == 0 {
		weights = map[string]int{
			"completeness":      25,
			"consistency":       25,
			"attribute_quality": 25,
			"delivery_data":     25,
		}
	}

	scores := map[string]int{
		"completeness":      dims.Completeness,
		"consistency":       dims.Consistency,
		"attribute_quality": dims.AttributeQuality,
		"delivery_data":     dims.DeliveryData,
	}

	var weightedSum, totalWeight int
	for dim, w := range weights {
		weightedSum += scores[dim] * w
		totalWeight += w
	}
	if totalWeight == 0 {
		return 0
	}
	// Round to nearest rather than truncate.
	return (weightedSum + totalWeight/2) / totalWeight
}

// newProductIssue converts a triggered ScoringRule into the issue shape the
// catalog store's readiness slot persists: field, problem, dimension,
// why_it_matters, blocks_checkout, remediable, revenue_impact_monthly.
// RevenueImpactMinor/Currency are left nil — revenue impact requires live
// traffic data, which V1 does not have yet.
func newProductIssue(rule ScoringRule, rubric *ScoringRubric) ProductIssue {
	remRule := rubric.FindRemediation(rule.RemediationID)

	remediable := "manual"
	whyItMatters := fmt.Sprintf("Required by the UCP conformance rubric (%s)", rule.Dimension)
	if remRule != nil {
		if remediationApplicable(remRule) {
			remediable = "rule"
		}
		if remRule.Description != "" {
			whyItMatters = remRule.Description
		}
	}

	return ProductIssue{
		IssueID:        rule.ID,
		Field:          rule.Field,
		Problem:        rule.Condition,
		Dimension:      rule.Dimension,
		WhyItMatters:   whyItMatters,
		BlocksCheckout: rule.BlocksCheckout,
		Remediable:     remediable,
	}
}

// sortIssues orders issues by priority: revenue impact first (when known),
// falling back to blocks_checkout, falling back to the triggering rule's
// eligibility weight — the fallback chain the score stage uses until
// traffic data makes revenue impact computable.
func sortIssues(issues []ProductIssue, rubric *ScoringRubric) {
	weightByRule := make(map[string]int, len(rubric.ScoringRules))
	for _, r := range rubric.ScoringRules {
		weightByRule[r.ID] = r.Weight
	}

	sort.SliceStable(issues, func(i, j int) bool {
		a, b := issues[i], issues[j]

		if a.RevenueImpactMinor != nil || b.RevenueImpactMinor != nil {
			var ra, rb int64
			if a.RevenueImpactMinor != nil {
				ra = *a.RevenueImpactMinor
			}
			if b.RevenueImpactMinor != nil {
				rb = *b.RevenueImpactMinor
			}
			if ra != rb {
				return ra > rb
			}
		}

		if a.BlocksCheckout != b.BlocksCheckout {
			return a.BlocksCheckout
		}

		return weightByRule[a.IssueID] > weightByRule[b.IssueID]
	})
}

// anyBlocksCheckout reports whether any issue blocks checkout — the
// product-level BlocksCheckout gate, per the rubric's zero_blockers
// threshold (CatalogThresholds.ZeroBlockers).
func anyBlocksCheckout(issues []ProductIssue) bool {
	for _, iss := range issues {
		if iss.BlocksCheckout {
			return true
		}
	}
	return false
}

// applyReadiness writes a ReadinessResult onto the product's readiness slot
// (the fields Domain 2a stage 3 owns: score, has_blocking_issue, the four
// dimensions, issues, catalogue_version). This is the only place those
// fields are set — everywhere else treats them as read-only.
func applyReadiness(product *CatalogProduct, result *ReadinessResult) {
	product.ReadinessScore = result.Overall
	product.BlocksCheckout = anyBlocksCheckout(result.Issues)
	product.Completeness = result.Dimensions.Completeness
	product.Consistency = result.Dimensions.Consistency
	product.AttributeQuality = result.Dimensions.AttributeQuality
	product.DeliveryData = result.Dimensions.DeliveryData
	product.Issues = result.Issues
	product.OpenIssueCount = len(result.Issues)
	product.CatalogueVersion = result.CatalogueVersion
}

// ScoreDeps groups the score stage's write seams: the catalog store (the
// product's readiness slot, one writer), the event bus (the serving rebuild
// request + the journey threshold signal, both genuinely cross-plane), and
// the store-level readiness aggregate.
type ScoreDeps struct {
	Store     CatalogStore
	Bus       eventbus.EventBus
	Readiness ReadinessAggregateStore
	// Remediations keeps the product's remediation records in step with its
	// issues (nil skips it).
	Remediations RemediationStore
}

// ScoreAndPersist runs the score stage end-to-end for one mapped product:
// evaluate against the rubric, persist the readiness result to the
// product's readiness slot via the catalog store's write seam, request a
// serving cache rebuild, and (unless recomputeStoreAggregate is false)
// recompute the store-level aggregate and emit the journey threshold signal
// on a zero-blockers crossing. Callers never write CatalogProduct readiness
// fields directly — this is the seam.
//
// recomputeStoreAggregate must be false while a bulk catalog import is still
// scoring products one at a time: the store-level aggregate would be
// computed against an as-yet-incomplete set of products, and the resulting
// crossing signal can race ahead of the import's own first_sync_complete
// signal (see CatalogImportHandler.RunImport, which scores products
// mid-loop but only publishes first_sync_complete once the whole import is
// done). Firing readiness_threshold_met before first_sync_complete lands
// finds the journey still in "connected", which has no transition for that
// signal, so it gets silently dropped and never re-fires (the crossing
// already happened) — permanently stranding the store past "synced". Bulk
// callers pass false here and call recomputeStoreReadinessAndSignal once,
// themselves, after the import completes and its own signal is published.
func ScoreAndPersist(ctx context.Context, deps ScoreDeps, product *CatalogProduct, rubric *ScoringRubric, findings []ConsistencyIssue, recomputeStoreAggregate bool) (*ReadinessResult, error) {
	result := EvaluateReadiness(product, rubric, findings)
	applyReadiness(product, result)

	if err := deps.Store.UpsertProduct(ctx, product); err != nil {
		return nil, fmt.Errorf("persist readiness result: %w", err)
	}

	// The product's open remediations follow its issues: one per issue the
	// rubric can remediate, retired when the issue resolves. Idempotent, so a
	// retried score converges.
	if deps.Remediations != nil {
		if err := deps.Remediations.SyncForSKU(ctx, product.MerchantID, product.StoreID, product.SKU, remediationsFor(product, rubric)); err != nil {
			return nil, fmt.Errorf("sync remediations: %w", err)
		}
	}

	// A re-score is a same-process pipeline step (evaluate -> persist ->
	// rebuild-trigger) inside the sync plane; only the rebuild request
	// itself crosses into the serving plane, so that's the only part
	// published to the bus.
	//
	// Skipped mid-bulk-import (recomputeStoreAggregate == false — the same
	// signal RescoreProductBatch already uses to skip the aggregate
	// recompute above, for the same reason: the product set is still
	// incomplete). The caller's own end-of-import rebuild request
	// (CatalogImportHandler.emitRebuildRequest, "catalog_import_completed",
	// priority) is the single correct trigger for the whole set — firing
	// one here too, unconditionally, meant an N-product import produced N
	// redundant full-snapshot rebuilds, each rewriting every product key
	// via SwapSnapshot's non-atomic Redis pipeline (delete-then-set, not a
	// transaction), so a concurrent catalog/:itemId read could land in the
	// gap and see a transient 404 for a SKU that was already imported.
	if recomputeStoreAggregate {
		if err := requestRebuild(ctx, deps.Bus, product.StoreID, product.MerchantID, "product_changed", false); err != nil {
			return nil, err
		}
	}

	if deps.Readiness != nil && recomputeStoreAggregate {
		// Steady-state single-product rescore — never a fresh connection.
		if err := recomputeStoreReadinessAndSignal(ctx, deps, product.MerchantID, product.StoreID, false); err != nil {
			return nil, err
		}
	}

	return result, nil
}
