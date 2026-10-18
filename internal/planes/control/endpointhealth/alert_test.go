// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"context"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAlertRepo is an in-memory AlertRepository for unit tests.
type fakeAlertRepo struct {
	mu     sync.Mutex
	alerts map[string]*Alert
}

func newFakeAlertRepo() *fakeAlertRepo { return &fakeAlertRepo{alerts: map[string]*Alert{}} }

func (r *fakeAlertRepo) Insert(_ context.Context, a *Alert) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *a
	r.alerts[a.ID] = &cp
	return nil
}

func (r *fakeAlertRepo) GetOpenByCheck(_ context.Context, storeID string, checkType CheckType) (*Alert, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.alerts {
		if a.StoreID == storeID && a.CheckType == checkType && (a.Status == AlertOpen || a.Status == AlertAcknowledged) {
			cp := *a
			return &cp, nil
		}
	}
	return nil, nil
}

func (r *fakeAlertRepo) Get(_ context.Context, merchantID, alertID string) (*Alert, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.alerts[alertID]
	if !ok || a.MerchantID != merchantID {
		return nil, ErrAlertNotFound
	}
	cp := *a
	return &cp, nil
}

func (r *fakeAlertRepo) Update(_ context.Context, a *Alert) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.alerts[a.ID]; !ok {
		return ErrAlertNotFound
	}
	cp := *a
	r.alerts[a.ID] = &cp
	return nil
}

func (r *fakeAlertRepo) List(_ context.Context, merchantID string, filter AlertListFilter, _ string, _ int) ([]*Alert, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Alert
	for _, a := range r.alerts {
		if a.MerchantID != merchantID {
			continue
		}
		if filter.Status != "" && a.Status != filter.Status {
			continue
		}
		if filter.Severity != "" && a.Severity != filter.Severity {
			continue
		}
		if filter.StoreID != "" && a.StoreID != filter.StoreID {
			continue
		}
		cp := *a
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, false, nil
}

func TestAlertService_ForwardOnlyTransitions(t *testing.T) {
	repo := newFakeAlertRepo()
	svc := NewAlertService(repo, nil, nil)
	ctx := context.Background()

	alert, isNew, err := svc.OpenOrGetActive(ctx, "merch-1", "store-1", CheckSignatureRoundtrip, SeverityCritical, "signature failing")
	require.NoError(t, err)
	assert.True(t, isNew)
	assert.Equal(t, AlertOpen, alert.Status)

	t.Run("open -> acknowledged succeeds", func(t *testing.T) {
		got, err := svc.Acknowledge(ctx, "merch-1", alert.ID, "owner@acme.test")
		require.NoError(t, err)
		assert.Equal(t, AlertAcknowledged, got.Status)
		assert.Equal(t, "owner@acme.test", got.AcknowledgedBy)
	})

	t.Run("acknowledged -> acknowledged fails", func(t *testing.T) {
		_, err := svc.Acknowledge(ctx, "merch-1", alert.ID, "owner@acme.test")
		assert.ErrorIs(t, err, ErrInvalidTransition)
	})

	t.Run("acknowledged -> resolved succeeds", func(t *testing.T) {
		got, err := svc.Resolve(ctx, "merch-1", alert.ID, false)
		require.NoError(t, err)
		assert.Equal(t, AlertResolved, got.Status)
		assert.False(t, got.AutoResolved)
	})

	t.Run("resolved -> resolved fails (no backward or repeat transition)", func(t *testing.T) {
		_, err := svc.Resolve(ctx, "merch-1", alert.ID, false)
		assert.ErrorIs(t, err, ErrInvalidTransition)
	})

	t.Run("resolved -> acknowledged fails", func(t *testing.T) {
		_, err := svc.Acknowledge(ctx, "merch-1", alert.ID, "owner@acme.test")
		assert.ErrorIs(t, err, ErrInvalidTransition)
	})
}

func TestAlertService_OpenDirectlyToResolved_AutoResolve(t *testing.T) {
	repo := newFakeAlertRepo()
	svc := NewAlertService(repo, nil, nil)
	ctx := context.Background()

	alert, _, err := svc.OpenOrGetActive(ctx, "merch-1", "store-1", CheckCatalogLookup, SeverityDegraded, "catalog degraded")
	require.NoError(t, err)

	got, err := svc.Resolve(ctx, "merch-1", alert.ID, true)
	require.NoError(t, err)
	assert.Equal(t, AlertResolved, got.Status)
	assert.True(t, got.AutoResolved)
}

func TestAlertService_OpenOrGetActive_IdempotentPerCheck(t *testing.T) {
	repo := newFakeAlertRepo()
	svc := NewAlertService(repo, nil, nil)
	ctx := context.Background()

	first, isNew1, err := svc.OpenOrGetActive(ctx, "merch-1", "store-1", CheckSignatureRoundtrip, SeverityCritical, "d1")
	require.NoError(t, err)
	assert.True(t, isNew1)

	second, isNew2, err := svc.OpenOrGetActive(ctx, "merch-1", "store-1", CheckSignatureRoundtrip, SeverityCritical, "d2")
	require.NoError(t, err)
	assert.False(t, isNew2, "an open alert for the same (store, check) is reused, not duplicated")
	assert.Equal(t, first.ID, second.ID)
}

func TestAlertService_Get_NotFound(t *testing.T) {
	repo := newFakeAlertRepo()
	svc := NewAlertService(repo, nil, nil)
	_, err := svc.Acknowledge(context.Background(), "merch-1", "alt_does_not_exist", "owner@acme.test")
	assert.ErrorIs(t, err, ErrAlertNotFound)
}

func TestAlertService_Get_WrongMerchant_NotFound(t *testing.T) {
	repo := newFakeAlertRepo()
	svc := NewAlertService(repo, nil, nil)
	ctx := context.Background()

	alert, _, err := svc.OpenOrGetActive(ctx, "merch-1", "store-1", CheckSignatureRoundtrip, SeverityCritical, "d")
	require.NoError(t, err)

	_, err = svc.Acknowledge(ctx, "merch-2", alert.ID, "someone-else@evil.test")
	assert.ErrorIs(t, err, ErrAlertNotFound, "cross-tenant access must 404, not succeed")
}
