// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package checkoutcause is the single shared classification taxonomy for
// why a checkout session failed, stalled, or was abandoned. It exists so
// the Checkout Escalation Console's diagnosis layer — and any future
// consumer that needs the same vocabulary — share one rule table instead
// of each inventing its own classification independently.
//
// This is deliberately narrow: a closed enum plus a lookup function, no
// aggregation, no revenue totals, no reporting, no persistence of its own.
// sarvex-v1-architecture.md "analytics as a product" out of scope for V1; a
// merchant-facing revenue-loss ledger would cross that line, this package
// stops well short of it.
package checkoutcause

import "fmt"

// Cause is a closed classification for why a checkout session didn't
// reach completed.
type Cause string

const (
	// CauseConnectorCreateFailed — the store rejected checkout creation
	// (stock, pricing, or a store-side error at the create call).
	CauseConnectorCreateFailed Cause = "connector_create_failed"
	// CauseConnectorUpdateFailed — the store rejected a cart/buyer/
	// fulfillment update (shipping/tax resolution failed at the store).
	CauseConnectorUpdateFailed Cause = "connector_update_failed"
	// CausePricingStale — the freshness guard refused to complete against
	// a stale catalog cache.
	CausePricingStale Cause = "pricing_stale"
	// CauseOrderCreationFailed — the connector's complete call failed to
	// place the order at the merchant (payment declined, store error).
	CauseOrderCreationFailed Cause = "order_creation_failed"
	// CauseEscalationStalled — the buyer was handed off via continue_url
	// and never returned within the threshold. Distinct from
	// CauseBuyerAbandoned: here the buyer was actively redirected
	// somewhere and the merchant knows exactly where.
	CauseEscalationStalled Cause = "escalation_stalled"
	// CauseBuyerAbandoned — no terminal event and no activity for longer
	// than the abandonment threshold, with no escalation in play.
	CauseBuyerAbandoned Cause = "buyer_abandoned"
	// CauseUnknown — no rule matched the observed signal.
	CauseUnknown Cause = "unknown"
)

// Time-derived signals — not a checkout_service.go SessionError.Code,
// produced by the session-projection worker's periodic sweep rather than
// by an event on the bus. Exported so callers don't have to guess the
// string.
const (
	SignalEscalationStalled = "escalation_stalled"
	SignalBuyerAbandoned    = "buyer_abandoned"
)

// rule is one entry in the classification table.
type rule struct {
	cause             Cause
	summary           string
	recommendedAction string
}

// rules maps a raw signal — a checkout_service.go SessionError.Code, or one
// of the two time-derived signals above — to a Cause, a plain-language
// summary, and a recommended action. Keys here must track
// checkout_service.go's error codes exactly; if that file adds a new
// failure code, add its rule here, don't let it fall through to Unknown.
var rules = map[string]rule{
	"CONNECTOR_CREATE_FAILED": {
		cause:             CauseConnectorCreateFailed,
		summary:           "The store rejected the checkout at creation — typically stock or pricing was no longer valid by the time the agent submitted the cart.",
		recommendedAction: "Check the store's stock/pricing for the requested items around the time of this session.",
	},
	"CONNECTOR_UPDATE_FAILED": {
		cause:             CauseConnectorUpdateFailed,
		summary:           "The store rejected an update to the buyer, fulfillment address, or line items — often a shipping or tax calculation failure at the store.",
		recommendedAction: "Check the store's shipping/tax configuration for the fulfillment address used in this session.",
	},
	"FRESHNESS_GUARD_FAILED": {
		cause:             CausePricingStale,
		summary:           "Sarvex's catalog cache was too stale to safely complete the checkout against, so completion was refused rather than risk charging the wrong price.",
		recommendedAction: "Check the store's catalog sync health — a stale cache points at a sync-plane lag, not the buyer or the store rejecting the order.",
	},
	"ORDER_CREATION_FAILED": {
		cause:             CauseOrderCreationFailed,
		summary:           "The store failed to place the order when payment was submitted — commonly a declined payment or a store-side error at order creation.",
		recommendedAction: "Check the store's order log and payment gateway for this checkout session's timestamp.",
	},
	SignalEscalationStalled: {
		cause:             CauseEscalationStalled,
		summary:           "The buyer was handed off to the merchant site to complete an out-of-band action (e.g. 3DS) and never returned.",
		recommendedAction: "Nothing to fix at the store — this is a buyer drop-off after handoff. Consider whether the continue_url destination is a smooth, fast experience.",
	},
	SignalBuyerAbandoned: {
		cause:             CauseBuyerAbandoned,
		summary:           "The session went quiet with no further activity and never reached a terminal state.",
		recommendedAction: "No store-side issue detected. Typical of buyer drop-off mid-checkout; no action needed unless this session's stage recurs across many sessions.",
	},
}

// Classify maps a raw signal to a Cause, a plain-language summary, and a
// recommended action. An unrecognized signal maps to CauseUnknown rather
// than erroring — the diagnosis layer must always be able to produce a
// diagnosis, even for a code this table hasn't seen yet.
func Classify(signal string) (cause Cause, summary string, recommendedAction string) {
	r, ok := rules[signal]
	if !ok {
		return CauseUnknown,
			fmt.Sprintf("Checkout failed for a reason not yet in the classification table (%q).", signal),
			"Escalate to engineering — an unclassified failure signal was observed; add a rule for it."
	}
	return r.cause, r.summary, r.recommendedAction
}
