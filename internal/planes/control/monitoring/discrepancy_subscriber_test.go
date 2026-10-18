// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package monitoring

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// fakeFindingRepo is a test double for FindingRepository, recording every
// persisted finding (keyed alongside the store it was scoped to).
type fakeFindingRepo struct {
	saved   []savedFinding
	err     error
	deletes []deleteCall
}

// deleteCall records one DeleteRecoveredProbeFindings invocation, so tests
// can assert what a probe pass considered recovered.
type deleteCall struct {
	storeID         string
	probeSKUs       []string
	stillFailingIDs []string
}

func (f *fakeFindingRepo) DeleteRecoveredProbeFindings(_ context.Context, storeID string, probeSKUs, stillFailingIDs []string) error {
	f.deletes = append(f.deletes, deleteCall{storeID: storeID, probeSKUs: probeSKUs, stillFailingIDs: stillFailingIDs})
	return nil
}

type savedFinding struct {
	storeID string
	finding *domain.ConsistencyFinding
}

func (f *fakeFindingRepo) Save(_ context.Context, storeID string, finding *domain.ConsistencyFinding) error {
	if f.err != nil {
		return f.err
	}
	f.saved = append(f.saved, savedFinding{storeID: storeID, finding: finding})
	return nil
}

// fakeDiscrepancyEvent implements eventbus.DomainEvent for testing, standing
// in for the sync plane's sync.reconciliation.discrepancy_found message.
type fakeDiscrepancyEvent struct {
	merchantID string
	storeID    string
	payload    []byte
}

func (e *fakeDiscrepancyEvent) EventType() string     { return "sync.reconciliation.discrepancy_found" }
func (e *fakeDiscrepancyEvent) StoreID() string       { return e.storeID }
func (e *fakeDiscrepancyEvent) MerchantID() string    { return e.merchantID }
func (e *fakeDiscrepancyEvent) OccurredAt() time.Time { return time.Now().UTC() }
func (e *fakeDiscrepancyEvent) Payload() []byte       { return e.payload }

func newDiscrepancyEvent(t *testing.T, storeID, merchantID string, payload reconciliationDiscrepancyPayload) eventbus.DomainEvent {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal discrepancy payload: %v", err)
	}
	return &fakeDiscrepancyEvent{merchantID: merchantID, storeID: storeID, payload: b}
}

// TestDiscrepancySubscriber_Handle_RoutesToTask verifies the genuine
// cross-plane leg of sync.reconciliation.discrepancy_found: the control
// plane surfaces the sync plane's sweeper finding as a task via the
// existing FindingRouter, rather than the finding vanishing (as it did
// before this change, when the sync-plane side of this channel was a
// one-line log stub with no control-plane consumer at all).
func TestDiscrepancySubscriber_Handle_RoutesToTask(t *testing.T) {
	taskSvc := &fakeTaskService{}
	router := NewTaskServiceRouter(taskSvc, testLog)
	subscriber := NewDiscrepancySubscriber(router, &fakeFindingRepo{}, nil, testLog)

	event := newDiscrepancyEvent(t, "store-1", "merchant-1", reconciliationDiscrepancyPayload{
		SKU:         "SKU-1",
		Field:       "price_minor",
		StoreValue:  "2500",
		SarvexValue: "2000",
	})

	if err := subscriber.Handle(context.Background(), event); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(taskSvc.raised) != 1 {
		t.Fatalf("expected exactly 1 task raised, got %d", len(taskSvc.raised))
	}
	req := taskSvc.raised[0]
	if req.SourceRef == nil || *req.SourceRef != "SKU-1" {
		t.Errorf("expected source_ref=SKU-1, got %v", req.SourceRef)
	}
	if req.Source != "monitoring" {
		t.Errorf("expected source=monitoring (TaskServiceRouter's warning-path source), got %q", req.Source)
	}
	if req.StoreID != "store-1" {
		t.Errorf("expected store_id=store-1, got %q", req.StoreID)
	}
}

// TestDiscrepancySubscriber_Handle_MalformedPayload verifies a malformed
// payload is logged and dropped, not propagated as an error (consistent
// with the rest of the codebase's bus-handler convention — see
// journey.Subscriber.Handle).
func TestDiscrepancySubscriber_Handle_MalformedPayload(t *testing.T) {
	taskSvc := &fakeTaskService{}
	router := NewTaskServiceRouter(taskSvc, testLog)
	subscriber := NewDiscrepancySubscriber(router, &fakeFindingRepo{}, nil, testLog)

	event := &fakeDiscrepancyEvent{merchantID: "merchant-1", storeID: "store-1", payload: []byte("not json")}

	if err := subscriber.Handle(context.Background(), event); err != nil {
		t.Fatalf("expected malformed payload to be swallowed, got %v", err)
	}
	if len(taskSvc.raised) != 0 {
		t.Errorf("expected no task raised for a malformed payload, got %d", len(taskSvc.raised))
	}
}

// TestDiscrepancySubscriber_Handle_RouterError verifies a task-service
// failure never propagates out of Handle as an error — a missed
// discrepancy task is not worth blocking bus redelivery over (see Handle's
// doc comment; RouteFindings itself already swallows per-finding Raise
// errors, this asserts the subscriber doesn't reintroduce a failure path).
func TestDiscrepancySubscriber_Handle_RouterError(t *testing.T) {
	taskSvc := &fakeTaskService{err: errTaskServiceUnavailable}
	router := NewTaskServiceRouter(taskSvc, testLog)
	subscriber := NewDiscrepancySubscriber(router, &fakeFindingRepo{}, nil, testLog)

	event := newDiscrepancyEvent(t, "store-1", "merchant-1", reconciliationDiscrepancyPayload{
		SKU: "SKU-1", Field: "price_minor", StoreValue: "2500", SarvexValue: "2000",
	})

	if err := subscriber.Handle(context.Background(), event); err != nil {
		t.Fatalf("expected router error to be swallowed, got %v", err)
	}
}

var errTaskServiceUnavailable = errors.New("task service unavailable")

// TestDiscrepancySubscriber_Handle_PersistsFinding verifies the new
// persistence leg: a discrepancy event becomes a queryable ConsistencyFinding
// (via FindingRepository), not just a task — this is what makes
// GET /stores/:store_id/health/consistency return real data instead of
// "not implemented" (see operator.OperatorService.ListConsistencyFindings).
func TestDiscrepancySubscriber_Handle_PersistsFinding(t *testing.T) {
	taskSvc := &fakeTaskService{}
	router := NewTaskServiceRouter(taskSvc, testLog)
	repo := &fakeFindingRepo{}
	subscriber := NewDiscrepancySubscriber(router, repo, nil, testLog)

	event := newDiscrepancyEvent(t, "store-1", "merchant-1", reconciliationDiscrepancyPayload{
		SKU: "SKU-1", Field: "price_minor", StoreValue: "2500", SarvexValue: "2000",
	})

	if err := subscriber.Handle(context.Background(), event); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(repo.saved) != 1 {
		t.Fatalf("expected 1 finding persisted, got %d", len(repo.saved))
	}
	if repo.saved[0].storeID != "store-1" {
		t.Errorf("expected store_id=store-1, got %q", repo.saved[0].storeID)
	}
	if repo.saved[0].finding.SKU != "SKU-1" {
		t.Errorf("expected sku=SKU-1, got %q", repo.saved[0].finding.SKU)
	}
	if repo.saved[0].finding.Severity != domain.SeverityWarning {
		t.Errorf("expected severity=warning, got %q", repo.saved[0].finding.Severity)
	}
}

// TestDiscrepancySubscriber_Handle_PersistErrorStillRoutesTask verifies a
// persistence failure doesn't suppress the task notification — consistent
// with the "best effort, log and continue" convention this subscriber
// already applies to router failures.
func TestDiscrepancySubscriber_Handle_PersistErrorStillRoutesTask(t *testing.T) {
	taskSvc := &fakeTaskService{}
	router := NewTaskServiceRouter(taskSvc, testLog)
	repo := &fakeFindingRepo{err: errors.New("mongo unavailable")}
	subscriber := NewDiscrepancySubscriber(router, repo, nil, testLog)

	event := newDiscrepancyEvent(t, "store-1", "merchant-1", reconciliationDiscrepancyPayload{
		SKU: "SKU-1", Field: "price_minor", StoreValue: "2500", SarvexValue: "2000",
	})

	if err := subscriber.Handle(context.Background(), event); err != nil {
		t.Fatalf("expected persist error to be swallowed, got %v", err)
	}
	if len(taskSvc.raised) != 1 {
		t.Errorf("expected task still raised despite persist failure, got %d", len(taskSvc.raised))
	}
}
