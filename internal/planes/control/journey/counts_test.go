// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package journey_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// open_task_counts and next_action are the two numbers the console puts in
// front of the merchant, so both have to mean what they say. The counts used
// to include done tasks, which made them grow forever and never return to
// zero; next_action used to be whatever happened to sort first, which could
// be a task the merchant neither owns nor can act on.

func journeyAtProspect(t *testing.T, tasks []domain.Task) domain.Journey {
	t.Helper()
	_, fake, svc := newTestJourneyService()
	fake.tasks = tasks

	ctx := context.Background()
	id := domain.MerchantID("merchant1")
	require.NoError(t, svc.Initialize(ctx, id, domain.StoreID(id)))

	j, err := svc.Get(ctx, id, domain.StoreID(id))
	require.NoError(t, err)
	return j
}

func TestJourney_OpenTaskCounts_ExcludeDone(t *testing.T) {
	j := journeyAtProspect(t, []domain.Task{
		{TaskID: "m-open", Owner: domain.TaskOwnerMerchant, State: domain.TaskStateOpen},
		{TaskID: "m-done", Owner: domain.TaskOwnerMerchant, State: domain.TaskStateDone},
		{TaskID: "c-done", Owner: domain.TaskOwnerSarvex, State: domain.TaskStateDone},
		{TaskID: "g-blocked", Owner: domain.TaskOwnerExternal, State: domain.TaskStateBlocked},
	})

	assert.Equal(t, 1, j.OpenTaskCounts.Merchant)
	assert.Equal(t, 0, j.OpenTaskCounts.Sarvex)
	assert.Equal(t, 1, j.OpenTaskCounts.External)
}

// in_progress and blocked are outstanding work, not finished work — every
// prescribed milestone is raised in one of those two states.
func TestJourney_OpenTaskCounts_IncludeInProgressAndBlocked(t *testing.T) {
	j := journeyAtProspect(t, []domain.Task{
		{TaskID: "c-running", Owner: domain.TaskOwnerSarvex, State: domain.TaskStateInProgress},
		{TaskID: "g-waiting", Owner: domain.TaskOwnerExternal, State: domain.TaskStateBlocked},
	})

	assert.Equal(t, 1, j.OpenTaskCounts.Sarvex)
	assert.Equal(t, 1, j.OpenTaskCounts.External)
}

func TestJourney_OpenTaskCounts_AllDoneIsZero(t *testing.T) {
	j := journeyAtProspect(t, []domain.Task{
		{TaskID: "m", Owner: domain.TaskOwnerMerchant, State: domain.TaskStateDone},
		{TaskID: "c", Owner: domain.TaskOwnerSarvex, State: domain.TaskStateDone},
		{TaskID: "g", Owner: domain.TaskOwnerExternal, State: domain.TaskStateDone},
	})

	assert.Equal(t, domain.TaskCounts{}, j.OpenTaskCounts)
}

// The field answers "what do I do next", so it can only ever name a task the
// merchant owns and hasn't finished.
func TestJourney_NextAction_SkipsOtherOwnersAndDoneTasks(t *testing.T) {
	j := journeyAtProspect(t, []domain.Task{
		{TaskID: "c-1", Owner: domain.TaskOwnerSarvex, State: domain.TaskStateInProgress, Title: "We're importing"},
		{TaskID: "g-1", Owner: domain.TaskOwnerExternal, State: domain.TaskStateBlocked, Title: "Waiting on Google"},
		{TaskID: "m-done", Owner: domain.TaskOwnerMerchant, State: domain.TaskStateDone, Title: "Already handled"},
		{TaskID: "m-open", Owner: domain.TaskOwnerMerchant, State: domain.TaskStateOpen, Title: "Add a size"},
	})

	require.NotNil(t, j.NextAction)
	assert.Equal(t, "m-open", j.NextAction.TaskID)
	assert.Equal(t, "Add a size", j.NextAction.Title)
}

// The list arrives in the spine's order, so "first merchant-owned open task"
// is already "most urgent" — no second sort here.
func TestJourney_NextAction_TakesTheFirstInFeedOrder(t *testing.T) {
	j := journeyAtProspect(t, []domain.Task{
		{TaskID: "m-blocking", Owner: domain.TaskOwnerMerchant, State: domain.TaskStateOpen, Title: "Blocking"},
		{TaskID: "m-later", Owner: domain.TaskOwnerMerchant, State: domain.TaskStateOpen, Title: "Later"},
	})

	require.NotNil(t, j.NextAction)
	assert.Equal(t, "m-blocking", j.NextAction.TaskID)
}

func TestJourney_NextAction_NilWhenMerchantHasNothingOpen(t *testing.T) {
	j := journeyAtProspect(t, []domain.Task{
		{TaskID: "c-1", Owner: domain.TaskOwnerSarvex, State: domain.TaskStateInProgress},
		{TaskID: "m-done", Owner: domain.TaskOwnerMerchant, State: domain.TaskStateDone},
	})

	assert.Nil(t, j.NextAction)
}
