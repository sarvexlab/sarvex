// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/commandbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/concurrency"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/catalogue"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/connectors/base"
)

// defaultSweepInterval is how often the reconciliation sweeper's scheduler
// emits one control.catalog.sweep command per known store.
const defaultSweepInterval = 1 * time.Hour

// Service defines the lifecycle contract for the Sync Plane background workers.
type Service interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

type syncService struct {
	db                *mongo.Database
	bus               eventbus.EventBus
	log               *slog.Logger
	registry          *base.Registry
	consistencyEngine *ConsistencyEngine
	snapshotBuilder   SnapshotBuilder
	provider          ucp.StoreProviderByStoreIDFunc
	remediations      RemediationStore
	catalogImport     *CatalogImportHandler
	orderIntent       *OrderIntentHandler
	ingestion         *IngestionPipeline
	jobServer         *JobServer
	sweepHandler      *SweepHandler
	resolver          Resolver
	surfaces          *cachedSurfaceScope // read-only view of control_store_surfaces; nil in tests = core only
	ontologyStore     OntologyRuleUpserter
	catalogStore      CatalogStore
	readinessStore    ReadinessAggregateStore

	mu   sync.Mutex
	subs []eventbus.Subscription
}

// surfaceScopeTTL bounds how long a store's enrolled-surface set is reused
// across products. Enrollment changes arrive as an audit request, which drops
// the entry, so this only limits staleness for a change that does not.
const surfaceScopeTTL = 30 * time.Second

// NewSyncService creates a new instance of the Sync Plane background service.
// lease backs the per-store overlap guard in SweepHandler (prevents two
// concurrent sweeps for the same store). redisConnOpt is used by the asynq
// JobServer for scheduling and task dispatch; it must point at the same Redis
// instance as lease so all instances share one coordination backend. redisClient
// backs signal de-dup state for persistence across restarts (ADR-4).
func NewSyncService(
	db *mongo.Database,
	bus eventbus.EventBus,
	registry *base.Registry,
	provider ucp.StoreProviderByStoreIDFunc,
	remediations RemediationStore,
	lease concurrency.Lease,
	redisConnOpt asynq.RedisConnOpt,
	redisClient redis.Cmdable,
	log *slog.Logger,
) Service {

	catalogStore, _ := NewMongoDBCatalogStore(db)
	engine := NewConsistencyEngine(log)

	// Create product transformer and snapshot builder for serving cache rebuilds
	transformer := NewDefaultProductTransformer()
	snapshotBuilder := NewMongoSnapshotBuilder(db, transformer, log)

	importJobRepo, _ := NewMongoImportJobRepository(db)
	orderRepo, _ := NewMongoOrderRepository(db)

	cat, err := catalogue.LoadCatalogue(context.Background(), log)
	if err != nil {
		log.Error("failed to load UCP catalogue for mapping resolver; resolve stage will mark all fields unresolved", "error", err)
	}
	ontologyStore := NewMongoOntologyStore(db)
	surfaces := NewCachedSurfaceScope(NewMongoSurfaceScope(db), surfaceScopeTTL)
	resolver := NewOntologyResolver(ontologyStore, cat, log).WithSurfaceScope(surfaces)

	readinessStore := NewMongoReadinessAggregateStore(db)

	// Two-phase construction: catalog import, ingestion, the sweep handler,
	// and the sweep scheduler each hand their normalised output directly to
	// a sync-plane sibling rather than round-tripping through the bus (see
	// mappingPipeline's doc comment in ingestion.go). That means they need a
	// reference to s itself (or, for the scheduler, to the handler), so s is
	// allocated first and wired into them below rather than in one literal.
	s := &syncService{
		db:                db,
		bus:               bus,
		log:               log,
		registry:          registry,
		consistencyEngine: engine,
		snapshotBuilder:   snapshotBuilder,
		provider:          provider,
		remediations:      remediations,
		resolver:          resolver,
		surfaces:          surfaces,
		ontologyStore:     ontologyStore,
		catalogStore:      catalogStore,
		readinessStore:    readinessStore,
	}

	s.catalogImport = NewCatalogImportHandler(importJobRepo, catalogStore, registry, provider, s, bus, log, defaultImportBatchSize)
	s.orderIntent = NewOrderIntentHandler(orderRepo, bus, log)
	s.ingestion = NewIngestionPipeline(catalogStore, registry, provider, s, redisClient, log)
	s.sweepHandler = NewSweepHandler(lease, catalogStore, registry, provider, bus, s, log, defaultRunningSweepLeaseTTL)
	s.jobServer = NewJobServer(redisConnOpt, s.sweepHandler, s.catalogImport, catalogStore, defaultSweepInterval, log)
	s.catalogImport.SetEnqueuer(s.jobServer)

	return s
}

// Start registers all event subscriptions and starts the asynq job server.
func (s *syncService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.log.Info("starting sync plane service workers")

	// ── 0. Seed the ontology (sync is its one writer) ─────────────────────
	// Before any signal can resolve a product; idempotent, seed-owned rows only.
	if s.ontologyStore != nil {
		if err := SeedOntology(ctx, s.ontologyStore, s.log); err != nil {
			return fmt.Errorf("seed ontology: %w", err)
		}
	}

	// ── 1. Register Subscriptions ──────────────────────────────────────────
	if err := s.registerSubscriptions(); err != nil {
		return fmt.Errorf("register sync subscriptions: %w", err)
	}

	// ── 2. Start asynq job server (sweep, feed safety-net, debounce, import)
	if err := s.jobServer.Start(); err != nil {
		return fmt.Errorf("job server start: %w", err)
	}

	s.log.Info("sync plane service workers started successfully")
	return nil
}

// Stop stops the asynq job server and drains all event subscriptions.
func (s *syncService) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.log.Info("stopping sync plane service workers")

	// 1. Gracefully shut down the asynq server (drains in-progress tasks)
	// and scheduler (stops new emissions).
	s.jobServer.Stop()

	// 2. Unsubscribe all event streams.
	for _, sub := range s.subs {
		s.log.Debug("unsubscribing topic", "topic", sub.Topic())
		if err := s.bus.Unsubscribe(ctx, sub); err != nil {
			s.log.Error("failed to unsubscribe from topic", "topic", sub.Topic(), "error", err)
		}
	}
	s.subs = nil

	s.log.Info("sync plane service workers stopped cleanly")
	return nil
}

func (s *syncService) registerSubscriptions() error {
	// All worker channels defined in sarvex-v1-syncing-plane-async.yaml
	subscriptions := []struct {
		topic   string
		handler eventbus.EventHandler
	}{
		// Control plane commands (cross-plane)
		{topic: ucp.CommandRequestAudit, handler: s.handleAuditRequested},
		{topic: ucp.CommandApplyRemediations, handler: s.handleRemediationsApplied},
		{topic: ucp.CommandRevertRemediations, handler: s.handleRemediationsReverted},
		{topic: ucp.EventCatalogImport, handler: s.catalogImport.HandleCommand},

		// Raw signal topic published from control plane signal ingress
		{topic: ucp.EventConnectorSignalReceived, handler: s.ingestion.HandleSignalReceived},

		// Note: sync.product.changed, sync.price.changed, sync.inventory.changed,
		// sync.catalog.scored, and control.catalog.sweep are no longer bus
		// topics: ingestion -> mapping/scoring and the sweep scheduler ->
		// sweep handler are same-process handoffs inside this one sync-plane
		// binary (see mappingPipeline's doc comment in ingestion.go and
		// sweepRunner's in sweep_scheduler.go), so they're direct calls now.
		// sync.reconciliation.discrepancy_found is still published (sweep
		// handler emits it) but the sync plane no longer subscribes to its
		// own event — the only real consumer is control-plane
		// monitoring.DiscrepancySubscriber.

		// TODO(Phase2): consistency.finding_raised - publisher not yet implemented
		// {topic: "consistency.finding_raised", handler: s.handleConsistencyFinding},

		// Serving cache rebuild triggers
		{topic: ucp.EventRebuildRequested, handler: s.handleServingRebuildRequested},

		// Order handoff: checkout already created the order synchronously
		// (serving/api/handler.go CheckoutComplete); this just records it.
		{topic: ucp.EventOrderIntent, handler: s.orderIntent.HandleOrderIntent},

		// Order status-sync-back to Google is not built: the plugin doesn't
		// signal order-status changes yet (ingestion.go's order_status
		// entity kind is a no-op stub) and there's no Google order-status
		// API client in this codebase. Left out of scope; not silently
		// dropped — see sarvex-v1-sequence.md §5's gap note.
	}

	ctx := context.Background()
	for _, subDef := range subscriptions {
		sub, err := s.bus.Subscribe(ctx, subDef.topic, subDef.handler)
		if err != nil {
			return fmt.Errorf("subscribe %s: %w", subDef.topic, err)
		}
		s.subs = append(s.subs, sub)
	}

	return nil
}

// ── Command Handlers (Cross-plane Control → Sync) ─────────────────────────

// handleAuditRequested processes audit_requested commands from the control plane.
// The control plane requests a full re-score of a merchant's catalog.
func (s *syncService) handleAuditRequested(ctx context.Context, event eventbus.DomainEvent) error {
	var payload commandbus.AuditRequestedPayload
	if err := json.Unmarshal(event.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal audit_requested: %w", err)
	}

	storeID := event.StoreID()
	merchantID := event.MerchantID()
	s.log.Info("audit requested",
		"store_id", storeID,
		"merchant_id", merchantID,
		"job_id", payload.JobID,
		"source", payload.Source)

	// Load scoring rubric
	rubric, err := LoadScoringRubric(ctx, s.log)
	if err != nil {
		s.log.Error("failed to load scoring_rubric", "error", err, "store_id", storeID)
		return fmt.Errorf("load scoring_rubric: %w", err)
	}

	// An audit follows an enrollment change as often as anything else: drop the
	// cached surface set so the rescore below sees the store's current one.
	if s.surfaces != nil {
		s.surfaces.Invalidate(merchantID, storeID)
	}

	// Fetch all products for the store (catalog data is partitioned by store_id, not merchant_id).
	col := s.db.Collection(catalogProductsCollection)
	cursor, err := col.Find(ctx, bson.M{"store_id": storeID})
	if err != nil {
		return fmt.Errorf("list products: %w", err)
	}
	defer cursor.Close(ctx)

	var products []CatalogProduct
	if err := cursor.All(ctx, &products); err != nil {
		return fmt.Errorf("decode products: %w", err)
	}

	s.log.Info("audit: scoring products", "store_id", storeID, "count", len(products))

	// Re-resolve and re-score every product through the one score stage
	// (RescoreProductBatch -> runScoreStage), so an audit can never produce a
	// different score, issue list or task set than a live product change does.
	// The store-level aggregate is recomputed once at the end, against the
	// complete set, and owns the journey threshold signal (a signal is emitted
	// only on a zero-blockers crossing, never unconditionally).
	for i := range products {
		if err := s.RescoreProductBatch(ctx, &products[i], rubric); err != nil {
			s.log.Error("failed to score product", "error", err, "sku", products[i].SKU)
			// Non-fatal: continue scoring others
		}
	}
	if err := s.FinalizeBatchReadiness(ctx, merchantID, storeID, false); err != nil {
		s.log.Error("audit: store readiness recompute failed", "error", err, "store_id", storeID)
	}

	s.log.Info("audit completed", "store_id", storeID, "job_id", payload.JobID, "products", len(products))
	return nil
}

// handleRemediationsApplied processes the apply-remediations command the
// control plane sends when a merchant approves a batch of rule-based fixes.
// The sync plane owns the remediation records (sync_catalog_remediations): for
// each approved record it applies that record's rubric remediation to that
// record's product only, writes the change back to the merchant's store,
// re-scores the product through the one score stage, and marks the record
// applied under the command's write-back id.
//
// Redelivery is the normal case (CLAUDE.md §2.6): records already applied are
// skipped, so a repeated command changes nothing. A record whose fix cannot be
// applied (a manual fix, a missing product, a failed store write) stays open
// and is not marked.
func (s *syncService) handleRemediationsApplied(ctx context.Context, event eventbus.DomainEvent) error {
	rubric, err := LoadScoringRubric(ctx, s.log)
	if err != nil {
		return fmt.Errorf("load scoring_rubric: %w", err)
	}
	return s.handleRemediationsAppliedWith(ctx, event, rubric)
}

// handleRemediationsAppliedWith is handleRemediationsApplied against a given
// rubric.
func (s *syncService) handleRemediationsAppliedWith(ctx context.Context, event eventbus.DomainEvent, rubric *ScoringRubric) error {
	var payload commandbus.RemediationsAppliedPayload
	if err := json.Unmarshal(event.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal remediations_applied: %w", err)
	}

	storeID := event.StoreID()
	merchantID := event.MerchantID()
	if s.remediations == nil {
		return fmt.Errorf("apply remediations for %s: no remediation store configured", storeID)
	}

	records, err := s.remediations.GetByIDs(ctx, merchantID, storeID, payload.RemediationIDs)
	if err != nil {
		return fmt.Errorf("load remediations for %s: %w", storeID, err)
	}

	applied := 0
	for _, rec := range records {
		if rec.Applied {
			continue
		}
		ok, err := s.applyRemediation(ctx, merchantID, storeID, rec, rubric, payload.WriteBackID)
		if err != nil {
			return err
		}
		if ok {
			applied++
		}
	}

	s.log.Info("remediations applied", "store_id", storeID, "write_back_id", payload.WriteBackID,
		"requested", len(payload.RemediationIDs), "applied", applied, "actor", payload.Actor)
	return nil
}

// applyRemediation applies one record's fix and reports whether it took. A
// fix that cannot be applied leaves the record open (false, nil); only a
// failure to record the application is an error, so the command is retried.
//
// The record is marked applied before the product is re-scored: the re-score
// retires the records of resolved issues, and an applied record is history
// that must survive it.
func (s *syncService) applyRemediation(ctx context.Context, merchantID, storeID string, rec Remediation, rubric *ScoringRubric, writeBackID string) (bool, error) {
	log := s.log.With("remediation_id", rec.ID, "store_id", storeID, "sku", rec.SKU)

	remRule := rubric.FindRemediation(rec.RemediationRuleID)
	if rec.Type != "rule" || !remediationApplicable(remRule) {
		log.Warn("remediation is not rule-applicable; left open")
		return false, nil
	}
	product, err := s.catalogStore.GetProduct(ctx, storeID, rec.SKU)
	if err != nil || product == nil || product.MerchantID != merchantID {
		log.Warn("remediation product not found; left open", "error", err)
		return false, nil
	}

	field := remediationFields[remRule.Field]
	before := field.get(product)
	if err := ApplyRemediation(product, remRule); err != nil {
		log.Warn("remediation needs manual intervention; left open", "error", err)
		return false, nil
	}
	after := field.get(product)
	if after != before {
		if err := s.writeProductToStore(ctx, merchantID, storeID, rec.SKU, map[string]interface{}{field.storeKey: after}); err != nil {
			log.Error("remediation write-back to store failed; left open", "error", err)
			return false, nil
		}
	}

	if err := s.remediations.MarkApplied(ctx, merchantID, storeID, rec.ID, writeBackID, before); err != nil {
		return false, err
	}
	if err := s.runScoreStage(ctx, product, rubric, true); err != nil {
		log.Error("re-score after remediation failed", "error", err)
	}
	log.Info("remediation applied", "field", remRule.Field, "old_value", before, "new_value", after)
	return true, nil
}

// handleRemediationsReverted undoes a write-back (ucp.CommandRevertRemediations).
func (s *syncService) handleRemediationsReverted(ctx context.Context, event eventbus.DomainEvent) error {
	rubric, err := LoadScoringRubric(ctx, s.log)
	if err != nil {
		return fmt.Errorf("load scoring_rubric: %w", err)
	}
	return s.handleRemediationsRevertedWith(ctx, event, rubric)
}

// handleRemediationsRevertedWith is handleRemediationsReverted against a given
// rubric. Each record the write-back applied gets its field's prior value back
// — on the product and in the merchant's store — and is reopened; the product
// is then re-scored, so its issue is open again. Records the write-back did not
// apply (never applied, already reverted, or applied by another write-back) are
// left alone, so a redelivered command changes nothing.
func (s *syncService) handleRemediationsRevertedWith(ctx context.Context, event eventbus.DomainEvent, rubric *ScoringRubric) error {
	var payload commandbus.RemediationsRevertedPayload
	if err := json.Unmarshal(event.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal remediations_reverted: %w", err)
	}
	storeID := event.StoreID()
	merchantID := event.MerchantID()
	if s.remediations == nil {
		return fmt.Errorf("revert remediations for %s: no remediation store configured", storeID)
	}

	records, err := s.remediations.GetByIDs(ctx, merchantID, storeID, payload.RemediationIDs)
	if err != nil {
		return fmt.Errorf("load remediations for %s: %w", storeID, err)
	}
	reverted := 0
	for _, rec := range records {
		if !rec.Applied || rec.WriteBackID != payload.WriteBackID {
			continue
		}
		if err := s.revertRemediation(ctx, merchantID, storeID, rec, rubric, payload.WriteBackID); err != nil {
			return err
		}
		reverted++
	}
	s.log.Info("remediations reverted", "store_id", storeID, "write_back_id", payload.WriteBackID,
		"requested", len(payload.RemediationIDs), "reverted", reverted, "actor", payload.Actor)
	return nil
}

// revertRemediation restores one applied record's prior value. Unlike an
// apply, a failure here is an error, so the command is retried (and
// dead-lettered with an alert if it keeps failing): the merchant asked for the
// change to be undone, and leaving it in place silently is not an outcome.
func (s *syncService) revertRemediation(ctx context.Context, merchantID, storeID string, rec Remediation, rubric *ScoringRubric, writeBackID string) error {
	product, err := s.catalogStore.GetProduct(ctx, storeID, rec.SKU)
	if err != nil || product == nil || product.MerchantID != merchantID {
		return fmt.Errorf("revert remediation %s: product %s not found: %w", rec.ID, rec.SKU, err)
	}
	if field, ok := remediationFields[rec.Field]; ok {
		if field.get(product) != rec.FromValue {
			if err := s.writeProductToStore(ctx, merchantID, storeID, rec.SKU, map[string]interface{}{field.storeKey: rec.FromValue}); err != nil {
				return fmt.Errorf("revert remediation %s: write back to store: %w", rec.ID, err)
			}
			field.set(product, rec.FromValue)
			if err := s.catalogStore.UpsertProduct(ctx, product); err != nil {
				return fmt.Errorf("revert remediation %s: save product: %w", rec.ID, err)
			}
		}
	} else {
		// A record applied before write-back paths were tied to real fields
		// changed only the mapped attribute (nothing reached the store).
		if product.MappedFields == nil {
			product.MappedFields = map[string]string{}
		}
		if product.MappedFields[rec.Field] != rec.FromValue {
			if rec.FromValue == "" {
				delete(product.MappedFields, rec.Field)
			} else {
				product.MappedFields[rec.Field] = rec.FromValue
			}
			if err := s.catalogStore.UpsertProduct(ctx, product); err != nil {
				return fmt.Errorf("revert remediation %s: save product: %w", rec.ID, err)
			}
		}
	}
	if err := s.remediations.MarkReverted(ctx, merchantID, storeID, rec.ID, writeBackID); err != nil {
		return err
	}
	if err := s.runScoreStage(ctx, product, rubric, true); err != nil {
		s.log.Error("re-score after revert failed", "remediation_id", rec.ID, "store_id", storeID, "error", err)
	}
	s.log.Info("remediation reverted", "remediation_id", rec.ID, "store_id", storeID, "sku", rec.SKU, "field", rec.Field)
	return nil
}

func (s *syncService) handleServingRebuildRequested(ctx context.Context, event eventbus.DomainEvent) error {
	storeID := event.StoreID()
	merchantID := event.MerchantID()
	s.log.Info("serving rebuild requested", "store_id", storeID)

	// Parse rebuild request payload
	var payload map[string]any
	if err := json.Unmarshal(event.Payload(), &payload); err != nil {
		s.log.Error("failed to unmarshal rebuild request", "error", err, "store_id", storeID)
		return fmt.Errorf("unmarshal rebuild request: %w", err)
	}

	// Build product snapshot from the catalog store (partitioned by store_id).
	// Serving plane will add profile and other metadata to create final cache snapshot
	productSnapshot, err := s.snapshotBuilder.BuildProductSnapshot(ctx, storeID, nil)
	if err != nil {
		s.log.Error("failed to build product snapshot", "error", err, "store_id", storeID)
		return fmt.Errorf("build product snapshot: %w", err)
	}

	s.log.Info("product snapshot built",
		"store_id", storeID,
		"product_count", len(productSnapshot.Products),
		"source_sync", productSnapshot.SourceSync)

	// Emit catalog.products_ready for serving plane to orchestrate full rebuild
	// Serving plane (CacheRebuilder) will:
	// 1. Get store profile from control plane
	// 2. Call ProfileRenderer with catalogue
	// 3. Assemble final CacheSnapshot (products + profile + shipping)
	// 4. Swap into serving cache
	productsReadyBytes, _ := json.Marshal(map[string]any{
		"merchant_id":   merchantID,
		"store_id":      storeID,
		"product_count": len(productSnapshot.Products),
		"source_sync":   productSnapshot.SourceSync,
	})
	if err := s.bus.Publish(ctx, newDomainEvent(ucp.EventCatalogProductsReady, storeID, merchantID, productsReadyBytes)); err != nil {
		s.log.Error("failed to publish catalog.products_ready", "error", err, "store_id", storeID)
	}

	return nil
}

// writeProductToStore writes a remediated product back to the merchant's store via the connector.
func (s *syncService) writeProductToStore(ctx context.Context, merchantID string, storeID string, sku string, updates map[string]interface{}) error {
	instance, err := s.provider(merchantID, storeID)
	if err != nil {
		return fmt.Errorf("get store provider: %w", err)
	}
	connector, err := s.registry.Get(instance)
	if err != nil {
		return fmt.Errorf("get connector: %w", err)
	}

	return connector.UpdateProduct(ctx, storeID, sku, updates)
}
