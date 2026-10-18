// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package monitoring

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
)

var testLog = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeTaskService captures raised tasks for testing, and tracks enough state
// (resolved task IDs) to test recovery reconciliation: List reports a raised
// task as open until Resolve has been called for its ID.
type fakeTaskService struct {
	raised     []*task.RaiseTaskRequest
	resolved   map[string]*string // task_id -> resolve note
	err        error              // returned by Raise
	listErr    error
	resolveErr error
	reopened   []string // task ids passed to Reopen
}

func (f *fakeTaskService) List(ctx context.Context, id domain.MerchantID, storeID *domain.StoreID, cursor string, limit int, owner *domain.TaskOwner, state *domain.TaskState) (domain.ListOf[domain.Task], error) {
	if f.listErr != nil {
		return domain.ListOf[domain.Task]{}, f.listErr
	}
	var data []domain.Task
	for _, r := range f.raised {
		if storeID != nil && r.StoreID != string(*storeID) {
			continue
		}
		_, isResolved := f.resolved[r.TaskID]
		if state != nil && *state == domain.TaskStateOpen && isResolved {
			continue
		}
		st := domain.TaskStateOpen
		if isResolved {
			st = domain.TaskStateDone
		}
		data = append(data, domain.Task{
			TaskID:  r.TaskID,
			StoreID: domain.StoreID(r.StoreID),
			Source:  domain.TaskSource(r.Source),
			State:   st,
			Finding: r.Finding,
		})
	}
	// No pagination in the fake — always a single, complete page.
	return domain.ListOf[domain.Task]{Data: data, Page: domain.Page{HasMore: false}}, nil
}
func (f *fakeTaskService) Get(ctx context.Context, id domain.MerchantID, taskID string) (domain.Task, error) {
	for _, r := range f.raised {
		if r.TaskID == taskID {
			st := domain.TaskStateOpen
			if _, done := f.resolved[taskID]; done {
				st = domain.TaskStateDone
			}
			return domain.Task{TaskID: r.TaskID, State: st}, nil
		}
	}
	return domain.Task{}, task.ErrNotFound
}
func (f *fakeTaskService) Resolve(ctx context.Context, id domain.MerchantID, taskID string, req domain.ResolveTaskRequest) (domain.Task, error) {
	return f.Complete(ctx, id, taskID, req.Note)
}

func (f *fakeTaskService) Complete(_ context.Context, _ domain.MerchantID, taskID string, note *string) (domain.Task, error) {
	if f.resolveErr != nil {
		return domain.Task{}, f.resolveErr
	}
	if f.resolved == nil {
		f.resolved = make(map[string]*string)
	}
	f.resolved[taskID] = note
	return domain.Task{TaskID: taskID, State: domain.TaskStateDone}, nil
}
func (f *fakeTaskService) Reopen(_ context.Context, _ domain.MerchantID, taskID string, _ *string) (domain.Task, error) {
	f.reopened = append(f.reopened, taskID)
	delete(f.resolved, taskID)
	return domain.Task{TaskID: taskID, State: domain.TaskStateOpen}, nil
}
func (f *fakeTaskService) Raise(ctx context.Context, merchantID domain.MerchantID, req task.RaiseTaskRequest) (domain.Task, error) {
	if f.err != nil {
		return domain.Task{}, f.err
	}
	f.raised = append(f.raised, &req)
	return domain.Task{TaskID: req.TaskID}, nil
}
func (f *fakeTaskService) ListOpenByMerchants(ctx context.Context, merchantIDs []domain.MerchantID) (map[domain.StoreID]domain.StoreTaskSummary, error) {
	return nil, task.ErrNotImplemented
}

// fakePlanReader returns a fixed plan for every merchant, or an error if err is set.
type fakePlanReader struct {
	plan domain.Plan
	err  error
}

func (f *fakePlanReader) Get(context.Context, domain.MerchantID) (domain.Billing, error) {
	if f.err != nil {
		return domain.Billing{}, f.err
	}
	return domain.Billing{Plan: f.plan}, nil
}

// TestTaskServiceRouter_RoutesFindings_Critical routes critical findings to merchant tasks.
func TestTaskServiceRouter_RoutesFindings_Critical(t *testing.T) {
	taskSvc := &fakeTaskService{}
	router := NewTaskServiceRouter(taskSvc, testLog)

	findings := []*domain.ConsistencyFinding{
		{
			SKU:        "sku123",
			Field:      "stock",
			Severity:   domain.SeverityCritical,
			Sources:    map[string]string{"claim": "in_stock", "quantity": "0"},
			DetectedAt: time.Now(),
		},
	}

	err := router.RouteFindings(context.Background(), "merchant1", "store1", findings)
	if err != nil {
		t.Errorf("RouteFindings failed: %v", err)
	}

	if len(taskSvc.raised) != 1 {
		t.Errorf("expected 1 task raised, got %d", len(taskSvc.raised))
	}

	task := taskSvc.raised[0]
	if task.Owner != "merchant" {
		t.Errorf("expected owner merchant, got %s", task.Owner)
	}
	if !task.BlocksCheckout {
		t.Error("expected BlocksCheckout=true for critical finding")
	}
}

// TestTaskServiceRouter_RoutesFindings_DedupesOngoingIssue verifies the fix
// for a bug where an unresolved finding re-detected on every 5-minute probe
// pass raised a brand-new task each time (the task ID was suffixed with
// time.Now().UnixNano()) instead of being treated as the same ongoing issue.
func TestTaskServiceRouter_RoutesFindings_DedupesOngoingIssue(t *testing.T) {
	taskSvc := &fakeTaskService{}
	router := NewTaskServiceRouter(taskSvc, testLog)

	finding := &domain.ConsistencyFinding{
		SKU:        "system:plugin_gateway",
		Field:      "endpoint_reachability",
		Severity:   domain.SeverityCritical,
		Sources:    map[string]string{"error": "not responding"},
		DetectedAt: time.Now(),
	}

	// Same finding, detected on three separate probe passes.
	for i := range 3 {
		err := router.RouteFindings(context.Background(), "merchant1", "store1", []*domain.ConsistencyFinding{finding})
		if err != nil {
			t.Fatalf("RouteFindings pass %d failed: %v", i, err)
		}
	}

	if len(taskSvc.raised) != 1 {
		t.Fatalf("expected exactly 1 task raised across repeated passes for the same ongoing issue, got %d", len(taskSvc.raised))
	}
	if taskSvc.raised[0].TaskID == "" {
		t.Error("expected a non-empty, stable task id")
	}
}

// TestTaskServiceRouter_RoutesFindings_ReopensRecoveredIssue: a finding whose
// task was closed by recovery and then fails again is the same issue — it must
// come back as an open task, not be dropped as a duplicate of the done one.
func TestTaskServiceRouter_RoutesFindings_ReopensRecoveredIssue(t *testing.T) {
	taskSvc := &fakeTaskService{}
	router := NewTaskServiceRouter(taskSvc, testLog)
	finding := &domain.ConsistencyFinding{
		SKU:      "system:plugin_gateway",
		Field:    "endpoint_reachability",
		Severity: domain.SeverityCritical,
	}
	ctx := context.Background()

	if err := router.RouteFindings(ctx, "merchant1", "store1", []*domain.ConsistencyFinding{finding}); err != nil {
		t.Fatal(err)
	}
	id := taskSvc.raised[0].TaskID
	taskSvc.resolved = map[string]*string{id: nil} // recovered

	if err := router.RouteFindings(ctx, "merchant1", "store1", []*domain.ConsistencyFinding{finding}); err != nil {
		t.Fatal(err)
	}
	if len(taskSvc.reopened) != 1 || taskSvc.reopened[0] != id {
		t.Fatalf("expected task %s to be reopened once, got %v", id, taskSvc.reopened)
	}
	if len(taskSvc.raised) != 1 {
		t.Fatalf("a recurrence must reopen the task, not raise a second one; raised=%d", len(taskSvc.raised))
	}
}

// TestTaskServiceRouter_RoutesFindings_Warning routes warnings to operator tasks.
func TestTaskServiceRouter_RoutesFindings_Warning(t *testing.T) {
	taskSvc := &fakeTaskService{}
	router := NewTaskServiceRouter(taskSvc, testLog)

	findings := []*domain.ConsistencyFinding{
		{
			SKU:        "sku123",
			Field:      "price",
			Severity:   domain.SeverityWarning,
			Sources:    map[string]string{"store": "1000", "served": "900"},
			DetectedAt: time.Now(),
		},
	}

	err := router.RouteFindings(context.Background(), "merchant1", "store1", findings)
	if err != nil {
		t.Errorf("RouteFindings failed: %v", err)
	}

	if len(taskSvc.raised) != 1 {
		t.Errorf("expected 1 task raised, got %d", len(taskSvc.raised))
	}

	task := taskSvc.raised[0]
	if task.Owner != "sarvex" {
		t.Errorf("expected owner sarvex for warning, got %s", task.Owner)
	}
	if task.BlocksCheckout {
		t.Error("expected BlocksCheckout=false for warning")
	}
}

// TestTaskServiceRouter_RoutesFindings_Warning_PlanAware checks that a
// warning-severity finding's owner follows the merchant's plan once a
// PlanReader is wired: Sarvex fixes it automatically for concierge/scale
// merchants, a self-serve merchant fixes it themselves.
func TestTaskServiceRouter_RoutesFindings_Warning_PlanAware(t *testing.T) {
	finding := func() []*domain.ConsistencyFinding {
		return []*domain.ConsistencyFinding{{
			SKU:        "sku123",
			Field:      "price",
			Severity:   domain.SeverityWarning,
			Sources:    map[string]string{"store": "1000", "served": "900"},
			DetectedAt: time.Now(),
		}}
	}

	cases := []struct {
		name           string
		plans          *fakePlanReader
		wantOwner      string
		wantResolution string
	}{
		{"no plan reader defaults to sarvex", nil, "sarvex", "automatic"},
		{"concierge is sarvex-owned", &fakePlanReader{plan: domain.PlanConcierge}, "sarvex", "automatic"},
		{"scale is sarvex-owned", &fakePlanReader{plan: domain.PlanScale}, "sarvex", "automatic"},
		{"self_serve is merchant-owned", &fakePlanReader{plan: domain.PlanSelfServe}, "merchant", "guided"},
		{"plan lookup failure defaults to sarvex", &fakePlanReader{err: errors.New("boom")}, "sarvex", "automatic"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			taskSvc := &fakeTaskService{}
			router := NewTaskServiceRouter(taskSvc, testLog)
			if tc.plans != nil {
				router = router.WithPlanReader(tc.plans)
			}

			if err := router.RouteFindings(context.Background(), "merchant1", "store1", finding()); err != nil {
				t.Fatalf("RouteFindings failed: %v", err)
			}
			if len(taskSvc.raised) != 1 {
				t.Fatalf("expected 1 task raised, got %d", len(taskSvc.raised))
			}

			got := taskSvc.raised[0]
			if got.Owner != tc.wantOwner {
				t.Errorf("expected owner %s, got %s", tc.wantOwner, got.Owner)
			}
			if got.Resolution != tc.wantResolution {
				t.Errorf("expected resolution %s, got %s", tc.wantResolution, got.Resolution)
			}
		})
	}
}

// TestTaskServiceRouter_RoutesFindings_Info ignores info-level findings.
func TestTaskServiceRouter_RoutesFindings_Info(t *testing.T) {
	taskSvc := &fakeTaskService{}
	router := NewTaskServiceRouter(taskSvc, testLog)

	findings := []*domain.ConsistencyFinding{
		{
			SKU:        "sku123",
			Field:      "price",
			Severity:   domain.SeverityInfo,
			Sources:    map[string]string{"note": "minor drift"},
			DetectedAt: time.Now(),
		},
	}

	err := router.RouteFindings(context.Background(), "merchant1", "store1", findings)
	if err != nil {
		t.Errorf("RouteFindings failed: %v", err)
	}

	if len(taskSvc.raised) != 0 {
		t.Errorf("expected 0 tasks for info level, got %d", len(taskSvc.raised))
	}
}

// TestTaskServiceRouter_RoutesFindings_AttachesFindingPayload verifies the
// raised task carries the full finding (field, sources, severity,
// detected_at), not just the SKU — this is the typed drill-in payload the
// task drawer renders for source=monitoring tasks.
func TestTaskServiceRouter_RoutesFindings_AttachesFindingPayload(t *testing.T) {
	detectedAt := time.Now()
	finding := &domain.ConsistencyFinding{
		SKU:        "system:plugin_gateway",
		Field:      "profile_format",
		Severity:   domain.SeverityWarning,
		Sources:    map[string]string{"error": "Invalid JSON: unexpected end of JSON input"},
		DetectedAt: detectedAt,
	}

	taskSvc := &fakeTaskService{}
	router := NewTaskServiceRouter(taskSvc, testLog)

	if err := router.RouteFindings(context.Background(), "merchant1", "store1", []*domain.ConsistencyFinding{finding}); err != nil {
		t.Fatalf("RouteFindings failed: %v", err)
	}

	if len(taskSvc.raised) != 1 {
		t.Fatalf("expected 1 task raised, got %d", len(taskSvc.raised))
	}

	got := taskSvc.raised[0].Finding
	if got == nil {
		t.Fatal("expected Finding to be attached to the raised task, got nil")
	}
	if got.Field != finding.Field || got.Severity != finding.Severity || !got.DetectedAt.Equal(detectedAt) {
		t.Errorf("attached finding does not match source finding: got %+v, want %+v", got, finding)
	}
	if got.Sources["error"] != finding.Sources["error"] {
		t.Errorf("attached finding lost diagnostic detail: got sources %+v", got.Sources)
	}
}

// TestMonitoringLoop_RunProbesForStore routes probe findings.
func TestMonitoringLoop_RunProbesForStore(t *testing.T) {
	taskSvc := &fakeTaskService{}
	router := NewTaskServiceRouter(taskSvc, testLog)

	loop := NewMonitoringLoop(nil, nil, nil, router, nil, nil, testLog)

	// No probe configured, should skip gracefully
	result, err := loop.RunProbesForStore(context.Background(), "merchant1", "store1", "woocommerce")
	if err != nil {
		t.Errorf("RunProbesForStore failed: %v", err)
	}

	if result.Error == "" {
		t.Error("expected error when probe not configured")
	}
	if len(taskSvc.raised) != 0 {
		t.Errorf("expected 0 tasks when probe skipped, got %d", len(taskSvc.raised))
	}
}

// TestMonitoringLoop_CheckConsistencyOnChange routes consistency findings.
func TestMonitoringLoop_CheckConsistencyOnChange(t *testing.T) {
	taskSvc := &fakeTaskService{}
	router := NewTaskServiceRouter(taskSvc, testLog)

	loop := NewMonitoringLoop(nil, nil, nil, router, nil, nil, testLog)

	// No consistency engine configured, should skip gracefully
	findings, err := loop.CheckConsistencyOnChange(context.Background(), "merchant1", "store1", "sku123")
	if err != nil {
		t.Errorf("CheckConsistencyOnChange failed: %v", err)
	}

	if len(findings) > 0 {
		t.Errorf("expected no findings when consistency not configured, got %d", len(findings))
	}
	if len(taskSvc.raised) != 0 {
		t.Errorf("expected 0 tasks when consistency skipped, got %d", len(taskSvc.raised))
	}
}

// TestMonitoringLoop_SweepStore routes sweep findings.
func TestMonitoringLoop_SweepStore(t *testing.T) {
	taskSvc := &fakeTaskService{}
	router := NewTaskServiceRouter(taskSvc, testLog)

	loop := NewMonitoringLoop(nil, nil, nil, router, nil, nil, testLog)

	// No sweeper configured, should skip gracefully
	result, err := loop.SweepStore(context.Background(), "merchant1", "store1")
	if err != nil {
		t.Errorf("SweepStore failed: %v", err)
	}

	if result.Error == "" {
		t.Error("expected error when sweeper not configured")
	}
	if len(taskSvc.raised) != 0 {
		t.Errorf("expected 0 tasks when sweeper skipped, got %d", len(taskSvc.raised))
	}
}

// TestMonitoringLoop_RoutingError handles task service errors gracefully.
func TestMonitoringLoop_RoutingError(t *testing.T) {
	taskSvc := &fakeTaskService{err: errors.New("task service down")}
	router := NewTaskServiceRouter(taskSvc, testLog)

	err := router.RouteFindings(context.Background(), "merchant1", "store1", []*domain.ConsistencyFinding{
		{
			SKU:        "sku123",
			Field:      "stock",
			Severity:   domain.SeverityCritical,
			DetectedAt: time.Now(),
		},
	})

	// Should not error; routing errors are logged but not fatal
	if err != nil {
		t.Errorf("RouteFindings should not error on task service failure: %v", err)
	}
}

// A probe finding must disappear once the probe stops reporting it.
//
// Findings were write-only: a single failed pass — the plugin restarting, a
// deploy, a blip — left a permanent record. GetHealth counts findings, so the
// Health page showed "1 critical consistency finding" forever, sitting
// directly above the profile_edge statement (refreshed by the same pass)
// reporting the very same endpoint healthy. Closing the recovered *task* was
// already handled; the finding was not.
func TestMonitoringLoop_DeletesFindingsThatNoLongerRecur(t *testing.T) {
	repo := &fakeFindingRepo{}
	loop := NewMonitoringLoop(nil, nil, nil, nil, repo, nil, testLog)

	// A pass that found nothing: everything previously raised has recovered.
	loop.deleteRecoveredFindings(context.Background(), "store1", nil)

	if len(repo.deletes) != 1 {
		t.Fatalf("expected one delete call, got %d", len(repo.deletes))
	}
	call := repo.deletes[0]
	if call.storeID != "store1" {
		t.Errorf("deleted for the wrong store: %q", call.storeID)
	}
	if len(call.stillFailingIDs) != 0 {
		t.Errorf("a pass with no findings has nothing still failing, got %v", call.stillFailingIDs)
	}
	// Scoped to the probe battery's sentinel SKUs — never product findings,
	// which this pass did not re-check.
	if len(call.probeSKUs) != len(probeSKUs) {
		t.Errorf("expected the probe SKU scope, got %v", call.probeSKUs)
	}
	for _, sku := range call.probeSKUs {
		if !probeSKUs[sku] {
			t.Errorf("delete scope leaked a non-probe SKU: %q", sku)
		}
	}
}

// The findings a pass *does* report must survive it.
func TestMonitoringLoop_KeepsFindingsStillFailing(t *testing.T) {
	repo := &fakeFindingRepo{}
	loop := NewMonitoringLoop(nil, nil, nil, nil, repo, nil, testLog)

	loop.deleteRecoveredFindings(context.Background(), "store1", []*domain.ConsistencyFinding{
		{FindingID: "probe:store1:system:plugin_gateway:server_health", SKU: SKUPluginGateway, Field: "server_health"},
	})

	if len(repo.deletes) != 1 {
		t.Fatalf("expected one delete call, got %d", len(repo.deletes))
	}
	got := repo.deletes[0].stillFailingIDs
	if len(got) != 1 || got[0] != "probe:store1:system:plugin_gateway:server_health" {
		t.Errorf("the still-failing finding must be excluded from deletion, got %v", got)
	}
}

// A product-SKU finding raised by the sweeper or the discrepancy subscriber is
// out of scope: a probe pass never re-checks it, so "absent this pass" says
// nothing about whether it recovered.
func TestMonitoringLoop_DeleteScopeExcludesProductFindings(t *testing.T) {
	repo := &fakeFindingRepo{}
	loop := NewMonitoringLoop(nil, nil, nil, nil, repo, nil, testLog)

	loop.deleteRecoveredFindings(context.Background(), "store1", []*domain.ConsistencyFinding{
		{FindingID: "sweep:store1:SKU-1:price", SKU: "SKU-1", Field: "price"},
	})

	if ids := repo.deletes[0].stillFailingIDs; len(ids) != 0 {
		t.Errorf("a product finding must not enter the probe recovery scope, got %v", ids)
	}
}
