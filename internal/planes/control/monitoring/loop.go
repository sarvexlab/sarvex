// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package monitoring

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/task"
)

// FindingRouter converts consistency findings into tasks for operators/merchants.
type FindingRouter interface {
	RouteFindings(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, findings []*domain.ConsistencyFinding) error

	// ReconcileRecoveredProbeFindings closes previously-raised monitoring
	// tasks whose probe-derived finding no longer recurs. currentFindings
	// must be the complete result of a full RunProbesForStore pass — see the
	// method's doc comment on TaskServiceRouter for why a partial pass
	// (CheckConsistencyOnChange, the discrepancy subscriber) must never call
	// this.
	ReconcileRecoveredProbeFindings(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, currentFindings []*domain.ConsistencyFinding) error
}

// PlanReader resolves a merchant's billing record — needed to decide who
// owns a warning-severity monitoring fix: Sarvex fixes it automatically for
// concierge-tier merchants, but a self-serve merchant fixes it themselves.
// Satisfied by billing.Service; declared here so monitoring does not import
// billing.
type PlanReader interface {
	Get(ctx context.Context, merchantID domain.MerchantID) (domain.Billing, error)
}

// TaskServiceRouter routes findings to the task service.
type TaskServiceRouter struct {
	taskService task.Service
	logger      *slog.Logger
	plans       PlanReader
}

// NewTaskServiceRouter creates a finding router backed by the task service.
func NewTaskServiceRouter(taskService task.Service, logger *slog.Logger) *TaskServiceRouter {
	return &TaskServiceRouter{
		taskService: taskService,
		logger:      logger,
	}
}

// WithPlanReader wires the merchant's plan into the router so a
// warning-severity finding's owner can follow the commercial plan (see
// warningOwner) rather than always defaulting to Sarvex. Left nil, every
// merchant gets the pre-plan-aware behavior: owner sarvex, resolution
// automatic.
func (r *TaskServiceRouter) WithPlanReader(p PlanReader) *TaskServiceRouter {
	r.plans = p
	return r
}

// warningOwner decides who owns a warning-severity monitoring finding.
// Concierge and scale merchants get it fixed for them by Sarvex,
// automatically; self-serve merchants fix it themselves, guided. Defaults to
// sarvex/automatic when no PlanReader is wired, or the plan lookup fails —
// the safe fallback, and the behavior every merchant had before plan
// mattered here.
func (r *TaskServiceRouter) warningOwner(ctx context.Context, merchantID domain.MerchantID) (domain.TaskOwner, domain.TaskResolution) {
	if r.plans == nil {
		return domain.TaskOwnerSarvex, domain.TaskResolutionAutomatic
	}

	billing, err := r.plans.Get(ctx, merchantID)
	if err != nil {
		if r.logger != nil {
			r.logger.Warn("plan lookup failed for monitoring warning owner, defaulting to sarvex",
				"error", err, "merchant_id", merchantID)
		}
		return domain.TaskOwnerSarvex, domain.TaskResolutionAutomatic
	}

	if billing.Plan.IsConcierge() {
		return domain.TaskOwnerSarvex, domain.TaskResolutionAutomatic
	}
	return domain.TaskOwnerMerchant, domain.TaskResolutionGuided
}

// RouteFindings converts findings into tasks by severity.
// Critical: blocks checkout task for merchant.
// Warning: informational task for operator review.
// Info: logged but no task created.
//
// The probe/sweep/on-change loops re-detect an ongoing issue every pass
// (probes run every 5 minutes), not only when it's first found — so the
// task ID is derived deterministically from (store, SKU, field) rather than
// a timestamp, and an existing task for that triple is treated as the
// idempotent common case (mirrors task.Subscriber.Handle's dedup for
// sync.task.raised) instead of raising a fresh duplicate task every pass.
func (r *TaskServiceRouter) RouteFindings(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, findings []*domain.ConsistencyFinding) error {
	for _, finding := range findings {
		var taskReq task.RaiseTaskRequest

		switch finding.Severity {
		case domain.SeverityCritical:
			taskReq = task.RaiseTaskRequest{
				TaskID:         monitoringTaskID(finding.Severity, storeID, finding.SKU, finding.Field),
				StoreID:        string(storeID),
				Title:          fmt.Sprintf("Critical issue detected: %s", finding.Field),
				WhyItMatters:   fmt.Sprintf("Product %s has a critical issue that blocks checkout: %s", finding.SKU, findingSummary(finding)),
				Owner:          string(domain.TaskOwnerMerchant),
				Resolution:     string(domain.TaskResolutionGuided),
				BlocksCheckout: true,
				Source:         "monitoring",
				SourceRef:      &finding.SKU,
				Finding:        finding,
			}

		case domain.SeverityWarning:
			owner, resolution := r.warningOwner(ctx, merchantID)
			taskReq = task.RaiseTaskRequest{
				TaskID:         monitoringTaskID(finding.Severity, storeID, finding.SKU, finding.Field),
				StoreID:        string(storeID),
				Title:          fmt.Sprintf("Warning: %s mismatch on %s", finding.Field, finding.SKU),
				WhyItMatters:   fmt.Sprintf("Product data divergence detected: %s", findingSummary(finding)),
				Owner:          string(owner),
				Resolution:     string(resolution),
				BlocksCheckout: false,
				Source:         "monitoring",
				SourceRef:      &finding.SKU,
				Finding:        finding,
			}

		default:
			// Info level: just log, don't create task
			r.logger.Info("monitoring finding",
				"merchant_id", merchantID,
				"sku", finding.SKU,
				"field", finding.Field,
				"severity", finding.Severity,
			)
			continue
		}

		if existing, err := r.taskService.Get(ctx, merchantID, taskReq.TaskID); err == nil {
			if existing.State == domain.TaskStateDone {
				// Recovered earlier (ReconcileRecoveredProbeFindings) and
				// failing again: the deterministic id is the same issue.
				note := "Reopened — issue detected again."
				if _, err := r.taskService.Reopen(ctx, merchantID, taskReq.TaskID, &note); err != nil {
					r.logger.Error("failed to reopen task", "error", err, "merchant_id", merchantID, "task_id", taskReq.TaskID)
				}
			}
			// Otherwise already raised for this ongoing issue; skip re-raising it this pass.
			continue
		} else if !errors.Is(err, task.ErrNotFound) {
			r.logger.Error("failed to check existing task", "error", err, "merchant_id", merchantID, "task_id", taskReq.TaskID)
			continue
		}

		_, err := r.taskService.Raise(ctx, merchantID, taskReq)
		if err != nil {
			r.logger.Error("failed to route finding to task", "error", err, "merchant_id", merchantID, "sku", finding.SKU)
			// Continue with next finding even on error
		}
	}

	return nil
}

// monitoringTaskID derives a raised task's deterministic ID from the finding
// that produced it. The same formula RouteFindings uses to raise a task and
// ReconcileRecoveredProbeFindings uses to compute which currently-open tasks
// still have a matching finding this pass — kept in one place so the two
// can't drift.
func monitoringTaskID(severity domain.Severity, storeID domain.StoreID, sku, field string) string {
	switch severity {
	case domain.SeverityCritical:
		return fmt.Sprintf("monitoring_critical_%s_%s_%s", storeID, sku, field)
	case domain.SeverityWarning:
		return fmt.Sprintf("monitoring_warning_%s_%s_%s", storeID, sku, field)
	default:
		return ""
	}
}

// probeSKUs is the fixed, exhaustive set of sentinel SKUs the semantic probe
// battery can raise findings against (see probe.go's SKUProfile /
// SKUPluginGateway doc comment) — the scope ReconcileRecoveredProbeFindings
// is allowed to touch.
var probeSKUs = map[string]bool{
	SKUProfile:       true,
	SKUPluginGateway: true,
}

// ReconcileRecoveredProbeFindings closes ("resolves") previously-raised
// monitoring tasks whose probe-derived finding no longer recurs.
//
// RouteFindings only ever raises or no-ops — nothing in this package
// previously closed a task once its underlying issue cleared, so a critical
// "profile serves 404" task (for example) stayed open forever even after the
// merchant fixed it, requiring a manual resolve.
//
// This is only safe to call with the *complete* result of a RunProbesForStore
// pass, because it works by exclusion: any currently-open monitoring task
// whose finding SKU is in probeSKUs, and whose task ID isn't present among
// currentFindings, is assumed recovered and gets resolved. RunProbesForStore
// always runs the full, fixed probe battery, so "not present this pass"
// reliably means "no longer failing" for that scope. CheckConsistencyOnChange
// (single SKU) and the discrepancy subscriber (single finding) are partial by
// design and must never call this — and never do; RouteFindings, the method
// they do call, doesn't reconcile anything on its own.
func (r *TaskServiceRouter) ReconcileRecoveredProbeFindings(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, currentFindings []*domain.ConsistencyFinding) error {
	stillFailing := make(map[string]bool, len(currentFindings))
	for _, f := range currentFindings {
		if id := monitoringTaskID(f.Severity, storeID, f.SKU, f.Field); id != "" {
			stillFailing[id] = true
		}
	}

	// Listed unfiltered and narrowed with IsOpen rather than asking the API
	// for state == "open": every non-done state is outstanding, so a state
	// filter would silently skip any monitoring task not raised in exactly
	// that state.
	cursor := ""
	for {
		page, err := r.taskService.List(ctx, merchantID, &storeID, cursor, 100, nil, nil)
		if err != nil {
			return fmt.Errorf("list open tasks for recovery check: %w", err)
		}

		for _, t := range page.Data {
			if !t.IsOpen() || t.Source != domain.TaskSourceMonitoring || t.Finding == nil || !probeSKUs[t.Finding.SKU] {
				continue
			}
			if stillFailing[t.TaskID] {
				continue
			}

			// Complete, not Resolve: the probe no longer seeing the issue is
			// the system observing the fix, not the merchant claiming it.
			note := "Automatically resolved — monitoring no longer detects this issue."
			if _, err := r.taskService.Complete(ctx, merchantID, t.TaskID, &note); err != nil {
				r.logger.Error("failed to auto-resolve recovered monitoring task", "error", err, "merchant_id", merchantID, "task_id", t.TaskID)
				continue
			}
			r.logger.Info("auto-resolved recovered monitoring task", "merchant_id", merchantID, "task_id", t.TaskID)
		}

		if !page.Page.HasMore || page.Page.Cursor == nil {
			break
		}
		cursor = *page.Page.Cursor
	}

	return nil
}

// findingSummary creates a short summary of a finding's sources.
func findingSummary(finding *domain.ConsistencyFinding) string {
	if len(finding.Sources) == 0 {
		return "data inconsistency"
	}
	// Return first source as summary
	for k, v := range finding.Sources {
		return fmt.Sprintf("%s: %s", k, v)
	}
	return "data inconsistency"
}

// HealthRepository persists the latest semantic-probe-derived health
// statement per (store, kind), read back by
// operator.OperatorService.GetHealth. Deliberately narrow (only Save) — see
// internal/planes/control/operator.HealthRepository for the concrete
// Mongo-backed implementation and the read side; it satisfies this
// interface structurally.
type HealthRepository interface {
	Save(ctx context.Context, storeID string, statement domain.HealthStatement) error
}

// MonitoringLoop orchestrates semantic probes, consistency checks, and reconciliation.
type MonitoringLoop struct {
	probe         *SemanticProbe
	consistency   *ConsistencyEngine
	sweeper       *ReconciliationSweeper
	router        FindingRouter
	findings      FindingRepository // optional; persists findings for GET .../health/consistency
	health        HealthRepository  // optional; persists statements for GET .../health
	logger        *slog.Logger
	probeInterval time.Duration
	sweepInterval time.Duration
}

// NewMonitoringLoop creates a monitoring loop orchestrator. findings/health
// may be nil (findings/statements then are routed to tasks but not
// persisted for direct query).
func NewMonitoringLoop(
	probe *SemanticProbe,
	consistency *ConsistencyEngine,
	sweeper *ReconciliationSweeper,
	router FindingRouter,
	findings FindingRepository,
	health HealthRepository,
	logger *slog.Logger,
) *MonitoringLoop {
	return &MonitoringLoop{
		probe:         probe,
		consistency:   consistency,
		sweeper:       sweeper,
		router:        router,
		findings:      findings,
		health:        health,
		logger:        logger,
		probeInterval: 5 * time.Minute, // Run probes every 5 minutes
		sweepInterval: 1 * time.Hour,   // Run sweeper every hour
	}
}

// ProbeInterval returns the configured interval between semantic-probe runs.
func (ml *MonitoringLoop) ProbeInterval() time.Duration { return ml.probeInterval }

// SweepInterval returns the configured interval between reconciliation sweeps.
func (ml *MonitoringLoop) SweepInterval() time.Duration { return ml.sweepInterval }

// MonitoringRunResult holds the aggregated result of a monitoring run.
type MonitoringRunResult struct {
	MerchantID  string
	StoreID     string
	RunID       string
	StartedAt   time.Time
	CompletedAt time.Time
	Probes      []*ProbeResult
	Findings    []*domain.ConsistencyFinding
	Error       string
}

// persistFindings saves findings (assigning a stable dedup ID where the
// producer didn't set one) via ml.findings, if configured. Errors are
// logged, not returned — a missed persist must not block task routing.
func (ml *MonitoringLoop) persistFindings(ctx context.Context, source, storeID string, findings []*domain.ConsistencyFinding) {
	if ml.findings == nil {
		return
	}
	for _, f := range findings {
		if f.FindingID == "" {
			f.FindingID = fmt.Sprintf("%s:%s:%s:%s", source, storeID, f.SKU, f.Field)
		}
		if err := ml.findings.Save(ctx, storeID, f); err != nil {
			ml.logger.Error("failed to persist finding", "error", err, "store_id", storeID, "source", source, "sku", f.SKU, "field", f.Field)
		}
	}
}

// deleteRecoveredFindings removes the probe-raised findings a completed pass
// no longer reports. Errors are logged, not returned: a failed cleanup leaves
// a stale finding until the next pass retries, which is the condition this
// exists to end, not a reason to fail the run.
//
// Only valid with the complete result of a full probe battery — see
// TaskServiceRouter.ReconcileRecoveredProbeFindings.
func (ml *MonitoringLoop) deleteRecoveredFindings(ctx context.Context, storeID string, findings []*domain.ConsistencyFinding) {
	if ml.findings == nil {
		return
	}

	stillFailing := make([]string, 0, len(findings))
	for _, f := range findings {
		if probeSKUs[f.SKU] && f.FindingID != "" {
			stillFailing = append(stillFailing, f.FindingID)
		}
	}

	skus := make([]string, 0, len(probeSKUs))
	for sku := range probeSKUs {
		skus = append(skus, sku)
	}

	if err := ml.findings.DeleteRecoveredProbeFindings(ctx, storeID, skus, stillFailing); err != nil {
		ml.logger.Error("failed to delete recovered probe findings", "error", err, "store_id", storeID)
	}
}

// persistProbeHealth rolls probe results up into the semantic_endpoint and
// profile_edge health statements and saves them via ml.health, if
// configured.
func (ml *MonitoringLoop) persistProbeHealth(ctx context.Context, storeID string, probes []*ProbeResult) {
	if ml.health == nil {
		return
	}
	now := time.Now().UTC()

	endpoint := domain.HealthStatement{Kind: domain.HealthSemanticEndpoint, OK: true, Message: "All semantic endpoints reachable.", CheckedAt: now}
	for _, p := range probes {
		if p.ProbeName == "profile_edge_cache" {
			continue // rolled into its own statement below
		}
		if !p.Passed {
			endpoint.OK = false
			endpoint.Message = fmt.Sprintf("%s failed: %s", p.ProbeName, p.Error)
			break
		}
	}
	if err := ml.health.Save(ctx, storeID, endpoint); err != nil {
		ml.logger.Error("failed to persist semantic_endpoint health statement", "error", err, "store_id", storeID)
	}

	for _, p := range probes {
		if p.ProbeName != "profile_edge_cache" {
			continue
		}
		edge := domain.HealthStatement{Kind: domain.HealthProfileEdge, OK: p.Passed, CheckedAt: now}
		if p.Passed {
			edge.Message = "Plugin serves the profile correctly at the merchant's origin."
		} else {
			edge.Message = p.Error
		}
		if err := ml.health.Save(ctx, storeID, edge); err != nil {
			ml.logger.Error("failed to persist profile_edge health statement", "error", err, "store_id", storeID)
		}
	}
}

// RunProbesForStore executes all semantic probes for a store.
func (ml *MonitoringLoop) RunProbesForStore(ctx context.Context, merchantID, storeID, platform string) (MonitoringRunResult, error) {
	runID := uuid.New().String()
	start := time.Now()
	result := MonitoringRunResult{
		MerchantID: merchantID,
		StoreID:    storeID,
		RunID:      runID,
		StartedAt:  start,
		Probes:     make([]*ProbeResult, 0),
		Findings:   make([]*domain.ConsistencyFinding, 0),
	}

	if ml.probe == nil {
		result.Error = "semantic probe not configured"
		ml.logger.Warn("probes skipped", "store_id", storeID, "reason", "probe not configured")
		return result, nil
	}

	probes, err := ml.probe.RunProbes(ctx, storeID, platform)
	if err != nil {
		result.Error = fmt.Sprintf("probe run failed: %v", err)
		ml.logger.Error("probe run failed", "error", err, "store_id", storeID, "run_id", runID)
		return result, nil
	}

	result.Probes = probes
	for _, probe := range probes {
		result.Findings = append(result.Findings, probe.Findings...)
	}

	ml.persistFindings(ctx, "probe", storeID, result.Findings)
	ml.persistProbeHealth(ctx, storeID, probes)

	// Route findings to task service if router configured
	if ml.router != nil {
		if len(result.Findings) > 0 {
			if err := ml.router.RouteFindings(ctx, domain.MerchantID(merchantID), domain.StoreID(storeID), result.Findings); err != nil {
				ml.logger.Error("failed to route probe findings", "error", err, "merchant_id", merchantID, "count", len(result.Findings))
			}
		}
		// Unconditional (not gated on len(result.Findings) > 0): a store that
		// recovered on every check produces zero findings this pass, and
		// that's exactly the case that needs to close out any previously-open
		// probe task. Safe here specifically because RunProbes just ran the
		// complete, fixed probe battery — see the method's doc comment.
		if err := ml.router.ReconcileRecoveredProbeFindings(ctx, domain.MerchantID(merchantID), domain.StoreID(storeID), result.Findings); err != nil {
			ml.logger.Error("failed to reconcile recovered probe findings", "error", err, "merchant_id", merchantID, "store_id", storeID)
		}
	}

	// The other half of that recovery: drop the persisted findings the pass
	// no longer reports. Closing the task while leaving the finding kept
	// GetHealth counting a critical finding forever after a transient
	// failure, directly contradicting the probe statement — also refreshed
	// by this same pass — that reported the endpoint healthy.
	//
	// Unconditional for the same reason the task reconciliation is: a store
	// that recovered on every check produces zero findings, which is exactly
	// the pass that has cleaning up to do.
	ml.deleteRecoveredFindings(ctx, storeID, result.Findings)

	result.CompletedAt = time.Now()
	ml.logger.Info("probes completed",
		"store_id", storeID,
		"run_id", runID,
		"probes", len(probes),
		"findings", len(result.Findings),
		"elapsed_ms", result.CompletedAt.Sub(start).Milliseconds(),
	)

	return result, nil
}

// SweepStore performs full-catalog reconciliation for a store.
func (ml *MonitoringLoop) SweepStore(ctx context.Context, merchantID, storeID string) (MonitoringRunResult, error) {
	start := time.Now()
	result := MonitoringRunResult{
		MerchantID: merchantID,
		StoreID:    storeID,
		RunID:      uuid.New().String(),
		StartedAt:  start,
		Probes:     make([]*ProbeResult, 0),
		Findings:   make([]*domain.ConsistencyFinding, 0),
	}

	if ml.sweeper == nil {
		result.Error = "reconciliation sweeper not configured"
		ml.logger.Warn("sweep skipped", "store_id", storeID, "reason", "sweeper not configured")
		return result, nil
	}

	findings, err := ml.sweeper.SweepStore(ctx, storeID)
	if err != nil {
		result.Error = fmt.Sprintf("sweep failed: %v", err)
		ml.logger.Error("sweep failed", "error", err, "store_id", storeID)
		return result, nil
	}

	result.Findings = findings
	ml.persistFindings(ctx, "sweep", storeID, findings)

	// Route findings to task service if router configured
	if ml.router != nil && len(findings) > 0 {
		if err := ml.router.RouteFindings(ctx, domain.MerchantID(merchantID), domain.StoreID(storeID), findings); err != nil {
			ml.logger.Error("failed to route sweep findings", "error", err, "merchant_id", merchantID, "count", len(findings))
		}
	}

	result.CompletedAt = time.Now()
	ml.logger.Info("sweep completed",
		"store_id", storeID,
		"findings", len(findings),
		"elapsed_ms", result.CompletedAt.Sub(start).Milliseconds(),
	)

	return result, nil
}

// CheckConsistencyOnChange runs consistency checks when a product changes.
// This is called from a control-plane subscriber to ucp.EventCatalogProductsReady
// (see catalog_change_subscriber.go), the event the sync plane's
// mapping/scoring workers emit after writing scored products to the catalog
// store.
func (ml *MonitoringLoop) CheckConsistencyOnChange(ctx context.Context, merchantID, storeID, sku string) ([]*domain.ConsistencyFinding, error) {
	if ml.consistency == nil {
		return nil, nil
	}

	findings, err := ml.consistency.CheckProductConsistency(ctx, storeID, sku)
	if err != nil {
		ml.logger.Error("consistency check on change failed", "error", err, "store_id", storeID, "sku", sku)
		return nil, err
	}

	ml.persistFindings(ctx, "onchange", storeID, findings)

	// Route findings to task service if router configured
	if ml.router != nil && len(findings) > 0 {
		if err := ml.router.RouteFindings(ctx, domain.MerchantID(merchantID), domain.StoreID(storeID), findings); err != nil {
			ml.logger.Error("failed to route consistency findings", "error", err, "merchant_id", merchantID, "sku", sku, "count", len(findings))
		}
	}

	return findings, nil
}
