// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v20260408

import "testing"

func TestCheckoutStatusKnown(t *testing.T) {
	if !CheckoutStatusRequiresEscalation.Known() {
		t.Error("CheckoutStatusRequiresEscalation.Known() = false, want true")
	}
	if CheckoutStatus("bogus").Known() {
		t.Error(`CheckoutStatus("bogus").Known() = true, want false`)
	}
}

func TestMessageSeverityRequiresEscalation(t *testing.T) {
	cases := map[MessageSeverity]bool{
		MessageSeverityRecoverable:         false,
		MessageSeverityRequiresBuyerInput:  true,
		MessageSeverityRequiresBuyerReview: true,
		MessageSeverityUnrecoverable:       false,
	}
	for sev, want := range cases {
		if got := sev.RequiresEscalation(); got != want {
			t.Errorf("%s.RequiresEscalation() = %v, want %v", sev, got, want)
		}
	}
}

func TestErrorCodeUnknownIsNotRejected(t *testing.T) {
	// Freeform codes are permitted by the spec; Known() just reports
	// whether the code has standardized semantics.
	c := ErrorCode("com.example.custom_error")
	if c.Known() {
		t.Error("Known() = true for a freeform code, want false")
	}
}

func TestTotalTypeKnown(t *testing.T) {
	if !TotalTypeSubtotal.Known() {
		t.Error("TotalTypeSubtotal.Known() = false, want true")
	}
	if TotalType("custom_line").Known() {
		t.Error(`TotalType("custom_line").Known() = true, want false`)
	}
}
