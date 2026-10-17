// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/samber/lo"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/connectors/base"
)

// importEnqueuer is the seam HandleCommand uses to dispatch a catalog import
// task to the asynq job queue. Implemented by *JobServer.
type importEnqueuer interface {
	EnqueueImport(ctx context.Context, payload CatalogImportCommandPayload) error

	// ReleaseImportLock clears the asynq uniqueness lock EnqueueImport left
	// behind for storeID, so the next control.catalog.import command for this
	// store can actually be enqueued rather than being silently swallowed as
	// a duplicate. See failJob's call site for why this matters.
	ReleaseImportLock(ctx context.Context, storeID string) error

	// CancelActiveImport asks the worker currently processing storeID's
	// import (if any) to stop, and clears its asynq lock so a superseding
	// command's own EnqueueImport isn't swallowed as a duplicate of the run
	// being replaced. See HandleCommand's supersede branch.
	CancelActiveImport(ctx context.Context, storeID string) error
}

// defaultImportBatchSize is used both as the FetchCatalog page-size hint and
// as the number of products committed between progress checkpoints when the
// handler isn't given an explicit size.
const defaultImportBatchSize = 50

// defaultFetchRetryDelay backs off FetchCatalog failures that might clear on
// their own (store not yet reachable, rate limited, breaker open) — a
// couple of retries beyond the first attempt before giving up and raising a
// merchant task. len(delay)+1 is the total number of attempts. Credential
// failures are never retried regardless of this setting (see
// isRetryableFetchError): no amount of waiting fixes a bad API key.
var defaultFetchRetryDelay = []time.Duration{30 * time.Second, 2 * time.Minute}

// CatalogImportCommandPayload is the body of the control.catalog.import
// command: pull a store's full catalog, normalise it, and write it to the
// catalog store. See sarvex-v1-syncing-plane-async.yaml's note that
// imperative commands (source.resource.operation) are a separate contract
// from the fan-out events it specifies.
type CatalogImportCommandPayload struct {
	StoreID        string `json:"store_id"`
	Reason         string `json:"reason"` // "initial" | "resync"
	IdempotencyKey string `json:"idempotency_key"`
}

// CatalogImportProgressPayload reports products committed so far for a
// store's in-flight import.
type CatalogImportProgressPayload struct {
	StoreID  string `json:"store_id"`
	Fetched  int    `json:"fetched"`
	Upserted int    `json:"upserted"`
}

// CatalogImportHandler handles control.catalog.import: it is the one writer
// (via CatalogStore) that performs a store's initial/resync catalog pull,
// resumably and idempotently, per store_id (sync plane, architecture §4.5).
type CatalogImportHandler struct {
	jobRepo      ImportJobRepository
	catalogStore CatalogStore
	registry     *base.Registry
	provider     ucp.StoreProviderByStoreIDFunc
	scorer       productBatchRescorer
	eventBus     eventbus.EventBus
	log          *slog.Logger
	batchSize    int
	enqueuer     importEnqueuer

	fetchRetryDelay []time.Duration
}

// NewCatalogImportHandler constructs a CatalogImportHandler. batchSize <= 0
// uses defaultImportBatchSize. registry/provider resolve the per-store
// base.Connector used to pull the catalog (registry.Get(provider(...))),
// the same pattern service.go's writeProductToStore already uses for
// UpdateProduct. scorer re-derives each imported product's mapping and score
// (productRescorer, sweep_handler.go — normally the sync-plane's own
// *syncService) so initially-imported products get readiness/score/tasks
// exactly as steady-state ingestion signals and sweep corrections do.
func NewCatalogImportHandler(
	jobRepo ImportJobRepository,
	catalogStore CatalogStore,
	registry *base.Registry,
	provider ucp.StoreProviderByStoreIDFunc,
	scorer productBatchRescorer,
	eventBus eventbus.EventBus,
	log *slog.Logger,
	batchSize int,
) *CatalogImportHandler {
	if log == nil {
		log = slog.Default()
	}
	if batchSize <= 0 {
		batchSize = defaultImportBatchSize
	}
	return &CatalogImportHandler{
		jobRepo:         jobRepo,
		catalogStore:    catalogStore,
		registry:        registry,
		provider:        provider,
		scorer:          scorer,
		eventBus:        eventBus,
		log:             log,
		batchSize:       batchSize,
		fetchRetryDelay: defaultFetchRetryDelay,
	}
}

// SetEnqueuer wires the asynq-backed job enqueuer. Must be called before
// HandleCommand receives any events.
func (h *CatalogImportHandler) SetEnqueuer(e importEnqueuer) {
	h.enqueuer = e
}

// SetFetchRetryDelay overrides the backoff between FetchCatalog retry
// attempts (production default: defaultFetchRetryDelay). Tests use this to
// shrink backoff to near-zero so retry behavior can be exercised without
// waiting minutes.
func (h *CatalogImportHandler) SetFetchRetryDelay(delay []time.Duration) {
	h.fetchRetryDelay = delay
}

// HandleCommand is the control.catalog.import subscription entrypoint.
// It validates and dedupes the command, persists the initial job state, and
// acknowledges immediately: the actual pull runs in a background goroutine
// so the triggering call never blocks on a potentially long-running import.
func (h *CatalogImportHandler) HandleCommand(ctx context.Context, event eventbus.DomainEvent) error {
	var payload CatalogImportCommandPayload
	if err := json.Unmarshal(event.Payload(), &payload); err != nil {
		h.log.Error("catalog import: malformed command payload", "error", err)
		return nil // malformed command; dropping it is not retryable
	}

	if payload.StoreID == "" || payload.IdempotencyKey == "" {
		h.log.Error("catalog import: command missing store_id or idempotency_key", "store_id", payload.StoreID)
		return nil
	}
	if payload.Reason != "initial" && payload.Reason != "resync" {
		h.log.Error("catalog import: invalid reason", "store_id", payload.StoreID, "reason", payload.Reason)
		return nil
	}

	merchantID := event.MerchantID()
	if merchantID == "" {
		merchantID = payload.StoreID // V1: one store per merchant (simplified)
	}

	existing, err := h.jobRepo.Get(ctx, payload.StoreID)
	if err != nil && !errors.Is(err, ErrImportJobNotFound) {
		return fmt.Errorf("get existing import job: %w", err)
	}
	if err == nil {
		if existing.IdempotencyKey == payload.IdempotencyKey {
			// Exact re-send of the same command: no-op, regardless of
			// whether it's still running or already finished.
			h.log.Info("catalog import: duplicate command, no-op", "store_id", payload.StoreID, "idempotency_key", payload.IdempotencyKey, "status", existing.Status)
			return nil
		}
		if existing.Status == ImportStatusRunning {
			// A genuinely new command (different idempotency_key) for a
			// store that's still mid-import: this used to be silently
			// dropped to preserve one-writer-per-store, but that left a
			// store permanently stuck if its running attempt was wedged
			// (crashed mid-run, or just still working through its own
			// retry backoff against a store that wasn't reachable yet) —
			// every subsequent resync request, including ones sent after
			// whatever was blocking the store got fixed, silently did
			// nothing. Supersede it instead: best-effort ask the in-flight
			// worker to stop, then proceed exactly as a fresh import below.
			// One-writer-per-store is still upheld — not by refusing the
			// new request, but by RunImport's fencing check (stillCurrent),
			// which makes the superseded run stop touching shared state
			// (job doc, catalog store) as soon as it notices it's no
			// longer current, regardless of whether cancellation lands in
			// time.
			h.log.Info("catalog import: superseding in-flight import", "store_id", payload.StoreID, "old_idempotency_key", existing.IdempotencyKey, "new_idempotency_key", payload.IdempotencyKey)
			if h.enqueuer != nil {
				if err := h.enqueuer.CancelActiveImport(ctx, payload.StoreID); err != nil {
					h.log.Warn("catalog import: failed to cancel in-flight import", "store_id", payload.StoreID, "error", err)
				}
			}
		}
	}

	now := time.Now().UTC()
	job := &ImportJob{
		StoreID:        payload.StoreID,
		MerchantID:     merchantID,
		Reason:         payload.Reason,
		IdempotencyKey: payload.IdempotencyKey,
		Status:         ImportStatusRunning,
		StartedAt:      now,
		UpdatedAt:      now,
	}
	if err := h.jobRepo.Upsert(ctx, job); err != nil {
		return fmt.Errorf("create import job: %w", err)
	}

	if h.enqueuer != nil {
		if err := h.enqueuer.EnqueueImport(ctx, payload); err != nil {
			h.log.Error("catalog import: failed to enqueue import task", "store_id", payload.StoreID, "error", err)
		}
	}

	return nil
}

// RunImport drives the paging loop for one store's import: fetch the full
// catalog, normalise and upsert it in committed batches, persisting the
// cursor after each batch so a restart resumes rather than starting over.
// Exported so tests can drive it synchronously (HandleCommand/ResumeIncomplete
// only ever call it from a background goroutine).
func (h *CatalogImportHandler) RunImport(ctx context.Context, storeID string) error {
	job, err := h.jobRepo.Get(ctx, storeID)
	if err != nil {
		return fmt.Errorf("load import job: %w", err)
	}
	// myKey identifies which attempt this particular RunImport call is
	// driving. A later command (HandleCommand, given a fresh
	// idempotency_key) can supersede it while it's still running — see
	// stillCurrent, checked before every write below that touches state a
	// superseding attempt now owns.
	myKey := job.IdempotencyKey

	// A resumed run may find the job left in "failed" (a prior typed error)
	// or, after a process restart, still "running" from before the kill.
	// Normalise to "running" for the duration of this attempt so job state
	// reflects reality while it executes.
	if job.Status != ImportStatusRunning {
		job.Status = ImportStatusRunning
		job.Error = ""
		job.UpdatedAt = time.Now().UTC()
		if err := h.jobRepo.Upsert(ctx, job); err != nil {
			return fmt.Errorf("mark import running: %w", err)
		}
	}

	instance, err := h.provider(job.MerchantID, storeID)
	if err != nil {
		return h.failJob(ctx, job, myKey, fmt.Errorf("get store provider: %w", err))
	}
	connector, err := h.registry.Get(instance)
	if err != nil {
		return h.failJob(ctx, job, myKey, fmt.Errorf("get connector: %w", err))
	}

	entities, err := h.fetchCatalogWithRetry(ctx, connector, storeID)
	if err != nil {
		return h.failJob(ctx, job, myKey, err)
	}

	rubric, err := LoadScoringRubric(ctx, h.log)
	if err != nil {
		return h.failJob(ctx, job, myKey, fmt.Errorf("load scoring_rubric: %w", err))
	}

	job.Counts.Fetched = len(entities)

	startIdx := 0
	if job.Cursor != "" {
		startIdx = len(entities) // default: nothing left if cursor not found in a fresh fetch
		if i := slices.IndexFunc(entities, func(e base.CatalogEntity) bool { return e.SKU == job.Cursor }); i >= 0 {
			startIdx = i + 1
		}
	}

	for _, batch := range lo.Chunk(entities[startIdx:], h.batchSize) {
		if !h.stillCurrent(ctx, storeID, myKey) {
			h.log.Info("catalog import: superseded mid-run, stopping", "store_id", storeID)
			return nil
		}
		for _, e := range batch {
			product := catalogEntityToProduct(job.MerchantID, storeID, e)
			if err := h.catalogStore.UpsertProduct(ctx, product); err != nil {
				return h.failJob(ctx, job, myKey, fmt.Errorf("upsert product %s: %w", e.SKU, err))
			}
			if err := h.scorer.RescoreProductBatch(ctx, product, rubric); err != nil {
				// Non-fatal: the product is already committed to the catalog
				// store; failing the whole import over a scoring error would
				// throw away an otherwise-successful catalog write. A later
				// sweep or ingest signal re-derives the score.
				h.log.Error("catalog import: failed to score product", "store_id", storeID, "sku", e.SKU, "error", err)
			}
			job.Cursor = e.SKU
			job.Counts.Upserted++
		}

		job.UpdatedAt = time.Now().UTC()
		if err := h.jobRepo.Upsert(ctx, job); err != nil {
			return fmt.Errorf("persist import progress: %w", err)
		}
		h.emitProgress(ctx, job)
	}

	if !h.stillCurrent(ctx, storeID, myKey) {
		h.log.Info("catalog import: superseded before completion could be recorded, stopping", "store_id", storeID)
		return nil
	}
	completedAt := time.Now().UTC()
	job.Status = ImportStatusCompleted
	job.CompletedAt = &completedAt
	job.UpdatedAt = completedAt
	job.Error = ""
	if err := h.jobRepo.Upsert(ctx, job); err != nil {
		return fmt.Errorf("persist import completion: %w", err)
	}

	h.emitCompletion(ctx, job)
	h.finalizeReadiness(ctx, job)
	h.emitRebuildRequest(ctx, job)
	h.resolveImportFailedTasks(ctx, job)
	h.log.Info("catalog import: completed", "store_id", storeID, "fetched", job.Counts.Fetched, "upserted", job.Counts.Upserted)
	return nil
}

// stillCurrent reports whether myKey is still storeID's current import
// attempt, i.e. no later control.catalog.import command has superseded it
// (HandleCommand's supersede branch) since this RunImport call started.
// Checked before every write that touches state a superseding attempt now
// owns (job progress/failure/completion), so a superseded run can never
// clobber the run that replaced it — even if CancelActiveImport's
// cooperative cancellation didn't land in time, this is what actually
// upholds one-writer-per-store; cancellation only shortens how much
// redundant work the superseded run does before it notices and stops.
//
// This is a check-then-write, not an atomic compare-and-swap, so a second
// supersede landing in the narrow gap between this check and the following
// Upsert could still race — accepted here the same way the analogous asynq
// Unique-lock is documented as a backstop rather than a hard guarantee
// (job_server.go's importUniqueTTL): the realistic trigger is a human
// clicking resync, not concurrent automated callers.
//
// Fails open (true) on an ordinary lookup error: an infrastructure hiccup
// here shouldn't make an otherwise healthy run second-guess itself. A
// canceled/expired ctx is the one exception — CancelActiveImport cancels
// exactly this run's context as part of superseding it, so ctx.Err() being
// set here is itself the clearest possible signal that this run is the one
// being replaced. Treating that as "still current" (as a plain lookup
// failure would) let a canceled run's own failure-handling race the run
// that superseded it to persist stale state — caught live: RunImport's
// jobRepo.Get here failed with "context canceled" immediately after
// CancelActiveImport fired, and only happened to be harmless because the
// subsequent Upsert also inherited the same canceled ctx and failed too.
func (h *CatalogImportHandler) stillCurrent(ctx context.Context, storeID, myKey string) bool {
	if ctx.Err() != nil {
		return false
	}
	current, err := h.jobRepo.Get(ctx, storeID)
	if err != nil {
		return true
	}
	return current.IdempotencyKey == myKey
}

// failJob marks the job failed and persists it. FetchCatalog/Upsert errors
// are typed (base.ConnectorError, CatalogStore errors) and are not swallowed:
// the job stops and reports rather than partially completing silently. A
// resend of control.catalog.import (new idempotency_key) is how a full
// resync retry is triggered; the persisted cursor means that resend picks up
// from wherever this run got to, not from zero. A merchant-facing task is
// always raised here too (raiseImportFailedTask) — a failed import with no
// task leaves the merchant staring at an unexplained stuck onboarding.
//
// Releasing the asynq lock here is what makes that resend promise true.
// EnqueueImport reserves storeID's TaskID under a 48h Unique TTL as a
// startup-race backstop (job_server.go) — asynq does not clear that
// reservation when the task fails/archives, only on success or TTL expiry.
// Without releasing it here, every resync attempt after the first failure
// would hit ErrTaskIDConflict, which EnqueueImport treats as "already
// enqueued, nothing to do" and silently swallows — so the retry this
// failure explicitly promises would silently never run, and the store's
// job stays wedged in Running (from HandleCommand's next attempt) with no
// further logs, for up to 48 hours.
// myKey is checked via stillCurrent before anything is written: a
// superseded run (see HandleCommand's supersede branch and stillCurrent's
// doc comment) must not clobber the attempt that replaced it with its own
// stale failure state.
func (h *CatalogImportHandler) failJob(ctx context.Context, job *ImportJob, myKey string, cause error) error {
	if !h.stillCurrent(ctx, job.StoreID, myKey) {
		h.log.Info("catalog import: superseded before failure could be recorded, skipping", "store_id", job.StoreID)
		return nil
	}

	job.Status = ImportStatusFailed
	job.Error = cause.Error()
	job.UpdatedAt = time.Now().UTC()

	var connErr *base.ConnectorError
	if errors.As(cause, &connErr) {
		h.log.Error("catalog import: store-API failure", "store_id", job.StoreID, "kind", connErr.Kind, "op", connErr.Op, "error", cause)
	} else {
		h.log.Error("catalog import: failed", "store_id", job.StoreID, "error", cause)
	}

	if err := h.jobRepo.Upsert(ctx, job); err != nil {
		h.log.Error("catalog import: failed to persist failure state", "store_id", job.StoreID, "error", err)
	}

	if h.enqueuer != nil {
		if err := h.enqueuer.ReleaseImportLock(ctx, job.StoreID); err != nil {
			h.log.Error("catalog import: failed to release import lock", "store_id", job.StoreID, "error", err)
		}
	}

	h.raiseImportFailedTask(ctx, job, cause)

	return cause
}

// fetchCatalogWithRetry calls connector.FetchCatalog, retrying failures
// classified as transient (isRetryableFetchError) with backoff between
// attempts (h.fetchRetryDelay). A non-retryable failure (e.g. rejected
// credentials) or the final attempt's failure is returned as-is for the
// caller to fail the job on.
func (h *CatalogImportHandler) fetchCatalogWithRetry(ctx context.Context, connector base.Connector, storeID string) ([]base.CatalogEntity, error) {
	var entities []base.CatalogEntity
	var err error
	for attempt := 0; ; attempt++ {
		entities, err = connector.FetchCatalog(ctx, storeID, h.batchSize)
		if err == nil {
			return entities, nil
		}
		if attempt >= len(h.fetchRetryDelay) || !isRetryableFetchError(err) {
			return nil, err
		}
		delay := h.fetchRetryDelay[attempt]
		h.log.Warn("catalog import: fetch failed, retrying", "store_id", storeID, "attempt", attempt+1, "next_retry_in", delay, "error", err)
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(delay):
		}
	}
}

// isRetryableFetchError reports whether a FetchCatalog failure is worth
// retrying. Every base.ConnectorError kind qualifies except credentials,
// which needs the merchant to fix their store connection and will not clear
// on its own — retrying it would just delay the task that tells them so.
// Untyped errors are treated as non-retryable since we can't tell whether
// they're transient.
func isRetryableFetchError(err error) bool {
	var connErr *base.ConnectorError
	if !errors.As(err, &connErr) {
		return false
	}
	return connErr.Kind != base.ConnectorErrCredentials
}

// raiseImportFailedTask publishes sync.task.raised so a failed import (after
// retries, for retryable failures) surfaces as a merchant-facing task
// instead of only a log line. Best-effort: a publish failure is logged, not
// propagated, mirroring emitProgress/emitCompletion below.
func (h *CatalogImportHandler) raiseImportFailedTask(ctx context.Context, job *ImportJob, cause error) {
	payload := TaskRaisedPayload{
		Source:         "catalog_import",
		SourceRef:      job.Reason,
		Owner:          "merchant",
		Title:          importFailureTitle(cause),
		BlocksCheckout: job.Reason == "initial",
	}
	taskBytes, err := json.Marshal(payload)
	if err != nil {
		h.log.Error("catalog import: failed to marshal task.raised", "store_id", job.StoreID, "error", err)
		return
	}
	evt := newDomainEvent(ucp.EventSyncTaskRaised, job.StoreID, job.MerchantID, taskBytes)
	if err := h.eventBus.Publish(ctx, evt); err != nil {
		h.log.Error("catalog import: failed to publish task.raised", "store_id", job.StoreID, "error", err)
	}
}

// importFailureTitle turns a (possibly typed) import failure into a
// merchant-readable task title. Typed base.ConnectorError kinds get a
// specific, actionable message; anything else (e.g. a catalog-store write
// failure) falls back to a generic one.
func importFailureTitle(cause error) string {
	var connErr *base.ConnectorError
	if errors.As(cause, &connErr) {
		switch connErr.Kind {
		case base.ConnectorErrCredentials:
			return "Store connection needs attention: your store credentials were rejected. Reconnect your store to fix this."
		case base.ConnectorErrNotFound:
			return "Catalog import failed: we couldn't reach your store's catalog after retrying. Check your store connection and try a resync."
		case base.ConnectorErrRateLimited:
			return "Catalog import failed: your store rate-limited the import after retrying. Try a resync shortly."
		case base.ConnectorErrUnavailable:
			return "Catalog import failed: your store was unreachable after retrying. Check that it's online and try a resync."
		case base.ConnectorErrCircuitOpen:
			return "Catalog import failed: repeated failures paused import attempts. Try a resync once your store is reachable."
		}
	}
	return "Catalog import failed. Try a resync; contact support if this keeps happening."
}

// emitProgress publishes a best-effort per-store progress signal. Progress
// is not a channel sarvex-v1-syncing-plane-async.yaml defines; the durable
// ImportJob record (counts + cursor) is the authoritative progress state.
// This event is an informational convenience for any live listener, in the
// same spirit as this codebase's existing journey.signal convention.
func (h *CatalogImportHandler) emitProgress(ctx context.Context, job *ImportJob) {
	payload, _ := json.Marshal(CatalogImportProgressPayload{
		StoreID:  job.StoreID,
		Fetched:  job.Counts.Fetched,
		Upserted: job.Counts.Upserted,
	})
	evt := newDomainEvent(ucp.EventCatalogImportProgress, job.StoreID, job.MerchantID, payload)
	if err := h.eventBus.Publish(ctx, evt); err != nil {
		h.log.Error("catalog import: failed to publish progress", "store_id", job.StoreID, "error", err)
	}
}

// emitCompletion publishes the journey signal that advances a store from
// "connected" to "synced" (journey.SignalFirstSyncComplete in the control
// plane's transition table). Sent once, when the import finishes.
func (h *CatalogImportHandler) emitCompletion(ctx context.Context, job *ImportJob) {
	payload, _ := json.Marshal(JourneySignalPayload{Signal: "first_sync_complete"})
	evt := newDomainEvent(ucp.EventJourneySignal, job.StoreID, job.MerchantID, payload)
	if err := h.eventBus.Publish(ctx, evt); err != nil {
		h.log.Error("catalog import: failed to publish journey signal", "store_id", job.StoreID, "error", err)
	}
}

// finalizeReadiness computes the store-level readiness aggregate once,
// against the now-complete product set, and emits the journey threshold
// signal on a zero-blockers crossing — the one-time counterpart to the
// per-product recompute RescoreProduct does outside of import (see
// RescoreProductBatch's doc comment in mapping.go). Called after
// emitCompletion so first_sync_complete is always published first: if this
// store's readiness happens to cross to zero blockers on its very first
// score, the journey must already be "synced" (not "connected") by the time
// readiness_threshold_met is handled, or the signal has no matching
// transition and is silently dropped for good.
//
// job.Reason == "initial" marks this store's first import in a new
// connection lifecycle — passed through as freshConnection so a store that
// reconnects already-clean (see recomputeStoreReadinessAndSignal's doc
// comment) still gets its readiness_threshold_met instead of silently
// inheriting a stale "already crossed" from before the disconnect.
func (h *CatalogImportHandler) finalizeReadiness(ctx context.Context, job *ImportJob) {
	if err := h.scorer.FinalizeBatchReadiness(ctx, job.MerchantID, job.StoreID, job.Reason == "initial"); err != nil {
		h.log.Error("catalog import: failed to finalize store readiness", "store_id", job.StoreID, "error", err)
	}
}

// emitRebuildRequest requests a serving-cache rebuild after a completed
// import (initial or resync). Without this, a store's presence/manifest
// stay empty until either an operator manually rebuilds (POST
// .../presence/rebuild) or a steady-state signal happens to fire
// (OnProductChanged/OnPriceChanged/OnInventoryChanged in mapping.go, the
// only other callers of requestRebuild) — neither of which is guaranteed
// to ever happen for a store that's just been paired.
func (h *CatalogImportHandler) emitRebuildRequest(ctx context.Context, job *ImportJob) {
	if err := requestRebuild(ctx, h.eventBus, job.StoreID, job.MerchantID, "catalog_import_completed", true); err != nil {
		h.log.Error("catalog import: failed to request serving cache rebuild", "store_id", job.StoreID, "error", err)
	}
}

// resolveImportFailedTasks closes any stale "catalog import failed" task
// left open from a prior attempt, now that this run succeeded. It resolves
// both possible reasons ("initial", "resync" — the only two valid values)
// rather than just this job's own reason, since the merchant doesn't care
// which earlier attempt failed, only that import works now; resolving a
// task that was never raised (or is already done) is a safe no-op on the
// control-plane side.
func (h *CatalogImportHandler) resolveImportFailedTasks(ctx context.Context, job *ImportJob) {
	for _, reason := range []string{"initial", "resync"} {
		payload := TaskRaisedPayload{
			Source:    "catalog_import",
			SourceRef: reason,
			Owner:     "merchant",
			Title:     "Catalog import recovered",
			Resolved:  true,
		}
		taskBytes, err := json.Marshal(payload)
		if err != nil {
			h.log.Error("catalog import: failed to marshal task.raised (resolve)", "store_id", job.StoreID, "reason", reason, "error", err)
			continue
		}
		evt := newDomainEvent(ucp.EventSyncTaskRaised, job.StoreID, job.MerchantID, taskBytes)
		if err := h.eventBus.Publish(ctx, evt); err != nil {
			h.log.Error("catalog import: failed to publish task.raised (resolve)", "store_id", job.StoreID, "reason", reason, "error", err)
		}
	}
}

// catalogEntityToProduct maps a connector-normalised catalog entity onto the
// sync-plane's canonical CatalogProduct shape.
func catalogEntityToProduct(merchantID, storeID string, e base.CatalogEntity) *CatalogProduct {
	p := &CatalogProduct{
		StoreID:       storeID,
		MerchantID:    merchantID,
		SKU:           e.SKU,
		RawAttributes: map[string]string{},
		MappedFields:  map[string]string{},
		UpdatedAt:     time.Now().UTC(),
	}
	p.applyEntity(e)
	return p
}
