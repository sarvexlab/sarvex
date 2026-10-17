// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package engagement_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/engagement"
)

// The package had no tests at all, which is part of why a set of unreachable
// entry/exit predicates sat in it contradicting the journey's transition
// table. What is left is a lookup table, so what is worth pinning is that
// the table is total and self-consistent.

var allJourneyStates = []domain.JourneyState{
	domain.JourneyProspect,
	domain.JourneyConnected,
	domain.JourneySynced,
	domain.JourneyReady,
	domain.JourneyLive,
}

// Every stage the mapping can produce must be one GetStage can resolve —
// otherwise a store lands on a stage with no name, and the console renders
// blank stage info for it.
func TestComputeStageFromState_AlwaysResolvesToAKnownStage(t *testing.T) {
	for _, state := range allJourneyStates {
		t.Run(string(state), func(t *testing.T) {
			stage := engagement.ComputeStageFromState(state)
			criteria := engagement.GetStage(stage)
			require.NotNil(t, criteria, "stage %q has no entry in WorkflowStages", stage)
			assert.NotEmpty(t, criteria.Name)
			assert.NotEmpty(t, criteria.Description)
		})
	}
}

// An unrecognised state must not produce a stage that claims progress.
func TestComputeStageFromState_UnknownStateFallsBackToProspect(t *testing.T) {
	assert.Equal(t, domain.StageProsepct, engagement.ComputeStageFromState(domain.JourneyState("nonsense")))
}

func TestWorkflowStages_HasNoDuplicates(t *testing.T) {
	seen := map[domain.EngagementStage]bool{}
	for _, s := range engagement.WorkflowStages {
		assert.False(t, seen[s.Stage], "stage %q appears twice", s.Stage)
		seen[s.Stage] = true
	}
}

func TestGetStage_UnknownStageReturnsNil(t *testing.T) {
	assert.Nil(t, engagement.GetStage(domain.EngagementStage("nonsense")))
}

// The rollup only ever reports owners the stage actually requires, and only
// while they still have unfinished work.
func TestUnresolvedRequiredOwners(t *testing.T) {
	remediation := engagement.GetStage(domain.StageRemediation)
	require.NotNil(t, remediation)
	require.Equal(t, []domain.TaskOwner{domain.TaskOwnerMerchant}, remediation.RequiredTasks)

	tests := []struct {
		name  string
		tasks []domain.Task
		want  []domain.TaskOwner
	}{
		{
			name:  "merchant still has an open task",
			tasks: []domain.Task{{Owner: domain.TaskOwnerMerchant, State: domain.TaskStateOpen}},
			want:  []domain.TaskOwner{domain.TaskOwnerMerchant},
		},
		{
			name:  "merchant's work is all done",
			tasks: []domain.Task{{Owner: domain.TaskOwnerMerchant, State: domain.TaskStateDone}},
		},
		{
			// in_progress and blocked are unfinished too — the same rule the
			// rest of the spine uses (domain.Task.IsOpen).
			name:  "merchant task is blocked",
			tasks: []domain.Task{{Owner: domain.TaskOwnerMerchant, State: domain.TaskStateBlocked}},
			want:  []domain.TaskOwner{domain.TaskOwnerMerchant},
		},
		{
			name:  "only other owners have open work",
			tasks: []domain.Task{{Owner: domain.TaskOwnerSarvex, State: domain.TaskStateOpen}},
		},
		{
			name: "no tasks at all",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := remediation.UnresolvedRequiredOwners(tt.tasks)
			if len(tt.want) == 0 {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
