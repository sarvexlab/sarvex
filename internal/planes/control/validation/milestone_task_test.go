// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"errors"
	"testing"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
)

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

// fakeTaskService is an in-memory task.Service for milestone-task tests.
type fakeTaskService struct {
	raised   map[string]task.RaiseTaskRequest
	resolved map[string]bool
}

func newFakeTaskService() *fakeTaskService {
	return &fakeTaskService{raised: make(map[string]task.RaiseTaskRequest), resolved: make(map[string]bool)}
}

func (f *fakeTaskService) List(_ context.Context, _ domain.MerchantID, _ *domain.StoreID, _ string, _ int, _ *domain.TaskOwner, _ *domain.TaskState) (domain.ListOf[domain.Task], error) {
	return domain.ListOf[domain.Task]{}, task.ErrNotImplemented
}

func (f *fakeTaskService) Get(_ context.Context, _ domain.MerchantID, taskID string) (domain.Task, error) {
	if f.resolved[taskID] {
		return domain.Task{TaskID: taskID, State: domain.TaskStateDone}, nil
	}
	if _, ok := f.raised[taskID]; ok {
		return domain.Task{TaskID: taskID, State: domain.TaskStateOpen}, nil
	}
	return domain.Task{}, task.ErrNotFound
}

func (f *fakeTaskService) Resolve(_ context.Context, _ domain.MerchantID, taskID string, _ domain.ResolveTaskRequest) (domain.Task, error) {
	req, ok := f.raised[taskID]
	if !ok {
		return domain.Task{}, task.ErrNotFound
	}
	if req.Resolution == string(domain.TaskResolutionExternalWait) {
		return domain.Task{}, task.ErrNotResolvable
	}
	f.resolved[taskID] = true
	return domain.Task{TaskID: taskID, State: domain.TaskStateDone}, nil
}

func (f *fakeTaskService) Complete(_ context.Context, _ domain.MerchantID, taskID string, _ *string) (domain.Task, error) {
	if _, ok := f.raised[taskID]; !ok {
		return domain.Task{}, task.ErrNotFound
	}
	f.resolved[taskID] = true
	return domain.Task{TaskID: taskID, State: domain.TaskStateDone}, nil
}

func (f *fakeTaskService) Reopen(_ context.Context, _ domain.MerchantID, taskID string, _ *string) (domain.Task, error) {
	return domain.Task{TaskID: taskID, State: domain.TaskStateOpen}, nil
}
func (f *fakeTaskService) Raise(_ context.Context, _ domain.MerchantID, req task.RaiseTaskRequest) (domain.Task, error) {
	f.raised[req.TaskID] = req
	return domain.Task{TaskID: req.TaskID}, nil
}

func (f *fakeTaskService) ListOpenByMerchants(_ context.Context, _ []domain.MerchantID) (map[domain.StoreID]domain.StoreTaskSummary, error) {
	return nil, task.ErrNotImplemented
}

func TestOrchestrator_EnsureMilestoneTask_RaisesOnFirstCall(t *testing.T) {
	tasks := newFakeTaskService()
	store := NewMemoryConformanceStore()
	orch := NewOrchestrator(nil, store, nil, nil, nil, testLogger(), "2026-04-08").WithTaskService(tasks)

	err := orch.EnsureMilestoneTask(context.Background(), "merchant123", "store-1")
	if err != nil {
		t.Fatalf("EnsureMilestoneTask failed: %v", err)
	}

	req, ok := tasks.raised["validation:store-1:run"]
	if !ok {
		t.Fatal("expected the milestone task to be raised")
	}
	if req.Owner != string(domain.TaskOwnerSarvex) {
		t.Errorf("expected owner sarvex, got %s", req.Owner)
	}
	if req.Resolution != string(domain.TaskResolutionOneClick) {
		t.Errorf("expected resolution one_click, got %s", req.Resolution)
	}
	if req.Source != string(domain.TaskSourceValidation) {
		t.Errorf("expected source validation, got %s", req.Source)
	}
}

func TestOrchestrator_EnsureMilestoneTask_IdempotentAcrossCalls(t *testing.T) {
	tasks := newFakeTaskService()
	store := NewMemoryConformanceStore()
	orch := NewOrchestrator(nil, store, nil, nil, nil, testLogger(), "2026-04-08").WithTaskService(tasks)

	if err := orch.EnsureMilestoneTask(context.Background(), "merchant123", "store-1"); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if err := orch.EnsureMilestoneTask(context.Background(), "merchant123", "store-1"); err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	if len(tasks.raised) != 1 {
		t.Errorf("expected exactly 1 raised task, got %d", len(tasks.raised))
	}
}

func TestOrchestrator_EnsureMilestoneTask_NoTaskService_NoOp(t *testing.T) {
	store := NewMemoryConformanceStore()
	orch := NewOrchestrator(nil, store, nil, nil, nil, testLogger(), "2026-04-08")

	if err := orch.EnsureMilestoneTask(context.Background(), "merchant123", "store-1"); err != nil {
		t.Fatalf("expected no-op, got error: %v", err)
	}
}

// TestOrchestrator_EnsureMilestoneTask_PlanAware checks that the
// pre-validation milestone's owner follows the merchant's plan once a
// PlanReader is wired: Sarvex runs it hands-on for concierge/scale
// merchants, a self-serve merchant runs it themselves.
func TestOrchestrator_EnsureMilestoneTask_PlanAware(t *testing.T) {
	cases := []struct {
		name           string
		plans          *fakePlanReader
		wantOwner      string
		wantResolution string
	}{
		{"no plan reader defaults to sarvex", nil, "sarvex", "one_click"},
		{"concierge is sarvex-owned", &fakePlanReader{plan: domain.PlanConcierge}, "sarvex", "one_click"},
		{"scale is sarvex-owned", &fakePlanReader{plan: domain.PlanScale}, "sarvex", "one_click"},
		{"self_serve is merchant-owned", &fakePlanReader{plan: domain.PlanSelfServe}, "merchant", "guided"},
		{"plan lookup failure defaults to sarvex", &fakePlanReader{err: errors.New("boom")}, "sarvex", "one_click"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tasks := newFakeTaskService()
			store := NewMemoryConformanceStore()
			orch := NewOrchestrator(nil, store, nil, nil, nil, testLogger(), "2026-04-08").WithTaskService(tasks)
			if tc.plans != nil {
				orch = orch.WithPlanReader(tc.plans)
			}

			if err := orch.EnsureMilestoneTask(context.Background(), "merchant123", "store-1"); err != nil {
				t.Fatalf("EnsureMilestoneTask failed: %v", err)
			}

			req, ok := tasks.raised["validation:store-1:run"]
			if !ok {
				t.Fatal("expected the milestone task to be raised")
			}
			if req.Owner != tc.wantOwner {
				t.Errorf("expected owner %s, got %s", tc.wantOwner, req.Owner)
			}
			if req.Resolution != tc.wantResolution {
				t.Errorf("expected resolution %s, got %s", tc.wantResolution, req.Resolution)
			}
		})
	}
}

func TestOrchestrator_RunValidation_ResolvesMilestoneTask(t *testing.T) {
	harness := &stubTestHarness{
		results: &TestResults{Passed: []string{"test1"}, Failed: []*domain.ValidationFailure{}},
	}
	tasks := newFakeTaskService()
	store := NewMemoryConformanceStore()

	orch := NewOrchestrator(harness, store, &stubPlatformResolver{platform: "woocommerce"}, nil, nil, testLogger(), "2026-04-08").WithTaskService(tasks)

	if err := orch.EnsureMilestoneTask(context.Background(), "merchant123", "store1"); err != nil {
		t.Fatalf("EnsureMilestoneTask failed: %v", err)
	}
	if tasks.resolved["validation:store1:run"] {
		t.Fatal("milestone task should not be resolved before a run executes")
	}

	if _, err := orch.RunValidation(context.Background(), "merchant123", "store1"); err != nil {
		t.Fatalf("RunValidation failed: %v", err)
	}

	if !tasks.resolved["validation:store1:run"] {
		t.Error("expected the milestone task to be resolved after RunValidation")
	}
}

func TestOrchestrator_RunValidation_ResolvesMilestoneTaskEvenOnFailure(t *testing.T) {
	harness := &stubTestHarness{
		results: &TestResults{
			Passed: []string{},
			Failed: []*domain.ValidationFailure{{Requirement: "profile_required"}},
		},
	}
	tasks := newFakeTaskService()
	store := NewMemoryConformanceStore()

	orch := NewOrchestrator(harness, store, &stubPlatformResolver{platform: "woocommerce"}, nil, nil, testLogger(), "2026-04-08").WithTaskService(tasks)

	if err := orch.EnsureMilestoneTask(context.Background(), "merchant123", "store1"); err != nil {
		t.Fatalf("EnsureMilestoneTask failed: %v", err)
	}

	if _, err := orch.RunValidation(context.Background(), "merchant123", "store1"); err != nil {
		t.Fatalf("RunValidation failed: %v", err)
	}

	// The milestone ("you must run validation") is discharged regardless of
	// pass/fail — a failing run separately raises its own detected task.
	if !tasks.resolved["validation:store1:run"] {
		t.Error("expected the milestone task to be resolved even though the run failed")
	}
}
