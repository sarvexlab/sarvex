// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStatusRepo is an in-memory StatusRepository for unit tests.
type fakeStatusRepo struct {
	mu   sync.Mutex
	docs map[string]*StatusDocument
}

func newFakeStatusRepo() *fakeStatusRepo { return &fakeStatusRepo{docs: map[string]*StatusDocument{}} }

func (r *fakeStatusRepo) Get(_ context.Context, merchantID, storeID string) (*StatusDocument, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	doc, ok := r.docs[storeID]
	if !ok {
		return nil, nil
	}
	// Verify tenant isolation: the document's merchant_id must match the requester's.
	if doc.MerchantID != merchantID {
		return nil, nil
	}
	// Return a copy of the Checks map so callers can't mutate our stored
	// state without going through Upsert — same aliasing discipline a real
	// Mongo-backed repo gets for free by round-tripping through BSON.
	cp := *doc
	cp.Checks = make(map[CheckType]CheckState, len(doc.Checks))
	for k, v := range doc.Checks {
		cp.Checks[k] = v
	}
	return &cp, nil
}

func (r *fakeStatusRepo) Upsert(_ context.Context, doc *StatusDocument) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *doc
	r.docs[doc.StoreID] = &cp
	return nil
}

func TestStatusEngine_FlapSuppression_OpensAfterThreeBreaches(t *testing.T) {
	statusRepo := newFakeStatusRepo()
	alertRepo := newFakeAlertRepo()
	alerts := NewAlertService(alertRepo, nil, nil)
	cfg := DefaultConfig()
	engine := NewStatusEngine(statusRepo, alerts, cfg, nil)
	ctx := context.Background()

	now := time.Now()
	require.NoError(t, engine.Evaluate(ctx, "merch-1", "store-1", CheckSignatureRoundtrip, true, 1, "breach 1", now))
	open, err := alertRepo.GetOpenByCheck(ctx, "store-1", CheckSignatureRoundtrip)
	require.NoError(t, err)
	assert.Nil(t, open, "no alert after 1 breach")

	require.NoError(t, engine.Evaluate(ctx, "merch-1", "store-1", CheckSignatureRoundtrip, true, 1, "breach 2", now))
	open, err = alertRepo.GetOpenByCheck(ctx, "store-1", CheckSignatureRoundtrip)
	require.NoError(t, err)
	assert.Nil(t, open, "no alert after 2 breaches")

	require.NoError(t, engine.Evaluate(ctx, "merch-1", "store-1", CheckSignatureRoundtrip, true, 1, "breach 3", now))
	open, err = alertRepo.GetOpenByCheck(ctx, "store-1", CheckSignatureRoundtrip)
	require.NoError(t, err)
	require.NotNil(t, open, "alert opens on the 3rd consecutive breach")
	firstAlertID := open.ID

	// A 4th breach must not open a second alert for the same check.
	require.NoError(t, engine.Evaluate(ctx, "merch-1", "store-1", CheckSignatureRoundtrip, true, 1, "breach 4", now))
	all, _, err := alertRepo.List(ctx, "merch-1", AlertListFilter{}, "", 100)
	require.NoError(t, err)
	assert.Len(t, all, 1)
	assert.Equal(t, firstAlertID, all[0].ID)

	doc, err := statusRepo.Get(ctx, "merch-1", "store-1")
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, SeverityCritical, doc.Status, "signature_roundtrip is a critical-mapped check")
	assert.Contains(t, doc.ActiveAlerts, firstAlertID)
}

func TestStatusEngine_FlapSuppression_AutoResolvesAfterTwoRecoveries(t *testing.T) {
	statusRepo := newFakeStatusRepo()
	alertRepo := newFakeAlertRepo()
	alerts := NewAlertService(alertRepo, nil, nil)
	cfg := DefaultConfig()
	engine := NewStatusEngine(statusRepo, alerts, cfg, nil)
	ctx := context.Background()
	now := time.Now()

	for i := 0; i < 3; i++ {
		require.NoError(t, engine.Evaluate(ctx, "merch-1", "store-1", CheckCatalogLookup, true, 1, "breach", now))
	}
	open, err := alertRepo.GetOpenByCheck(ctx, "store-1", CheckCatalogLookup)
	require.NoError(t, err)
	require.NotNil(t, open)
	assert.Equal(t, AlertOpen, open.Status)

	require.NoError(t, engine.Evaluate(ctx, "merch-1", "store-1", CheckCatalogLookup, false, 0, "", now))
	open, err = alertRepo.GetOpenByCheck(ctx, "store-1", CheckCatalogLookup)
	require.NoError(t, err)
	require.NotNil(t, open, "alert stays open after 1 recovery")

	require.NoError(t, engine.Evaluate(ctx, "merch-1", "store-1", CheckCatalogLookup, false, 0, "", now))
	open, err = alertRepo.GetOpenByCheck(ctx, "store-1", CheckCatalogLookup)
	require.NoError(t, err)
	assert.Nil(t, open, "alert auto-resolves after 2 consecutive recoveries")

	all, _, err := alertRepo.List(ctx, "merch-1", AlertListFilter{}, "", 100)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, AlertResolved, all[0].Status)
	assert.True(t, all[0].AutoResolved)

	doc, err := statusRepo.Get(ctx, "merch-1", "store-1")
	require.NoError(t, err)
	assert.Equal(t, SeverityHealthy, doc.Status)
	assert.Empty(t, doc.ActiveAlerts)
}

func TestStatusEngine_DegradedCheck_NeverOpensCriticalSeverity(t *testing.T) {
	statusRepo := newFakeStatusRepo()
	alertRepo := newFakeAlertRepo()
	alerts := NewAlertService(alertRepo, nil, nil)
	engine := NewStatusEngine(statusRepo, alerts, DefaultConfig(), nil)
	ctx := context.Background()
	now := time.Now()

	for i := 0; i < 3; i++ {
		require.NoError(t, engine.Evaluate(ctx, "merch-1", "store-1", CheckTimeoutRate, true, 0.2, "high timeout rate", now))
	}
	open, err := alertRepo.GetOpenByCheck(ctx, "store-1", CheckTimeoutRate)
	require.NoError(t, err)
	require.NotNil(t, open)
	assert.Equal(t, SeverityDegraded, open.Severity)

	doc, err := statusRepo.Get(ctx, "merch-1", "store-1")
	require.NoError(t, err)
	assert.Equal(t, SeverityDegraded, doc.Status, "a degraded-only check never escalates composite status to critical")
}
