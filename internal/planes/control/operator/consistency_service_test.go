// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package operator_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/serving"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/operator"
)

// fakeConsistencyRepo is an in-memory test double for operator.ConsistencyRepository.
type fakeConsistencyRepo struct {
	byStore map[string][]*domain.ConsistencyFinding
	err     error
}

func newFakeConsistencyRepo() *fakeConsistencyRepo {
	return &fakeConsistencyRepo{byStore: make(map[string][]*domain.ConsistencyFinding)}
}

// DeleteRecoveredProbeFindings drops the store's findings whose SKU is in
// probeSKUs and whose id is not still failing — the same by-exclusion rule
// the Mongo implementation applies.
func (f *fakeConsistencyRepo) DeleteRecoveredProbeFindings(_ context.Context, storeID string, probeSKUs, stillFailingIDs []string) error {
	if f.err != nil {
		return f.err
	}
	inScope := make(map[string]bool, len(probeSKUs))
	for _, sku := range probeSKUs {
		inScope[sku] = true
	}
	failing := make(map[string]bool, len(stillFailingIDs))
	for _, id := range stillFailingIDs {
		failing[id] = true
	}

	kept := f.byStore[storeID][:0]
	for _, finding := range f.byStore[storeID] {
		if inScope[finding.SKU] && !failing[finding.FindingID] {
			continue
		}
		kept = append(kept, finding)
	}
	f.byStore[storeID] = kept
	return nil
}

func (f *fakeConsistencyRepo) Save(_ context.Context, storeID string, finding *domain.ConsistencyFinding) error {
	if f.err != nil {
		return f.err
	}
	for i, existing := range f.byStore[storeID] {
		if existing.FindingID == finding.FindingID {
			f.byStore[storeID][i] = finding
			return nil
		}
	}
	f.byStore[storeID] = append(f.byStore[storeID], finding)
	return nil
}

func (f *fakeConsistencyRepo) ListByStore(_ context.Context, storeID string, before string, limit int, severity *domain.Severity) ([]*domain.ConsistencyFinding, bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	var out []*domain.ConsistencyFinding
	for _, finding := range f.byStore[storeID] {
		if severity != nil && finding.Severity != *severity {
			continue
		}
		out = append(out, finding)
	}
	// Test double keeps insertion order; no cursor/limit semantics needed
	// for the cases exercised here.
	return out, false, nil
}

func (f *fakeConsistencyRepo) CountByStore(_ context.Context, storeID string, severity domain.Severity) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	count := 0
	for _, finding := range f.byStore[storeID] {
		if finding.Severity == severity {
			count++
		}
	}
	return count, nil
}

// newTestOperatorServiceWithConsistency builds an operator.Service with a
// real (fake) ConsistencyRepository and stub board/task/journey/presence/order
// dependencies, for tests exercising GetHealth/ListConsistencyFindings.
func newTestOperatorServiceWithConsistency(consistency operator.ConsistencyRepository) operator.Service {
	return newTestOperatorServiceWithHealth(consistency, nil, nil)
}

// fakeHealthRepo is an in-memory test double for operator.HealthRepository.
type fakeHealthRepo struct {
	byStore map[string][]domain.HealthStatement
	err     error
}

func newFakeHealthRepo() *fakeHealthRepo {
	return &fakeHealthRepo{byStore: make(map[string][]domain.HealthStatement)}
}

func (f *fakeHealthRepo) Save(_ context.Context, storeID string, statement domain.HealthStatement) error {
	if f.err != nil {
		return f.err
	}
	for i, existing := range f.byStore[storeID] {
		if existing.Kind == statement.Kind {
			f.byStore[storeID][i] = statement
			return nil
		}
	}
	f.byStore[storeID] = append(f.byStore[storeID], statement)
	return nil
}

func (f *fakeHealthRepo) GetByStore(_ context.Context, storeID string) ([]domain.HealthStatement, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byStore[storeID], nil
}

// newTestOperatorServiceWithHealth builds an operator.Service with real
// (fake) ConsistencyRepository/HealthRepository/ServingCacheReader
// dependencies, for tests exercising GetHealth's full 4-kind rollup.
func newTestOperatorServiceWithHealth(consistency operator.ConsistencyRepository, health operator.HealthRepository, servingCache operator.ServingCacheReader) operator.Service {
	steps := &fakeManualStepRepo{docs: make(map[string]*operator.ManualStepModel)}
	audit := &fakeAuditLogRepo{}
	board := &fakeBoardRepo{}
	tasks := &fakeTaskService{}
	journey := &fakeJourneyService{journey: domain.Journey{State: domain.JourneySynced, Stage: domain.StageRemediation}}
	return operator.NewOperatorService(steps, audit, board, tasks, journey, servingCache, nil, nil, consistency, health, nil, nil, nil)
}

func TestOperatorService_GetHealth_NoFindings_OK(t *testing.T) {
	svc := newTestOperatorServiceWithConsistency(newFakeConsistencyRepo())

	health, err := svc.GetHealth(context.Background(), "merch-1", "store-1")
	require.NoError(t, err)
	require.Len(t, health.Statements, 1)
	assert.Equal(t, domain.HealthConsistency, health.Statements[0].Kind)
	assert.True(t, health.Statements[0].OK)
}

func TestOperatorService_GetHealth_WithCriticalFinding(t *testing.T) {
	repo := newFakeConsistencyRepo()
	require.NoError(t, repo.Save(context.Background(), "store-1", &domain.ConsistencyFinding{
		FindingID: "f1", SKU: "SKU-1", Field: "stock_safety",
		Severity: domain.SeverityCritical, DetectedAt: time.Now().UTC(),
	}))
	svc := newTestOperatorServiceWithConsistency(repo)

	health, err := svc.GetHealth(context.Background(), "merch-1", "store-1")
	require.NoError(t, err)
	require.Len(t, health.Statements, 1)
	assert.False(t, health.Statements[0].OK)
	assert.Contains(t, health.Statements[0].Message, "critical")
}

func TestOperatorService_GetHealth_WithWarningOnly(t *testing.T) {
	repo := newFakeConsistencyRepo()
	require.NoError(t, repo.Save(context.Background(), "store-1", &domain.ConsistencyFinding{
		FindingID: "f1", SKU: "SKU-1", Field: "price_rebuild_lag",
		Severity: domain.SeverityWarning, DetectedAt: time.Now().UTC(),
	}))
	svc := newTestOperatorServiceWithConsistency(repo)

	health, err := svc.GetHealth(context.Background(), "merch-1", "store-1")
	require.NoError(t, err)
	require.Len(t, health.Statements, 1)
	assert.False(t, health.Statements[0].OK)
	assert.Contains(t, health.Statements[0].Message, "warning")
}

func TestOperatorService_GetHealth_ScopedToStore(t *testing.T) {
	repo := newFakeConsistencyRepo()
	require.NoError(t, repo.Save(context.Background(), "store-other", &domain.ConsistencyFinding{
		FindingID: "f1", SKU: "SKU-1", Field: "stock_safety",
		Severity: domain.SeverityCritical, DetectedAt: time.Now().UTC(),
	}))
	svc := newTestOperatorServiceWithConsistency(repo)

	health, err := svc.GetHealth(context.Background(), "merch-1", "store-1")
	require.NoError(t, err)
	require.Len(t, health.Statements, 1)
	assert.True(t, health.Statements[0].OK, "finding on a different store must not affect this store's health")
}

func TestOperatorService_ListConsistencyFindings_OK(t *testing.T) {
	repo := newFakeConsistencyRepo()
	require.NoError(t, repo.Save(context.Background(), "store-1", &domain.ConsistencyFinding{
		FindingID: "f1", SKU: "SKU-1", Field: "price_rebuild_lag",
		Severity: domain.SeverityWarning, DetectedAt: time.Now().UTC(),
	}))
	require.NoError(t, repo.Save(context.Background(), "store-1", &domain.ConsistencyFinding{
		FindingID: "f2", SKU: "SKU-2", Field: "stock_safety",
		Severity: domain.SeverityCritical, DetectedAt: time.Now().UTC(),
	}))
	svc := newTestOperatorServiceWithConsistency(repo)

	list, err := svc.ListConsistencyFindings(context.Background(), "merch-1", "store-1", "", 20, nil)
	require.NoError(t, err)
	assert.Len(t, list.Data, 2)
}

func TestOperatorService_ListConsistencyFindings_FilterBySeverity(t *testing.T) {
	repo := newFakeConsistencyRepo()
	require.NoError(t, repo.Save(context.Background(), "store-1", &domain.ConsistencyFinding{
		FindingID: "f1", SKU: "SKU-1", Field: "price_rebuild_lag",
		Severity: domain.SeverityWarning, DetectedAt: time.Now().UTC(),
	}))
	require.NoError(t, repo.Save(context.Background(), "store-1", &domain.ConsistencyFinding{
		FindingID: "f2", SKU: "SKU-2", Field: "stock_safety",
		Severity: domain.SeverityCritical, DetectedAt: time.Now().UTC(),
	}))
	svc := newTestOperatorServiceWithConsistency(repo)

	critical := domain.SeverityCritical
	list, err := svc.ListConsistencyFindings(context.Background(), "merch-1", "store-1", "", 20, &critical)
	require.NoError(t, err)
	require.Len(t, list.Data, 1)
	assert.Equal(t, "SKU-2", list.Data[0].SKU)
}

func TestOperatorService_ListConsistencyFindings_Empty(t *testing.T) {
	svc := newTestOperatorServiceWithConsistency(newFakeConsistencyRepo())

	list, err := svc.ListConsistencyFindings(context.Background(), "merch-1", "store-1", "", 20, nil)
	require.NoError(t, err)
	assert.NotNil(t, list.Data)
	assert.Len(t, list.Data, 0)
}

func TestOperatorService_GetHealth_RepoError(t *testing.T) {
	repo := newFakeConsistencyRepo()
	repo.err = assert.AnError
	svc := newTestOperatorServiceWithConsistency(repo)

	_, err := svc.GetHealth(context.Background(), "merch-1", "store-1")
	assert.Error(t, err)
}

// TestOperatorService_GetHealth_AllKinds verifies the full rollup: consistency
// (live from findings), semantic_endpoint/profile_edge (from the persisted
// probe results a scheduled monitoring run writes), and sync_freshness (live
// from the serving cache watermark) all show up together.
func TestOperatorService_GetHealth_AllKinds(t *testing.T) {
	consistency := newFakeConsistencyRepo()
	health := newFakeHealthRepo()
	require.NoError(t, health.Save(context.Background(), "store-1", domain.HealthStatement{
		Kind: domain.HealthSemanticEndpoint, OK: true, Message: "All semantic endpoints reachable.",
	}))
	require.NoError(t, health.Save(context.Background(), "store-1", domain.HealthStatement{
		Kind: domain.HealthProfileEdge, OK: false, Message: "plugin unreachable",
	}))
	servingCache := &fakeServingCacheReader{watermark: &serving.CacheWatermark{IsStale: false}}

	svc := newTestOperatorServiceWithHealth(consistency, health, servingCache)

	got, err := svc.GetHealth(context.Background(), "merch-1", "store-1")
	require.NoError(t, err)

	kinds := make(map[domain.HealthStatementKind]domain.HealthStatement)
	for _, s := range got.Statements {
		kinds[s.Kind] = s
	}
	require.Len(t, got.Statements, 4)
	assert.True(t, kinds[domain.HealthConsistency].OK)
	assert.True(t, kinds[domain.HealthSemanticEndpoint].OK)
	assert.False(t, kinds[domain.HealthProfileEdge].OK)
	assert.True(t, kinds[domain.HealthSyncFreshness].OK)
}

// TestOperatorService_GetHealth_NoCacheSnapshotYet verifies a store with no
// serving cache snapshot yet (brand new, never synced) omits sync_freshness
// rather than erroring.
func TestOperatorService_GetHealth_NoCacheSnapshotYet(t *testing.T) {
	servingCache := &fakeServingCacheReader{watermarkErr: serving.ErrNotFound}
	svc := newTestOperatorServiceWithHealth(newFakeConsistencyRepo(), newFakeHealthRepo(), servingCache)

	got, err := svc.GetHealth(context.Background(), "merch-1", "store-1")
	require.NoError(t, err)
	for _, s := range got.Statements {
		assert.NotEqual(t, domain.HealthSyncFreshness, s.Kind)
	}
}

// TestOperatorService_GetHealth_StaleCacheReported verifies a stale serving
// cache surfaces as a failing sync_freshness statement.
func TestOperatorService_GetHealth_StaleCacheReported(t *testing.T) {
	servingCache := &fakeServingCacheReader{watermark: &serving.CacheWatermark{IsStale: true, Age: 10 * time.Minute}}
	svc := newTestOperatorServiceWithHealth(newFakeConsistencyRepo(), newFakeHealthRepo(), servingCache)

	got, err := svc.GetHealth(context.Background(), "merch-1", "store-1")
	require.NoError(t, err)

	var found bool
	for _, s := range got.Statements {
		if s.Kind == domain.HealthSyncFreshness {
			found = true
			assert.False(t, s.OK)
		}
	}
	assert.True(t, found, "expected a sync_freshness statement")
}

// TestOperatorService_GetHealth_OnlyConsistencyWhenOthersUnconfigured
// verifies GetHealth degrades gracefully (consistency only, no panic) when
// health/serving-cache dependencies aren't wired — matches how a brand-new
// store looks before the scheduler's first pass.
func TestOperatorService_GetHealth_OnlyConsistencyWhenOthersUnconfigured(t *testing.T) {
	svc := newTestOperatorServiceWithConsistency(newFakeConsistencyRepo())

	got, err := svc.GetHealth(context.Background(), "merch-1", "store-1")
	require.NoError(t, err)
	require.Len(t, got.Statements, 1)
	assert.Equal(t, domain.HealthConsistency, got.Statements[0].Kind)
}
