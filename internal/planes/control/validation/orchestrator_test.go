// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

var errPlatformNotFound = errors.New("connector not found for store")

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// stubTestHarness is a mock test harness for testing. It records the last
// storeID/platform it was called with so tests can assert the orchestrator
// forwards the resolved platform for the store actually being validated.
type stubTestHarness struct {
	results *TestResults
	err     error

	gotStoreID  domain.StoreID
	gotPlatform domain.Platform
}

func (s *stubTestHarness) RunTests(ctx context.Context, storeID domain.StoreID, platform domain.Platform) (*TestResults, error) {
	s.gotStoreID = storeID
	s.gotPlatform = platform
	if s.err != nil {
		return nil, s.err
	}
	return s.results, nil
}

// stubPlatformResolver is a fixed-platform PlatformResolver for testing.
type stubPlatformResolver struct {
	platform domain.Platform
	err      error
}

func (s *stubPlatformResolver) GetPlatform(ctx context.Context, storeID domain.StoreID) (domain.Platform, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.platform, nil
}

// fakeEventBus captures published events for verification in tests.
type fakeEventBus struct {
	publishedEvents []eventbus.DomainEvent
}

func (e *fakeEventBus) Publish(ctx context.Context, event eventbus.DomainEvent) error {
	e.publishedEvents = append(e.publishedEvents, event)
	return nil
}

func (e *fakeEventBus) Subscribe(ctx context.Context, topic string, handler eventbus.EventHandler) (eventbus.Subscription, error) {
	return nil, nil
}

func (e *fakeEventBus) Unsubscribe(ctx context.Context, sub eventbus.Subscription) error {
	return nil
}

func (e *fakeEventBus) Close() error {
	return nil
}

func TestOrchestratorRunValidationPass(t *testing.T) {
	harness := &stubTestHarness{
		results: &TestResults{
			Passed: []string{"test1", "test2"},
			Failed: []*domain.ValidationFailure{},
		},
	}

	store := NewMemoryConformanceStore()

	cat, _ := catalogue.LoadCatalogue(context.Background(), testLogger())
	orch := NewOrchestrator(harness, store, &stubPlatformResolver{platform: "woocommerce"}, nil, cat, testLogger(), "2026-04-08")

	record, err := orch.RunValidation(context.Background(), domain.MerchantID("merchant123"), domain.StoreID("store1"))
	if err != nil {
		t.Fatalf("RunValidation failed: %v", err)
	}

	if record.Result != domain.ConformancePass {
		t.Errorf("expected pass, got %s", record.Result)
	}

	if record.Passed != 2 {
		t.Errorf("expected 2 passed, got %d", record.Passed)
	}

	if record.Failed != 0 {
		t.Errorf("expected 0 failed, got %d", record.Failed)
	}

	if record.SpecVersion != "2026-04-08" {
		t.Errorf("expected spec version 2026-04-08, got %s", record.SpecVersion)
	}
}

func TestOrchestratorRunValidationFail(t *testing.T) {
	harness := &stubTestHarness{
		results: &TestResults{
			Passed: []string{"test1"},
			Failed: []*domain.ValidationFailure{
				{
					Requirement: "profile_required",
					SpecSection: "ucp.profile",
					Summary:     "profile missing",
				},
			},
		},
	}

	store := NewMemoryConformanceStore()

	cat, _ := catalogue.LoadCatalogue(context.Background(), testLogger())
	orch := NewOrchestrator(harness, store, &stubPlatformResolver{platform: "woocommerce"}, nil, cat, testLogger(), "2026-04-08")

	record, err := orch.RunValidation(context.Background(), domain.MerchantID("merchant123"), domain.StoreID("store1"))
	if err != nil {
		t.Fatalf("RunValidation failed: %v", err)
	}

	if record.Result != domain.ConformanceFail {
		t.Errorf("expected fail, got %s", record.Result)
	}

	if record.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", record.Failed)
	}
}

func TestOrchestratorGetStatus(t *testing.T) {
	harness := &stubTestHarness{
		results: &TestResults{
			Passed: []string{"test1", "test2"},
			Failed: []*domain.ValidationFailure{},
		},
	}

	store := NewMemoryConformanceStore()

	cat, _ := catalogue.LoadCatalogue(context.Background(), testLogger())
	orch := NewOrchestrator(harness, store, &stubPlatformResolver{platform: "woocommerce"}, nil, cat, testLogger(), "2026-04-08")

	// Run validation
	_, err := orch.RunValidation(context.Background(), domain.MerchantID("merchant123"), domain.StoreID("store1"))
	if err != nil {
		t.Fatalf("RunValidation failed: %v", err)
	}

	// Get status
	status, err := orch.GetStatus(context.Background(), domain.MerchantID("merchant123"), domain.StoreID("store1"))
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	if status.PreValidation != domain.PreValidationPassing {
		t.Errorf("expected pre-validation passing, got %s", status.PreValidation)
	}

	if status.TargetSpecVersion != "2026-04-08" {
		t.Errorf("expected spec version 2026-04-08, got %s", status.TargetSpecVersion)
	}

	if status.LastRunAt == nil {
		t.Error("expected LastRunAt to be set")
	}
}

func TestOrchestratorListRuns(t *testing.T) {
	harness := &stubTestHarness{
		results: &TestResults{
			Passed: []string{"test1"},
			Failed: []*domain.ValidationFailure{},
		},
	}

	store := NewMemoryConformanceStore()

	cat, _ := catalogue.LoadCatalogue(context.Background(), testLogger())
	orch := NewOrchestrator(harness, store, &stubPlatformResolver{platform: "woocommerce"}, nil, cat, testLogger(), "2026-04-08")

	// Run validation multiple times
	for i := range 3 {
		_ = i
		_, err := orch.RunValidation(context.Background(), domain.MerchantID("merchant123"), domain.StoreID("store1"))
		if err != nil {
			t.Fatalf("RunValidation failed: %v", err)
		}
	}

	// List runs
	runs, cursor, err := orch.ListRuns(context.Background(), "store1", 2, "")
	if err != nil {
		t.Fatalf("ListRuns failed: %v", err)
	}

	if len(runs) != 2 {
		t.Errorf("expected 2 runs, got %d", len(runs))
	}

	if cursor == nil {
		t.Error("expected cursor for pagination")
	}

	// List next page
	runs2, cursor2, err := orch.ListRuns(context.Background(), "store1", 2, *cursor)
	if err != nil {
		t.Fatalf("ListRuns (page 2) failed: %v", err)
	}

	// We have 3 items total, limit 2:
	// Page 1: items 0,1 with cursor pointing to item 1
	// Page 2: items 2 with no cursor (less than limit)
	// Actually, the cursor points to where to continue, not the current page.
	// So with cursor at item 1's timestamp, we should start from item 2.
	if len(runs2) < 1 {
		t.Errorf("expected at least 1 run on page 2, got %d", len(runs2))
	}

	// cursor2 should be nil if we got fewer items than the limit
	if len(runs2) < 2 && cursor2 != nil {
		t.Error("expected no cursor when receiving fewer items than limit")
	}
}

func TestOrchestratorEmitsJourneySignalOnPass(t *testing.T) {
	harness := &stubTestHarness{
		results: &TestResults{
			Passed: []string{"test1", "test2"},
			Failed: []*domain.ValidationFailure{},
		},
	}

	store := NewMemoryConformanceStore()
	bus := &fakeEventBus{}

	cat, _ := catalogue.LoadCatalogue(context.Background(), testLogger())
	orch := NewOrchestrator(harness, store, &stubPlatformResolver{platform: "woocommerce"}, bus, cat, testLogger(), "2026-04-08")

	_, err := orch.RunValidation(context.Background(), domain.MerchantID("merchant123"), domain.StoreID("store1"))
	if err != nil {
		t.Fatalf("RunValidation failed: %v", err)
	}

	if len(bus.publishedEvents) != 1 {
		t.Fatalf("expected 1 event published, got %d", len(bus.publishedEvents))
	}

	// Asserted against the constant, not a literal: internal/pkg/ucp is the
	// single source of truth for bus topic names (sarvex-v1-architecture.md). The literal
	// this replaced ("journey.signal") had drifted from the topic the
	// orchestrator actually publishes on.
	evt := bus.publishedEvents[0]
	if evt.EventType() != ucp.EventJourneySignal {
		t.Errorf("expected event type %s, got %s", ucp.EventJourneySignal, evt.EventType())
	}

	if evt.MerchantID() != "merchant123" {
		t.Errorf("expected merchant_id merchant123, got %s", evt.MerchantID())
	}
	// The journey is per store: without store_id the signal is dropped as
	// "signal received for non-existent journey".
	if evt.StoreID() != "store1" {
		t.Errorf("expected store_id store1, got %q", evt.StoreID())
	}

	var payload struct {
		Signal string `json:"signal"`
	}
	if err := json.Unmarshal(evt.Payload(), &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if payload.Signal != "validation_passed" {
		t.Errorf("expected signal validation_passed, got %s", payload.Signal)
	}
}

func TestOrchestratorEmitsJourneySignalOnFail(t *testing.T) {
	harness := &stubTestHarness{
		results: &TestResults{
			Passed: []string{},
			Failed: []*domain.ValidationFailure{
				{
					Requirement: "profile_required",
					SpecSection: "ucp.profile",
					Summary:     "profile missing",
				},
			},
		},
	}

	store := NewMemoryConformanceStore()
	bus := &fakeEventBus{}

	cat, _ := catalogue.LoadCatalogue(context.Background(), testLogger())
	orch := NewOrchestrator(harness, store, &stubPlatformResolver{platform: "woocommerce"}, bus, cat, testLogger(), "2026-04-08")

	_, err := orch.RunValidation(context.Background(), domain.MerchantID("merchant123"), domain.StoreID("store1"))
	if err != nil {
		t.Fatalf("RunValidation failed: %v", err)
	}

	if len(bus.publishedEvents) != 1 {
		t.Fatalf("expected 1 event published, got %d", len(bus.publishedEvents))
	}

	evt := bus.publishedEvents[0]
	if evt.StoreID() != "store1" {
		t.Errorf("expected store_id store1, got %q", evt.StoreID())
	}
	var payload struct {
		Signal string `json:"signal"`
	}
	if err := json.Unmarshal(evt.Payload(), &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if payload.Signal != "validation_failed" {
		t.Errorf("expected signal validation_failed, got %s", payload.Signal)
	}
}

func TestOrchestratorSkipsJourneySignalWhenNoBus(t *testing.T) {
	harness := &stubTestHarness{
		results: &TestResults{
			Passed: []string{"test1"},
			Failed: []*domain.ValidationFailure{},
		},
	}

	store := NewMemoryConformanceStore()

	// No event bus passed
	cat, _ := catalogue.LoadCatalogue(context.Background(), testLogger())
	orch := NewOrchestrator(harness, store, &stubPlatformResolver{platform: "woocommerce"}, nil, cat, testLogger(), "2026-04-08")

	_, err := orch.RunValidation(context.Background(), domain.MerchantID("merchant123"), domain.StoreID("store1"))
	if err != nil {
		t.Fatalf("RunValidation failed: %v", err)
	}

	// Should still complete without error, just silently skip event emission
}

// TestOrchestratorScopesHarnessToRequestedStore is the regression test for
// the bug where RunValidation ran the harness against a fixed global target
// regardless of which store was being validated. The orchestrator must
// resolve storeID's own platform and forward storeID (not merchantID) to
// the harness.
func TestOrchestratorScopesHarnessToRequestedStore(t *testing.T) {
	harness := &stubTestHarness{
		results: &TestResults{Passed: []string{"test1"}, Failed: []*domain.ValidationFailure{}},
	}
	platforms := &stubPlatformResolver{platform: "magento"}

	store := NewMemoryConformanceStore()

	cat, _ := catalogue.LoadCatalogue(context.Background(), testLogger())
	orch := NewOrchestrator(harness, store, platforms, nil, cat, testLogger(), "2026-04-08")

	_, err := orch.RunValidation(context.Background(), domain.MerchantID("merchant123"), domain.StoreID("store-xyz"))
	if err != nil {
		t.Fatalf("RunValidation failed: %v", err)
	}

	if harness.gotStoreID != domain.StoreID("store-xyz") {
		t.Errorf("expected harness to be called with storeID store-xyz, got %q", harness.gotStoreID)
	}
	if harness.gotPlatform != domain.Platform("magento") {
		t.Errorf("expected harness to be called with the store's resolved platform magento, got %q", harness.gotPlatform)
	}
}

// TestOrchestratorFailsClosedOnPlatformResolutionError ensures a validation
// run is never silently attempted against the wrong (or no) target when the
// store's platform can't be resolved.
func TestOrchestratorFailsClosedOnPlatformResolutionError(t *testing.T) {
	harness := &stubTestHarness{
		results: &TestResults{Passed: []string{"test1"}, Failed: []*domain.ValidationFailure{}},
	}
	platforms := &stubPlatformResolver{err: errPlatformNotFound}

	store := NewMemoryConformanceStore()

	cat, _ := catalogue.LoadCatalogue(context.Background(), testLogger())
	orch := NewOrchestrator(harness, store, platforms, nil, cat, testLogger(), "2026-04-08")

	_, err := orch.RunValidation(context.Background(), domain.MerchantID("merchant123"), domain.StoreID("store-xyz"))
	if err == nil {
		t.Fatal("expected RunValidation to fail when platform resolution fails")
	}
	if harness.gotStoreID != "" {
		t.Errorf("expected harness not to be called when platform resolution fails, but it was called with storeID %q", harness.gotStoreID)
	}
}
