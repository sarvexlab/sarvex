// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// There is only ever one answer to "is this store failing pre-validation",
// so there is only ever one task. It used to be keyed on time.Now().UnixNano(),
// which meant every failing run — and the suite re-runs on every catalog
// change — left another permanent task behind, none of which anything ever
// closed.

func newFailureTaskOrchestrator() (*fakeTaskService, *Orchestrator) {
	tasks := newFakeTaskService()
	orch := NewOrchestrator(nil, NewMemoryConformanceStore(), nil, nil, nil, testLogger(), "2026-04-08").
		WithTaskService(tasks)
	return tasks, orch
}

const (
	failStoreID  = domain.StoreID("store-1")
	failMerchant = domain.MerchantID("merchant123")
	failTaskID   = "validation:store-1:pre_validation_failed"
)

func TestReconcileFailureTask_RaisesOnFail(t *testing.T) {
	tasks, orch := newFailureTaskOrchestrator()

	orch.reconcileFailureTask(context.Background(), failMerchant, failStoreID, domain.ConformanceFail, 3)

	req, ok := tasks.raised[failTaskID]
	require.True(t, ok, "a failing run must raise the pre-validation failure task")
	assert.Equal(t, string(domain.TaskOwnerMerchant), req.Owner)
	assert.Equal(t, string(domain.TaskSourceValidation), req.Source)
	require.NotNil(t, req.SourceRef)
	assert.Equal(t, failureSourceRef, *req.SourceRef,
		"source_ref must be the task id's last segment, not the whole id")
	assert.Contains(t, req.WhyItMatters, "3 requirement(s) not met")
}

// Every resolution written to the spine has to be one the contract's enum
// allows — the console renders a label from it, and an unknown value shows
// the merchant a blank.
func TestReconcileFailureTask_UsesContractResolution(t *testing.T) {
	tasks, orch := newFailureTaskOrchestrator()

	orch.reconcileFailureTask(context.Background(), failMerchant, failStoreID, domain.ConformanceFail, 1)

	assert.Equal(t, string(domain.TaskResolutionGuided), tasks.raised[failTaskID].Resolution)
}

func TestReconcileFailureTask_RepeatedFailuresRaiseOneTask(t *testing.T) {
	tasks, orch := newFailureTaskOrchestrator()
	ctx := context.Background()

	for range 3 {
		orch.reconcileFailureTask(ctx, failMerchant, failStoreID, domain.ConformanceFail, 2)
	}

	assert.Len(t, tasks.raised, 1, "a store that stays broken must not accumulate tasks")
}

func TestReconcileFailureTask_PassingRunClosesIt(t *testing.T) {
	tasks, orch := newFailureTaskOrchestrator()
	ctx := context.Background()

	orch.reconcileFailureTask(ctx, failMerchant, failStoreID, domain.ConformanceFail, 2)
	require.False(t, tasks.resolved[failTaskID])

	orch.reconcileFailureTask(ctx, failMerchant, failStoreID, domain.ConformancePass, 0)
	assert.True(t, tasks.resolved[failTaskID],
		"once the suite passes, the Path must stop saying the store fails pre-validation")
}

// The common case: a store that has never failed. Closing a task that was
// never raised is a no-op, not an error.
func TestReconcileFailureTask_PassWithNoTaskIsQuiet(t *testing.T) {
	tasks, orch := newFailureTaskOrchestrator()

	orch.reconcileFailureTask(context.Background(), failMerchant, failStoreID, domain.ConformancePass, 0)

	assert.Empty(t, tasks.raised)
	assert.Empty(t, tasks.resolved)
}

// The milestone task's id and its source_ref have to agree, since the id is
// built as source:store:source_ref and the console matches presentation on
// source_ref.
func TestMilestoneTaskIDMatchesItsSourceRef(t *testing.T) {
	tasks, orch := newFailureTaskOrchestrator()

	require.NoError(t, orch.EnsureMilestoneTask(context.Background(), failMerchant, failStoreID))

	req, ok := tasks.raised[validationMilestoneTaskID(failStoreID)]
	require.True(t, ok)
	require.NotNil(t, req.SourceRef)
	assert.Equal(t, "validation:"+string(failStoreID)+":"+*req.SourceRef, req.TaskID)
}
