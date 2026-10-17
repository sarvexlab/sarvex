// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package task_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
)

// Complete is the system's close and Resolve is the merchant's. The split
// exists because of external_wait: Resolve must keep refusing it (a merchant
// cannot declare Google's verdict), while the reconcilers must be able to
// close it (they are reporting the verdict that landed). Before the split,
// the two Google milestones and the Google-owned checklist items could be
// raised and never closed — they sat on the Path of a live store forever.

func newTaskSvcWith(m *task.TaskModel) (*fakeTaskRepo, task.Service) {
	repo := &fakeTaskRepo{tasks: map[string]*task.TaskModel{m.ID: m}}
	return repo, task.NewTaskService(repo, nil, nil, nil)
}

func externalWaitTask() *task.TaskModel {
	now := time.Now().UTC()
	return &task.TaskModel{
		ID:         "validation:store-1:sandbox",
		MerchantID: "merchant-1",
		StoreID:    "store-1",
		Title:      "Sandbox checkout validation",
		Owner:      string(domain.TaskOwnerExternal),
		Resolution: string(domain.TaskResolutionExternalWait),
		State:      string(domain.TaskStateBlocked),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func TestTaskService_Complete_ClosesExternalWait(t *testing.T) {
	m := externalWaitTask()
	_, svc := newTaskSvcWith(m)

	got, err := svc.Complete(context.Background(), "merchant-1", m.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStateDone, got.State)
}

// The guard Resolve keeps is the whole reason Complete exists — if this ever
// starts passing, the split has collapsed and a merchant can mark Google's
// verdict done themselves.
func TestTaskService_Resolve_StillRefusesExternalWait(t *testing.T) {
	m := externalWaitTask()
	_, svc := newTaskSvcWith(m)

	_, err := svc.Resolve(context.Background(), "merchant-1", m.ID, domain.ResolveTaskRequest{})
	require.ErrorIs(t, err, task.ErrNotResolvable)
}

// Reconciliation converges on a state and runs on every read, so re-closing
// an already-closed task is the common path, not an error. (Resolve, by
// contrast, still reports ErrNotResolvable — a merchant clicking resolve on
// a done task is a conflict worth surfacing.)
func TestTaskService_Complete_AlreadyDoneIsNoOp(t *testing.T) {
	m := externalWaitTask()
	m.State = string(domain.TaskStateDone)
	_, svc := newTaskSvcWith(m)

	got, err := svc.Complete(context.Background(), "merchant-1", m.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStateDone, got.State)
}

func TestTaskService_Complete_RecordsNote(t *testing.T) {
	m := externalWaitTask()
	_, svc := newTaskSvcWith(m)

	note := "Cleared by Google."
	got, err := svc.Complete(context.Background(), "merchant-1", m.ID, &note)
	require.NoError(t, err)
	require.NotNil(t, got.Note)
	assert.Equal(t, note, *got.Note)
}

func TestTaskService_Complete_NotFound(t *testing.T) {
	repo := &fakeTaskRepo{tasks: map[string]*task.TaskModel{}}
	svc := task.NewTaskService(repo, nil, nil, nil)

	_, err := svc.Complete(context.Background(), "merchant-1", "nope", nil)
	require.ErrorIs(t, err, task.ErrNotFound)
}

// Complete is a close, not a back door around tenancy.
func TestTaskService_Complete_TenantIsolation(t *testing.T) {
	m := externalWaitTask()
	_, svc := newTaskSvcWith(m)

	_, err := svc.Complete(context.Background(), "merchant-2", m.ID, nil)
	require.ErrorIs(t, err, task.ErrNotFound)
}
