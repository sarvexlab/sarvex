// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package journey_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/journey"
)

// fakeValidationMilestoneRaiser is a test double for journey.ValidationMilestoneRaiser.
type fakeValidationMilestoneRaiser struct {
	calls []domain.StoreID
	err   error
}

func (f *fakeValidationMilestoneRaiser) EnsureMilestoneTask(_ context.Context, _ domain.MerchantID, storeID domain.StoreID) error {
	f.calls = append(f.calls, storeID)
	return f.err
}

func newTestJourneyServiceWithValidationRaiser() (*fakeValidationMilestoneRaiser, journey.Service) {
	repo := &fakeRepo{docs: make(map[string]*journey.JourneyModel)}
	validationRaiser := &fakeValidationMilestoneRaiser{}
	svc := journey.NewJourneyService(repo, &fakeTaskService{}, &fakeEventBus{}, nil).
		WithValidationMilestoneRaiser(validationRaiser)
	return validationRaiser, svc
}

func TestJourneyService_HandleSignal_EnteringReady_RaisesValidationMilestone(t *testing.T) {
	validationRaiser, svc := newTestJourneyServiceWithValidationRaiser()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	require.NoError(t, svc.Initialize(ctx, id, domain.StoreID(id)))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalConnectorCreated))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalFirstSyncComplete))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalReadinessThresholdMet))

	require.Len(t, validationRaiser.calls, 1)
	assert.Equal(t, domain.StoreID(id), validationRaiser.calls[0])
}

func TestJourneyService_HandleSignal_OtherTransitions_DoNotRaiseValidationMilestone(t *testing.T) {
	validationRaiser, svc := newTestJourneyServiceWithValidationRaiser()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	require.NoError(t, svc.Initialize(ctx, id, domain.StoreID(id)))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalConnectorCreated))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalFirstSyncComplete))

	assert.Empty(t, validationRaiser.calls, "reaching Synced (not Ready) must not raise the validation milestone")
}

func TestJourneyService_HandleSignal_NilValidationRaiser_NoPanic(t *testing.T) {
	repo := &fakeRepo{docs: make(map[string]*journey.JourneyModel)}
	svc := journey.NewJourneyService(repo, &fakeTaskService{}, &fakeEventBus{}, nil)
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	require.NoError(t, svc.Initialize(ctx, id, domain.StoreID(id)))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalConnectorCreated))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalFirstSyncComplete))

	err := svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalReadinessThresholdMet)
	assert.NoError(t, err)
}

func TestJourneyService_HandleSignal_ValidationRaiserError_TransitionStillSucceeds(t *testing.T) {
	repo := &fakeRepo{docs: make(map[string]*journey.JourneyModel)}
	validationRaiser := &fakeValidationMilestoneRaiser{err: errors.New("boom")}
	svc := journey.NewJourneyService(repo, &fakeTaskService{}, &fakeEventBus{}, nil).
		WithValidationMilestoneRaiser(validationRaiser)
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	require.NoError(t, svc.Initialize(ctx, id, domain.StoreID(id)))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalConnectorCreated))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalFirstSyncComplete))

	err := svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalReadinessThresholdMet)
	require.NoError(t, err, "a reconciliation failure must not fail the (already-persisted) transition")

	j, err := svc.Get(ctx, id, domain.StoreID(id))
	require.NoError(t, err)
	assert.Equal(t, domain.JourneyReady, j.State)
}

// ── Get self-heals a store already parked in Ready ────────────────────────
//
// reconcileStageWork only ran from HandleSignal's transition, so a store
// that reached Ready before this wiring existed (or before validation was
// reachable for any other reason) would never get its prescribed tasks
// raised — no transition, no reconciliation, forever. Get now re-runs the
// same idempotent reconciliation on every read so the Path self-corrects
// without a backfill migration.

func TestJourneyService_Get_StoreAlreadyInReady_RaisesValidationMilestone(t *testing.T) {
	validationRaiser, svc := newTestJourneyServiceWithValidationRaiser()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	require.NoError(t, svc.Initialize(ctx, id, domain.StoreID(id)))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalConnectorCreated))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalFirstSyncComplete))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalReadinessThresholdMet))

	// Simulate a store that reached Ready before validation reconciliation
	// existed: nothing has called EnsureMilestoneTask for it yet.
	validationRaiser.calls = nil

	_, err := svc.Get(ctx, id, domain.StoreID(id))
	require.NoError(t, err)

	require.Len(t, validationRaiser.calls, 1, "Get must raise the validation milestone for a store sitting in Ready, not just on transition")
	assert.Equal(t, domain.StoreID(id), validationRaiser.calls[0])
}

func TestJourneyService_Get_OtherStates_DoesNotRaiseValidationMilestone(t *testing.T) {
	validationRaiser, svc := newTestJourneyServiceWithValidationRaiser()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	require.NoError(t, svc.Initialize(ctx, id, domain.StoreID(id)))
	require.NoError(t, svc.HandleSignal(ctx, id, domain.StoreID(id), journey.SignalConnectorCreated))

	_, err := svc.Get(ctx, id, domain.StoreID(id))
	require.NoError(t, err)

	assert.Empty(t, validationRaiser.calls, "Connected is not Ready; Get must not raise the validation milestone")
}
