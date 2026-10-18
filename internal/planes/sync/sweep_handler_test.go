// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

func handlerTestLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, nil))
}

func newTestSweepHandler(lease *fakeLease, catalogStore *fakeCatalogStore, connector base.Connector, bus *fakeEventBus) *SweepHandler {
	reg, provider := newTestRegistry("store-1", connector)
	return NewSweepHandler(lease, catalogStore, reg, provider, bus, newFakeProductRescorer(), handlerTestLogger(), time.Minute)
}

func intp(i int) *int { return &i }

// TestSweepHandler_Diff_AddRemoveDrift covers the three discrepancy shapes:
// an add (in store truth, missing from the catalog store), a remove (in the
// catalog store, missing from store truth), and a field-drift (price
// differs), asserting each produces the right discrepancy event and
// corrects the catalog store's mirror.
func TestSweepHandler_Diff_AddRemoveDrift(t *testing.T) {
	catalogStore := newFakeCatalogStore()
	catalogStore.products["merchant-1:store-1:sku-match"] = &CatalogProduct{
		StoreID: "store-1", MerchantID: "merchant-1", SKU: "sku-match",
		Title: "Widget", PriceMinor: 1000, Currency: "USD", InStock: true, Quantity: intp(5),
	}
	catalogStore.products["merchant-1:store-1:sku-drift"] = &CatalogProduct{
		StoreID: "store-1", MerchantID: "merchant-1", SKU: "sku-drift",
		Title: "Gadget", PriceMinor: 2000, Currency: "USD", InStock: true, Quantity: intp(3),
		MappedFields: map[string]string{"custom": "preserved"},
	}
	catalogStore.products["merchant-1:store-1:sku-removed"] = &CatalogProduct{
		StoreID: "store-1", MerchantID: "merchant-1", SKU: "sku-removed",
		Title: "Gone", PriceMinor: 500, Currency: "USD", InStock: true,
	}

	connector := &fakeStoreConnector{entities: []base.CatalogEntity{
		{SKU: "sku-match", Name: "Widget", PriceMinor: 1000, Currency: "USD", InStock: true, Quantity: intp(5)},
		{SKU: "sku-drift", Name: "Gadget", PriceMinor: 2500, Currency: "USD", InStock: true, Quantity: intp(3)},
		{SKU: "sku-added", Name: "New Thing", PriceMinor: 750, Currency: "USD", InStock: true, Quantity: intp(1)},
	}}

	lease := newFakeLease()
	bus := newFakeEventBus()
	h := newTestSweepHandler(lease, catalogStore, connector, bus)

	if err := h.RunSweep(context.Background(), "store-1", "merchant-1"); err != nil {
		t.Fatalf("RunSweep: %v", err)
	}

	discrepancies := make(map[string]ReconciliationDiscrepancyPayload)
	for _, evt := range bus.published {
		if evt.EventType() != "sync.reconciliation.discrepancy_found" {
			t.Errorf("unexpected event type %q", evt.EventType())
			continue
		}
		var p ReconciliationDiscrepancyPayload
		if err := json.Unmarshal(evt.Payload(), &p); err != nil {
			t.Fatalf("unmarshal discrepancy: %v", err)
		}
		discrepancies[p.SKU+":"+p.Field] = p
	}

	// sku-match: no discrepancy.
	for key := range discrepancies {
		if key == "sku-match:price_minor" || key == "sku-match:presence" {
			t.Errorf("did not expect a discrepancy for matching sku-match, got %s", key)
		}
	}

	// sku-drift: price_minor discrepancy with both values.
	drift, ok := discrepancies["sku-drift:price_minor"]
	if !ok {
		t.Fatalf("expected a price_minor discrepancy for sku-drift, got %v", discrepancies)
	}
	if drift.StoreValue != "2500" || drift.SarvexValue != "2000" {
		t.Errorf("expected store_value=2500 sarvex_value=2000, got store_value=%s sarvex_value=%s", drift.StoreValue, drift.SarvexValue)
	}

	// sku-removed: presence discrepancy (absent in store, present in Sarvex).
	removed, ok := discrepancies["sku-removed:presence"]
	if !ok {
		t.Fatalf("expected a presence discrepancy for sku-removed, got %v", discrepancies)
	}
	if removed.StoreValue != "absent" || removed.SarvexValue != "present" {
		t.Errorf("unexpected removed discrepancy values: %+v", removed)
	}

	// sku-added: presence discrepancy (present in store, absent from Sarvex).
	added, ok := discrepancies["sku-added:presence"]
	if !ok {
		t.Fatalf("expected a presence discrepancy for sku-added, got %v", discrepancies)
	}
	if added.StoreValue != "present" || added.SarvexValue != "absent" {
		t.Errorf("unexpected added discrepancy values: %+v", added)
	}

	// The catalog store mirror should now be corrected: sku-drift's price
	// updated (mapped fields preserved), sku-removed deleted, sku-added
	// upserted.
	corrected, err := catalogStore.GetProduct(context.Background(), "store-1", "sku-drift")
	if err != nil {
		t.Fatalf("get corrected sku-drift: %v", err)
	}
	if corrected.PriceMinor != 2500 {
		t.Errorf("expected sku-drift price corrected to 2500, got %d", corrected.PriceMinor)
	}
	if corrected.MappedFields["custom"] != "preserved" {
		t.Errorf("expected mapped fields to survive the correction, got %v", corrected.MappedFields)
	}

	if _, err := catalogStore.GetProduct(context.Background(), "store-1", "sku-removed"); !errors.Is(err, ErrProductNotFound) {
		t.Errorf("expected sku-removed to be deleted from the catalog store, got err=%v", err)
	}

	addedProduct, err := catalogStore.GetProduct(context.Background(), "store-1", "sku-added")
	if err != nil {
		t.Fatalf("get corrected sku-added: %v", err)
	}
	if addedProduct.PriceMinor != 750 {
		t.Errorf("expected sku-added upserted with price 750, got %d", addedProduct.PriceMinor)
	}
}

// TestSweepHandler_CorrectedAndAddedProducts_AreRescored verifies the sweep
// handler re-runs mapping + scoring on every product it corrects or adds
// (not just the ones a live signal happens to touch later) — the gap this
// change closes: previously a sweep-corrected product's readiness score
// silently went stale.
func TestSweepHandler_CorrectedAndAddedProducts_AreRescored(t *testing.T) {
	catalogStore := newFakeCatalogStore()
	catalogStore.products["merchant-1:store-1:sku-drift"] = &CatalogProduct{
		StoreID: "store-1", MerchantID: "merchant-1", SKU: "sku-drift",
		Title: "Gadget", PriceMinor: 2000, Currency: "USD",
	}
	catalogStore.products["merchant-1:store-1:sku-match"] = &CatalogProduct{
		StoreID: "store-1", MerchantID: "merchant-1", SKU: "sku-match",
		Title: "Widget", PriceMinor: 1000, Currency: "USD",
	}

	connector := &fakeStoreConnector{entities: []base.CatalogEntity{
		{SKU: "sku-match", Name: "Widget", PriceMinor: 1000, Currency: "USD"},
		{SKU: "sku-drift", Name: "Gadget", PriceMinor: 2500, Currency: "USD"},
		{SKU: "sku-added", Name: "New Thing", PriceMinor: 750, Currency: "USD"},
	}}

	lease := newFakeLease()
	bus := newFakeEventBus()
	reg, provider := newTestRegistry("store-1", connector)
	rescorer := newFakeProductRescorer()
	h := NewSweepHandler(lease, catalogStore, reg, provider, bus, rescorer, handlerTestLogger(), time.Minute)

	if err := h.RunSweep(context.Background(), "store-1", "merchant-1"); err != nil {
		t.Fatalf("RunSweep: %v", err)
	}

	rescored := make(map[string]bool)
	for _, sku := range rescorer.rescored {
		rescored[sku] = true
	}
	if !rescored["sku-drift"] {
		t.Errorf("expected the drifted product to be rescored, got %v", rescorer.rescored)
	}
	if !rescored["sku-added"] {
		t.Errorf("expected the newly-added product to be rescored, got %v", rescorer.rescored)
	}
	if rescored["sku-match"] {
		t.Errorf("expected the matching (unchanged) product not to be rescored, got %v", rescorer.rescored)
	}
}

// TestSweepHandler_RunningLeaseCoalescesOverlap verifies the per-store
// overlap guard: a second sweep for a store already mid-sweep no-ops rather
// than running a concurrent second sweep.
func TestSweepHandler_RunningLeaseCoalescesOverlap(t *testing.T) {
	catalogStore := newFakeCatalogStore()
	lease := newFakeLease()
	bus := newFakeEventBus()

	release := make(chan struct{})
	// Buffered: the connector signals with a non-blocking send, so an unbuffered
	// channel drops the signal whenever the sweep goroutine reaches FetchCatalog
	// before this goroutine reaches <-started, and the test then waits forever.
	started := make(chan struct{}, 1)
	connector := &blockingSweepConnector{started: started, release: release}
	h := newTestSweepHandler(lease, catalogStore, connector, bus)

	done := make(chan error, 1)
	go func() {
		done <- h.RunSweep(context.Background(), "store-1", "merchant-1")
	}()

	<-started // first sweep has acquired the lease and is "running"

	// A second sweep for the same store arrives while the first is still in
	// flight: it must coalesce (no-op), not run a second fetch.
	if err := h.RunSweep(context.Background(), "store-1", "merchant-1"); err != nil {
		t.Fatalf("expected overlapping sweep to no-op without error, got %v", err)
	}
	if connector.calls() != 1 {
		t.Fatalf("expected exactly 1 FetchCatalog call while the first sweep is still running, got %d", connector.calls())
	}

	close(release) // let the first sweep finish
	if err := <-done; err != nil {
		t.Fatalf("first sweep returned error: %v", err)
	}

	// Now that the first sweep released its lease, a new sweep must run normally.
	if err := h.RunSweep(context.Background(), "store-1", "merchant-1"); err != nil {
		t.Fatalf("expected a fresh sweep to succeed after release, got %v", err)
	}
	if connector.calls() != 2 {
		t.Fatalf("expected a second FetchCatalog call after the lease was released, got %d", connector.calls())
	}
}

// TestSweepHandler_CompetingConsumer_OneInstanceExecutes models two
// sync-plane process instances (two SweepHandler values sharing the same
// distributed lease) both running a sweep for the same store at once. Only
// one may actually run FetchCatalog; the other must coalesce.
func TestSweepHandler_CompetingConsumer_OneInstanceExecutes(t *testing.T) {
	lease := newFakeLease() // shared "Redis" between both instances
	catalogStoreA := newFakeCatalogStore()
	catalogStoreB := catalogStoreA // same underlying catalog store (shared Mongo)
	bus := newFakeEventBus()

	release := make(chan struct{})
	started := make(chan struct{}, 2)
	connectorA := &blockingSweepConnector{started: started, release: release}
	connectorB := &blockingSweepConnector{started: started, release: release}

	instanceA := newTestSweepHandler(lease, catalogStoreA, connectorA, bus)
	instanceB := newTestSweepHandler(lease, catalogStoreB, connectorB, bus)

	doneA := make(chan error, 1)
	go func() { doneA <- instanceA.RunSweep(context.Background(), "store-1", "merchant-1") }()

	// Deterministically establish that instance A holds the lease and is
	// mid-sweep before instance B ever attempts it — otherwise "start both
	// goroutines and hope they race" is exactly the kind of scheduling-order
	// flake this test must not have.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for instance A to start")
	}

	// Instance B — a second sync-plane process sharing the same distributed
	// lease — now runs the identical sweep. With A provably still holding
	// the lease, B must coalesce synchronously, never touching its
	// connector.
	if err := instanceB.RunSweep(context.Background(), "store-1", "merchant-1"); err != nil {
		t.Fatalf("expected instance B to coalesce without error, got %v", err)
	}
	if connectorB.calls() != 0 {
		t.Fatalf("expected instance B to never call FetchCatalog while instance A holds the lease, got %d calls", connectorB.calls())
	}

	close(release)
	if err := <-doneA; err != nil {
		t.Errorf("instance A returned error: %v", err)
	}
	if connectorA.calls() != 1 {
		t.Fatalf("expected instance A to execute exactly 1 FetchCatalog call, got %d", connectorA.calls())
	}
}

// TestSweepHandler_FetchCatalogFailure_ReleasesLeaseAndErrors verifies a
// typed FetchCatalog failure surfaces as an error and still releases the
// running-sweep lease, leaving no partial state (no discrepancies emitted,
// catalog store untouched).
func TestSweepHandler_FetchCatalogFailure_ReleasesLeaseAndErrors(t *testing.T) {
	catalogStore := newFakeCatalogStore()
	catalogStore.products["merchant-1:store-1:sku-1"] = &CatalogProduct{StoreID: "store-1", MerchantID: "merchant-1", SKU: "sku-1"}
	lease := newFakeLease()
	bus := newFakeEventBus()

	connector := &fakeStoreConnector{fetchErr: &base.ConnectorError{
		Kind: base.ConnectorErrUnavailable, StoreID: "store-1", Op: "FetchCatalog", Err: errors.New("upstream down"),
	}}
	h := newTestSweepHandler(lease, catalogStore, connector, bus)

	err := h.RunSweep(context.Background(), "store-1", "merchant-1")
	if err == nil {
		t.Fatal("expected FetchCatalog failure to surface as an error")
	}
	var connErr *base.ConnectorError
	if !errors.As(err, &connErr) {
		t.Errorf("expected a typed *base.ConnectorError in the error chain, got %v", err)
	}

	if len(bus.published) != 0 {
		t.Errorf("expected no discrepancy events on a fetch failure, got %d", len(bus.published))
	}

	// Lease must be released: a follow-up sweep for the same store must be
	// able to acquire it rather than being coalesced forever.
	acquired, err := lease.TryAcquire(context.Background(), runningSweepLeaseKeyPrefix+"store-1", "someone-else", time.Minute)
	if err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	if !acquired {
		t.Fatal("expected the running-sweep lease to have been released after a FetchCatalog failure")
	}
}

// blockingSweepConnector is a base.Connector test double whose FetchCatalog
// signals started, then blocks until release is closed, letting tests
// control the timing of "a sweep is in flight" precisely. The embedded nil
// base.Connector satisfies the rest of the interface, unused here.
type blockingSweepConnector struct {
	base.Connector

	started chan struct{}
	release chan struct{}

	mu    sync.Mutex
	count int
}

func (c *blockingSweepConnector) FetchCatalog(_ context.Context, _ string, _ int) ([]base.CatalogEntity, error) {
	c.mu.Lock()
	c.count++
	c.mu.Unlock()
	select {
	case c.started <- struct{}{}:
	default:
	}
	<-c.release
	return nil, nil
}

func (c *blockingSweepConnector) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

var _ base.Connector = (*blockingSweepConnector)(nil)

// The sweep must carry the owning merchant: without it an added product cannot
// be written (catalog writes need the tenant key) and discrepancies are
// published under no merchant. It refuses to run rather than half-run.
func TestSweepHandler_RequiresMerchantID(t *testing.T) {
	catalogStore := newFakeCatalogStore()
	h := newTestSweepHandler(newFakeLease(), catalogStore, &fakeStoreConnector{}, newFakeEventBus())

	if err := h.RunSweep(context.Background(), "store-1", ""); err == nil {
		t.Fatal("expected an error sweeping a store with no merchant_id")
	}
}

// The scheduled sweep resolves the merchant from the store's import job and
// passes it through, so a product missing from the mirror is added under that
// merchant and its discrepancy event carries the merchant.
func TestJobServer_SweepStore_UsesMerchantFromImportJob(t *testing.T) {
	catalogStore := newFakeCatalogStore()
	connector := &fakeStoreConnector{entities: []base.CatalogEntity{
		{SKU: "sku-added", Name: "New Thing", PriceMinor: 750, Currency: "USD", InStock: true, Quantity: intp(1)},
	}}
	bus := newFakeEventBus()
	jobs := newFakeImportJobRepo()
	_ = jobs.Upsert(context.Background(), &ImportJob{StoreID: "store-1", MerchantID: "merchant-1", Status: ImportStatusCompleted})

	js := &JobServer{
		sweepHandler:  newTestSweepHandler(newFakeLease(), catalogStore, connector, bus),
		importHandler: &CatalogImportHandler{jobRepo: jobs},
	}
	task := asynq.NewTask(taskSweepStore, []byte(`{"store_id":"store-1"}`))
	if err := js.handleSweepStore(context.Background(), task); err != nil {
		t.Fatalf("handleSweepStore: %v", err)
	}

	p, err := catalogStore.GetProduct(context.Background(), "store-1", "sku-added")
	if err != nil {
		t.Fatalf("added product was not written to the mirror: %v", err)
	}
	if p.MerchantID != "merchant-1" {
		t.Errorf("added product merchant = %q, want merchant-1", p.MerchantID)
	}
	if len(bus.published) == 0 {
		t.Fatal("expected a discrepancy event")
	}
	for _, e := range bus.published {
		if e.MerchantID() != "merchant-1" {
			t.Errorf("event %s merchant = %q, want merchant-1", e.EventType(), e.MerchantID())
		}
	}
}

// A store with no import job cannot be attributed to a merchant; the task is
// skipped (not retried) instead of sweeping under an empty merchant.
func TestJobServer_SweepStore_NoImportJobSkipsRetry(t *testing.T) {
	js := &JobServer{
		sweepHandler:  newTestSweepHandler(newFakeLease(), newFakeCatalogStore(), &fakeStoreConnector{}, newFakeEventBus()),
		importHandler: &CatalogImportHandler{jobRepo: newFakeImportJobRepo()},
	}
	err := js.handleSweepStore(context.Background(), asynq.NewTask(taskSweepStore, []byte(`{"store_id":"store-1"}`)))
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("expected asynq.SkipRetry, got %v", err)
	}
}
