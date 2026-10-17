// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package checkoutescalation

import (
	"reflect"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/checkoutcause"
)

// TestDiagnose_TableDrivenAcrossEveryFailureCause is A4: table-driven
// across every failure cause, and diagnosis cause must always equal the
// checkoutcause (the shared classification table this diagnosis layer
// reuses rather than duplicates, per R8) cause for the same signal.
func TestDiagnose_TableDrivenAcrossEveryFailureCause(t *testing.T) {
	tests := []struct {
		name      string
		signal    string
		wantCause checkoutcause.Cause
	}{
		{"connector create failed", "CONNECTOR_CREATE_FAILED", checkoutcause.CauseConnectorCreateFailed},
		{"connector update failed", "CONNECTOR_UPDATE_FAILED", checkoutcause.CauseConnectorUpdateFailed},
		{"freshness guard failed", "FRESHNESS_GUARD_FAILED", checkoutcause.CausePricingStale},
		{"order creation failed", "ORDER_CREATION_FAILED", checkoutcause.CauseOrderCreationFailed},
		{"escalation stalled", checkoutcause.SignalEscalationStalled, checkoutcause.CauseEscalationStalled},
		{"buyer abandoned", checkoutcause.SignalBuyerAbandoned, checkoutcause.CauseBuyerAbandoned},
		{"unrecognized signal", "SOMETHING_NEW", checkoutcause.CauseUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diag := Diagnose(tt.signal, "some_stage", []int64{7})

			// Divergence between the diagnosis cause and the classification
			// table's cause is a bug (task hard rule) — Diagnose must be a
			// thin wrapper over checkoutcause.Classify, never its own
			// parallel judgment.
			wantCause, wantSummary, wantAction := checkoutcause.Classify(tt.signal)
			if diag.Cause != string(wantCause) {
				t.Errorf("Diagnose(%q).Cause = %q, want %q (checkoutcause.Classify)", tt.signal, diag.Cause, wantCause)
			}
			if string(wantCause) != string(tt.wantCause) {
				t.Errorf("test table itself out of sync with checkoutcause: got %q want %q", wantCause, tt.wantCause)
			}
			if diag.PlainLanguageSummary != wantSummary {
				t.Errorf("Diagnose(%q).PlainLanguageSummary diverged from checkoutcause.Classify", tt.signal)
			}
			if diag.RecommendedAction != wantAction {
				t.Errorf("Diagnose(%q).RecommendedAction diverged from checkoutcause.Classify", tt.signal)
			}
			if diag.StageReached != "some_stage" {
				t.Errorf("Diagnose(%q).StageReached = %q, want %q", tt.signal, diag.StageReached, "some_stage")
			}
			if len(diag.Evidence) != 1 || diag.Evidence[0] != 7 {
				t.Errorf("Diagnose(%q).Evidence = %v, want [7]", tt.signal, diag.Evidence)
			}
		})
	}
}

// TestDiagnose_IsIdempotent is R10: recomputing from the same inputs must
// yield an identical Diagnosis.
func TestDiagnose_IsIdempotent(t *testing.T) {
	a := Diagnose("ORDER_CREATION_FAILED", "complete", []int64{3, 5})
	b := Diagnose("ORDER_CREATION_FAILED", "complete", []int64{3, 5})
	if !reflect.DeepEqual(a, b) {
		t.Errorf("Diagnose is not idempotent: %+v != %+v", a, b)
	}
}
