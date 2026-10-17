// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package webbaseline_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/sync/webbaseline"
)

type fakeStore struct {
	mu   sync.Mutex
	days map[string]webbaseline.BaselineDay
}

func newFakeStore() *fakeStore { return &fakeStore{days: map[string]webbaseline.BaselineDay{}} }

func (f *fakeStore) Upsert(_ context.Context, day webbaseline.BaselineDay) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.days[day.ID] = day
	return nil
}

type fakeLister struct{ refs []webbaseline.StoreRef }

func (f *fakeLister) ListAllStores(_ context.Context, afterID string, limit int) ([]webbaseline.StoreRef, bool, error) {
	if afterID != "" {
		return nil, false, nil
	}
	return f.refs, false, nil
}

// TestSyncer_NoSourceResolver_WritesUnavailable is R10/R12: when a store's
// connector doesn't implement Source (NoSourceResolver — the only
// resolver wired in this codebase today, see model.go's package doc
// comment), Syncer must record confidence=unavailable, never a fabricated
// number.
func TestSyncer_NoSourceResolver_WritesUnavailable(t *testing.T) {
	store := newFakeStore()
	lister := &fakeLister{refs: []webbaseline.StoreRef{{MerchantID: "merch-1", StoreID: "store-1", Platform: "woocommerce"}}}
	syncer := webbaseline.NewSyncer(store, lister, webbaseline.NoSourceResolver, time.Hour, nil)

	// runOnce is unexported; Run's initial pass (before it blocks on the
	// ticker) exercises it — cancel immediately after to avoid waiting a
	// full interval.
	ctx, cancel := context.WithCancel(context.Background())
	go syncer.Run(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()

	store.mu.Lock()
	defer store.mu.Unlock()
	require.Len(t, store.days, 1)
	for _, day := range store.days {
		assert.Equal(t, webbaseline.ConfidenceUnavailable, day.Confidence)
		assert.Equal(t, "woocommerce", day.Source)
		assert.Equal(t, int64(0), day.Orders)
		assert.Equal(t, int64(0), day.RevenueMinor)
	}
}

// TestSyncer_ConnectorReportedSource_RecordsRealNumbers proves the seam
// works end to end once a connector *does* implement Source — no worker
// or schema change required, only the resolver.
func TestSyncer_ConnectorReportedSource_RecordsRealNumbers(t *testing.T) {
	store := newFakeStore()
	lister := &fakeLister{refs: []webbaseline.StoreRef{{MerchantID: "merch-1", StoreID: "store-1", Platform: "woocommerce"}}}
	resolver := func(_ context.Context, storeID, platform string) (webbaseline.Source, bool, error) {
		return fakeSource{orders: 12, revenueMinor: 480000, currency: "USD"}, true, nil
	}
	syncer := webbaseline.NewSyncer(store, lister, resolver, time.Hour, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go syncer.Run(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()

	store.mu.Lock()
	defer store.mu.Unlock()
	require.Len(t, store.days, 1)
	for _, day := range store.days {
		assert.Equal(t, webbaseline.ConfidenceConnectorReported, day.Confidence)
		assert.Equal(t, int64(12), day.Orders)
		assert.Equal(t, int64(480000), day.RevenueMinor)
		assert.Equal(t, int64(40000), day.AOVMinor)
	}
}

// TestSyncer_ResolverError_StillRecordsUnavailable ensures a connector
// resolution failure doesn't leave the day silently unrecorded — the
// comparison API needs a document to explain unavailability from.
func TestSyncer_ResolverError_StillRecordsUnavailable(t *testing.T) {
	store := newFakeStore()
	lister := &fakeLister{refs: []webbaseline.StoreRef{{MerchantID: "merch-1", StoreID: "store-1", Platform: "woocommerce"}}}
	resolver := func(context.Context, string, string) (webbaseline.Source, bool, error) {
		return nil, false, errors.New("credentials unavailable")
	}
	syncer := webbaseline.NewSyncer(store, lister, resolver, time.Hour, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go syncer.Run(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()

	store.mu.Lock()
	defer store.mu.Unlock()
	require.Len(t, store.days, 1)
	for _, day := range store.days {
		assert.Equal(t, webbaseline.ConfidenceUnavailable, day.Confidence)
	}
}

type fakeSource struct {
	orders       int64
	revenueMinor int64
	currency     string
}

func (f fakeSource) DailyBaseline(context.Context, string, time.Time) (webbaseline.DailyBaselineResult, error) {
	return webbaseline.DailyBaselineResult{Orders: f.orders, RevenueMinor: f.revenueMinor, Currency: f.currency}, nil
}
