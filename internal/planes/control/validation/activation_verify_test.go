// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"errors"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue"
	"github.com/sarvexlab/sarvex/internal/planes/control/activation"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// stubActivationService records whether the orchestrator asked it to verify,
// and for which store.
type stubActivationService struct {
	calls      int
	gotStoreID domain.StoreID
	err        error
}

func (s *stubActivationService) Verify(_ context.Context, _ domain.MerchantID, storeID domain.StoreID) (activation.VerificationReport, error) {
	s.calls++
	s.gotStoreID = storeID
	if s.err != nil {
		return activation.VerificationReport{}, s.err
	}
	return activation.VerificationReport{Passed: true, State: domain.UCPStateVerified}, nil
}

func newOrchestratorWithActivation(t *testing.T, results *TestResults, act ActivationService) *Orchestrator {
	t.Helper()
	cat, _ := catalogue.LoadCatalogue(context.Background(), testLogger())
	return NewOrchestrator(
		&stubTestHarness{results: results},
		NewMemoryConformanceStore(),
		&stubPlatformResolver{platform: "woocommerce"},
		nil,
		cat,
		testLogger(),
		"2026-04-08",
	).WithActivationService(act)
}

// A passing pre-validation run is also what verifies the store's own UCP
// endpoints — the two halves are one merchant-visible action, so the merchant
// is never asked to trigger verification separately.
func TestRunValidation_PassAlsoVerifiesActivation(t *testing.T) {
	act := &stubActivationService{}
	orch := newOrchestratorWithActivation(t, &TestResults{Passed: []string{"a", "b"}}, act)

	if _, err := orch.RunValidation(context.Background(), "merchant-1", "store-1"); err != nil {
		t.Fatalf("RunValidation: %v", err)
	}

	if act.calls != 1 {
		t.Fatalf("expected exactly one verification, got %d", act.calls)
	}
	if act.gotStoreID != "store-1" {
		t.Errorf("verified the wrong store: %q", act.gotStoreID)
	}
}

// A failing run must not verify: verification would emit verification_failed,
// which suspends a store that had previously verified. A store that can't pass
// the suite isn't ready to be probed at all.
func TestRunValidation_FailDoesNotVerifyActivation(t *testing.T) {
	act := &stubActivationService{}
	orch := newOrchestratorWithActivation(t, &TestResults{
		Failed: []*domain.ValidationFailure{{Requirement: "r1"}},
	}, act)

	if _, err := orch.RunValidation(context.Background(), "merchant-1", "store-1"); err != nil {
		t.Fatalf("RunValidation: %v", err)
	}

	if act.calls != 0 {
		t.Fatalf("expected no verification after a failing run, got %d", act.calls)
	}
}

// The conformance record is the run's result. A verification that errors is
// logged and dropped — it must not turn a completed run into a failed request.
func TestRunValidation_VerificationErrorDoesNotFailTheRun(t *testing.T) {
	act := &stubActivationService{err: errors.New("endpoints unreachable")}
	orch := newOrchestratorWithActivation(t, &TestResults{Passed: []string{"a"}}, act)

	record, err := orch.RunValidation(context.Background(), "merchant-1", "store-1")
	if err != nil {
		t.Fatalf("RunValidation should not fail when verification errors: %v", err)
	}
	if record.Result != domain.ConformancePass {
		t.Errorf("expected the run to still record a pass, got %s", record.Result)
	}
}

// Verification is optional wiring: an orchestrator with no activation service
// runs the suite exactly as before rather than panicking.
func TestRunValidation_NoActivationServiceWired(t *testing.T) {
	cat, _ := catalogue.LoadCatalogue(context.Background(), testLogger())
	orch := NewOrchestrator(
		&stubTestHarness{results: &TestResults{Passed: []string{"a"}}},
		NewMemoryConformanceStore(),
		&stubPlatformResolver{platform: "woocommerce"},
		nil,
		cat,
		testLogger(),
		"2026-04-08",
	)

	if _, err := orch.RunValidation(context.Background(), "merchant-1", "store-1"); err != nil {
		t.Fatalf("RunValidation: %v", err)
	}
}
