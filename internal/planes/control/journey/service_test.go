// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package journey_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/journey"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRepo is a map-backed Repository for tests.
type fakeRepo struct {
	docs map[string]*journey.JourneyModel
}

func (r *fakeRepo) Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (*journey.JourneyModel, error) {
	m, ok := r.docs[string(storeID)]
	if !ok || m.MerchantID != string(merchantID) {
		return nil, journey.ErrNotFound
	}
	// Return a copy to prevent test mutations.
	c := *m
	return &c, nil
}

func (r *fakeRepo) Upsert(ctx context.Context, m *journey.JourneyModel) error {
	c := *m
	r.docs[m.ID] = &c
	return nil
}

// fakeTaskService is a stub task.Service for tests.
type fakeTaskService struct {
	tasks []domain.Task
	err   error
}

func (f *fakeTaskService) List(ctx context.Context, id domain.MerchantID, storeID *domain.StoreID, cursor string, limit int, owner *domain.TaskOwner, state *domain.TaskState) (domain.ListOf[domain.Task], error) {
	if f.err != nil {
		return domain.ListOf[domain.Task]{}, f.err
	}
	return domain.ListOf[domain.Task]{
		Data: f.tasks,
		Page: domain.Page{Cursor: nil, HasMore: false},
	}, nil
}

func (f *fakeTaskService) Get(ctx context.Context, id domain.MerchantID, taskID string) (domain.Task, error) {
	return domain.Task{}, task.ErrNotImplemented
}

func (f *fakeTaskService) Resolve(ctx context.Context, id domain.MerchantID, taskID string, req domain.ResolveTaskRequest) (domain.Task, error) {
	return domain.Task{}, task.ErrNotImplemented
}

func (f *fakeTaskService) Complete(ctx context.Context, id domain.MerchantID, taskID string, note *string) (domain.Task, error) {
	return domain.Task{}, task.ErrNotImplemented
}

func (f *fakeTaskService) Reopen(_ context.Context, _ domain.MerchantID, taskID string, _ *string) (domain.Task, error) {
	return domain.Task{TaskID: taskID, State: domain.TaskStateOpen}, nil
}
func (f *fakeTaskService) Raise(ctx context.Context, id domain.MerchantID, req task.RaiseTaskRequest) (domain.Task, error) {
	return domain.Task{}, task.ErrNotImplemented
}

func (f *fakeTaskService) ListOpenByMerchants(ctx context.Context, merchantIDs []domain.MerchantID) (map[domain.StoreID]domain.StoreTaskSummary, error) {
	return nil, task.ErrNotImplemented
}

func newTestJourneyService() (*fakeRepo, *fakeTaskService, journey.Service) {
	repo := &fakeRepo{docs: make(map[string]*journey.JourneyModel)}
	tasks := &fakeTaskService{}
	svc := journey.NewJourneyService(repo, tasks, &fakeEventBus{}, nil)
	return repo, tasks, svc
}

func TestJourneyService_Get_StageInfo(t *testing.T) {
	_, _, svc := newTestJourneyService()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	require.NoError(t, svc.Initialize(ctx, id, domain.StoreID(id)))

	j, err := svc.Get(ctx, id, domain.StoreID(id))
	require.NoError(t, err)
	assert.Equal(t, "Prospect", j.StageName)
	assert.NotEmpty(t, j.StageDescription)
	assert.NotEmpty(t, j.OperatorActions)
	assert.Equal(t, 7, j.TypicalDaysInStage)
	assert.GreaterOrEqual(t, j.StageElapsedDays, 0)
	// Prospect has no RequiredTasks, so no status rollup.
	assert.Empty(t, j.RequiredTasksStatus)
}

func TestJourneyService_Get_RequiredTasksStatus_Unresolved(t *testing.T) {
	_, tasks, svc := newTestJourneyService()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	require.NoError(t, svc.Initialize(ctx, id, domain.StoreID(id)))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalConnectorCreated))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalFirstSyncComplete))

	// Remediation stage requires the merchant owner to have no open tasks.
	tasks.tasks = []domain.Task{
		{TaskID: "t1", Owner: domain.TaskOwnerMerchant, State: domain.TaskStateOpen},
		{TaskID: "t2", Owner: domain.TaskOwnerSarvex, State: domain.TaskStateOpen},
	}

	j, err := svc.Get(ctx, id, domain.StoreID(id))
	require.NoError(t, err)
	assert.Equal(t, domain.StageRemediation, j.Stage)
	require.Len(t, j.RequiredTasksStatus, 1)
	assert.Equal(t, domain.TaskOwnerMerchant, j.RequiredTasksStatus[0].Owner)
	assert.False(t, j.RequiredTasksStatus[0].Resolved)
}

func TestJourneyService_Get_RequiredTasksStatus_Resolved(t *testing.T) {
	_, tasks, svc := newTestJourneyService()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	require.NoError(t, svc.Initialize(ctx, id, domain.StoreID(id)))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalConnectorCreated))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalFirstSyncComplete))

	// All merchant-owned tasks resolved; sarvex's optional task stays open.
	tasks.tasks = []domain.Task{
		{TaskID: "t1", Owner: domain.TaskOwnerMerchant, State: domain.TaskStateDone},
		{TaskID: "t2", Owner: domain.TaskOwnerSarvex, State: domain.TaskStateOpen},
	}

	j, err := svc.Get(ctx, id, domain.StoreID(id))
	require.NoError(t, err)
	require.Len(t, j.RequiredTasksStatus, 1)
	assert.Equal(t, domain.TaskOwnerMerchant, j.RequiredTasksStatus[0].Owner)
	assert.True(t, j.RequiredTasksStatus[0].Resolved)
}

func TestJourneyService_Get_NotFound(t *testing.T) {
	_, _, svc := newTestJourneyService()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	_, err := svc.Get(ctx, id, domain.StoreID("nonexistent"))
	assert.ErrorIs(t, err, journey.ErrNotFound)
}

func TestJourneyService_Initialize_CreatesProspect(t *testing.T) {
	_, _, svc := newTestJourneyService()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	err := svc.Initialize(ctx, id, domain.StoreID(id))
	require.NoError(t, err)

	j, err := svc.Get(ctx, id, domain.StoreID(id))
	require.NoError(t, err)
	assert.Equal(t, domain.JourneyProspect, j.State)
	assert.Equal(t, domain.StageProsepct, j.Stage)
	assert.NotZero(t, j.EnteredStateAt)
}

func TestJourneyService_Initialize_Idempotent(t *testing.T) {
	_, _, svc := newTestJourneyService()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	err := svc.Initialize(ctx, id, domain.StoreID(id))
	require.NoError(t, err)

	j1, _ := svc.Get(ctx, id, domain.StoreID(id))
	t1 := j1.EnteredStateAt

	// Initialize again; should be idempotent.
	time.Sleep(10 * time.Millisecond)
	err = svc.Initialize(ctx, id, domain.StoreID(id))
	require.NoError(t, err)

	j2, _ := svc.Get(ctx, id, domain.StoreID(id))
	t2 := j2.EnteredStateAt

	assert.Equal(t, t1, t2)
}

func TestJourneyService_HandleSignal_ValidTransition(t *testing.T) {
	_, _, svc := newTestJourneyService()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	// Initialize to prospect.
	err := svc.Initialize(ctx, id, domain.StoreID(id))
	require.NoError(t, err)

	// Send connector_created signal (prospect → connected).
	err = svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalConnectorCreated)
	require.NoError(t, err)

	j, _ := svc.Get(ctx, id, domain.StoreID(id))
	assert.Equal(t, domain.JourneyConnected, j.State)
	assert.Equal(t, domain.StageKickoff, j.Stage)

	// Send first_sync_complete signal (connected → synced).
	err = svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalFirstSyncComplete)
	require.NoError(t, err)

	j, _ = svc.Get(ctx, id, domain.StoreID(id))
	assert.Equal(t, domain.JourneySynced, j.State)
	assert.Equal(t, domain.StageRemediation, j.Stage)
}

func TestJourneyService_HandleSignal_FullTransitionPath(t *testing.T) {
	_, _, svc := newTestJourneyService()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	svc.Initialize(ctx, id, domain.StoreID(id))

	// Walk through full transition path.
	svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalConnectorCreated)
	j, _ := svc.Get(ctx, id, domain.StoreID(id))
	assert.Equal(t, domain.JourneyConnected, j.State)
	assert.Equal(t, domain.StageKickoff, j.Stage)

	svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalFirstSyncComplete)
	j, _ = svc.Get(ctx, id, domain.StoreID(id))
	assert.Equal(t, domain.JourneySynced, j.State)
	assert.Equal(t, domain.StageRemediation, j.Stage)

	svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalReadinessThresholdMet)
	j, _ = svc.Get(ctx, id, domain.StoreID(id))
	assert.Equal(t, domain.JourneyReady, j.State)

	_ = svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalStoreActivated)
	j, _ = svc.Get(ctx, id, domain.StoreID(id))
	assert.Equal(t, domain.JourneyLive, j.State)
}

// fakeActivationReader is a minimal journey.ActivationStateReader for tests.
type fakeActivationReader struct {
	state domain.UCPState
	err   error
}

func (f *fakeActivationReader) Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Activation, error) {
	if f.err != nil {
		return domain.Activation{}, f.err
	}
	return domain.Activation{StoreID: storeID, UCPState: f.state}, nil
}

// TestJourneyService_HandleSignal_SelfHealsReadyToLiveWhenActivationAlreadyActive
// pins the fix for a race the conformance suite hit: control.store.activation_state_changed
// fires at most once, on the accepted transition to active — never on a
// timer — and journey/subscriber.go's HandleActivation reacts to that single
// event by sending SignalStoreActivated, which transitionTable only matches
// from Ready. If activation reaches "active" before the journey reaches
// Ready (readiness/catalog import still in flight), that earlier signal
// finds no matching transition, gets logged and dropped, and nothing ever
// re-sends it — the journey is stuck at Ready forever even though the store
// is genuinely active. Reaching Ready must check activation's current state
// itself and finish the transition to Live immediately when it's already
// there, instead of waiting on a signal that will never arrive again.
func TestJourneyService_HandleSignal_SelfHealsReadyToLiveWhenActivationAlreadyActive(t *testing.T) {
	repo := &fakeRepo{docs: make(map[string]*journey.JourneyModel)}
	activationReader := &fakeActivationReader{state: domain.UCPStateActive}
	svc := journey.NewJourneyService(repo, &fakeTaskService{}, &fakeEventBus{}, nil).
		WithActivationStateReader(activationReader)

	ctx := context.Background()
	id := domain.MerchantID("merchant1")
	require.NoError(t, svc.Initialize(ctx, id, domain.StoreID(id)))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalConnectorCreated))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalFirstSyncComplete))

	// The critical step: activation is already "active" per the fake reader,
	// but SignalStoreActivated is never sent — exactly the race where the
	// real event fired too early and was dropped for good.
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalReadinessThresholdMet))

	j, err := svc.Get(ctx, id, domain.StoreID(id))
	require.NoError(t, err)
	assert.Equal(t, domain.JourneyLive, j.State, "reaching Ready while activation is already active must self-heal straight to Live")
}

// TestJourneyService_HandleSignal_DoesNotSelfHealWhenNotYetActive guards the
// other half of the fix above: reaching Ready while activation genuinely
// isn't active yet must leave the journey at Ready, waiting for the real
// SignalStoreActivated event as normal — the self-heal must not skip ahead
// of activation's own state.
func TestJourneyService_HandleSignal_DoesNotSelfHealWhenNotYetActive(t *testing.T) {
	repo := &fakeRepo{docs: make(map[string]*journey.JourneyModel)}
	activationReader := &fakeActivationReader{state: domain.UCPStateVerified}
	svc := journey.NewJourneyService(repo, &fakeTaskService{}, &fakeEventBus{}, nil).
		WithActivationStateReader(activationReader)

	ctx := context.Background()
	id := domain.MerchantID("merchant1")
	require.NoError(t, svc.Initialize(ctx, id, domain.StoreID(id)))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalConnectorCreated))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalFirstSyncComplete))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalReadinessThresholdMet))

	j, err := svc.Get(ctx, id, domain.StoreID(id))
	require.NoError(t, err)
	assert.Equal(t, domain.JourneyReady, j.State)
}

// seedJourneyState drives svc's journey for id from Prospect up to (and
// including) target via the real signal sequence, so tests exercise actual
// transitions rather than hand-constructed fixtures.
func seedJourneyState(t *testing.T, svc journey.Service, ctx context.Context, id domain.MerchantID, target domain.JourneyState) {
	t.Helper()
	sequence := []journey.Signal{
		journey.SignalConnectorCreated,
		journey.SignalFirstSyncComplete,
		journey.SignalReadinessThresholdMet,
		journey.SignalStoreActivated,
	}
	states := []domain.JourneyState{
		domain.JourneyConnected,
		domain.JourneySynced,
		domain.JourneyReady,
		domain.JourneyLive,
	}
	for i, sig := range sequence {
		require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), sig))
		if states[i] == target {
			return
		}
	}
}

// TestJourneyService_HandleSignal_ConnectorDisconnectedRegressesToProspect
// guards the fix for a console bug: disconnecting a connector left the
// journey (and thus the console's Path view) parked at whatever state it
// was in, with nothing telling an open session to route back to /connect.
// SignalConnectorDisconnected must regress every state from Connected
// onward straight to Prospect, since ConnectorService.Disconnect deletes
// the connector entirely — re-pairing has to start over regardless of how
// far onboarding had progressed.
func TestJourneyService_HandleSignal_ConnectorDisconnectedRegressesToProspect(t *testing.T) {
	cases := []domain.JourneyState{
		domain.JourneyConnected,
		domain.JourneySynced,
		domain.JourneyReady,
		domain.JourneyLive,
	}
	for _, from := range cases {
		t.Run(string(from), func(t *testing.T) {
			_, _, svc := newTestJourneyService()
			ctx := context.Background()
			id := domain.MerchantID("merchant1")

			require.NoError(t, svc.Initialize(ctx, id, domain.StoreID(id)))
			seedJourneyState(t, svc, ctx, id, from)

			require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalConnectorDisconnected))

			j, err := svc.Get(ctx, id, domain.StoreID(id))
			require.NoError(t, err)
			assert.Equal(t, domain.JourneyProspect, j.State)
			assert.Equal(t, domain.StageProsepct, j.Stage)
		})
	}
}

func TestJourneyService_HandleSignal_NoOpOnUnknownSignal(t *testing.T) {
	_, _, svc := newTestJourneyService()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	svc.Initialize(ctx, id, domain.StoreID(id))

	j1, _ := svc.Get(ctx, id, domain.StoreID(id))
	state1 := j1.State

	// Send an invalid signal for the current state.
	err := svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalStoreActivated)
	require.NoError(t, err)

	j2, _ := svc.Get(ctx, id, domain.StoreID(id))
	// State should not change.
	assert.Equal(t, state1, j2.State)
}

func TestJourneyService_HandleSignal_NoOpOnDuplicateSignal(t *testing.T) {
	_, _, svc := newTestJourneyService()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	svc.Initialize(ctx, id, domain.StoreID(id))
	svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalConnectorCreated)  // prospect → connected
	svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalFirstSyncComplete) // connected → synced

	j1, _ := svc.Get(ctx, id, domain.StoreID(id))

	// Send the same signal again.
	time.Sleep(10 * time.Millisecond)
	err := svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalFirstSyncComplete)
	require.NoError(t, err)

	j2, _ := svc.Get(ctx, id, domain.StoreID(id))
	// State and timestamp should not change.
	assert.Equal(t, j1.State, j2.State)
	assert.Equal(t, j1.EnteredStateAt, j2.EnteredStateAt)
}

func TestJourneyService_HandleSignal_NonexistentMerchant(t *testing.T) {
	_, _, svc := newTestJourneyService()
	ctx := context.Background()
	id := domain.MerchantID("nonexistent")

	// Should silently no-op (idempotent).
	err := svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalFirstSyncComplete)
	require.NoError(t, err)
}

func TestJourneyService_Get_ComputesTaskCounts(t *testing.T) {
	_, taskSvc, svc := newTestJourneyService()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	svc.Initialize(ctx, id, domain.StoreID(id))

	// Populate task service with some tasks.
	taskSvc.tasks = []domain.Task{
		{TaskID: "t1", Owner: domain.TaskOwner("merchant")},
		{TaskID: "t2", Owner: domain.TaskOwner("merchant")},
		{TaskID: "t3", Owner: domain.TaskOwner("sarvex")},
	}

	j, _ := svc.Get(ctx, id, domain.StoreID(id))
	assert.Equal(t, 2, j.OpenTaskCounts.Merchant)
	assert.Equal(t, 1, j.OpenTaskCounts.Sarvex)
	assert.Equal(t, 0, j.OpenTaskCounts.External)
	assert.NotNil(t, j.NextAction)
	assert.Equal(t, "t1", j.NextAction.TaskID)
}

func TestJourneyService_Get_ZeroTaskCountsOnStubError(t *testing.T) {
	_, taskSvc, svc := newTestJourneyService()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	svc.Initialize(ctx, id, domain.StoreID(id))

	// Set task service to return ErrNotImplemented (stub state).
	taskSvc.err = task.ErrNotImplemented

	j, _ := svc.Get(ctx, id, domain.StoreID(id))
	assert.Equal(t, 0, j.OpenTaskCounts.Merchant)
	assert.Equal(t, 0, j.OpenTaskCounts.Sarvex)
	assert.Equal(t, 0, j.OpenTaskCounts.External)
	assert.Nil(t, j.NextAction)
}

func TestJourneyService_Get_ZeroTaskCountsOnOtherError(t *testing.T) {
	_, taskSvc, svc := newTestJourneyService()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	svc.Initialize(ctx, id, domain.StoreID(id))

	// Set task service to return a different error.
	taskSvc.err = errors.New("some error")

	// Should not fail, just log the error and use zero values.
	j, err := svc.Get(ctx, id, domain.StoreID(id))
	require.NoError(t, err)
	assert.Equal(t, 0, j.OpenTaskCounts.Merchant)
	assert.Nil(t, j.NextAction)
}

func TestJourneyService_History_RecordsEachTransition(t *testing.T) {
	_, _, svc := newTestJourneyService()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	svc.Initialize(ctx, id, domain.StoreID(id))
	svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalConnectorCreated)  // prospect -> connected
	svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalFirstSyncComplete) // connected -> synced

	j, err := svc.Get(ctx, id, domain.StoreID(id))
	require.NoError(t, err)

	require.Len(t, j.History, 3)
	assert.Equal(t, domain.JourneyProspect, j.History[0].State)
	assert.Equal(t, domain.JourneyConnected, j.History[1].State)
	assert.Equal(t, domain.JourneySynced, j.History[2].State)
	assert.Equal(t, j.EnteredStateAt, j.History[2].EnteredAt)
}

func TestJourneyService_History_UnaffectedByRejectedTransition(t *testing.T) {
	_, _, svc := newTestJourneyService()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	svc.Initialize(ctx, id, domain.StoreID(id))
	svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalConnectorCreated) // prospect -> connected

	// Illegal transition: skip straight to live.
	err := svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalStoreActivated)
	require.NoError(t, err)

	j, err := svc.Get(ctx, id, domain.StoreID(id))
	require.NoError(t, err)
	require.Len(t, j.History, 2)
	assert.Equal(t, domain.JourneyConnected, j.State)
}

func TestJourneyService_AvoidReadinessLostOutOfOrder(t *testing.T) {
	_, _, svc := newTestJourneyService()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	svc.Initialize(ctx, id, domain.StoreID(id))
	svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalConnectorCreated) // prospect → connected

	// Connected state; try to send readiness_threshold_lost (invalid transition).
	err := svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalReadinessThresholdLost)
	require.NoError(t, err)

	j, _ := svc.Get(ctx, id, domain.StoreID(id))
	// Should still be in connected state (invalid signal ignored).
	assert.Equal(t, domain.JourneyConnected, j.State)
}
