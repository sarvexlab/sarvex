// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

func statusOrchestrator(t *testing.T) (*Orchestrator, *MemoryConformanceStore) {
	t.Helper()
	store := NewMemoryConformanceStore()
	cat, _ := catalogue.LoadCatalogue(context.Background(), testLogger())
	orch := NewOrchestrator(
		&stubTestHarness{results: &TestResults{Passed: []string{"t1"}, Failed: []*domain.ValidationFailure{}}},
		store,
		&stubPlatformResolver{platform: "woocommerce"},
		nil,
		cat,
		testLogger(),
		"2026-04-08",
	)
	return orch, store
}

// Sandbox and Production are external verdicts Sarvex does not run, so both
// always report not_submitted.
func TestGetStatusReportsSandboxAndProductionNotSubmitted(t *testing.T) {
	orch, _ := statusOrchestrator(t)

	status, err := orch.GetStatus(context.Background(), "mer-1", "store1")
	require.NoError(t, err)
	assert.Equal(t, domain.SandboxNotSubmitted, status.Sandbox)
	assert.Equal(t, domain.ProductionNotSubmitted, status.Production)
	assert.Equal(t, "2026-04-08", status.TargetSpecVersion)
}

// pre_validation is Sarvex's own suite and stays record-driven.
func TestGetStatusPreValidationStaysRecordDriven(t *testing.T) {
	orch, _ := statusOrchestrator(t)

	before, err := orch.GetStatus(context.Background(), "mer-1", "store1")
	require.NoError(t, err)
	assert.Equal(t, domain.PreValidationNotRun, before.PreValidation)
	assert.Nil(t, before.LastRunAt)

	_, err = orch.RunValidation(context.Background(), "mer-1", "store1")
	require.NoError(t, err)

	after, err := orch.GetStatus(context.Background(), "mer-1", "store1")
	require.NoError(t, err)
	assert.Equal(t, domain.PreValidationPassing, after.PreValidation)
	assert.NotNil(t, after.LastRunAt)
}

// The store is keyed by store id, taken from the record. It previously keyed
// writes off a separately-set merchant id while reads came in by store, so a
// run could be saved and never read back.
func TestConformanceStoreRoundTripsByStore(t *testing.T) {
	orch, store := statusOrchestrator(t)

	_, err := orch.RunValidation(context.Background(), "mer-1", "store-a")
	require.NoError(t, err)

	latest, err := store.GetLatest(context.Background(), "store-a")
	require.NoError(t, err)
	assert.Equal(t, domain.StoreID("store-a"), latest.StoreID)

	runs, _, err := store.ListRuns(context.Background(), "store-a", 10, "")
	require.NoError(t, err)
	assert.Len(t, runs, 1)
}

func TestConformanceStoreKeepsStoresApart(t *testing.T) {
	orch, store := statusOrchestrator(t)

	_, err := orch.RunValidation(context.Background(), "mer-1", "store-a")
	require.NoError(t, err)
	_, err = orch.RunValidation(context.Background(), "mer-1", "store-b")
	require.NoError(t, err)

	// A second merchant's store must not see either.
	_, err = store.GetLatest(context.Background(), "store-c")
	require.Error(t, err)

	a, _, err := store.ListRuns(context.Background(), "store-a", 10, "")
	require.NoError(t, err)
	assert.Len(t, a, 1)
}

func TestConformanceStoreRejectsRecordWithoutStore(t *testing.T) {
	store := NewMemoryConformanceStore()

	err := store.SaveRecord(context.Background(), &domain.ConformanceRecord{RunID: "r1"})
	require.Error(t, err)
}
