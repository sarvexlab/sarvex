// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
)

const (
	taskSweepCycle    = "sync:sweep:cycle"
	taskSweepStore    = "sync:sweep:store"
	taskCatalogImport = "sync:catalog:import"

	// asynqQueue is the single asynq queue all sync-plane tasks run in.
	asynqQueue = "sync"

	// asynqWorkerConcurrency is the number of tasks the asynq server
	// processes concurrently. Sweep tasks acquire a per-store Redis lease
	// internally, so more concurrency here just means faster store coverage
	// within a sweep cycle.
	asynqWorkerConcurrency = 5

	// importUniqueTTL must comfortably exceed the longest plausible catalog
	// import run. It's the outer guard against two instances racing to enqueue
	// a second import for the same store — the MongoDB "running" check is the
	// primary guard; asynq deduplication is the startup-race backstop.
	importUniqueTTL = 48 * time.Hour

	// importLockPollBudget/Interval bound how long waitForInactiveAndDelete
	// waits for an import task to leave asynq's "active" state before
	// giving up on clearing its Unique-TTL reservation. Both
	// CancelActiveImport (superseding a still-running import) and
	// ReleaseImportLock (releasing after a natural failure, from a
	// background goroutine) go through this. Generous relative to how
	// quickly a task actually leaves "active" — near-instant for
	// ReleaseImportLock, since its caller returns right after invoking it;
	// well under a second for CancelActiveImport in practice, since
	// RunImport's fencing check (stillCurrent, catalog_import.go) reacts
	// quickly — without stalling either caller for long on a slow case.
	importLockPollBudget   = 3 * time.Second
	importLockPollInterval = 100 * time.Millisecond
)

// JobServer replaces four previous in-process background mechanisms with a
// single asynq-backed distributed job queue, moving all scheduling state into
// Redis so that every sync-plane instance is stateless with respect to which
// jobs run:
//
//   - reconciliation sweep — was SweepScheduler (leader-election lease + in-process tickers)
//   - feed safety-net      — was raw go func ticker (unguarded: N instances = N× runs/hour)
//   - feed debounce regen  — was time.AfterFunc map (unguarded: N× per burst)
//   - catalog import       — was goroutine-per-job (startup race in ResumeIncomplete)
//
// All four are now correct under horizontal scaling without additional Redis
// locks beyond the per-store sweep-overlap guard that already lives in
// SweepHandler.
type JobServer struct {
	server    *asynq.Server
	scheduler *asynq.Scheduler
	inspector *asynq.Inspector
	client    *asynq.Client

	sweepHandler  *SweepHandler
	importHandler *CatalogImportHandler
	catalogStore  CatalogStore
	log           *slog.Logger
	sweepInterval time.Duration
}

// NewJobServer constructs a JobServer.
func NewJobServer(
	redisConnOpt asynq.RedisConnOpt,
	sweepHandler *SweepHandler,
	importHandler *CatalogImportHandler,
	catalogStore CatalogStore,
	sweepInterval time.Duration,
	log *slog.Logger,
) *JobServer {
	if sweepInterval <= 0 {
		sweepInterval = defaultSweepInterval
	}
	return &JobServer{
		server:        asynq.NewServer(redisConnOpt, asynq.Config{Concurrency: asynqWorkerConcurrency, Queues: map[string]int{asynqQueue: 1}}),
		scheduler:     asynq.NewScheduler(redisConnOpt, nil),
		inspector:     asynq.NewInspector(redisConnOpt),
		client:        asynq.NewClient(redisConnOpt),
		sweepHandler:  sweepHandler,
		importHandler: importHandler,
		catalogStore:  catalogStore,
		log:           log,
		sweepInterval: sweepInterval,
	}
}

// Start registers task handlers on the asynq server and starts both the
// server (worker pool) and the scheduler (periodic emission).
func (s *JobServer) Start() error {
	mux := asynq.NewServeMux()
	mux.HandleFunc(taskSweepCycle, s.handleSweepCycle)
	mux.HandleFunc(taskSweepStore, s.handleSweepStore)
	mux.HandleFunc(taskCatalogImport, s.handleCatalogImport)

	if err := s.server.Start(mux); err != nil {
		return fmt.Errorf("asynq server start: %w", err)
	}

	sweepCronSpec := "@every " + s.sweepInterval.String()
	if _, err := s.scheduler.Register(sweepCronSpec,
		asynq.NewTask(taskSweepCycle, nil),
		asynq.Queue(asynqQueue),
		asynq.TaskID("sweep:cycle"),
		asynq.Unique(s.sweepInterval),
	); err != nil {
		s.server.Shutdown()
		return fmt.Errorf("register sweep cycle: %w", err)
	}

	if err := s.scheduler.Start(); err != nil {
		s.server.Shutdown()
		return fmt.Errorf("asynq scheduler start: %w", err)
	}

	return nil
}

// Stop gracefully shuts down the scheduler (stops new emissions) then the
// server (drains in-progress tasks).
func (s *JobServer) Stop() {
	s.scheduler.Shutdown()
	s.server.Shutdown()
	if err := s.client.Close(); err != nil {
		s.log.Error("asynq client close", "error", err)
	}
}

// ── importEnqueuer implementation ──────────────────────────────────────────

// EnqueueImport implements importEnqueuer. asynq.TaskID provides an atomic
// cross-instance guard so two instances racing at startup cannot both spawn
// an import for the same store. The MongoDB "running" check in HandleCommand
// remains the primary guard; this is the distributed backstop that eliminates
// the ResumeIncomplete startup-race.
func (s *JobServer) EnqueueImport(ctx context.Context, payload CatalogImportCommandPayload) error {
	b, _ := json.Marshal(payload)
	_, err := s.client.Enqueue(
		asynq.NewTask(taskCatalogImport, b),
		asynq.Queue(asynqQueue),
		asynq.TaskID("catalog:import:"+payload.StoreID),
		asynq.Unique(importUniqueTTL),
		asynq.MaxRetry(0),
	)
	if isDuplicateTask(err) {
		return nil
	}
	return err
}

// waitForInactiveAndDelete polls until taskID is no longer in asynq's
// "active" state (or the poll budget runs out), then deletes it — per
// asynq's deleteTaskCmd, deleting a task also clears the Unique-TTL
// reservation EnqueueImport placed on its TaskID, freeing the next
// EnqueueImport call for this store to actually enqueue instead of being
// silently treated as a duplicate of a task that's done. DeleteTask itself
// refuses to touch a task still "active" (still being processed) — the
// poll is what makes calling this from ReleaseImportLock safe (see its doc
// comment for why that case always starts out "active"). "not found" at
// either step is not an error — already cleared, or never enqueued.
func (s *JobServer) waitForInactiveAndDelete(taskID string) error {
	deadline := time.Now().Add(importLockPollBudget)
	for time.Now().Before(deadline) {
		info, err := s.inspector.GetTaskInfo(asynqQueue, taskID)
		if err != nil || info.State != asynq.TaskStateActive {
			break
		}
		time.Sleep(importLockPollInterval)
	}
	err := s.inspector.DeleteTask(asynqQueue, taskID)
	if err == nil || errors.Is(err, asynq.ErrTaskNotFound) {
		return nil
	}
	return err
}

// ReleaseImportLock implements importEnqueuer. failJob calls this from
// inside the very task handler goroutine that IS the task being released
// (RunImport, called synchronously from handleCatalogImport) — so at the
// moment of the call, asynq always still considers this task "active": it
// only stops once handleCatalogImport returns, which hasn't happened yet.
// A synchronous DeleteTask here would therefore always fail with
// FAILED_PRECONDITION ("cannot delete task in active state"), silently
// leaving the Unique-TTL reservation in place forever and permanently
// wedging every future resync for the store — this was a real bug, caught
// by exercising a real failed-then-resynced import against the dev stack,
// not a hypothetical.
//
// The fix: release from a short-lived background goroutine instead, which
// waits (via waitForInactiveAndDelete) for the task to actually leave
// "active" — normally near-instant, since this goroutine's caller
// (handleCatalogImport) returns right after invoking this — before
// deleting it. Fire-and-forget: logged on failure, not propagated, same as
// every caller of this method already treats its return value.
func (s *JobServer) ReleaseImportLock(_ context.Context, storeID string) error {
	taskID := "catalog:import:" + storeID
	go func() {
		if err := s.waitForInactiveAndDelete(taskID); err != nil {
			s.log.Error("catalog import: background lock release failed", "task_id", taskID, "error", err)
		}
	}()
	return nil
}

// CancelActiveImport implements importEnqueuer. Signals the worker (if any)
// currently processing storeID's import to stop, via asynq's cooperative
// task cancellation (Inspector.CancelProcessing broadcasts to whichever
// process holds it, so this works cross-instance same as EnqueueImport's
// Unique lock). Unlike ReleaseImportLock, the caller here (HandleCommand,
// handling a distinct superseding command) is never the task being
// cancelled, so it's safe to block waiting for it to actually clear before
// returning — the caller's very next EnqueueImport call depends on that
// having happened, or it would itself be silently treated as a duplicate of
// the run being superseded.
//
// Best-effort in both directions: cancellation is cooperative, so a worker
// stuck in a non-cancellable call may not vacate within the poll budget —
// the reservation is then left in place and the caller's enqueue may still
// no-op. Correctness never rests on this landing in time; it only bounds
// how much wasted work a superseded run does before RunImport's own
// fencing check (stillCurrent) makes it stop touching shared state
// regardless.
func (s *JobServer) CancelActiveImport(_ context.Context, storeID string) error {
	taskID := "catalog:import:" + storeID
	if err := s.inspector.CancelProcessing(taskID); err != nil && !errors.Is(err, asynq.ErrTaskNotFound) {
		return err
	}
	return s.waitForInactiveAndDelete(taskID)
}

// ── asynq task handlers ───────────────────────────────────────────────────

func (s *JobServer) handleSweepCycle(ctx context.Context, _ *asynq.Task) error {
	storeIDs, err := s.catalogStore.ListStoreIDs(ctx)
	if err != nil {
		return fmt.Errorf("sweep cycle: list store ids: %w", err)
	}
	for _, storeID := range storeIDs {
		b, _ := json.Marshal(map[string]string{"store_id": storeID})
		_, enqErr := s.client.Enqueue(
			asynq.NewTask(taskSweepStore, b),
			asynq.Queue(asynqQueue),
			asynq.TaskID("sweep:store:"+storeID),
			asynq.Unique(s.sweepInterval),
		)
		if enqErr != nil && !isDuplicateTask(enqErr) {
			s.log.Error("sweep cycle: failed to enqueue store sweep", "store_id", storeID, "error", enqErr)
		}
	}
	return nil
}

func (s *JobServer) handleSweepStore(ctx context.Context, t *asynq.Task) error {
	var p map[string]string
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("sweep store: unmarshal payload: %w", err)
	}
	storeID := p["store_id"]
	merchantID, err := s.merchantForStore(ctx, storeID)
	if err != nil {
		return err
	}
	return s.sweepHandler.RunSweep(ctx, storeID, merchantID)
}

// merchantForStore resolves the merchant that owns storeID from the store's
// import job, which control.catalog.import stamped with the merchant id when
// pairing completed. The sweep cycle only knows store ids; a sweep run
// without the merchant cannot write the products it adds (every catalog write
// needs the tenant key) and publishes discrepancies no merchant can see. A
// store with no import job cannot be attributed, so retrying would not help:
// the task is skipped rather than retried.
func (s *JobServer) merchantForStore(ctx context.Context, storeID string) (string, error) {
	job, err := s.importHandler.jobRepo.Get(ctx, storeID)
	if errors.Is(err, ErrImportJobNotFound) {
		return "", fmt.Errorf("sweep store %s: no import job to resolve its merchant: %w", storeID, asynq.SkipRetry)
	}
	if err != nil {
		return "", fmt.Errorf("sweep store %s: resolve merchant: %w", storeID, err)
	}
	if job.MerchantID == "" {
		return "", fmt.Errorf("sweep store %s: import job has no merchant_id: %w", storeID, asynq.SkipRetry)
	}
	return job.MerchantID, nil
}

func (s *JobServer) handleCatalogImport(ctx context.Context, t *asynq.Task) error {
	var payload CatalogImportCommandPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("catalog import: unmarshal payload: %w", err)
	}
	return s.importHandler.RunImport(ctx, payload.StoreID)
}

// isDuplicateTask reports whether err is an asynq deduplication rejection:
// either the task ID already exists in the queue, or the Unique TTL window
// is still active for the same task type+payload+queue combination.
func isDuplicateTask(err error) bool {
	return errors.Is(err, asynq.ErrTaskIDConflict) || errors.Is(err, asynq.ErrDuplicateTask)
}

// compile-time check: *JobServer satisfies the seam it implements.
var _ importEnqueuer = (*JobServer)(nil)
