// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package task_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/task"
)

// source=readiness means "a per-SKU finding" for every task but one: the
// initial-audit milestone, which is about the audit itself. Treating that
// one as a SKU meant two catalog lookups for a product called
// "initial_audit" on every read — and the journey reads it on every Path
// load — plus a drawer telling the merchant to go fix it in their store
// while Sarvex was in the middle of doing it for them.

func readinessTask(sourceRef string) *task.TaskModel {
	now := time.Now().UTC()
	return &task.TaskModel{
		ID:         "readiness:store-1:" + sourceRef,
		MerchantID: "merchant-1",
		StoreID:    "store-1",
		Title:      "A readiness task",
		Owner:      string(domain.TaskOwnerSarvex),
		Resolution: string(domain.TaskResolutionAutomatic),
		State:      string(domain.TaskStateInProgress),
		Source:     string(domain.TaskSourceReadiness),
		SourceRef:  &sourceRef,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func TestTaskService_Get_SkipsDrillInForTheAuditMilestone(t *testing.T) {
	m := readinessTask(domain.TaskRefInitialAudit)
	repo := &fakeTaskRepo{tasks: map[string]*task.TaskModel{m.ID: m}}
	lookup := &fakeRemediationLookup{}
	svc := task.NewTaskService(repo, lookup, nil, nil)

	got, err := svc.Get(context.Background(), "merchant-1", m.ID)
	require.NoError(t, err)

	assert.Empty(t, got.Remediations)
	assert.Empty(t, got.Issues)
	assert.Zero(t, lookup.callCount, "the milestone's source_ref is not a SKU — it must not be looked up as one")
	assert.Zero(t, lookup.issuesCalled)
}

func TestTaskService_Get_StillDrillsInForPerSKUFindings(t *testing.T) {
	m := readinessTask("SKU-1")
	repo := &fakeTaskRepo{tasks: map[string]*task.TaskModel{m.ID: m}}
	lookup := &fakeRemediationLookup{issues: map[string][]domain.ProductIssue{
		"SKU-1": {{IssueID: "iss-1", Field: "size", Problem: domain.ProblemFreeText}},
	}}
	svc := task.NewTaskService(repo, lookup, nil, nil)

	got, err := svc.Get(context.Background(), "merchant-1", m.ID)
	require.NoError(t, err)

	require.Len(t, got.Issues, 1)
	assert.Equal(t, 1, lookup.issuesCalled)
}

func TestTask_HasProductDrillIn(t *testing.T) {
	sku := "SKU-1"
	audit := domain.TaskRefInitialAudit
	item := "gpay-setup"

	tests := []struct {
		name string
		task domain.Task
		want bool
	}{
		{"per-SKU readiness finding", domain.Task{Source: domain.TaskSourceReadiness, SourceRef: &sku}, true},
		{"the audit milestone", domain.Task{Source: domain.TaskSourceReadiness, SourceRef: &audit}, false},
		{"readiness with no ref", domain.Task{Source: domain.TaskSourceReadiness}, false},
		{"a checklist task", domain.Task{Source: domain.TaskSourceChecklist, SourceRef: &item}, false},
		{"a monitoring task", domain.Task{Source: domain.TaskSourceMonitoring, SourceRef: &sku}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.task.HasProductDrillIn())
		})
	}
}
