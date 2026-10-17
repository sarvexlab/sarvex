// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/connectors/base"
)

// fakeStoreConnector is a test double for base.Connector; only FetchCatalog
// is exercised by the catalog import handler (the other methods belong to
// checkout/ingestion/order-handoff, out of scope here — the embedded nil
// base.Connector satisfies the rest of the interface).
type fakeStoreConnector struct {
	base.Connector
	entities          []base.CatalogEntity
	fetchErr          error
	failTimes         int // fetchErr != nil: fail the first N calls, then succeed; 0 means always fail
	fetchCatalogCalls int
	// onFetch, if set, runs synchronously at the top of every FetchCatalog
	// call — used to simulate another actor (e.g. a superseding
	// HandleCommand) mutating shared state mid-fetch.
	onFetch func()
}

func (f *fakeStoreConnector) FetchCatalog(_ context.Context, _ string, _ int) ([]base.CatalogEntity, error) {
	f.fetchCatalogCalls++
	if f.onFetch != nil {
		f.onFetch()
	}
	if f.fetchErr != nil && (f.failTimes == 0 || f.fetchCatalogCalls <= f.failTimes) {
		return nil, f.fetchErr
	}
	out := make([]base.CatalogEntity, len(f.entities))
	copy(out, f.entities)
	return out, nil
}

var _ base.Connector = (*fakeStoreConnector)(nil)

// countingCatalogStore wraps fakeCatalogStore to count UpsertProduct calls,
// so tests can assert exactly how many writes a resumed run performed.
type countingCatalogStore struct {
	*fakeCatalogStore
	upsertCalls int
}

func (c *countingCatalogStore) UpsertProduct(ctx context.Context, product *CatalogProduct) error {
	c.upsertCalls++
	return c.fakeCatalogStore.UpsertProduct(ctx, product)
}

// failAfterCatalogStore wraps fakeCatalogStore, failing UpsertProduct once
// the given number of successful upserts have already happened. Used to
// simulate a store-write failure partway through an import.
type failAfterCatalogStore struct {
	*fakeCatalogStore
	failAfter int
	succeeded int
}

func (f *failAfterCatalogStore) UpsertProduct(ctx context.Context, product *CatalogProduct) error {
	if f.succeeded >= f.failAfter {
		return errors.New("simulated store write failure")
	}
	f.succeeded++
	return f.fakeCatalogStore.UpsertProduct(ctx, product)
}

// fakeImportJobRepo is an in-memory ImportJobRepository.
type fakeImportJobRepo struct {
	jobs map[string]*ImportJob
}

func newFakeImportJobRepo() *fakeImportJobRepo {
	return &fakeImportJobRepo{jobs: make(map[string]*ImportJob)}
}

func (r *fakeImportJobRepo) Get(_ context.Context, storeID string) (*ImportJob, error) {
	job, ok := r.jobs[storeID]
	if !ok {
		return nil, ErrImportJobNotFound
	}
	cp := *job
	return &cp, nil
}

func (r *fakeImportJobRepo) Upsert(_ context.Context, job *ImportJob) error {
	cp := *job
	r.jobs[job.StoreID] = &cp
	return nil
}

func (r *fakeImportJobRepo) ListByStatus(_ context.Context, status ImportStatus) ([]*ImportJob, error) {
	var out []*ImportJob
	for _, j := range r.jobs {
		if j.Status == status {
			cp := *j
			out = append(out, &cp)
		}
	}
	return out, nil
}

var _ ImportJobRepository = (*fakeImportJobRepo)(nil)

// fakeImportEnqueuer records EnqueueImport/ReleaseImportLock/
// CancelActiveImport calls for assertions. Mimics *JobServer's real
// behavior enough to exercise the stale-lock bug: once a store's lock is
// "held" (EnqueueImport called and not yet released), further EnqueueImport
// calls for the same store are silently swallowed (mirrors asynq's
// Unique-TTL dedup via TaskID), exactly as ErrTaskIDConflict is swallowed
// in the real EnqueueImport. Only ReleaseImportLock/CancelActiveImport
// clear it — mirroring *JobServer.waitForInactiveAndDelete, which backs
// both.
type fakeImportEnqueuer struct {
	calls        []CatalogImportCommandPayload
	releaseCalls []string
	cancelCalls  []string
	locked       map[string]bool
}

func (f *fakeImportEnqueuer) EnqueueImport(_ context.Context, payload CatalogImportCommandPayload) error {
	if f.locked == nil {
		f.locked = map[string]bool{}
	}
	if f.locked[payload.StoreID] {
		return nil // real EnqueueImport swallows ErrTaskIDConflict the same way
	}
	f.locked[payload.StoreID] = true
	f.calls = append(f.calls, payload)
	return nil
}

func (f *fakeImportEnqueuer) ReleaseImportLock(_ context.Context, storeID string) error {
	f.releaseCalls = append(f.releaseCalls, storeID)
	if f.locked != nil {
		delete(f.locked, storeID)
	}
	return nil
}

func (f *fakeImportEnqueuer) CancelActiveImport(_ context.Context, storeID string) error {
	f.cancelCalls = append(f.cancelCalls, storeID)
	if f.locked != nil {
		delete(f.locked, storeID)
	}
	return nil
}

func testEntities(n int) []base.CatalogEntity {
	entities := make([]base.CatalogEntity, n)
	for i := 0; i < n; i++ {
		entities[i] = base.CatalogEntity{
			SKU:        fmt.Sprintf("SKU-%03d", i),
			Name:       fmt.Sprintf("Product %d", i),
			PriceMinor: int64(1000 + i),
			Currency:   "USD",
			InStock:    true,
		}
	}
	return entities
}

func testImportLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

// TestCatalogImportHandler_RunImport_PagesUpsertsAndCompletes verifies the
// core paging loop: every fetched product lands in the catalog store in
// batches, the cursor advances, and completion is reported exactly once.
func TestCatalogImportHandler_RunImport_PagesUpsertsAndCompletes(t *testing.T) {
	jobRepo := newFakeImportJobRepo()
	catalogStore := newFakeCatalogStore()
	connector := &fakeStoreConnector{entities: testEntities(5)}
	eventBus := newFakeEventBus()
	reg, provider := newTestRegistry("store-a", connector)
	scorer := newFakeProductRescorer()
	handler := NewCatalogImportHandler(jobRepo, catalogStore, reg, provider, scorer, eventBus, testImportLogger(), 2) // batches: [0,1] [2,3] [4]

	ctx := context.Background()
	now := time.Now().UTC()
	if err := jobRepo.Upsert(ctx, &ImportJob{
		StoreID: "store-a", MerchantID: "merch-a", Reason: "initial",
		IdempotencyKey: "key-1", Status: ImportStatusRunning, StartedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	if err := handler.RunImport(ctx, "store-a"); err != nil {
		t.Fatalf("RunImport failed: %v", err)
	}

	products, _, err := catalogStore.ListByStore(ctx, "store-a", 100, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(products) != 5 {
		t.Fatalf("expected 5 products written, got %d", len(products))
	}

	job, err := jobRepo.Get(ctx, "store-a")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != ImportStatusCompleted {
		t.Errorf("expected status completed, got %s", job.Status)
	}
	if job.Counts.Upserted != 5 {
		t.Errorf("expected 5 upserted, got %d", job.Counts.Upserted)
	}
	if job.Cursor != "SKU-004" {
		t.Errorf("expected cursor at last SKU, got %q", job.Cursor)
	}
	if job.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}

	if len(scorer.rescored) != 5 {
		t.Errorf("expected every imported product to be rescored, got %d rescore calls", len(scorer.rescored))
	}

	var progressCount, completionCount, rebuildCount int
	for _, e := range eventBus.published {
		switch e.EventType() {
		case "sync.catalog.import_progress":
			progressCount++
		case ucp.EventJourneySignal:
			completionCount++
			var payload JourneySignalPayload
			if err := json.Unmarshal(e.Payload(), &payload); err != nil {
				t.Fatalf("decode journey signal: %v", err)
			}
			if payload.Signal != "first_sync_complete" {
				t.Errorf("expected first_sync_complete signal, got %q", payload.Signal)
			}
			if e.StoreID() != "store-a" {
				t.Errorf("expected journey signal keyed by store-a, got %q", e.StoreID())
			}
		case ucp.EventRebuildRequested:
			rebuildCount++
			if e.StoreID() != "store-a" {
				t.Errorf("expected rebuild request keyed by store-a, got %q", e.StoreID())
			}
		}
	}
	if progressCount != 3 {
		t.Errorf("expected 3 progress events (one per batch), got %d", progressCount)
	}
	if completionCount != 1 {
		t.Errorf("expected exactly 1 completion signal, got %d", completionCount)
	}
	// Without this, a store's presence/manifest stay empty until an operator
	// manually rebuilds or a steady-state signal happens to fire — neither
	// guaranteed after a fresh import.
	if rebuildCount != 1 {
		t.Errorf("expected exactly 1 serving cache rebuild request after import completion, got %d", rebuildCount)
	}
}

// TestCatalogImportHandler_RunImport_DefersReadinessSignalUntilAfterCompletion
// guards against the journey-stranding race this handler used to have: each
// per-product score during the loop used to recompute the store-level
// readiness aggregate and could fire readiness_threshold_met against an
// incomplete product set — before first_sync_complete published, while the
// journey was still "connected" (a state with no transition for that
// signal, so it was silently and permanently dropped there; see
// mapping.go's RescoreProductBatch doc comment). This asserts the import
// loop now scores every product via RescoreProductBatch (no per-product
// aggregate recompute) and calls FinalizeBatchReadiness exactly once, after
// first_sync_complete is already on the bus.
func TestCatalogImportHandler_RunImport_DefersReadinessSignalUntilAfterCompletion(t *testing.T) {
	jobRepo := newFakeImportJobRepo()
	catalogStore := newFakeCatalogStore()
	connector := &fakeStoreConnector{entities: testEntities(5)}
	eventBus := newFakeEventBus()
	reg, provider := newTestRegistry("store-a", connector)
	scorer := newFakeProductRescorer()
	scorer.bus = eventBus
	handler := NewCatalogImportHandler(jobRepo, catalogStore, reg, provider, scorer, eventBus, testImportLogger(), 2)

	ctx := context.Background()
	now := time.Now().UTC()
	if err := jobRepo.Upsert(ctx, &ImportJob{
		StoreID: "store-a", MerchantID: "merch-a", Reason: "initial",
		IdempotencyKey: "key-1", Status: ImportStatusRunning, StartedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	if err := handler.RunImport(ctx, "store-a"); err != nil {
		t.Fatalf("RunImport failed: %v", err)
	}

	if len(scorer.finalized) != 1 || scorer.finalized[0] != "store-a" {
		t.Fatalf("expected FinalizeBatchReadiness called exactly once for store-a, got %v", scorer.finalized)
	}

	firstSyncCompleteIdx := -1
	for i, e := range eventBus.published {
		if e.EventType() != ucp.EventJourneySignal {
			continue
		}
		var payload JourneySignalPayload
		if err := json.Unmarshal(e.Payload(), &payload); err != nil {
			t.Fatalf("decode journey signal: %v", err)
		}
		if payload.Signal == "first_sync_complete" {
			firstSyncCompleteIdx = i
		}
	}
	if firstSyncCompleteIdx < 0 {
		t.Fatal("expected first_sync_complete to be published")
	}

	if len(scorer.finalizedAtBusLen) != 1 {
		t.Fatalf("expected exactly one recorded FinalizeBatchReadiness bus snapshot, got %v", scorer.finalizedAtBusLen)
	}
	if scorer.finalizedAtBusLen[0] <= firstSyncCompleteIdx {
		t.Errorf("expected FinalizeBatchReadiness to run after first_sync_complete was published (idx %d), but bus only had %d events at call time",
			firstSyncCompleteIdx, scorer.finalizedAtBusLen[0])
	}
}

// TestCatalogImportHandler_RunImport_ScoringFailureIsNonFatal verifies a
// scoring error for one product doesn't fail the whole import: the catalog
// write already succeeded, so the run must still complete (a later sweep or
// ingest signal re-derives the score).
func TestCatalogImportHandler_RunImport_ScoringFailureIsNonFatal(t *testing.T) {
	jobRepo := newFakeImportJobRepo()
	catalogStore := newFakeCatalogStore()
	connector := &fakeStoreConnector{entities: testEntities(3)}
	eventBus := newFakeEventBus()
	reg, provider := newTestRegistry("store-a", connector)
	scorer := newFakeProductRescorer()
	scorer.err = errors.New("simulated scoring failure")
	handler := NewCatalogImportHandler(jobRepo, catalogStore, reg, provider, scorer, eventBus, testImportLogger(), 10)

	ctx := context.Background()
	now := time.Now().UTC()
	if err := jobRepo.Upsert(ctx, &ImportJob{
		StoreID: "store-a", MerchantID: "merch-a", Reason: "initial",
		IdempotencyKey: "key-1", Status: ImportStatusRunning, StartedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	if err := handler.RunImport(ctx, "store-a"); err != nil {
		t.Fatalf("expected RunImport to succeed despite scoring failures, got: %v", err)
	}

	products, _, err := catalogStore.ListByStore(ctx, "store-a", 100, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(products) != 3 {
		t.Errorf("expected 3 products written despite scoring failures, got %d", len(products))
	}

	job, err := jobRepo.Get(ctx, "store-a")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != ImportStatusCompleted {
		t.Errorf("expected status completed, got %s", job.Status)
	}
}

// TestCatalogImportHandler_HandleCommand_DuplicateIdempotencyKeyIsNoOp
// verifies a re-sent command (same idempotency_key) enqueues exactly once:
// the second call sees the existing job with the same key and is a no-op.
func TestCatalogImportHandler_HandleCommand_DuplicateIdempotencyKeyIsNoOp(t *testing.T) {
	jobRepo := newFakeImportJobRepo()
	catalogStore := newFakeCatalogStore()
	connector := &fakeStoreConnector{entities: testEntities(3)}
	eventBus := newFakeEventBus()
	reg, provider := newTestRegistry("store-a", connector)
	scorer := newFakeProductRescorer()
	enqueuer := &fakeImportEnqueuer{}
	handler := NewCatalogImportHandler(jobRepo, catalogStore, reg, provider, scorer, eventBus, testImportLogger(), 10)
	handler.SetEnqueuer(enqueuer)

	payload, _ := json.Marshal(CatalogImportCommandPayload{StoreID: "store-a", Reason: "initial", IdempotencyKey: "key-1"})
	event := newDomainEvent("control.catalog.import", "store-a", "merch-a", payload)

	ctx := context.Background()
	if err := handler.HandleCommand(ctx, event); err != nil {
		t.Fatalf("first command failed: %v", err)
	}
	if err := handler.HandleCommand(ctx, event); err != nil {
		t.Fatalf("duplicate command failed: %v", err)
	}

	if len(enqueuer.calls) != 1 {
		t.Errorf("expected exactly 1 EnqueueImport call (duplicate must not re-enqueue), got %d", len(enqueuer.calls))
	}
}

// TestCatalogImportHandler_RunImport_FailureReleasesImportLockForRetry is a
// regression test for a real bug: EnqueueImport reserves a store's asynq
// TaskID under a 48h Unique TTL (job_server.go's importUniqueTTL) as a
// startup-race backstop, but asynq does not clear that reservation when the
// task fails — only on success or TTL expiry. Without failJob releasing it,
// a resync sent after a failed import (exactly the retry failJob's own doc
// comment promises works) would hit EnqueueImport, get silently swallowed as
// a duplicate of the already-dead task, and the store would be wedged with
// no way to ever successfully import again short of waiting out the TTL —
// while HandleCommand's own "already running" guard blocks every attempt
// after the first, since nothing ever flips the new job out of Running.
func TestCatalogImportHandler_RunImport_FailureReleasesImportLockForRetry(t *testing.T) {
	jobRepo := newFakeImportJobRepo()
	catalogStore := newFakeCatalogStore()
	connErr := &base.ConnectorError{Kind: base.ConnectorErrNotFound, StoreID: "store-a", Op: "FetchCatalog", Err: errors.New("store not seeded yet")}
	connector := &fakeStoreConnector{fetchErr: connErr}
	eventBus := newFakeEventBus()
	reg, provider := newTestRegistry("store-a", connector)
	scorer := newFakeProductRescorer()
	enqueuer := &fakeImportEnqueuer{}
	handler := NewCatalogImportHandler(jobRepo, catalogStore, reg, provider, scorer, eventBus, testImportLogger(), 10)
	handler.SetEnqueuer(enqueuer)
	handler.SetFetchRetryDelay([]time.Duration{0, 0})

	ctx := context.Background()

	firstPayload, _ := json.Marshal(CatalogImportCommandPayload{StoreID: "store-a", Reason: "initial", IdempotencyKey: "key-1"})
	firstEvent := newDomainEvent("control.catalog.import", "store-a", "merch-a", firstPayload)
	if err := handler.HandleCommand(ctx, firstEvent); err != nil {
		t.Fatalf("first command failed: %v", err)
	}
	if err := handler.RunImport(ctx, "store-a"); err == nil {
		t.Fatal("expected RunImport to fail (not_found connector error)")
	}

	job, err := jobRepo.Get(ctx, "store-a")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != ImportStatusFailed {
		t.Fatalf("expected status failed, got %s", job.Status)
	}
	if len(enqueuer.releaseCalls) != 1 || enqueuer.releaseCalls[0] != "store-a" {
		t.Fatalf("expected failJob to release the import lock for store-a, got releaseCalls=%v", enqueuer.releaseCalls)
	}

	// The retry failJob's doc comment promises: a fresh resync (new
	// idempotency_key) after the failure.
	resyncPayload, _ := json.Marshal(CatalogImportCommandPayload{StoreID: "store-a", Reason: "resync", IdempotencyKey: "key-2"})
	resyncEvent := newDomainEvent("control.catalog.import", "store-a", "merch-a", resyncPayload)
	if err := handler.HandleCommand(ctx, resyncEvent); err != nil {
		t.Fatalf("resync command failed: %v", err)
	}

	if len(enqueuer.calls) != 2 {
		t.Fatalf("expected the post-failure resync to actually enqueue (lock released), got %d EnqueueImport calls total", len(enqueuer.calls))
	}

	job, err = jobRepo.Get(ctx, "store-a")
	if err != nil {
		t.Fatalf("get job after resync: %v", err)
	}
	if job.Status != ImportStatusRunning {
		t.Fatalf("expected the resync job to be Running (ready for the asynq worker to pick up), got %s", job.Status)
	}
}

// TestCatalogImportHandler_HandleCommand_AcknowledgesImmediately verifies
// HandleCommand persists the job and enqueues the import task without
// performing any catalog work itself — the actual import runs in the asynq
// worker, not in the command handler.
func TestCatalogImportHandler_HandleCommand_AcknowledgesImmediately(t *testing.T) {
	jobRepo := newFakeImportJobRepo()
	catalogStore := newFakeCatalogStore()
	connector := &fakeStoreConnector{entities: testEntities(2)}
	eventBus := newFakeEventBus()
	reg, provider := newTestRegistry("store-a", connector)
	scorer := newFakeProductRescorer()
	enqueuer := &fakeImportEnqueuer{}
	handler := NewCatalogImportHandler(jobRepo, catalogStore, reg, provider, scorer, eventBus, testImportLogger(), 10)
	handler.SetEnqueuer(enqueuer)

	payload, _ := json.Marshal(CatalogImportCommandPayload{StoreID: "store-a", Reason: "initial", IdempotencyKey: "key-1"})
	event := newDomainEvent("control.catalog.import", "store-a", "merch-a", payload)

	if err := handler.HandleCommand(context.Background(), event); err != nil {
		t.Fatalf("HandleCommand returned error: %v", err)
	}

	if connector.fetchCatalogCalls != 0 {
		t.Error("HandleCommand must not call FetchCatalog — the actual import runs in the asynq worker")
	}
	if len(enqueuer.calls) != 1 {
		t.Errorf("expected exactly 1 EnqueueImport call, got %d", len(enqueuer.calls))
	}

	products, _, err := catalogStore.ListByStore(context.Background(), "store-a", 100, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(products) != 0 {
		t.Errorf("expected no products written by HandleCommand itself (import is async), got %d", len(products))
	}
}

// TestCatalogImportHandler_RunImport_FetchCatalogErrorFailsJobCleanly
// verifies a typed connector failure that persists across every retry
// attempt still stops and reports rather than completing partially/silently.
func TestCatalogImportHandler_RunImport_FetchCatalogErrorFailsJobCleanly(t *testing.T) {
	jobRepo := newFakeImportJobRepo()
	catalogStore := newFakeCatalogStore()
	connErr := &base.ConnectorError{Kind: base.ConnectorErrUnavailable, StoreID: "store-a", Op: "FetchCatalog", Err: errors.New("store unreachable")}
	connector := &fakeStoreConnector{fetchErr: connErr}
	eventBus := newFakeEventBus()
	reg, provider := newTestRegistry("store-a", connector)
	scorer := newFakeProductRescorer()
	handler := NewCatalogImportHandler(jobRepo, catalogStore, reg, provider, scorer, eventBus, testImportLogger(), 10)
	handler.SetFetchRetryDelay([]time.Duration{0, 0}) // exercise retries without waiting on real backoff

	ctx := context.Background()
	now := time.Now().UTC()
	if err := jobRepo.Upsert(ctx, &ImportJob{
		StoreID: "store-a", MerchantID: "merch-a", Reason: "initial",
		IdempotencyKey: "key-1", Status: ImportStatusRunning, StartedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	if err := handler.RunImport(ctx, "store-a"); err == nil {
		t.Fatal("expected RunImport to return the connector error")
	}

	if connector.fetchCatalogCalls != 3 {
		t.Errorf("expected 3 attempts (1 initial + 2 retries), got %d", connector.fetchCatalogCalls)
	}

	job, err := jobRepo.Get(ctx, "store-a")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != ImportStatusFailed {
		t.Errorf("expected status failed, got %s", job.Status)
	}
	if job.Error == "" {
		t.Error("expected job.Error to be populated")
	}

	products, _, _ := catalogStore.ListByStore(ctx, "store-a", 100, "")
	if len(products) != 0 {
		t.Errorf("expected no products written on a fetch failure, got %d", len(products))
	}

	var taskRaised bool
	for _, e := range eventBus.published {
		if e.EventType() == ucp.EventJourneySignal {
			t.Error("completion signal must not be emitted when the import failed")
		}
		if e.EventType() == ucp.EventSyncTaskRaised {
			taskRaised = true
		}
	}
	if !taskRaised {
		t.Error("expected a merchant task to be raised once retries were exhausted")
	}
}

// TestCatalogImportHandler_RunImport_RetriesTransientFetchFailureThenSucceeds
// verifies a retryable connector failure (kind != credentials) is retried
// and the import completes normally once the underlying issue clears.
func TestCatalogImportHandler_RunImport_RetriesTransientFetchFailureThenSucceeds(t *testing.T) {
	jobRepo := newFakeImportJobRepo()
	catalogStore := newFakeCatalogStore()
	connErr := &base.ConnectorError{Kind: base.ConnectorErrUnavailable, StoreID: "store-a", Op: "FetchCatalog", Err: errors.New("store unreachable")}
	connector := &fakeStoreConnector{entities: testEntities(3), fetchErr: connErr, failTimes: 2}
	eventBus := newFakeEventBus()
	reg, provider := newTestRegistry("store-a", connector)
	scorer := newFakeProductRescorer()
	handler := NewCatalogImportHandler(jobRepo, catalogStore, reg, provider, scorer, eventBus, testImportLogger(), 10)
	handler.SetFetchRetryDelay([]time.Duration{0, 0})

	ctx := context.Background()
	now := time.Now().UTC()
	if err := jobRepo.Upsert(ctx, &ImportJob{
		StoreID: "store-a", MerchantID: "merch-a", Reason: "initial",
		IdempotencyKey: "key-1", Status: ImportStatusRunning, StartedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	if err := handler.RunImport(ctx, "store-a"); err != nil {
		t.Fatalf("expected RunImport to succeed once the transient failure clears, got: %v", err)
	}

	if connector.fetchCatalogCalls != 3 {
		t.Errorf("expected 3 attempts (2 failures + 1 success), got %d", connector.fetchCatalogCalls)
	}

	job, err := jobRepo.Get(ctx, "store-a")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != ImportStatusCompleted {
		t.Errorf("expected status completed, got %s", job.Status)
	}

	for _, e := range eventBus.published {
		if e.EventType() != ucp.EventSyncTaskRaised {
			continue
		}
		var payload TaskRaisedPayload
		if err := json.Unmarshal(e.Payload(), &payload); err != nil {
			t.Fatalf("decode task.raised: %v", err)
		}
		// A successful run always emits resolve signals for any stale prior
		// failure task (safe no-op if none exists) — that's not "raising a
		// task". A genuine raise (resolved: false) would mean a failure task
		// was created despite the import ultimately succeeding.
		if !payload.Resolved {
			t.Error("no merchant task should be raised when the import ultimately succeeds")
		}
	}
}

// TestCatalogImportHandler_RunImport_CredentialFailureIsNotRetried verifies
// a credentials failure fails fast (no retry attempts) and raises a
// merchant task immediately — retrying can't fix a rejected API key, and
// delaying the task just leaves the merchant confused for longer.
func TestCatalogImportHandler_RunImport_CredentialFailureIsNotRetried(t *testing.T) {
	jobRepo := newFakeImportJobRepo()
	catalogStore := newFakeCatalogStore()
	connErr := &base.ConnectorError{Kind: base.ConnectorErrCredentials, StoreID: "store-a", Op: "FetchCatalog", Err: errors.New("invalid consumer key")}
	connector := &fakeStoreConnector{fetchErr: connErr}
	eventBus := newFakeEventBus()
	reg, provider := newTestRegistry("store-a", connector)
	scorer := newFakeProductRescorer()
	handler := NewCatalogImportHandler(jobRepo, catalogStore, reg, provider, scorer, eventBus, testImportLogger(), 10)
	handler.SetFetchRetryDelay([]time.Duration{0, 0})

	ctx := context.Background()
	now := time.Now().UTC()
	if err := jobRepo.Upsert(ctx, &ImportJob{
		StoreID: "store-a", MerchantID: "merch-a", Reason: "initial",
		IdempotencyKey: "key-1", Status: ImportStatusRunning, StartedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	if err := handler.RunImport(ctx, "store-a"); err == nil {
		t.Fatal("expected RunImport to return the connector error")
	}

	if connector.fetchCatalogCalls != 1 {
		t.Errorf("expected exactly 1 attempt (credentials failures are not retried), got %d", connector.fetchCatalogCalls)
	}

	var taskPayload *TaskRaisedPayload
	for _, e := range eventBus.published {
		if e.EventType() == ucp.EventSyncTaskRaised {
			var p TaskRaisedPayload
			if err := json.Unmarshal(e.Payload(), &p); err != nil {
				t.Fatalf("decode task.raised: %v", err)
			}
			taskPayload = &p
		}
	}
	if taskPayload == nil {
		t.Fatal("expected a merchant task to be raised")
	}
	if taskPayload.Source != "catalog_import" {
		t.Errorf("expected source catalog_import, got %q", taskPayload.Source)
	}
	if taskPayload.Owner != "merchant" {
		t.Errorf("expected owner merchant, got %q", taskPayload.Owner)
	}
	if !taskPayload.BlocksCheckout {
		t.Error("expected an initial-import failure to block checkout")
	}
}

// TestCatalogImportHandler_RunImport_ResumesFromPersistedCursor simulates
// what ResumeIncomplete finds after a process restart: a job left "running"
// with a cursor from the last committed batch. RunImport must continue from
// there rather than re-processing already-committed products.
func TestCatalogImportHandler_RunImport_ResumesFromPersistedCursor(t *testing.T) {
	jobRepo := newFakeImportJobRepo()
	base := newFakeCatalogStore()
	counting := &countingCatalogStore{fakeCatalogStore: base}
	connector := &fakeStoreConnector{entities: testEntities(5)}
	eventBus := newFakeEventBus()
	reg, provider := newTestRegistry("store-a", connector)
	scorer := newFakeProductRescorer()
	handler := NewCatalogImportHandler(jobRepo, counting, reg, provider, scorer, eventBus, testImportLogger(), 2)

	ctx := context.Background()
	// Pre-populate the catalog store and job state as if a prior process
	// instance had already committed the first batch (SKU-000..SKU-002)
	// before being killed.
	for _, sku := range []string{"SKU-000", "SKU-001", "SKU-002"} {
		if err := base.UpsertProduct(ctx, &CatalogProduct{StoreID: "store-a", MerchantID: "merch-a", SKU: sku, Title: "pre-existing"}); err != nil {
			t.Fatalf("seed product %s: %v", sku, err)
		}
	}
	now := time.Now().UTC()
	if err := jobRepo.Upsert(ctx, &ImportJob{
		StoreID: "store-a", MerchantID: "merch-a", Reason: "initial", IdempotencyKey: "key-1",
		Status: ImportStatusRunning, Cursor: "SKU-002", Counts: ImportCounts{Fetched: 5, Upserted: 3},
		StartedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	if err := handler.RunImport(ctx, "store-a"); err != nil {
		t.Fatalf("resume run failed: %v", err)
	}

	if counting.upsertCalls != 2 {
		t.Errorf("expected exactly 2 upserts on resume (not a full restart), got %d", counting.upsertCalls)
	}

	products, _, err := base.ListByStore(ctx, "store-a", 100, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(products) != 5 {
		t.Errorf("expected 5 total products after resume (no gaps), got %d", len(products))
	}

	job, err := jobRepo.Get(ctx, "store-a")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != ImportStatusCompleted {
		t.Errorf("expected completed status, got %s", job.Status)
	}
	if job.Counts.Upserted != 5 {
		t.Errorf("expected 5 total upserted count, got %d", job.Counts.Upserted)
	}
}

// TestCatalogImportHandler_RunImport_ResumeAfterFailureNoDuplicates covers
// the other resume path: a typed store-write failure partway through a
// batch, followed by a resumed run (as a resend/resync would trigger)
// picking up exactly where it left off with no duplicate rows.
func TestCatalogImportHandler_RunImport_ResumeAfterFailureNoDuplicates(t *testing.T) {
	jobRepo := newFakeImportJobRepo()
	base := newFakeCatalogStore()
	failing := &failAfterCatalogStore{fakeCatalogStore: base, failAfter: 3}
	connector := &fakeStoreConnector{entities: testEntities(5)}
	eventBus := newFakeEventBus()
	reg, provider := newTestRegistry("store-a", connector)
	scorer := newFakeProductRescorer()
	handler := NewCatalogImportHandler(jobRepo, failing, reg, provider, scorer, eventBus, testImportLogger(), 2)

	ctx := context.Background()
	now := time.Now().UTC()
	if err := jobRepo.Upsert(ctx, &ImportJob{
		StoreID: "store-a", MerchantID: "merch-a", Reason: "initial",
		IdempotencyKey: "key-1", Status: ImportStatusRunning, StartedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	if err := handler.RunImport(ctx, "store-a"); err == nil {
		t.Fatal("expected the run to fail partway through")
	}

	job, err := jobRepo.Get(ctx, "store-a")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != ImportStatusFailed {
		t.Fatalf("expected status failed after partial run, got %s", job.Status)
	}
	if job.Counts.Upserted != 3 {
		t.Fatalf("expected 3 products committed before the failure, got %d", job.Counts.Upserted)
	}

	// "Restart": the store write path recovers; re-run picks up from the
	// persisted cursor instead of starting over.
	failing.failAfter = 1000

	if err := handler.RunImport(ctx, "store-a"); err != nil {
		t.Fatalf("resumed run failed: %v", err)
	}

	products, _, err := base.ListByStore(ctx, "store-a", 100, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(products) != 5 {
		t.Errorf("expected 5 total products after resume, got %d", len(products))
	}
	seen := make(map[string]int)
	for _, p := range products {
		seen[p.SKU]++
	}
	for sku, count := range seen {
		if count != 1 {
			t.Errorf("expected exactly 1 record for %s, got %d", sku, count)
		}
	}

	job, err = jobRepo.Get(ctx, "store-a")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != ImportStatusCompleted {
		t.Errorf("expected completed status after resume, got %s", job.Status)
	}
}

// TestCatalogImportHandler_HandleCommand_SupersedesRunningImport verifies
// that a second command for a store with an import already running
// (different idempotency_key) supersedes it — best-effort cancelling the
// in-flight worker and proceeding with the new request — rather than being
// silently dropped. Dropping it (the previous behavior) left a store
// permanently stuck if its running attempt was wedged: every subsequent
// resync, including ones sent after whatever was blocking the store got
// fixed, silently did nothing. One-writer-per-store is still upheld here,
// just not by refusing the new command — see stillCurrent's doc comment
// (catalog_import.go) for how RunImport itself guarantees it.
func TestCatalogImportHandler_HandleCommand_SupersedesRunningImport(t *testing.T) {
	jobRepo := newFakeImportJobRepo()
	now := time.Now().UTC()
	if err := jobRepo.Upsert(context.Background(), &ImportJob{
		StoreID: "store-a", MerchantID: "merch-a", Reason: "initial",
		IdempotencyKey: "key-1", Status: ImportStatusRunning, StartedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	catalogStore := newFakeCatalogStore()
	connector := &fakeStoreConnector{entities: testEntities(2)}
	eventBus := newFakeEventBus()
	reg, provider := newTestRegistry("store-a", connector)
	scorer := newFakeProductRescorer()
	enqueuer := &fakeImportEnqueuer{}
	handler := NewCatalogImportHandler(jobRepo, catalogStore, reg, provider, scorer, eventBus, testImportLogger(), 10)
	handler.SetEnqueuer(enqueuer)

	payload, _ := json.Marshal(CatalogImportCommandPayload{StoreID: "store-a", Reason: "resync", IdempotencyKey: "key-2"})
	event := newDomainEvent("control.catalog.import", "store-a", "merch-a", payload)

	if err := handler.HandleCommand(context.Background(), event); err != nil {
		t.Fatalf("HandleCommand failed: %v", err)
	}

	if len(enqueuer.cancelCalls) != 1 || enqueuer.cancelCalls[0] != "store-a" {
		t.Errorf("expected CancelActiveImport to be called once for store-a, got %v", enqueuer.cancelCalls)
	}
	if len(enqueuer.calls) != 1 {
		t.Fatalf("expected the superseding command to actually be enqueued, got %d EnqueueImport calls", len(enqueuer.calls))
	}
	if enqueuer.calls[0].IdempotencyKey != "key-2" {
		t.Errorf("expected the enqueued payload to carry the new idempotency_key, got %q", enqueuer.calls[0].IdempotencyKey)
	}

	job, err := jobRepo.Get(context.Background(), "store-a")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.IdempotencyKey != "key-2" || job.Status != ImportStatusRunning {
		t.Errorf("expected the job doc to reflect the new attempt (key-2, running), got key=%q status=%s", job.IdempotencyKey, job.Status)
	}
}

// TestCatalogImportHandler_RunImport_StopsWhenSuperseded verifies
// RunImport's fencing check: if the job doc's idempotency_key no longer
// matches the one this run started with (HandleCommand's supersede branch
// changed it mid-run), RunImport stops without touching the job doc or
// raising a failure task — the run that superseded it owns that state now.
func TestCatalogImportHandler_RunImport_StopsWhenSuperseded(t *testing.T) {
	jobRepo := newFakeImportJobRepo()
	now := time.Now().UTC()
	if err := jobRepo.Upsert(context.Background(), &ImportJob{
		StoreID: "store-a", MerchantID: "merch-a", Reason: "initial",
		IdempotencyKey: "key-1", Status: ImportStatusRunning, StartedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	catalogStore := newFakeCatalogStore()
	// A connector whose FetchCatalog supersedes the job mid-call, mimicking
	// a new command landing while this run is still working — RunImport
	// should notice on its very next checkpoint and stop cleanly.
	connector := &fakeStoreConnector{
		entities: testEntities(4),
		onFetch: func() {
			if err := jobRepo.Upsert(context.Background(), &ImportJob{
				StoreID: "store-a", MerchantID: "merch-a", Reason: "resync",
				IdempotencyKey: "key-2", Status: ImportStatusRunning, StartedAt: now, UpdatedAt: now,
			}); err != nil {
				t.Fatalf("supersede job: %v", err)
			}
		},
	}
	eventBus := newFakeEventBus()
	reg, provider := newTestRegistry("store-a", connector)
	scorer := newFakeProductRescorer()
	handler := NewCatalogImportHandler(jobRepo, catalogStore, reg, provider, scorer, eventBus, testImportLogger(), 10)

	if err := handler.RunImport(context.Background(), "store-a"); err != nil {
		t.Fatalf("expected RunImport to return nil for a superseded run, got: %v", err)
	}

	job, err := jobRepo.Get(context.Background(), "store-a")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.IdempotencyKey != "key-2" || job.Status != ImportStatusRunning {
		t.Errorf("expected the superseding job (key-2, running) to be untouched, got key=%q status=%s", job.IdempotencyKey, job.Status)
	}
	if len(catalogStore.products) != 0 {
		t.Errorf("expected the superseded run to write no products, got %d", len(catalogStore.products))
	}
}

// TestCatalogImportHandler_StillCurrent_FailsClosedOnCanceledContext
// verifies a regression caught live against the dev stack: a canceled ctx
// passed to stillCurrent must report "not current" rather than failing
// open. CancelActiveImport cancels exactly the superseded run's context,
// so a canceled ctx reaching stillCurrent (e.g. from failJob, after
// fetchCatalogWithRetry's wait was interrupted by that cancellation) is
// itself the signal that this run has been replaced — treating it as an
// ordinary lookup hiccup (fail open) would let the superseded run race the
// run that replaced it to persist stale job state.
func TestCatalogImportHandler_StillCurrent_FailsClosedOnCanceledContext(t *testing.T) {
	jobRepo := newFakeImportJobRepo()
	now := time.Now().UTC()
	if err := jobRepo.Upsert(context.Background(), &ImportJob{
		StoreID: "store-a", MerchantID: "merch-a", Reason: "initial",
		IdempotencyKey: "key-1", Status: ImportStatusRunning, StartedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	handler := NewCatalogImportHandler(jobRepo, newFakeCatalogStore(), nil, nil, nil, newFakeEventBus(), testImportLogger(), 10)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// key-1 is genuinely still the current attempt in the job repo — a
	// plain lookup would say "still current". The canceled context must
	// override that.
	if got := handler.stillCurrent(ctx, "store-a", "key-1"); got {
		t.Errorf("expected stillCurrent to fail closed (false) on a canceled context, got true")
	}
}

// TestCatalogImportHandler_RunImport_SuccessResolvesStaleFailureTasks verifies
// that a successful import publishes resolved:true task_raised events for
// both possible catalog_import task keys ("initial" and "resync"), so a
// stale open failure task from an earlier attempt (raised under whichever
// reason actually failed) gets closed once import starts working again.
func TestCatalogImportHandler_RunImport_SuccessResolvesStaleFailureTasks(t *testing.T) {
	jobRepo := newFakeImportJobRepo()
	catalogStore := newFakeCatalogStore()
	connector := &fakeStoreConnector{entities: testEntities(2)}
	eventBus := newFakeEventBus()
	reg, provider := newTestRegistry("store-a", connector)
	scorer := newFakeProductRescorer()
	handler := NewCatalogImportHandler(jobRepo, catalogStore, reg, provider, scorer, eventBus, testImportLogger(), 10)

	ctx := context.Background()
	now := time.Now().UTC()
	if err := jobRepo.Upsert(ctx, &ImportJob{
		StoreID: "store-a", MerchantID: "merch-a", Reason: "resync",
		IdempotencyKey: "key-1", Status: ImportStatusRunning, StartedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	if err := handler.RunImport(ctx, "store-a"); err != nil {
		t.Fatalf("RunImport failed: %v", err)
	}

	var resolved []TaskRaisedPayload
	for _, e := range eventBus.published {
		if e.EventType() != ucp.EventSyncTaskRaised {
			continue
		}
		var p TaskRaisedPayload
		if err := json.Unmarshal(e.Payload(), &p); err != nil {
			t.Fatalf("decode task.raised: %v", err)
		}
		resolved = append(resolved, p)
	}

	if len(resolved) != 2 {
		t.Fatalf("expected 2 resolve events (initial + resync), got %d: %+v", len(resolved), resolved)
	}

	bySourceRef := make(map[string]TaskRaisedPayload, len(resolved))
	for _, p := range resolved {
		bySourceRef[p.SourceRef] = p
	}
	for _, reason := range []string{"initial", "resync"} {
		p, ok := bySourceRef[reason]
		if !ok {
			t.Fatalf("expected a resolve event for source_ref %q", reason)
		}
		if p.Source != "catalog_import" {
			t.Errorf("expected source=catalog_import for %q, got %q", reason, p.Source)
		}
		if !p.Resolved {
			t.Errorf("expected resolved=true for %q", reason)
		}
	}
}
