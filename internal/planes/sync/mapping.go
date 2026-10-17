// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
)

// TaskRaisedPayload matches the schema of task.raised in sarvex-v1-syncing-plane-async.yaml.
type TaskRaisedPayload struct {
	Source         string `json:"source"`
	SourceRef      string `json:"source_ref"`
	IssueID        string `json:"issue_id,omitempty"`
	Owner          string `json:"owner"`
	Title          string `json:"title"`
	BlocksCheckout bool   `json:"blocks_checkout"`
	Resolved       bool   `json:"resolved,omitempty"`
}

// JourneySignalPayload matches the schema of journey.signal.
type JourneySignalPayload struct {
	Signal string `json:"signal"`
}

// OnProductChanged implements mappingPipeline for ingestion's direct handoff
// (ingestion and mapping/scoring are both sync-plane, same-process — this
// used to be a sync.product.changed bus round-trip; see mappingPipeline's
// doc comment in ingestion.go).
func (s *syncService) OnProductChanged(ctx context.Context, storeID, merchantID string, payload ProductChangedPayload) error {
	s.log.Info("mapping worker processing product change", "sku", payload.SKU, "store_id", storeID, "merchant_id", merchantID)

	if payload.Change == "deleted" {
		// Deletes go through OnProductDeleted. Re-scoring the tombstoned record
		// below would upsert it, which clears the tombstone and brings the
		// deleted product back.
		return nil
	}

	// 1. Load catalog product
	col := s.db.Collection(catalogProductsCollection)
	id := docID(merchantID, storeID, payload.SKU)

	var p CatalogProduct
	err := col.FindOne(ctx, bson.M{"_id": id}).Decode(&p)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			s.log.Warn("product not found in catalog store", "id", id)
			return nil
		}
		return fmt.Errorf("read catalog store for mapping: %w", err)
	}

	rubric, err := LoadScoringRubric(ctx, s.log)
	if err != nil {
		return fmt.Errorf("load scoring_rubric: %w", err)
	}
	return s.RescoreProduct(ctx, &p, rubric)
}

// OnProductDeleted retires everything a deleted product had open and tells the
// serving plane it is gone: its blocking-issue tasks are resolved, its
// unapplied remediations are dropped, the store-level readiness aggregate is
// recomputed (a deleted blocker no longer blocks the store), and a cache
// rebuild is requested (the snapshot excludes tombstoned products). p is the
// product as it was before the delete.
func (s *syncService) OnProductDeleted(ctx context.Context, storeID, merchantID string, p *CatalogProduct) error {
	s.log.Info("mapping worker processing product deletion", "sku", p.SKU, "store_id", storeID, "merchant_id", merchantID)

	for _, issue := range p.Issues {
		if !issue.BlocksCheckout {
			continue
		}
		payload := buildReadinessTaskRaisedPayload(p.SKU, issue)
		payload.Resolved = true
		taskBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal task.raised (resolve deleted product): %w", err)
		}
		if err := s.bus.Publish(ctx, newDomainEvent(ucp.EventSyncTaskRaised, storeID, merchantID, taskBytes)); err != nil {
			return fmt.Errorf("publish task.raised (resolve deleted product): %w", err)
		}
	}

	if s.remediations != nil {
		if err := s.remediations.SyncForSKU(ctx, merchantID, storeID, p.SKU, nil); err != nil {
			return fmt.Errorf("retire remediations of deleted product: %w", err)
		}
	}

	if s.readinessStore != nil {
		deps := ScoreDeps{Store: s.catalogStore, Bus: s.bus, Readiness: s.readinessStore}
		if err := recomputeStoreReadinessAndSignal(ctx, deps, merchantID, storeID, false); err != nil {
			return err
		}
	}

	return requestRebuild(ctx, s.bus, storeID, merchantID, "product_deleted", false)
}

// RescoreProduct runs Domain 2a stage 1 (resolve) + stage 3 (score) for a
// product already loaded into memory: map raw attributes onto the
// catalogue's UCP/Google target fields via the ontology's rule/learned
// tiers, then evaluate + persist + emit via runScoreStage. This is the
// shared entry point for every trigger that re-derives a product's mapping
// and score from its current raw fields — a live ingest signal
// (OnProductChanged above) and a sweep-corrected product
// (sweep_handler.go) both call it. Each call recomputes the store-level
// readiness aggregate and may emit the journey threshold signal — safe here
// because the store's product set is already complete (unlike mid-import;
// see RescoreProductBatch).
func (s *syncService) RescoreProduct(ctx context.Context, p *CatalogProduct, rubric *ScoringRubric) error {
	return s.rescoreProduct(ctx, p, rubric, true)
}

// RescoreProductBatch is RescoreProduct without the store-level readiness
// recompute/signal step — for bulk catalog import, whose loop scores
// products one at a time against a catalog store that doesn't yet hold the
// full set. Recomputing the aggregate per-product there would read
// transient partial state and can fire the journey threshold signal before
// the import's own first_sync_complete signal, which the journey state
// machine has no transition for from "connected" (see ScoreAndPersist's doc
// comment). Callers must call FinalizeBatchReadiness once, after the import
// completes and first_sync_complete has been published, to compute the
// aggregate against final data and emit any crossing signal in the right
// order.
func (s *syncService) RescoreProductBatch(ctx context.Context, p *CatalogProduct, rubric *ScoringRubric) error {
	return s.rescoreProduct(ctx, p, rubric, false)
}

func (s *syncService) rescoreProduct(ctx context.Context, p *CatalogProduct, rubric *ScoringRubric, recomputeStoreAggregate bool) error {
	mapped, err := s.resolver.Resolve(ctx, p)
	if err != nil {
		return fmt.Errorf("resolve product mapping: %w", err)
	}
	mapped.ApplyTo(p)

	if err := s.runScoreStage(ctx, p, rubric, recomputeStoreAggregate); err != nil {
		return fmt.Errorf("score product: %w", err)
	}
	return nil
}

// FinalizeBatchReadiness recomputes storeID's store-level readiness
// aggregate against its current (complete) product set and emits the
// journey threshold signal on a zero-blockers crossing. The one-time
// counterpart to the per-product recompute RescoreProduct does — see
// RescoreProductBatch's doc comment for why bulk import defers to this
// instead. freshConnection is forwarded to recomputeStoreReadinessAndSignal
// — see its doc comment.
func (s *syncService) FinalizeBatchReadiness(ctx context.Context, merchantID, storeID string, freshConnection bool) error {
	deps := ScoreDeps{Store: s.catalogStore, Bus: s.bus, Readiness: s.readinessStore}
	if deps.Readiness == nil {
		return nil
	}
	return recomputeStoreReadinessAndSignal(ctx, deps, merchantID, storeID, freshConnection)
}

// runScoreStage runs Domain 2a stage 3 (score) for a single mapped product:
// gather consistency findings, evaluate + persist + emit via
// ScoreAndPersist, then raise a task for any newly blocking issue. This is
// the shared entry point for every trigger that re-scores a product
// (product-changed, audit, remediation applied) — there is exactly one
// score-stage implementation in the sync plane. recomputeStoreAggregate is
// forwarded to ScoreAndPersist — see its doc comment.
func (s *syncService) runScoreStage(ctx context.Context, p *CatalogProduct, rubric *ScoringRubric, recomputeStoreAggregate bool) error {
	previousIssues := p.Issues

	// Score against the rubric this store is on: core rules plus the rules of
	// any surface it is enrolled on. The resolve stage applies the same scope.
	var applicable map[string]bool
	if s.surfaces != nil {
		var err error
		applicable, err = s.surfaces.ApplicableSurfaces(ctx, p.MerchantID, p.StoreID)
		if err != nil {
			return fmt.Errorf("resolve applicable surfaces: %w", err)
		}
	}
	rubric = rubric.ForSurfaces(applicable)
	p.ScoredForSurfaces = sortedKeys(applicable)

	findings, err := s.consistencyEngine.CheckConsistency(ctx, p)
	if err != nil {
		s.log.Error("consistency check failed", "error", err, "sku", p.SKU)
		// Non-fatal: score without consistency findings.
		findings = nil
	}

	deps := ScoreDeps{Store: s.catalogStore, Bus: s.bus, Readiness: s.readinessStore, Remediations: s.remediations}
	result, err := ScoreAndPersist(ctx, deps, p, rubric, findings, recomputeStoreAggregate)
	if err != nil {
		return fmt.Errorf("score and persist: %w", err)
	}

	stillBlocking := make(map[string]bool, len(result.Issues))
	for _, issue := range result.Issues {
		if !issue.BlocksCheckout {
			continue
		}
		stillBlocking[issue.IssueID] = true
		taskBytes, err := json.Marshal(buildReadinessTaskRaisedPayload(p.SKU, issue))
		if err != nil {
			return fmt.Errorf("marshal task.raised: %w", err)
		}
		if err := s.bus.Publish(ctx, newDomainEvent(ucp.EventSyncTaskRaised, p.StoreID, p.MerchantID, taskBytes)); err != nil {
			return fmt.Errorf("publish task.raised: %w", err)
		}
	}

	// Anything that used to block checkout and no longer appears in the new
	// result has cleared — resolve its task rather than leaving it open
	// forever (nothing else in the pipeline ever closes a raised task).
	for _, issue := range previousIssues {
		if !issue.BlocksCheckout || stillBlocking[issue.IssueID] {
			continue
		}
		payload := buildReadinessTaskRaisedPayload(p.SKU, issue)
		payload.Resolved = true
		taskBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal task.raised (resolve): %w", err)
		}
		if err := s.bus.Publish(ctx, newDomainEvent(ucp.EventSyncTaskRaised, p.StoreID, p.MerchantID, taskBytes)); err != nil {
			return fmt.Errorf("publish task.raised (resolve): %w", err)
		}
	}

	s.log.Debug("product scored", "sku", p.SKU, "score", result.Overall, "blocks_checkout", p.BlocksCheckout)
	return nil
}

// buildReadinessTaskRaisedPayload constructs the sync.task.raised payload for
// one blocking readiness issue. IssueID carries the issue's own id (distinct
// from SourceRef, which stays the sku so the control plane's typed drill-in
// lookups keep working) so the control-plane subscriber can give each issue
// on a product its own task id instead of colliding on (source, store, sku)
// when a product has more than one blocking issue.
func buildReadinessTaskRaisedPayload(sku string, issue ProductIssue) TaskRaisedPayload {
	return TaskRaisedPayload{
		Source:         "readiness",
		SourceRef:      sku,
		IssueID:        issue.IssueID,
		Owner:          "merchant",
		Title:          fmt.Sprintf("Product %s: %s (%s)", sku, issue.Field, issue.Problem),
		BlocksCheckout: true,
	}
}

// OnPriceChanged implements mappingPipeline. See OnProductChanged's doc
// comment on why this is a direct call, not a bus handler.
//
// A price is scored data (rule_price_missing, the consistency rules), so the
// product is re-scored, not just patched: a price edited to zero must open its
// blocking task, and one corrected must close it. The re-score also requests the
// serving cache rebuild.
func (s *syncService) OnPriceChanged(ctx context.Context, storeID, merchantID string, payload PriceChangedPayload) error {
	s.log.Info("mapping worker updating price", "sku", payload.SKU, "store_id", storeID, "merchant_id", merchantID)

	return s.rescoreAfter(ctx, storeID, merchantID, payload.SKU, func(p *CatalogProduct) {
		p.PriceMinor = payload.NewPrice.AmountMinor
		p.Currency = payload.NewPrice.Currency
	})
}

// OnInventoryChanged implements mappingPipeline. See OnProductChanged's doc
// comment on why this is a direct call, not a bus handler, and OnPriceChanged
// on why the product is re-scored.
func (s *syncService) OnInventoryChanged(ctx context.Context, storeID, merchantID string, payload InventoryChangedPayload) error {
	s.log.Info("mapping worker updating inventory", "sku", payload.SKU, "store_id", storeID, "merchant_id", merchantID)

	return s.rescoreAfter(ctx, storeID, merchantID, payload.SKU, func(p *CatalogProduct) {
		p.InStock = payload.InStock
		p.Quantity = payload.Quantity
	})
}

// rescoreAfter applies a targeted change (price or stock) to the SKU's catalog
// record and re-scores it. A SKU the catalog does not hold is dropped, as
// OnProductChanged does.
func (s *syncService) rescoreAfter(ctx context.Context, storeID, merchantID, sku string, apply func(*CatalogProduct)) error {
	p, err := s.catalogStore.GetProduct(ctx, storeID, sku)
	if errors.Is(err, ErrProductNotFound) {
		s.log.Warn("product not found in catalog store", "sku", sku, "store_id", storeID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("read catalog store for rescore: %w", err)
	}
	if p.MerchantID != merchantID {
		return nil // not this merchant's product
	}
	apply(p)
	p.UpdatedAt = time.Now().UTC()

	rubric, err := LoadScoringRubric(ctx, s.log)
	if err != nil {
		return fmt.Errorf("load scoring_rubric: %w", err)
	}
	return s.RescoreProduct(ctx, p, rubric)
}

// rebuildRequestPayload is the body of serving.rebuild.requested — the one
// event in this file that genuinely crosses into the serving plane
// (internal/planes/serving/consumer.go).
type rebuildRequestPayload struct {
	Reason   string `json:"reason"`
	Priority bool   `json:"priority"`
}

// requestRebuild publishes serving.rebuild.requested. Centralised here
// because price-changed, inventory-changed, and re-score all trigger it,
// varying only reason/priority.
func requestRebuild(ctx context.Context, bus eventbus.EventBus, storeID, merchantID, reason string, priority bool) error {
	rebuildBytes, err := json.Marshal(rebuildRequestPayload{Reason: reason, Priority: priority})
	if err != nil {
		return fmt.Errorf("marshal serving.rebuild.requested: %w", err)
	}
	if err := bus.Publish(ctx, newDomainEvent(ucp.EventRebuildRequested, storeID, merchantID, rebuildBytes)); err != nil {
		return fmt.Errorf("publish serving.rebuild.requested: %w", err)
	}
	return nil
}

// sortedKeys returns the set's members in a stable order (nil for an empty set).
func sortedKeys(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
