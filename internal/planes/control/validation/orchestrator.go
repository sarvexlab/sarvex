// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue"
	"github.com/sarvexlab/sarvex/internal/planes/control/activation"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
)

// Prescribed-task source refs owned by validation. Task ids are derived
// from these (task.Prescription.TaskID), so the two can never disagree.
const (
	milestoneSourceRef = "run"
	failureSourceRef   = "pre_validation_failed"
)

// milestonePrescription is the "run pre-validation" task the journey raises
// once a store's readiness threshold is met. owner/resolution are decided by
// the merchant's plan — see milestoneOwner.
func milestonePrescription(storeID domain.StoreID, owner domain.TaskOwner, resolution domain.TaskResolution) task.Prescription {
	return task.Prescription{
		StoreID:      storeID,
		Source:       domain.TaskSourceValidation,
		SourceRef:    milestoneSourceRef,
		Title:        "Run pre-validation",
		WhyItMatters: "Every store must pass Sarvex's internal pre-validation suite against its own UCP endpoints before going live.",
		Owner:        owner,
		Resolution:   resolution,
	}
}

// failurePrescription is "your store failed pre-validation".
//
// One per store, not one per run: the suite re-runs on every catalog change,
// and there is only ever one answer to "is this store failing
// pre-validation". It used to be keyed on the run's timestamp, so a store
// that stayed broken accumulated a permanent task per run and nothing ever
// closed any of them.
func failurePrescription(storeID domain.StoreID, failedCount int) task.Prescription {
	return task.Prescription{
		StoreID:   storeID,
		Source:    domain.TaskSourceValidation,
		SourceRef: failureSourceRef,
		Title:     "Pre-validation checks failed",
		WhyItMatters: "The store must pass pre-validation checks before going live. " +
			fmt.Sprintf("%d requirement(s) not met", failedCount),
		Owner:      domain.TaskOwnerMerchant,
		Resolution: domain.TaskResolutionGuided,
	}
}

// validationMilestoneTaskID is the deterministic Path task ID for the "run
// pre-validation" prescribed task, keeping raise/retire idempotent. Owner and
// resolution play no part in the id (task.Prescription.TaskID derives it from
// source/store/source_ref alone), so retiring never needs to know the plan.
func validationMilestoneTaskID(storeID domain.StoreID) string {
	return task.Prescription{StoreID: storeID, Source: domain.TaskSourceValidation, SourceRef: milestoneSourceRef}.TaskID()
}

// TestHarness runs pre-validation tests against a specific store's serving
// plane endpoints.
type TestHarness interface {
	// RunTests executes the full test suite against storeID's serving-plane
	// endpoints (addressed via platform, per the /ucp/:platform/:storeID
	// route convention) and returns failures.
	RunTests(ctx context.Context, storeID domain.StoreID, platform domain.Platform) (*TestResults, error)
}

// PlatformResolver resolves a store's connector platform — needed to
// address the serving plane's per-store routes. Deliberately narrower than
// monitoring's StoreLister: storeID here is already authorized by the
// inbound request (bearer claim / console scope), so this is a single-store
// lookup, never a cross-tenant one.
type PlatformResolver interface {
	GetPlatform(ctx context.Context, storeID domain.StoreID) (domain.Platform, error)
}

// PlanReader resolves a merchant's billing record — needed to decide who
// owns the "run pre-validation" milestone: Sarvex runs it by hand for
// concierge-tier merchants, but a self-serve merchant runs it themselves.
// Satisfied by billing.Service; declared here so validation does not import
// billing, mirroring JourneyStateReader above.
type PlanReader interface {
	Get(ctx context.Context, merchantID domain.MerchantID) (domain.Billing, error)
}

// ActivationService verifies a store's activation credentials and emits
// verification signals. Satisfied by activation.ActivationService.
type ActivationService interface {
	Verify(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (activation.VerificationReport, error)
}

// TestResults holds the outcome of a validation run.
type TestResults struct {
	Passed   []string
	Failed   []*domain.ValidationFailure
	Warnings []string
}

// ConformanceStore persists validation records.
//
// Records are keyed by store, not merchant: validation runs against a
// store's own serving-plane endpoints, and a merchant may have several
// stores at different points in the journey (sarvex-v1-architecture.md A10.4).
type ConformanceStore interface {
	// SaveRecord stores a conformance record under record.StoreID.
	SaveRecord(ctx context.Context, record *domain.ConformanceRecord) error
	// GetLatest returns the most recent record for a store.
	GetLatest(ctx context.Context, storeID string) (*domain.ConformanceRecord, error)
	// ListRuns returns paginated conformance records for a store.
	ListRuns(ctx context.Context, storeID string, limit int, cursor string) ([]*domain.ConformanceRecord, *string, error)
}

// Orchestrator drives validation runs and maintains conformance records.
type Orchestrator struct {
	harness           TestHarness
	store             ConformanceStore
	platforms         PlatformResolver
	eventPublisher    eventbus.EventBus
	taskService       task.Service
	plans             PlanReader
	activationService ActivationService
	catalogue         catalogue.Catalogue
	logger            *slog.Logger
	targetSpecVer     string
}

// NewOrchestrator creates a validation orchestrator.
func NewOrchestrator(
	harness TestHarness,
	store ConformanceStore,
	platforms PlatformResolver,
	eventPublisher eventbus.EventBus,
	cat catalogue.Catalogue,
	logger *slog.Logger,
	targetSpecVersion string,
) *Orchestrator {
	return &Orchestrator{
		harness:        harness,
		store:          store,
		platforms:      platforms,
		eventPublisher: eventPublisher,
		taskService:    nil,
		catalogue:      cat,
		logger:         logger,
		targetSpecVer:  targetSpecVersion,
	}
}

// WithTaskService adds task emission to the orchestrator.
func (o *Orchestrator) WithTaskService(ts task.Service) *Orchestrator {
	o.taskService = ts
	return o
}

// WithPlanReader wires the merchant's plan into the orchestrator so the
// pre-validation milestone's owner can follow the commercial plan (see
// milestoneOwner) rather than always defaulting to Sarvex. Left nil, every
// merchant gets the pre-plan-aware behavior: owner sarvex, resolution
// one_click.
func (o *Orchestrator) WithPlanReader(r PlanReader) *Orchestrator {
	o.plans = r
	return o
}

// WithActivationService wires activation verification into the orchestrator.
// When validation passes, the orchestrator calls Verify() to advance activation
// from profile_published → verified. Left nil, validation runs independently
// and activation verification must be triggered manually.
func (o *Orchestrator) WithActivationService(svc ActivationService) *Orchestrator {
	o.activationService = svc
	return o
}

// milestoneOwner decides who owns the "run pre-validation" milestone.
// Concierge and scale merchants get it run for them by Sarvex, one click;
// self-serve merchants run it themselves, guided. Defaults to
// sarvex/one_click when no PlanReader is wired, or the plan lookup fails —
// the safe fallback, and the behavior every merchant had before plan
// mattered here.
func (o *Orchestrator) milestoneOwner(ctx context.Context, merchantID domain.MerchantID) (domain.TaskOwner, domain.TaskResolution) {
	if o.plans == nil {
		return domain.TaskOwnerSarvex, domain.TaskResolutionOneClick
	}

	billing, err := o.plans.Get(ctx, merchantID)
	if err != nil {
		if o.logger != nil {
			o.logger.Warn("plan lookup failed for pre-validation milestone owner, defaulting to sarvex",
				"error", err, "merchant_id", merchantID)
		}
		return domain.TaskOwnerSarvex, domain.TaskResolutionOneClick
	}

	if billing.Plan.IsConcierge() {
		return domain.TaskOwnerSarvex, domain.TaskResolutionOneClick
	}
	return domain.TaskOwnerMerchant, domain.TaskResolutionGuided
}

// RunValidation executes a pre-validation run and records the result.
func (o *Orchestrator) RunValidation(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (*domain.ConformanceRecord, error) {
	runID := uuid.New().String()
	now := time.Now().UTC()

	if o.logger != nil {
		o.logger.Info("starting validation run", "merchant_id", merchantID, "store_id", storeID, "run_id", runID)
	}

	// Resolve the store's connector platform to address its serving-plane
	// endpoints (/ucp/:platform/:storeID) — never guess or default this.
	platform, err := o.platforms.GetPlatform(ctx, storeID)
	if err != nil {
		if o.logger != nil {
			o.logger.Error("resolve platform failed", "error", err, "merchant_id", merchantID, "store_id", storeID, "run_id", runID)
		}
		return nil, fmt.Errorf("resolve platform: %w", err)
	}

	// Run the test suite against this store's own serving-plane endpoints.
	results, err := o.harness.RunTests(ctx, storeID, platform)
	if err != nil {
		if o.logger != nil {
			o.logger.Error("validation tests failed", "error", err, "merchant_id", merchantID, "store_id", storeID, "run_id", runID)
		}
		return nil, fmt.Errorf("run tests: %w", err)
	}

	// Build conformance record.
	result := domain.ConformanceFail
	if len(results.Failed) == 0 {
		result = domain.ConformancePass
	}

	// Get catalogue versions for reproducibility
	catalogueVersion := ""
	scoringRubricVersion := ""
	if o.catalogue != nil {
		catalogueVersion = o.catalogue.Version()
		if rubric := o.catalogue.ScoringRubric(); rubric != nil {
			scoringRubricVersion = rubric.Version
		}
	}

	record := &domain.ConformanceRecord{
		RunID:                runID,
		MerchantID:           merchantID,
		StoreID:              storeID,
		RanAt:                now,
		SpecVersion:          o.targetSpecVer,
		CatalogueVersion:     catalogueVersion,
		ScoringRubricVersion: scoringRubricVersion,
		Result:               result,
		Passed:               len(results.Passed),
		Failed:               len(results.Failed),
		Warnings:             len(results.Warnings),
	}

	// Persist the record.
	if err := o.store.SaveRecord(ctx, record); err != nil {
		if o.logger != nil {
			o.logger.Error("failed to save conformance record", "error", err, "merchant_id", merchantID)
		}
		return nil, fmt.Errorf("save record: %w", err)
	}

	if o.logger != nil {
		o.logger.Info("validation run completed",
			"merchant_id", merchantID,
			"run_id", runID,
			"result", result,
			"passed", len(results.Passed),
			"failed", len(results.Failed),
		)
	}

	// Resolve the "run pre-validation" milestone task — this run discharges
	// it regardless of pass/fail; a failing run separately raises its own
	// detected task below.
	if o.taskService != nil {
		if err := task.Retire(ctx, o.taskService, merchantID, validationMilestoneTaskID(storeID)); err != nil && o.logger != nil {
			o.logger.Error("failed to retire validation milestone task", "error", err, "merchant_id", merchantID, "store_id", storeID)
		}
	}

	// A passing run then verifies the store's own UCP endpoints, which is what
	// advances activation profile_published → verified. The two belong to one
	// merchant-visible action ("run pre-validation"): the suite establishes the
	// catalog and profile are correct, verification establishes the endpoints
	// actually answer, and neither is worth asking the merchant to trigger
	// separately. The report is persisted onto the activation record, so the
	// Path's pre-validation drawer shows the checks alongside the suite result.
	//
	// Only on pass: a failing suite means the store is not ready to be probed,
	// and running verification anyway would emit verification_failed — which
	// suspends a store that had previously verified.
	if result == domain.ConformancePass && o.activationService != nil {
		if _, err := o.activationService.Verify(ctx, merchantID, storeID); err != nil && o.logger != nil {
			o.logger.Warn("activation verification after passing pre-validation failed",
				"error", err, "merchant_id", merchantID, "store_id", storeID, "run_id", runID)
		}
	}

	// Emit journey signal if result changed.
	if err := o.emitJourneySignal(ctx, string(merchantID), string(storeID), result); err != nil {
		if o.logger != nil {
			o.logger.Error("failed to emit journey signal", "error", err, "merchant_id", merchantID, "store_id", storeID)
		}
		// Log but don't fail the run.
	}

	// Reflect this run's verdict onto the one pre-validation-failure task: a
	// failing run raises it (if it isn't already up), a passing run closes
	// it. Keeping both halves here means the Path never shows a store as
	// failing pre-validation after it has started passing.
	if o.taskService != nil {
		o.reconcileFailureTask(ctx, merchantID, storeID, result, len(results.Failed))
	}

	return record, nil
}

// EnsureMilestoneTask raises the "run pre-validation" prescribed task on the
// Path (task spine, source=validation) the first time it's called for a
// store — idempotent, safe to call repeatedly. Called by the journey
// service once a store's readiness threshold is met (journey/stage_work.go's
// Ready-stage delegate).
func (o *Orchestrator) EnsureMilestoneTask(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) error {
	if o.taskService == nil {
		return nil
	}
	owner, resolution := o.milestoneOwner(ctx, merchantID)
	return task.Ensure(ctx, o.taskService, merchantID, milestonePrescription(storeID, owner, resolution))
}

// reconcileFailureTask brings the store's single pre-validation-failure task
// in line with the run that just finished. Best-effort: the conformance
// record is already persisted, so a failure here is logged and corrected by
// the next run.
func (o *Orchestrator) reconcileFailureTask(
	ctx context.Context,
	merchantID domain.MerchantID,
	storeID domain.StoreID,
	result domain.ConformanceResult,
	failedCount int,
) {
	p := failurePrescription(storeID, failedCount)

	// A passing run retires the task if one was up — including when the
	// merchant never touched it, which is why this is Retire and not a
	// merchant-facing resolve.
	if result != domain.ConformanceFail {
		if err := task.Retire(ctx, o.taskService, merchantID, p.TaskID()); err != nil && o.logger != nil {
			o.logger.Error("failed to retire pre-validation failure task", "error", err, "merchant_id", merchantID, "store_id", storeID)
		}
		return
	}

	if err := task.Ensure(ctx, o.taskService, merchantID, p); err != nil && o.logger != nil {
		o.logger.Error("failed to raise pre-validation failure task", "error", err, "merchant_id", merchantID, "store_id", storeID)
	}
}

// GetStatus returns the current validation state for a store.
//
// PreValidation is Sarvex's own suite, so it reads the latest conformance
// record this orchestrator wrote. Sandbox and Production are external
// verdicts Sarvex does not run; both always report not_submitted — kept on
// the contract for shape compatibility rather than removed, since nothing
// depends on their absence.
func (o *Orchestrator) GetStatus(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (*domain.ValidationStatus, error) {
	status := &domain.ValidationStatus{
		PreValidation:     domain.PreValidationNotRun,
		Sandbox:           domain.SandboxNotSubmitted,
		Production:        domain.ProductionNotSubmitted,
		TargetSpecVersion: o.targetSpecVer,
	}

	latest, err := o.store.GetLatest(ctx, string(storeID))
	if err != nil {
		// No records yet — pre-validation simply hasn't run.
		return status, nil
	}

	status.PreValidation = domain.PreValidationFailing
	if latest.Result == domain.ConformancePass {
		status.PreValidation = domain.PreValidationPassing
	}
	status.LastRunAt = &latest.RanAt

	return status, nil
}

// ListRuns returns paginated conformance records for a store.
func (o *Orchestrator) ListRuns(ctx context.Context, storeID string, limit int, cursor string) ([]*domain.ConformanceRecord, *string, error) {
	if limit == 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	return o.store.ListRuns(ctx, storeID, limit, cursor)
}

// emitJourneySignal emits a journey signal when validation state changes.
// Signal indicates whether pre-validation checks passed or failed. The journey
// is per store, so the event must carry the store it is about.
func (o *Orchestrator) emitJourneySignal(ctx context.Context, merchantID, storeID string, result domain.ConformanceResult) error {
	if o.eventPublisher == nil {
		return nil // Event bus not wired; skip silently.
	}

	signal := "validation_failed"
	if result == domain.ConformancePass {
		signal = "validation_passed"
	}

	payload := struct {
		Signal string `json:"signal"`
	}{Signal: signal}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	event := &simpleDomainEvent{
		eventType:  ucp.EventJourneySignal,
		merchantID: merchantID,
		storeID:    storeID,
		occurredAt: time.Now().UTC(),
		payload:    payloadBytes,
	}

	if err := o.eventPublisher.Publish(ctx, event); err != nil {
		return fmt.Errorf("publish journey.signal: %w", err)
	}

	return nil
}

// simpleDomainEvent implements eventbus.DomainEvent for validation signals.
type simpleDomainEvent struct {
	eventType  string
	merchantID string
	storeID    string
	occurredAt time.Time
	payload    []byte
}

func (e *simpleDomainEvent) EventType() string     { return e.eventType }
func (e *simpleDomainEvent) StoreID() string       { return e.storeID }
func (e *simpleDomainEvent) MerchantID() string    { return e.merchantID }
func (e *simpleDomainEvent) OccurredAt() time.Time { return e.occurredAt }
func (e *simpleDomainEvent) Payload() []byte       { return e.payload }
