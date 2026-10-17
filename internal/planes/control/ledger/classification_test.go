// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package ledger_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sarvexlab/sarvex/internal/planes/control/ledger"
)

// A2: classification test, table-driven over EVERY failure code enumerated
// in the prerequisite audit (internal/planes/serving/checkout_service.go's
// four SessionError.Code values) plus an unknown code -> unclassified with
// a non-empty, jargon-free recommended_action either way.
func TestClassify_EveryEnumeratedFailureCode(t *testing.T) {
	cases := []struct {
		name        string
		failureCode string
		stage       string
		wantCause   ledger.Cause
	}{
		{"connector_create_failed", "CONNECTOR_CREATE_FAILED", "create", ledger.CauseProfileMismatch},
		{"connector_update_failed", "CONNECTOR_UPDATE_FAILED", "update", ledger.CausePlatformError},
		{"freshness_guard_failed", "FRESHNESS_GUARD_FAILED", "complete_freshness_guard", ledger.CauseSyncFreshness},
		{"order_creation_failed", "ORDER_CREATION_FAILED", "complete", ledger.CausePaymentHandler},
		{"unknown_code", "SOME_FUTURE_CODE_NOT_YET_SEEN", "create", ledger.CauseUnclassified},
	}

	// Jargon this ledger's recommended_action strings must never contain —
	// R5's "no protocol jargon" bar, translated into a mechanical check for
	// the raw code/stage vocabulary this table's own inputs use.
	jargon := []string{"SessionError", "checkout_service.go", "CONNECTOR_CREATE_FAILED", "CONNECTOR_UPDATE_FAILED", "FRESHNESS_GUARD_FAILED", "ORDER_CREATION_FAILED"}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ledger.Classify(tc.failureCode, tc.stage)
			assert.Equal(t, tc.wantCause, got.Cause)
			assert.NotEmpty(t, got.RecommendedAction, "recommended_action must never be empty (R2/R4: an entry is still written)")
			for _, j := range jargon {
				assert.NotContains(t, got.RecommendedAction, j, "recommended_action must be merchant-readable, not protocol jargon (R5)")
			}
		})
	}
}

// A3: buyer_abandoned vs. fixable-cause discrimination, tested explicitly
// across abandonment scenarios (R6) — last stage reached is the
// discriminating signal. Stage values are serving.SessionState's two
// non-terminal, abandonment-reachable values (internal/planes/serving/
// abandonment_sweep.go's stage field on serving.checkout.abandoned) — see
// classification.go's classifyAbandoned doc comment for why
// escalated/awaiting_payment (the checkoutescalation-projection
// vocabulary) aren't reachable from this signal.
func TestClassify_AbandonmentDiscrimination(t *testing.T) {
	cases := []struct {
		stage       string
		wantCause   ledger.Cause
		wantFixable bool // true iff something Sarvex/the merchant can act on
	}{
		{"incomplete", ledger.CauseBuyerAbandoned, false},
		{"ready_for_complete", ledger.CauseTimeout, true},
		{"some_unrecognized_stage", ledger.CauseUnclassified, true},
	}

	for _, tc := range cases {
		t.Run(tc.stage, func(t *testing.T) {
			got := ledger.Classify(ledger.FailureCodeAbandoned, tc.stage)
			assert.Equal(t, tc.wantCause, got.Cause)
			assert.NotEmpty(t, got.RecommendedAction)

			isBuyerAbandoned := got.Cause == ledger.CauseBuyerAbandoned
			assert.Equal(t, !tc.wantFixable, isBuyerAbandoned,
				"buyer_abandoned must mean 'nothing broken'; every other cause must read as something to check")
		})
	}
}

// TestClassify_RecommendedActionsAreImperative is a light sanity check on
// R5's "imperative, what to do" bar — not a full NLP check, just that the
// action strings this table ships aren't simply restating what happened.
func TestClassify_RecommendedActionsAreImperative(t *testing.T) {
	codes := []string{"CONNECTOR_CREATE_FAILED", "CONNECTOR_UPDATE_FAILED", "FRESHNESS_GUARD_FAILED", "ORDER_CREATION_FAILED"}
	for _, code := range codes {
		got := ledger.Classify(code, "create")
		lower := strings.ToLower(got.RecommendedAction)
		startsImperative := strings.HasPrefix(lower, "check") || strings.HasPrefix(lower, "resync") || strings.HasPrefix(lower, "review") || strings.HasPrefix(lower, "no action")
		assert.True(t, startsImperative, "recommended_action for %s should read as an instruction: %q", code, got.RecommendedAction)
	}
}
