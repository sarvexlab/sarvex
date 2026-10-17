// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/concurrency"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/connectors/base"
)

// runningSweepLeaseKeyPrefix namespaces the per-store overlap-guard lease so
// it never collides with the scheduler's own leader-election key in the
// shared Lease keyspace.
const runningSweepLeaseKeyPrefix = "running:"

// defaultRunningSweepLeaseTTL bounds how long a single store's sweep may run
// before its overlap-guard lease self-expires. It must comfortably exceed a
// worst-case FetchCatalog + diff for one store; a stuck/crashed sweep then
// self-heals rather than permanently blocking that store's future sweeps.
const defaultRunningSweepLeaseTTL = 10 * time.Minute

// sweepDiffPageSize batches ListByStore reads while diffing so a
// large catalog store doesn't have to be loaded into memory in one page.
const sweepDiffPageSize = 200

// ReconciliationDiscrepancyPayload matches the schema of
// sync.reconciliation.discrepancy_found (sarvex-v1-syncing-plane-async.yaml
// components.messages.reconciliation_discrepancy_found).
type ReconciliationDiscrepancyPayload struct {
	SKU         string `json:"sku"`
	Field       string `json:"field"`
	StoreValue  string `json:"store_value"`
	SarvexValue string `json:"sarvex_value"`
}

// discrepancy is the sweep handler's internal representation of one
// mismatch before it is marshalled onto the wire.
type discrepancy struct {
	sku         string
	field       string
	storeValue  string
	sarvexValue string
}

// SweepHandler runs a store's reconciliation sweep: it runs on the sync
// plane's shared consumer group, so of however many sync-plane instances are
// running, exactly one runs any given store's sweep at a time — that's the
// "competing consumer" half of the design (the per-store running-sweep
// lease below); the scheduler's leader election is the "single trigger"
// half.
//
// Mirror-correction choice: on a detected discrepancy, the handler both
// emits sync.reconciliation.discrepancy_found (a genuine cross-plane signal
// — control-plane task_service surfaces it as an operator/merchant task) AND
// upserts/deletes the catalog store's own record to match the store's true
// state, then re-runs mapping + scoring on the corrected product (scorer,
// same-process — see RescoreProduct in mapping.go) so a sweep-driven
// correction doesn't leave a stale readiness score behind, exactly as a live
// ingest signal wouldn't. This is not remediation (which pushes fixes back
// to the merchant's live store — out of scope here); it is the same "pull
// truth, write the mirror" behaviour the ingestion pipeline already
// performs for every live signal (ingestion.go), and the catalog store's
// one-writer-per-store rule already names the sync plane as that writer.
// Without it, a discrepancy the signal path missed would stay wrong (and
// unscored) in the mirror forever, since nothing else in this codebase
// corrects raw catalog fields from store truth.
type SweepHandler struct {
	lease        concurrency.Lease
	catalogStore CatalogStore
	registry     *base.Registry
	provider     ucp.StoreProviderByStoreIDFunc
	bus          eventbus.EventBus
	scorer       productRescorer
	log          *slog.Logger

	leaseTTL time.Duration
}

// productRescorer is the sync-internal seam the sweep handler calls to
// re-derive mapping + score for a product it just corrected. Implemented by
// syncService (RescoreProduct, mapping.go).
type productRescorer interface {
	RescoreProduct(ctx context.Context, p *CatalogProduct, rubric *ScoringRubric) error
	// OnProductDeleted retires what a product removed from the store left
	// open (tasks, remediations, its share of the store aggregate).
	OnProductDeleted(ctx context.Context, storeID, merchantID string, p *CatalogProduct) error
}

// productBatchRescorer is the sync-internal seam the bulk catalog import
// handler calls: RescoreProductBatch scores a product without recomputing
// the store-level readiness aggregate (incomplete mid-import), and
// FinalizeBatchReadiness recomputes it once, after the import completes —
// see mapping.go's RescoreProductBatch doc comment for why this split
// exists. freshConnection distinguishes a store's first import in a
// connection lifecycle (reason="initial") from a resync — see
// recomputeStoreReadinessAndSignal's doc comment for why that matters.
// Implemented by syncService.
type productBatchRescorer interface {
	RescoreProductBatch(ctx context.Context, p *CatalogProduct, rubric *ScoringRubric) error
	FinalizeBatchReadiness(ctx context.Context, merchantID, storeID string, freshConnection bool) error
}

// NewSweepHandler constructs a SweepHandler. leaseTTL <= 0 uses
// defaultRunningSweepLeaseTTL. registry/provider resolve the per-store
// base.Connector used to pull the catalog, the same pattern
// service.go's writeProductToStore uses for UpdateProduct.
func NewSweepHandler(
	lease concurrency.Lease,
	catalogStore CatalogStore,
	registry *base.Registry,
	provider ucp.StoreProviderByStoreIDFunc,
	bus eventbus.EventBus,
	scorer productRescorer,
	log *slog.Logger,
	leaseTTL time.Duration,
) *SweepHandler {
	if log == nil {
		log = slog.Default()
	}
	if leaseTTL <= 0 {
		leaseTTL = defaultRunningSweepLeaseTTL
	}
	return &SweepHandler{
		lease:        lease,
		catalogStore: catalogStore,
		registry:     registry,
		provider:     provider,
		bus:          bus,
		scorer:       scorer,
		log:          log,
		leaseTTL:     leaseTTL,
	}
}

// RunSweep is the sweep entrypoint for one store, called directly by
// SweepScheduler (same sync-plane process — see sweepRunner's doc comment in
// sweep_scheduler.go).
func (h *SweepHandler) RunSweep(ctx context.Context, storeID, merchantID string) error {
	if storeID == "" {
		h.log.Error("sweep: missing store_id")
		return nil
	}
	if merchantID == "" {
		// Every catalog write and discrepancy event is tenant-keyed; sweeping
		// without the merchant would drop added products and publish
		// discrepancies under no merchant.
		return fmt.Errorf("sweep: store %s: missing merchant_id", storeID)
	}

	// attemptID identifies this one sweep attempt, not this process: two
	// concurrent sweeps for the same store must never both acquire the
	// lease, even if (improbably) issued by the very same handler instance,
	// so acquisition can't be a "renewal" of anything — each attempt is a
	// fresh, unique token.
	attemptID := uuid.NewString()

	leaseKey := runningSweepLeaseKeyPrefix + storeID
	acquired, err := h.lease.TryAcquire(ctx, leaseKey, attemptID, h.leaseTTL)
	if err != nil {
		return fmt.Errorf("sweep: acquire running lease for store %s: %w", storeID, err)
	}
	if !acquired {
		// A sweep for this store is already in flight (this instance or
		// another): coalesce rather than stack a second concurrent sweep.
		h.log.Info("sweep: previous sweep still running for store, coalescing", "store_id", storeID)
		return nil
	}
	defer func() {
		// Best-effort release on a fresh context: ctx may already be
		// cancelled (e.g. shutdown) by the time the sweep finishes, but the
		// lease must still be dropped so the next scheduled sweep for this
		// store isn't blocked until TTL expiry.
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.lease.Release(releaseCtx, leaseKey, attemptID); err != nil {
			h.log.Error("sweep: failed to release running lease", "store_id", storeID, "error", err)
		}
	}()

	instance, err := h.provider(merchantID, storeID)
	if err != nil {
		return fmt.Errorf("sweep: get store provider for store %s: %w", storeID, err)
	}
	connector, err := h.registry.Get(instance)
	if err != nil {
		return fmt.Errorf("sweep: get connector for store %s: %w", storeID, err)
	}

	entities, err := connector.FetchCatalog(ctx, storeID, sweepDiffPageSize)
	if err != nil {
		var connErr *base.ConnectorError
		if errors.As(err, &connErr) {
			h.log.Error("sweep: store-API failure", "store_id", storeID, "kind", connErr.Kind, "op", connErr.Op, "error", err)
		} else {
			h.log.Error("sweep: FetchCatalog failed", "store_id", storeID, "error", err)
		}
		// Typed/wrapped error, not swallowed: the lease is released by the
		// defer above (no partial silent state); the scheduler's next
		// interval retries every store, including this one.
		return fmt.Errorf("sweep: fetch catalog for store %s: %w", storeID, err)
	}

	rubric, err := LoadScoringRubric(ctx, h.log)
	if err != nil {
		return fmt.Errorf("sweep: load scoring_rubric: %w", err)
	}

	discrepancies, err := h.diffAndCorrect(ctx, storeID, merchantID, entities, rubric)
	if err != nil {
		return fmt.Errorf("sweep: diff store %s: %w", storeID, err)
	}

	for _, d := range discrepancies {
		if err := h.emitDiscrepancy(ctx, storeID, merchantID, d); err != nil {
			h.log.Error("sweep: failed to publish discrepancy", "store_id", storeID, "sku", d.sku, "field", d.field, "error", err)
		}
	}

	h.log.Info("sweep: completed", "store_id", storeID, "discrepancies", len(discrepancies))
	return nil
}

// diffAndCorrect compares the store's authoritative entities against the
// catalog store's mirror for storeID, returning one discrepancy per
// add/remove/field-drift found. It also brings the mirror in line with the
// store's truth for each discrepancy, and re-scores each corrected or
// newly-added product against rubric (see SweepHandler's doc comment for
// why neither of those is "remediation").
func (h *SweepHandler) diffAndCorrect(ctx context.Context, storeID, merchantID string, entities []base.CatalogEntity, rubric *ScoringRubric) ([]discrepancy, error) {
	storeBySKU := lo.KeyBy(entities, func(e base.CatalogEntity) string { return e.SKU })

	var discrepancies []discrepancy
	seenSKUs := make(map[string]struct{}, len(entities))

	cursor := ""
	for {
		products, next, err := h.catalogStore.ListByStore(ctx, storeID, sweepDiffPageSize, cursor)
		if err != nil {
			return nil, fmt.Errorf("list catalog store: %w", err)
		}

		for _, p := range products {
			seenSKUs[p.SKU] = struct{}{}
			entity, presentInStore := storeBySKU[p.SKU]

			if !presentInStore {
				// Remove: the catalog store has it, the merchant store no
				// longer does (a lost delete signal).
				discrepancies = append(discrepancies, discrepancy{
					sku: p.SKU, field: "presence", storeValue: "absent", sarvexValue: "present",
				})
				if err := h.catalogStore.DeleteProduct(ctx, storeID, p.SKU); err != nil && !errors.Is(err, ErrProductNotFound) {
					h.log.Error("sweep: failed to correct removed product", "store_id", storeID, "sku", p.SKU, "error", err)
				} else if err := h.scorer.OnProductDeleted(ctx, storeID, merchantID, p); err != nil {
					h.log.Error("sweep: failed to clean up removed product", "store_id", storeID, "sku", p.SKU, "error", err)
				}
				continue
			}

			if fieldDiscrepancies := diffFields(p, entity); len(fieldDiscrepancies) > 0 {
				discrepancies = append(discrepancies, fieldDiscrepancies...)
				p.applyEntity(entity)
				p.UpdatedAt = time.Now().UTC()
				if err := h.catalogStore.UpsertProduct(ctx, p); err != nil {
					h.log.Error("sweep: failed to correct drifted product", "store_id", storeID, "sku", p.SKU, "error", err)
				} else if err := h.scorer.RescoreProduct(ctx, p, rubric); err != nil {
					h.log.Error("sweep: failed to rescore corrected product", "store_id", storeID, "sku", p.SKU, "error", err)
				}
			}
		}

		if next == nil {
			break
		}
		cursor = *next
	}

	// Add: the merchant store has it, the catalog store has never seen it
	// (a lost create signal).
	for sku, entity := range storeBySKU {
		if _, ok := seenSKUs[sku]; ok {
			continue
		}
		discrepancies = append(discrepancies, discrepancy{
			sku: sku, field: "presence", storeValue: "present", sarvexValue: "absent",
		})
		product := catalogEntityToProduct(merchantID, storeID, entity)
		if err := h.catalogStore.UpsertProduct(ctx, product); err != nil {
			h.log.Error("sweep: failed to correct added product", "store_id", storeID, "sku", sku, "error", err)
		} else if err := h.scorer.RescoreProduct(ctx, product, rubric); err != nil {
			h.log.Error("sweep: failed to score added product", "store_id", storeID, "sku", sku, "error", err)
		}
	}

	return discrepancies, nil
}

// diffFields compares the catalog store's mirror of a product against the
// store's authoritative entity, field by field, returning one discrepancy
// per field that differs.
func diffFields(p *CatalogProduct, e base.CatalogEntity) []discrepancy {
	var out []discrepancy

	if p.PriceMinor != e.PriceMinor {
		out = append(out, discrepancy{
			sku: p.SKU, field: "price_minor",
			storeValue:  fmt.Sprintf("%d", e.PriceMinor),
			sarvexValue: fmt.Sprintf("%d", p.PriceMinor),
		})
	}
	if p.Currency != e.Currency {
		out = append(out, discrepancy{sku: p.SKU, field: "currency", storeValue: e.Currency, sarvexValue: p.Currency})
	}
	if p.InStock != e.InStock {
		out = append(out, discrepancy{
			sku: p.SKU, field: "in_stock",
			storeValue:  fmt.Sprintf("%t", e.InStock),
			sarvexValue: fmt.Sprintf("%t", p.InStock),
		})
	}
	if !quantityEqual(p.Quantity, e.Quantity) {
		out = append(out, discrepancy{
			sku: p.SKU, field: "quantity",
			storeValue:  quantityString(e.Quantity),
			sarvexValue: quantityString(p.Quantity),
		})
	}
	if p.Title != e.Name {
		out = append(out, discrepancy{sku: p.SKU, field: "title", storeValue: e.Name, sarvexValue: p.Title})
	}

	return out
}

func quantityEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func quantityString(q *int) string {
	if q == nil {
		return ""
	}
	return fmt.Sprintf("%d", *q)
}

// emitDiscrepancy publishes sync.reconciliation.discrepancy_found for one
// detected mismatch.
func (h *SweepHandler) emitDiscrepancy(ctx context.Context, storeID, merchantID string, d discrepancy) error {
	payload, err := json.Marshal(ReconciliationDiscrepancyPayload{
		SKU:         d.sku,
		Field:       d.field,
		StoreValue:  d.storeValue,
		SarvexValue: d.sarvexValue,
	})
	if err != nil {
		return fmt.Errorf("marshal discrepancy payload: %w", err)
	}
	evt := newDomainEvent(ucp.EventReconciliationDiscrepancyFound, storeID, merchantID, payload)
	return h.bus.Publish(ctx, evt)
}
