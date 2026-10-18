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

// Ensure and Retire are the converge idiom every prescriber shares. They are
// called on every reconciliation pass, so being idempotent is not a nicety
// here — it is the contract.

func prescriptionFixture() task.Prescription {
	return task.Prescription{
		StoreID:      "store-1",
		Source:       domain.TaskSourceValidation,
		SourceRef:    "sandbox",
		Title:        "Sandbox checkout validation",
		WhyItMatters: "Google runs test purchases against your catalog.",
		Owner:        domain.TaskOwnerExternal,
		Resolution:   domain.TaskResolutionExternalWait,
	}
}

func newEmptySvc() (*fakeTaskRepo, task.Service) {
	repo := &fakeTaskRepo{tasks: map[string]*task.TaskModel{}}
	return repo, task.NewTaskService(repo, nil, nil, nil)
}

// The id is the spine's identity and the console matches presentation on the
// source_ref inside it, so the two can never be allowed to disagree —
// deriving one from the other is what guarantees that.
func TestPrescription_TaskIDIsSourceStoreRef(t *testing.T) {
	assert.Equal(t, "validation:store-1:sandbox", prescriptionFixture().TaskID())
}

func TestEnsure_RaisesWhenMissing(t *testing.T) {
	repo, svc := newEmptySvc()
	p := prescriptionFixture()

	require.NoError(t, task.Ensure(context.Background(), svc, "merchant-1", p))

	m, ok := repo.tasks[p.TaskID()]
	require.True(t, ok)
	assert.Equal(t, string(domain.TaskOwnerExternal), m.Owner)
	assert.Equal(t, string(domain.TaskStateBlocked), m.State)
	require.NotNil(t, m.SourceRef)
	assert.Equal(t, "sandbox", *m.SourceRef)
}

func TestEnsure_IsIdempotent(t *testing.T) {
	repo, svc := newEmptySvc()
	p := prescriptionFixture()
	ctx := context.Background()

	for range 3 {
		require.NoError(t, task.Ensure(ctx, svc, "merchant-1", p))
	}

	assert.Len(t, repo.tasks, 1)
}

// A store regressing back through the funnel re-enters states it has already
// been through. Re-raising there would reopen work the merchant watched
// finish, so a finished prescription stays finished.
func TestEnsure_DoesNotReopenAFinishedPrescription(t *testing.T) {
	repo, svc := newEmptySvc()
	p := prescriptionFixture()
	ctx := context.Background()

	require.NoError(t, task.Ensure(ctx, svc, "merchant-1", p))
	require.NoError(t, task.Retire(ctx, svc, "merchant-1", p.TaskID()))
	require.NoError(t, task.Ensure(ctx, svc, "merchant-1", p))

	assert.Equal(t, string(domain.TaskStateDone), repo.tasks[p.TaskID()].State)
}

// The prescriptions only the system can ever finish are exactly the ones
// Resolve refuses, so Retire has to be able to close them.
func TestRetire_ClosesExternalWait(t *testing.T) {
	repo, svc := newEmptySvc()
	p := prescriptionFixture()
	ctx := context.Background()
	require.NoError(t, task.Ensure(ctx, svc, "merchant-1", p))

	require.NoError(t, task.Retire(ctx, svc, "merchant-1", p.TaskID()))

	assert.Equal(t, string(domain.TaskStateDone), repo.tasks[p.TaskID()].State)
}

func TestRetire_IsIdempotent(t *testing.T) {
	_, svc := newEmptySvc()
	p := prescriptionFixture()
	ctx := context.Background()
	require.NoError(t, task.Ensure(ctx, svc, "merchant-1", p))

	for range 3 {
		require.NoError(t, task.Retire(ctx, svc, "merchant-1", p.TaskID()))
	}
}

// Reconciliation retires every stage behind the store on every pass, most of
// which were never raised for that store.
func TestRetire_MissingTaskIsNotAnError(t *testing.T) {
	_, svc := newEmptySvc()

	assert.NoError(t, task.Retire(context.Background(), svc, "merchant-1", "validation:store-1:never-raised"))
}
