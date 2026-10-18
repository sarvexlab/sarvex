// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package task_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
)

// A task's starting state follows from who owns it and how it gets finished.
// Raisers used to pick their own, and picked differently for identical
// situations: Google checklist items were raised open while the identical
// Google milestones were raised blocked, so the console showed the same
// thing two ways.

func TestDeriveTaskState(t *testing.T) {
	tests := []struct {
		name       string
		owner      domain.TaskOwner
		resolution domain.TaskResolution
		want       domain.TaskState
	}{
		// Waiting on someone outside Sarvex, whoever nominally owns it.
		{"google sandbox verdict", domain.TaskOwnerExternal, domain.TaskResolutionExternalWait, domain.TaskStateBlocked},
		{"google checklist item", domain.TaskOwnerExternal, domain.TaskResolutionExternalWait, domain.TaskStateBlocked},

		// Sarvex is already doing it, unprompted.
		{"catalog import milestone", domain.TaskOwnerSarvex, domain.TaskResolutionAutomatic, domain.TaskStateInProgress},
		{"monitoring warning", domain.TaskOwnerSarvex, domain.TaskResolutionAutomatic, domain.TaskStateInProgress},

		// Waiting on a person to act.
		{"merchant guided steps", domain.TaskOwnerMerchant, domain.TaskResolutionGuided, domain.TaskStateOpen},
		{"merchant one-click fix", domain.TaskOwnerMerchant, domain.TaskResolutionOneClick, domain.TaskStateOpen},
		{"operator one-click run", domain.TaskOwnerSarvex, domain.TaskResolutionOneClick, domain.TaskStateOpen},
		{"sarvex guided", domain.TaskOwnerSarvex, domain.TaskResolutionGuided, domain.TaskStateOpen},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, domain.DeriveTaskState(tt.owner, tt.resolution))
		})
	}
}

// done is only ever reached through Resolve or Complete.
func TestDeriveTaskState_NeverStartsDone(t *testing.T) {
	owners := []domain.TaskOwner{domain.TaskOwnerMerchant, domain.TaskOwnerSarvex, domain.TaskOwnerExternal}
	resolutions := []domain.TaskResolution{
		domain.TaskResolutionOneClick,
		domain.TaskResolutionGuided,
		domain.TaskResolutionAutomatic,
		domain.TaskResolutionExternalWait,
	}

	for _, o := range owners {
		for _, r := range resolutions {
			state := domain.DeriveTaskState(o, r)
			assert.NotEqual(t, domain.TaskStateDone, state, "owner=%s resolution=%s", o, r)
			assert.True(t, domain.Task{State: state}.IsOpen(), "owner=%s resolution=%s", o, r)
		}
	}
}

// The rule is applied by the single writer, so a raiser cannot opt out of it.
func TestTaskService_Raise_DerivesState(t *testing.T) {
	tests := []struct {
		name       string
		owner      domain.TaskOwner
		resolution domain.TaskResolution
		want       domain.TaskState
	}{
		{"external wait blocks", domain.TaskOwnerExternal, domain.TaskResolutionExternalWait, domain.TaskStateBlocked},
		{"sarvex automatic is underway", domain.TaskOwnerSarvex, domain.TaskResolutionAutomatic, domain.TaskStateInProgress},
		{"merchant work is open", domain.TaskOwnerMerchant, domain.TaskResolutionGuided, domain.TaskStateOpen},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeTaskRepo{tasks: map[string]*task.TaskModel{}}
			svc := task.NewTaskService(repo, nil, nil, nil)

			got, err := svc.Raise(context.Background(), "merchant-1", task.RaiseTaskRequest{
				TaskID:     "t-1",
				StoreID:    "store-1",
				Title:      "A task",
				Owner:      string(tt.owner),
				Resolution: string(tt.resolution),
				Source:     string(domain.TaskSourceChecklist),
			})
			require.NoError(t, err)

			assert.Equal(t, tt.want, got.State)
			assert.Equal(t, string(tt.want), repo.tasks["t-1"].State, "the persisted state must match what was returned")
		})
	}
}
