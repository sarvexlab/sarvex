// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/concurrency"
	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	syncplane "github.com/sarvexlab/sarvex/internal/planes/sync"
)

// threadSafeRecordingBus captures published events for assertion. Unlike
// ingestion_test.go's recordingBus, this one is safe for concurrent
// Publish calls: the sweep scheduler's background goroutine and multiple
// concurrently-handling SweepHandler instances all publish onto it at once
// in this test.
type threadSafeRecordingBus struct {
	mu        sync.Mutex
	published []eventbus.DomainEvent
}

func (b *threadSafeRecordingBus) Publish(_ context.Context, event eventbus.DomainEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.published = append(b.published, event)
	return nil
}
func (b *threadSafeRecordingBus) Subscribe(_ context.Context, _ string, _ eventbus.EventHandler) (eventbus.Subscription, error) {
	return nil, nil
}
func (b *threadSafeRecordingBus) Unsubscribe(_ context.Context, _ eventbus.Subscription) error {
	return nil
}
func (b *threadSafeRecordingBus) Close() error { return nil }

func (b *threadSafeRecordingBus) eventsOfType(eventType string) []eventbus.DomainEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []eventbus.DomainEvent
	for _, e := range b.published {
		if e.EventType() == eventType {
			out = append(out, e)
		}
	}
	return out
}

// noopProductRescorer is a productRescorer test double: this test exercises
// sweep correction and discrepancy detection, not scoring (that is covered
// by internal/planes/sync's own sweep_handler_test.go), so RescoreProduct is
// a no-op here.
type noopProductRescorer struct{}

func (noopProductRescorer) OnProductDeleted(_ context.Context, _, _ string, _ *syncplane.CatalogProduct) error {
	return nil
}

func (noopProductRescorer) RescoreProduct(_ context.Context, _ *syncplane.CatalogProduct, _ *syncplane.ScoringRubric) error {
	return nil
}

// mockMultiPageConnector stands in for the real store-API client. It holds
// the "true" state of a merchant's catalog and answers FetchCatalog by
// paging over it internally in chunks of pageSize (mirroring how a real
// paginated store API would be walked), returning the assembled full set —
// the same contract base.Connector.FetchCatalog documents. fetchCalls counts
// invocations so tests can assert exactly one instance executed a sweep. The
// embedded nil base.Connector satisfies the rest of the interface, unused
// here.
type mockMultiPageConnector struct {
	base.Connector

	mu         sync.Mutex
	entities   []base.CatalogEntity
	fetchCalls int
}

func (m *mockMultiPageConnector) FetchCatalog(_ context.Context, _ string, pageSize int) ([]base.CatalogEntity, error) {
	m.mu.Lock()
	m.fetchCalls++
	m.mu.Unlock()

	if pageSize <= 0 {
		pageSize = 50
	}
	var out []base.CatalogEntity
	for start := 0; start < len(m.entities); start += pageSize {
		end := start + pageSize
		if end > len(m.entities) {
			end = len(m.entities)
		}
		page := make([]base.CatalogEntity, end-start)
		copy(page, m.entities[start:end])
		out = append(out, page...)
	}
	return out, nil
}

func (m *mockMultiPageConnector) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.fetchCalls
}

var _ base.Connector = (*mockMultiPageConnector)(nil)

func sweepIntegrationLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// TestReconciliationSweeper_MultiInstance exercises the sweeper's execution
// design end to end against real MongoDB and real Redis: two handler
// instances sharing one running-sweep lease (competing-consumer execution
// must stay single-owner), against a mock multi-page store carrying an
// injected add, remove, and field-drift.
//
// It used to also assert leader election across two SweepScheduler
// instances. That component no longer exists: scheduling moved into asynq
// (see internal/planes/sync/job_server.go), which owns single-emission in
// Redis, and the only lease the sync plane still holds is the per-store
// sweep-overlap guard inside SweepHandler — which is exactly what the
// surviving half of this test covers.
func TestReconciliationSweeper_MultiInstance(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	catalogStore, err := syncplane.NewMongoDBCatalogStore(env.db)
	if err != nil {
		t.Fatalf("NewMongoDBCatalogStore: %v", err)
	}

	const storeID = "store-int-sweep"
	const merchantID = "merchant-int-sweep"

	// Seed the catalog store's mirror: sku-match (agrees with the store),
	// sku-drift (price will disagree), sku-removed (no longer at the
	// store — a lost delete signal).
	seed := []*syncplane.CatalogProduct{
		{StoreID: storeID, MerchantID: merchantID, SKU: "sku-match", Title: "Widget", PriceMinor: 1000, Currency: "USD", InStock: true},
		{StoreID: storeID, MerchantID: merchantID, SKU: "sku-drift", Title: "Gadget", PriceMinor: 2000, Currency: "USD", InStock: true},
		{StoreID: storeID, MerchantID: merchantID, SKU: "sku-removed", Title: "Gone", PriceMinor: 500, Currency: "USD", InStock: true},
	}
	for _, p := range seed {
		if err := catalogStore.UpsertProduct(ctx, p); err != nil {
			t.Fatalf("seed UpsertProduct(%s): %v", p.SKU, err)
		}
	}

	// The store's authoritative truth (via a mock multi-page connector):
	// sku-match unchanged, sku-drift's price changed, sku-added is brand
	// new (a lost create signal), sku-removed is gone.
	storeTruth := []base.CatalogEntity{
		{SKU: "sku-match", Name: "Widget", PriceMinor: 1000, Currency: "USD", InStock: true},
		{SKU: "sku-drift", Name: "Gadget", PriceMinor: 2500, Currency: "USD", InStock: true},
		{SKU: "sku-added", Name: "New Thing", PriceMinor: 750, Currency: "USD", InStock: true},
	}

	lease := concurrency.NewRedisLease(env.redisClient, env.redisPrefix)
	log := sweepIntegrationLogger()

	// ── Handler side: 2 instances competing to execute the sweep ────────────
	handlerBus := &threadSafeRecordingBus{}
	connectorA := &mockMultiPageConnector{entities: storeTruth}
	connectorB := &mockMultiPageConnector{entities: storeTruth}
	regA, providerA := newTestRegistry(storeID, merchantID, connectorA)
	regB, providerB := newTestRegistry(storeID, merchantID, connectorB)
	handlerA := syncplane.NewSweepHandler(lease, catalogStore, regA, providerA, handlerBus, noopProductRescorer{}, log, 5*time.Second)
	handlerB := syncplane.NewSweepHandler(lease, catalogStore, regB, providerB, handlerBus, noopProductRescorer{}, log, 5*time.Second)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs <- handlerA.RunSweep(ctx, storeID, merchantID)
	}()
	go func() {
		defer wg.Done()
		errs <- handlerB.RunSweep(ctx, storeID, merchantID)
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("RunSweep returned error: %v", err)
		}
	}

	totalFetches := connectorA.calls() + connectorB.calls()
	if totalFetches != 1 {
		t.Fatalf("expected exactly 1 FetchCatalog call across 2 competing handler instances for store %s, got %d (A=%d, B=%d)",
			storeID, totalFetches, connectorA.calls(), connectorB.calls())
	}

	// ── Discrepancies: add, remove, and field-drift, each detected once ─────
	discByKey := make(map[string]syncplane.ReconciliationDiscrepancyPayload)
	for _, e := range handlerBus.eventsOfType("sync.reconciliation.discrepancy_found") {
		var p syncplane.ReconciliationDiscrepancyPayload
		if err := json.Unmarshal(e.Payload(), &p); err != nil {
			t.Fatalf("unmarshal discrepancy: %v", err)
		}
		discByKey[p.SKU+":"+p.Field] = p
	}

	drift, ok := discByKey["sku-drift:price_minor"]
	if !ok {
		t.Fatalf("expected a price_minor discrepancy for sku-drift, got %v", discByKey)
	}
	if drift.StoreValue != "2500" || drift.SarvexValue != "2000" {
		t.Errorf("expected store_value=2500 sarvex_value=2000, got %+v", drift)
	}

	removed, ok := discByKey["sku-removed:presence"]
	if !ok || removed.StoreValue != "absent" || removed.SarvexValue != "present" {
		t.Fatalf("expected a presence discrepancy for sku-removed (absent/present), got %+v ok=%v", removed, ok)
	}

	added, ok := discByKey["sku-added:presence"]
	if !ok || added.StoreValue != "present" || added.SarvexValue != "absent" {
		t.Fatalf("expected a presence discrepancy for sku-added (present/absent), got %+v ok=%v", added, ok)
	}

	// The catalog store's mirror must now be corrected to match store truth.
	if _, err := catalogStore.GetProduct(ctx, storeID, "sku-removed"); !errors.Is(err, syncplane.ErrProductNotFound) {
		t.Errorf("expected sku-removed deleted from the catalog store, got err=%v", err)
	}
	corrected, err := catalogStore.GetProduct(ctx, storeID, "sku-drift")
	if err != nil {
		t.Fatalf("get corrected sku-drift: %v", err)
	}
	if corrected.PriceMinor != 2500 {
		t.Errorf("expected sku-drift corrected to price 2500, got %d", corrected.PriceMinor)
	}
	addedProduct, err := catalogStore.GetProduct(ctx, storeID, "sku-added")
	if err != nil {
		t.Fatalf("get corrected sku-added: %v", err)
	}
	if addedProduct.PriceMinor != 750 {
		t.Errorf("expected sku-added upserted at price 750, got %d", addedProduct.PriceMinor)
	}

	// Store isolation: a second, unrelated store must be untouched by any
	// of the above (never read or written).
	if _, err := catalogStore.GetProduct(ctx, "store-unrelated", "sku-match"); !errors.Is(err, syncplane.ErrProductNotFound) {
		t.Errorf("expected store-unrelated to remain untouched, got err=%v", err)
	}

	fmt.Fprintf(os.Stderr, "sweep integration test: %d discrepancies detected\n", len(discByKey))
}
