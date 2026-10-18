// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package v20260825

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

func TestFulfillmentDestinationTypeKnown(t *testing.T) {
	if !FulfillmentDestinationTypeShippingAddress.Known() {
		t.Error("FulfillmentDestinationTypeShippingAddress.Known() = false, want true")
	}
	if !FulfillmentDestinationTypeBusinessLocation.Known() {
		t.Error("FulfillmentDestinationTypeBusinessLocation.Known() = false, want true")
	}
	if FulfillmentDestinationType("bogus").Known() {
		t.Error(`FulfillmentDestinationType("bogus").Known() = true, want false`)
	}
}

func TestAvailabilityStatusKnown(t *testing.T) {
	if !AvailabilityStatusInStock.Known() {
		t.Error("AvailabilityStatusInStock.Known() = false, want true")
	}
	if AvailabilityStatus("bogus").Known() {
		t.Error(`AvailabilityStatus("bogus").Known() = true, want false`)
	}
}
