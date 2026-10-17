// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package checkoutcause

import "testing"

func TestClassify_KnownSignals(t *testing.T) {
	tests := []struct {
		signal string
		want   Cause
	}{
		{"CONNECTOR_CREATE_FAILED", CauseConnectorCreateFailed},
		{"CONNECTOR_UPDATE_FAILED", CauseConnectorUpdateFailed},
		{"FRESHNESS_GUARD_FAILED", CausePricingStale},
		{"ORDER_CREATION_FAILED", CauseOrderCreationFailed},
		{SignalEscalationStalled, CauseEscalationStalled},
		{SignalBuyerAbandoned, CauseBuyerAbandoned},
	}
	for _, tt := range tests {
		t.Run(tt.signal, func(t *testing.T) {
			cause, summary, action := Classify(tt.signal)
			if cause != tt.want {
				t.Errorf("Classify(%q) cause = %q, want %q", tt.signal, cause, tt.want)
			}
			if summary == "" {
				t.Errorf("Classify(%q) returned empty summary", tt.signal)
			}
			if action == "" {
				t.Errorf("Classify(%q) returned empty recommendedAction", tt.signal)
			}
		})
	}
}

func TestClassify_UnknownSignalDoesNotPanic(t *testing.T) {
	cause, summary, action := Classify("SOMETHING_NEVER_SEEN_BEFORE")
	if cause != CauseUnknown {
		t.Errorf("Classify(unknown) cause = %q, want %q", cause, CauseUnknown)
	}
	if summary == "" || action == "" {
		t.Errorf("Classify(unknown) must still return a usable summary/action, got summary=%q action=%q", summary, action)
	}
}

func TestClassify_EmptySignal(t *testing.T) {
	cause, _, _ := Classify("")
	if cause != CauseUnknown {
		t.Errorf("Classify(\"\") cause = %q, want %q", cause, CauseUnknown)
	}
}
