// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package ledger

import "fmt"

// FailureCodeAbandoned is the synthetic failure_code stored on an entry
// built from CheckoutAbandonedEvent — there is no checkout_service.go
// SessionError.Code for a session that simply went quiet, but every entry
// (R10) carries a failure_code field, so this fills it deterministically
// rather than leaving it empty.
const FailureCodeAbandoned = "ABANDONED"

// Classification is one static rule's output: a closed Cause plus a
// merchant-readable, imperative recommended_action (R5) — no protocol
// jargon, no reference to the raw code/stage (those live in Entry's own
// structured fields, not folded into this string).
type Classification struct {
	Cause             Cause
	RecommendedAction string
}

// codeRules classifies the four raw SessionError codes checkout_service.go
// currently emits (internal/planes/serving/checkout_service.go — see the
// audit this package's PR description cites for the file:line evidence).
// Unlike internal/pkg/checkoutcause (built for the Checkout Escalation
// Console's session diagnosis), this table distinguishes causes by who can
// act on them and doesn't need stage to disambiguate any of these four —
// each code means one thing regardless of when it fired:
//
//   - CONNECTOR_CREATE_FAILED: the store rejected the checkout at the
//     create call — the profile the agent was shown (stock, pricing, SKU
//     mapping) didn't match what the store actually had -> profile_mismatch.
//   - CONNECTOR_UPDATE_FAILED: the store rejected a buyer/fulfillment/
//     line-item update — typically a shipping/tax computation failure at
//     the store, a generic store-side technical fault -> platform_error.
//   - FRESHNESS_GUARD_FAILED: Sarvex's own catalog cache was too stale to
//     safely complete against — literally a sync-freshness problem, not a
//     store or buyer problem -> sync_freshness.
//   - ORDER_CREATION_FAILED: the connector's complete call — the one call
//     that submits payment — failed to place the order. Of the four, this
//     is the one that sits on the payment path -> payment_handler.
var codeRules = map[string]Classification{
	"CONNECTOR_CREATE_FAILED": {
		Cause:             CauseProfileMismatch,
		RecommendedAction: "Check that the items in this cart still match your store's current stock and pricing — the checkout was rejected when it was created.",
	},
	"CONNECTOR_UPDATE_FAILED": {
		Cause:             CausePlatformError,
		RecommendedAction: "Check your store's shipping and tax settings for this order's delivery address — updating the checkout failed there.",
	},
	"FRESHNESS_GUARD_FAILED": {
		Cause:             CauseSyncFreshness,
		RecommendedAction: "Resync your store's catalog — your product data was too out of date to safely complete this checkout.",
	},
	"ORDER_CREATION_FAILED": {
		Cause:             CausePaymentHandler,
		RecommendedAction: "Check your payment handler configuration (e.g. card or Google Pay settings) — the order could not be placed when payment was submitted.",
	},
}

// Classify maps a raw signal to a Classification (R3). failureCode is a
// checkout_service.go SessionError.Code, or FailureCodeAbandoned; stage
// only changes the outcome for FailureCodeAbandoned (R6) — every other code
// means the same thing regardless of stage. An unrecognized code always
// produces CauseUnclassified rather than erroring (R2/R4): the ledger must
// never drop an event for a classification miss.
func Classify(failureCode, stage string) Classification {
	if failureCode == FailureCodeAbandoned {
		return classifyAbandoned(stage)
	}
	if c, ok := codeRules[failureCode]; ok {
		return c
	}
	return Classification{
		Cause:             CauseUnclassified,
		RecommendedAction: fmt.Sprintf("Escalate to engineering with this entry's failure_code (%q) — it isn't in the classification table yet.", failureCode),
	}
}

// classifyAbandoned discriminates buyer_abandoned (nothing broken) from a
// fixable abandonment cause (R6), using the last stage the session reached
// before it went quiet: internal/planes/serving/abandonment_sweep.go's
// stage field on serving.checkout.abandoned carries the session's
// serving.SessionState (session.go), whose only two non-terminal,
// abandonment-reachable values are "incomplete" and "ready_for_complete" —
// serving has no "escalated"/"awaiting_payment" state of its own (those are
// internal/pkg/checkoutcause/internal/planes/control/checkoutescalation
// concepts, coarser-grained than serving.SessionState, per that package's
// own doc comment). "A failure preceded the timeout" (R6's other named
// signal) is likewise not a fact serving's session model records for a
// non-terminal session, so this is a deliberate, reported simplification —
// see the PR description's ambiguity notes:
//
//   - incomplete: the buyer never even reached a resolved cart (no
//     successful Update yet) -> buyer_abandoned, nothing to fix.
//   - ready_for_complete: shipping/tax were resolved and the session sat
//     ready to capture, but the buyer never triggered complete — this is
//     the one state a stalled escalation retry would also leave the
//     session in (checkout_service.go's Complete: "session stays
//     ready_for_complete so the agent can retry... after the buyer
//     returns"), so it's the closer-to-actionable of the two reachable
//     states -> timeout (something stalled right before the final call,
//     not necessarily the buyer's choice).
//   - anything else (unexpected stage value): CauseUnclassified rather than
//     guessing. escalation_dropoff remains a defined cause (R4's enum is
//     closed, not append-only) but is not reachable from this signal today
//     — serving's abandonment sweep has no way to see that a session was
//     specifically mid-escalation.
func classifyAbandoned(stage string) Classification {
	switch stage {
	case "incomplete":
		return Classification{
			Cause:             CauseBuyerAbandoned,
			RecommendedAction: "No action needed — the buyer left before submitting payment and nothing in your store caused this.",
		}
	case "ready_for_complete":
		return Classification{
			Cause:             CauseTimeout,
			RecommendedAction: "The checkout was fully set up (shipping and tax resolved) but was never completed. If this recurs often, check whether checkout completion is timing out or stalling for your store.",
		}
	default:
		return Classification{
			Cause:             CauseUnclassified,
			RecommendedAction: fmt.Sprintf("Escalate to engineering with this entry's stage (%q) — an abandoned session reached a stage this table doesn't recognize.", stage),
		}
	}
}
