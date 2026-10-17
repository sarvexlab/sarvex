// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package journey_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/journey"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/task"
)

// recordingTaskService is a task.Service that actually stores what it is
// given, so milestone raise/resolve can be asserted end to end. The shared
// fakeTaskService returns ErrNotImplemented from Get/Raise/Resolve, which is
// enough for the journey's read paths but not for this.
type recordingTaskService struct {
	byID   map[string]domain.Task
	raised []task.RaiseTaskRequest
}

func newRecordingTaskService() *recordingTaskService {
	return &recordingTaskService{byID: map[string]domain.Task{}}
}

func (r *recordingTaskService) List(_ context.Context, _ domain.MerchantID, _ *domain.StoreID, _ string, _ int, _ *domain.TaskOwner, _ *domain.TaskState) (domain.ListOf[domain.Task], error) {
	out := make([]domain.Task, 0, len(r.byID))
	for _, t := range r.byID {
		out = append(out, t)
	}
	return domain.ListOf[domain.Task]{Data: out}, nil
}

func (r *recordingTaskService) Get(_ context.Context, _ domain.MerchantID, taskID string) (domain.Task, error) {
	t, ok := r.byID[taskID]
	if !ok {
		return domain.Task{}, task.ErrNotFound
	}
	return t, nil
}

// Resolve mirrors TaskService.Resolve's standing check, external_wait
// included. Without it this fake would accept closes the real service
// refuses, and the milestone tests would assert behaviour production does
// not have — which is exactly how the two Google milestones came to sit on
// the Path forever while these tests stayed green.
func (r *recordingTaskService) Resolve(_ context.Context, _ domain.MerchantID, taskID string, _ domain.ResolveTaskRequest) (domain.Task, error) {
	t, ok := r.byID[taskID]
	if !ok {
		return domain.Task{}, task.ErrNotFound
	}
	if t.State == domain.TaskStateDone || t.Resolution == domain.TaskResolutionExternalWait {
		return domain.Task{}, task.ErrNotResolvable
	}
	t.State = domain.TaskStateDone
	r.byID[taskID] = t
	return t, nil
}

func (r *recordingTaskService) Complete(_ context.Context, _ domain.MerchantID, taskID string, note *string) (domain.Task, error) {
	t, ok := r.byID[taskID]
	if !ok {
		return domain.Task{}, task.ErrNotFound
	}
	t.State = domain.TaskStateDone
	t.Note = note
	r.byID[taskID] = t
	return t, nil
}

func (r *recordingTaskService) Reopen(_ context.Context, _ domain.MerchantID, taskID string, _ *string) (domain.Task, error) {
	return domain.Task{TaskID: taskID, State: domain.TaskStateOpen}, nil
}
func (r *recordingTaskService) Raise(_ context.Context, _ domain.MerchantID, req task.RaiseTaskRequest) (domain.Task, error) {
	t := domain.Task{
		TaskID:     req.TaskID,
		StoreID:    domain.StoreID(req.StoreID),
		Title:      req.Title,
		Owner:      domain.TaskOwner(req.Owner),
		Resolution: domain.TaskResolution(req.Resolution),
		// Derived, exactly as the real service does it — a fake that let a
		// caller name its own state would hide the drift the derivation
		// exists to prevent.
		State:     domain.DeriveTaskState(domain.TaskOwner(req.Owner), domain.TaskResolution(req.Resolution)),
		Source:    domain.TaskSource(req.Source),
		SourceRef: req.SourceRef,
	}
	r.byID[req.TaskID] = t
	r.raised = append(r.raised, req)
	return t, nil
}

func (r *recordingTaskService) ListOpenByMerchants(_ context.Context, _ []domain.MerchantID) (map[domain.StoreID]domain.StoreTaskSummary, error) {
	return nil, task.ErrNotImplemented
}

func newMilestoneService() (*recordingTaskService, journey.Service) {
	repo := &fakeRepo{docs: make(map[string]*journey.JourneyModel)}
	tasks := newRecordingTaskService()
	return tasks, journey.NewJourneyService(repo, tasks, &fakeEventBus{}, nil)
}

const (
	mID = domain.MerchantID("merchant1")
	sID = domain.StoreID("store1")
)

// Task ids are asserted literally rather than via the unexported helper:
// they are the spine's stable identity (source:store:source_ref) and the
// console's widget registry matches on the source/source_ref inside them.
const (
	importTaskID = "catalog_import:store1:initial_import"
	auditTaskID  = "readiness:store1:initial_audit"
)

// advance walks the funnel from prospect to the named state.
func advance(t *testing.T, svc journey.Service, signals ...journey.Signal) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, svc.Initialize(ctx, mID, sID))
	for _, sig := range signals {
		require.NoError(t, svc.HandleSignal(ctx, mID, sID, sig))
	}
}

func TestMilestones_RaisedOnEnteringTheirState(t *testing.T) {
	tests := []struct {
		name    string
		signals []journey.Signal
		taskID  string
		title   string
		owner   domain.TaskOwner
		state   domain.TaskState
	}{
		{
			name:    "connected raises the catalog import",
			signals: []journey.Signal{journey.SignalConnectorCreated},
			taskID:  importTaskID,
			title:   "Importing your catalog",
			owner:   domain.TaskOwnerSarvex,
			state:   domain.TaskStateInProgress,
		},
		{
			name:    "synced raises the readiness audit",
			signals: []journey.Signal{journey.SignalConnectorCreated, journey.SignalFirstSyncComplete},
			taskID:  auditTaskID,
			title:   "Auditing your catalog",
			owner:   domain.TaskOwnerSarvex,
			state:   domain.TaskStateInProgress,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tasks, svc := newMilestoneService()
			advance(t, svc, tt.signals...)

			got, err := tasks.Get(context.Background(), mID, tt.taskID)
			require.NoError(t, err)
			assert.Equal(t, tt.title, got.Title)
			assert.Equal(t, tt.owner, got.Owner)
			assert.Equal(t, tt.state, got.State)
			assert.NotEmpty(t, got.SourceRef)
		})
	}
}

// Leaving a state is what finishes its milestone — the import is done
// because the store synced, the audit is done because it scored.
func TestMilestones_ResolvedOnLeavingTheirState(t *testing.T) {
	tasks, svc := newMilestoneService()
	ctx := context.Background()

	advance(t, svc, journey.SignalConnectorCreated)
	imported, err := tasks.Get(ctx, mID, importTaskID)
	require.NoError(t, err)
	require.Equal(t, domain.TaskStateInProgress, imported.State)

	require.NoError(t, svc.HandleSignal(ctx, mID, sID, journey.SignalFirstSyncComplete))

	imported, err = tasks.Get(ctx, mID, importTaskID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStateDone, imported.State)

	// And the next state's milestone is now the open one.
	audit, err := tasks.Get(ctx, mID, auditTaskID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStateInProgress, audit.State)
}

func TestMilestones_LiveResolvesEverything(t *testing.T) {
	tasks, svc := newMilestoneService()
	advance(t, svc,
		journey.SignalConnectorCreated, journey.SignalFirstSyncComplete,
		journey.SignalReadinessThresholdMet, journey.SignalStoreActivated,
	)

	for _, id := range []string{importTaskID, auditTaskID} {
		got, err := tasks.Get(context.Background(), mID, id)
		require.NoError(t, err, id)
		assert.Equal(t, domain.TaskStateDone, got.State, id)
	}
}

// Reconciliation converges on the current state, so a store that skipped
// transitions (or predates this wiring) catches up on the next read without
// a backfill — Get calls reconcileStageWork on every read.
func TestMilestones_SelfHealOnRead(t *testing.T) {
	tasks, svc := newMilestoneService()
	ctx := context.Background()

	advance(t, svc, journey.SignalConnectorCreated, journey.SignalFirstSyncComplete)

	// Wipe every trace, as though the store reached synced before milestones
	// existed.
	tasks.byID = map[string]domain.Task{}
	tasks.raised = nil

	_, err := svc.Get(ctx, mID, sID)
	require.NoError(t, err)

	audit, err := tasks.Get(ctx, mID, auditTaskID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStateInProgress, audit.State)
}

func TestMilestones_RaiseIsIdempotent(t *testing.T) {
	tasks, svc := newMilestoneService()
	ctx := context.Background()

	advance(t, svc, journey.SignalConnectorCreated)
	for range 3 {
		_, err := svc.Get(ctx, mID, sID)
		require.NoError(t, err)
	}

	raisedImport := 0
	for _, r := range tasks.raised {
		if r.TaskID == importTaskID {
			raisedImport++
		}
	}
	assert.Equal(t, 1, raisedImport)
}

// A resolved milestone must not reopen if the store regresses — the merchant
// already watched that work finish.
func TestMilestones_ResolvedMilestoneIsNotReopened(t *testing.T) {
	tasks, svc := newMilestoneService()
	ctx := context.Background()

	advance(t, svc, journey.SignalConnectorCreated, journey.SignalFirstSyncComplete)
	require.NoError(t, svc.HandleSignal(ctx, mID, sID, journey.SignalConnectorDisconnected))

	// Back at prospect, then paired again.
	require.NoError(t, svc.HandleSignal(ctx, mID, sID, journey.SignalConnectorCreated))

	imported, err := tasks.Get(ctx, mID, importTaskID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStateDone, imported.State)
}

func TestMilestones_ProspectRaisesNothing(t *testing.T) {
	tasks, svc := newMilestoneService()
	require.NoError(t, svc.Initialize(context.Background(), mID, sID))

	_, err := svc.Get(context.Background(), mID, sID)
	require.NoError(t, err)
	assert.Empty(t, tasks.raised)
}

// Ready and Live have no milestone of their own: ready's work is the
// checklist and readiness tasks, and live is meant to be quiet.
func TestMilestones_ReadyAndLiveHaveNoMilestoneOfTheirOwn(t *testing.T) {
	tasks, svc := newMilestoneService()
	advance(t, svc,
		journey.SignalConnectorCreated, journey.SignalFirstSyncComplete,
		journey.SignalReadinessThresholdMet,
	)

	for _, r := range tasks.raised {
		assert.NotEqual(t, domain.StoreID(""), r.StoreID)
	}
	// Only the two earlier milestones exist, both resolved by now.
	assert.Len(t, tasks.raised, 2)
	for _, id := range []string{importTaskID, auditTaskID} {
		got, err := tasks.Get(context.Background(), mID, id)
		require.NoError(t, err, id)
		assert.Equal(t, domain.TaskStateDone, got.State, id)
	}
}
