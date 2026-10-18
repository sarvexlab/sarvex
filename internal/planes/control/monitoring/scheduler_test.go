// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package monitoring

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStoreLister paginates a fixed, in-memory set of stores.
type fakeStoreLister struct {
	mu      sync.Mutex
	refs    []StoreRef
	calls   []string // afterID values, in call order
	listErr error
}

// callCount reads the call log under the lock — the scheduler probes from its
// own goroutine, so an unsynchronised read here is a data race the -race
// build will fail on.
func (f *fakeStoreLister) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeStoreLister) ListAllStores(_ context.Context, afterID string, limit int) ([]StoreRef, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, afterID)
	if f.listErr != nil {
		return nil, false, f.listErr
	}

	start := 0
	if afterID != "" {
		for i, r := range f.refs {
			if r.StoreID == afterID {
				start = i + 1
				break
			}
		}
	}
	end := start + limit
	hasMore := end < len(f.refs)
	if end > len(f.refs) {
		end = len(f.refs)
	}
	if start > len(f.refs) {
		return nil, false, nil
	}
	return f.refs[start:end], hasMore, nil
}

func TestScheduler_ForEachStore_PaginatesAll(t *testing.T) {
	refs := make([]StoreRef, 0, storeListPageSize+5)
	for i := range storeListPageSize + 5 {
		refs = append(refs, StoreRef{MerchantID: "merch-1", StoreID: fmtSKU(i), Platform: "woocommerce"})
	}
	lister := &fakeStoreLister{refs: refs}
	loop := NewMonitoringLoop(nil, nil, nil, NewTaskServiceRouter(&fakeTaskService{}, testLog), nil, nil, testLog)
	sched := NewScheduler(loop, lister, testLog)

	var mu sync.Mutex
	var visited []string
	sched.forEachStore(context.Background(), func(_ context.Context, ref StoreRef) {
		mu.Lock()
		defer mu.Unlock()
		visited = append(visited, ref.StoreID)
	})

	assert.Len(t, visited, len(refs))
	// More than one page was fetched.
	assert.Greater(t, len(lister.calls), 1)
}

func TestScheduler_ForEachStore_ListErrorStopsCleanly(t *testing.T) {
	lister := &fakeStoreLister{listErr: errors.New("mongo down")}
	loop := NewMonitoringLoop(nil, nil, nil, NewTaskServiceRouter(&fakeTaskService{}, testLog), nil, nil, testLog)
	sched := NewScheduler(loop, lister, testLog)

	var visited int
	sched.forEachStore(context.Background(), func(_ context.Context, _ StoreRef) {
		visited++
	})
	assert.Equal(t, 0, visited)
}

func TestScheduler_RunProbesAndSweep_SkipGracefullyWhenUnconfigured(t *testing.T) {
	lister := &fakeStoreLister{refs: []StoreRef{{MerchantID: "merch-1", StoreID: "store-1", Platform: "woocommerce"}}}
	loop := NewMonitoringLoop(nil, nil, nil, NewTaskServiceRouter(&fakeTaskService{}, testLog), nil, nil, testLog)
	sched := NewScheduler(loop, lister, testLog)

	// No probe/sweeper configured on the loop — must not panic, and both
	// passes should visit the store.
	require.NotPanics(t, func() {
		sched.runProbes(context.Background())
		sched.runSweep(context.Background())
	})
}

// Probes reach sibling services over HTTP, none of which is necessarily
// listening when the control plane finishes booting. Running the first pass
// immediately meant every restart manufactured a critical finding — a 502
// from the plugin doorway while the sync plane it proxies to was still coming
// up — which then stood until the next pass cleared it.
func TestScheduler_DelaysTheFirstProbePassPastStartup(t *testing.T) {
	lister := &fakeStoreLister{refs: []StoreRef{{MerchantID: "merch-1", StoreID: "store-1", Platform: "woocommerce"}}}
	loop := NewMonitoringLoop(nil, nil, nil, NewTaskServiceRouter(&fakeTaskService{}, testLog), nil, nil, testLog)

	// A grace longer than the test's own lifetime: Run must return on ctx
	// cancellation without ever having probed.
	sched := NewScheduler(loop, lister, testLog).WithStartupGrace(time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sched.Run(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run must return promptly when cancelled during the startup grace")
	}

	// The sweep has no sibling dependency, so it is not delayed — the store
	// was visited by it, and only by it.
	if lister.callCount() == 0 {
		t.Error("the sweep pass should still run immediately")
	}
}

// A zero grace keeps the old immediate behaviour, which is what tests and any
// caller that wants a pass right now rely on.
func TestScheduler_ZeroGraceProbesImmediately(t *testing.T) {
	lister := &fakeStoreLister{refs: []StoreRef{{MerchantID: "merch-1", StoreID: "store-1", Platform: "woocommerce"}}}
	loop := NewMonitoringLoop(nil, nil, nil, NewTaskServiceRouter(&fakeTaskService{}, testLog), nil, nil, testLog)
	sched := NewScheduler(loop, lister, testLog).WithStartupGrace(0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		sched.Run(ctx)
		close(done)
	}()

	// Both passes run before the tickers, so the store list is read at least
	// twice with no delay.
	deadline := time.After(2 * time.Second)
	for {
		if lister.callCount() >= 2 {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("expected both passes to run immediately, got %d list calls", lister.callCount())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// fakeKeyEnsurer records which stores were repaired.
type fakeKeyEnsurer struct {
	mu     sync.Mutex
	stores []string
	err    error
}

func (f *fakeKeyEnsurer) EnsureSigningKeys(_ context.Context, storeID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stores = append(f.stores, storeID)
	if f.err != nil {
		return "", f.err
	}
	return "kid-" + storeID, nil
}

func (f *fakeKeyEnsurer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.stores)
}

// Signing keys were provisioned once, at pairing, and never again — so a store
// that lost them published a profile no agent could verify, permanently and
// silently. The scheduler now repairs every store on each pass.
func TestScheduler_ReconcilesSigningKeys(t *testing.T) {
	lister := &fakeStoreLister{refs: []StoreRef{
		{MerchantID: "merch-1", StoreID: "store-1", Platform: "woocommerce"},
		{MerchantID: "merch-1", StoreID: "store-2", Platform: "woocommerce"},
	}}
	loop := NewMonitoringLoop(nil, nil, nil, NewTaskServiceRouter(&fakeTaskService{}, testLog), nil, nil, testLog)
	keys := &fakeKeyEnsurer{}
	sched := NewScheduler(loop, lister, testLog).WithKeyEnsurer(keys)

	sched.reconcileKeys(context.Background())

	if keys.count() != 2 {
		t.Fatalf("expected every store to be reconciled, got %d", keys.count())
	}
}

// A store whose repair fails must not stop the others, and must not fail the
// pass: the next interval retries.
func TestScheduler_KeyReconcileSurvivesFailures(t *testing.T) {
	lister := &fakeStoreLister{refs: []StoreRef{
		{MerchantID: "merch-1", StoreID: "store-1", Platform: "woocommerce"},
		{MerchantID: "merch-1", StoreID: "store-2", Platform: "woocommerce"},
	}}
	loop := NewMonitoringLoop(nil, nil, nil, NewTaskServiceRouter(&fakeTaskService{}, testLog), nil, nil, testLog)
	keys := &fakeKeyEnsurer{err: errors.New("keystore down")}
	sched := NewScheduler(loop, lister, testLog).WithKeyEnsurer(keys)

	require.NotPanics(t, func() { sched.reconcileKeys(context.Background()) })
	if keys.count() != 2 {
		t.Errorf("a failure on one store must not skip the rest, got %d", keys.count())
	}
}

// Unwired, the pass is a no-op rather than a nil dereference.
func TestScheduler_KeyReconcileSkippedWhenUnwired(t *testing.T) {
	lister := &fakeStoreLister{refs: []StoreRef{{MerchantID: "merch-1", StoreID: "store-1", Platform: "woocommerce"}}}
	loop := NewMonitoringLoop(nil, nil, nil, NewTaskServiceRouter(&fakeTaskService{}, testLog), nil, nil, testLog)
	sched := NewScheduler(loop, lister, testLog)

	require.NotPanics(t, func() { sched.reconcileKeys(context.Background()) })
}
