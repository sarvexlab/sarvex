// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package ledger is the Lost Revenue Ledger: it consumes failed and
// abandoned checkout events, classifies why each one didn't convert, prices
// the revenue impact from the cart snapshot at the point of failure, keeps
// a per-merchant/per-month/per-currency rollup in lockstep with the raw
// entries, and serves both as a read-only control-plane API. It is
// deliberately narrow — no console, no alerting, no remediation, no
// cross-merchant aggregation (sarvex-v1-architecture.md) — a merchant reads this and
// sees, in order of money, what they lost and why.
package ledger

import "time"

// Cause is the closed classification for why a checkout entry landed in
// this ledger. Distinct from (and a superset of the intent behind)
// internal/pkg/checkoutcause.Cause: that package's enum was built for the
// Checkout Escalation Console's session diagnosis and doesn't distinguish
// "something Sarvex/the merchant can fix" causes as finely as a revenue
// ledger needs to (see classification.go's doc comment for how the two
// relate). Closed by design (R4) — a new failure signal must get a rule in
// classification.go, never fall through silently to anything but
// CauseUnclassified.
type Cause string

const (
	CausePaymentHandler    Cause = "payment_handler"
	CauseProfileMismatch   Cause = "profile_mismatch"
	CauseSyncFreshness     Cause = "sync_freshness"
	CauseEscalationDropoff Cause = "escalation_dropoff"
	CausePlatformError     Cause = "platform_error"
	CauseTimeout           Cause = "timeout"
	CauseBuyerAbandoned    Cause = "buyer_abandoned"
	CauseUnclassified      Cause = "unclassified"
)

// Status is an entry's forward-only triage state (R13's PATCH endpoint).
type Status string

const (
	StatusOpen         Status = "open"
	StatusAcknowledged Status = "acknowledged"
	StatusResolved     Status = "resolved"
)

// nextAllowedStatus is the forward-only transition table (R13/A7): open ->
// acknowledged -> resolved, never backwards, never skipping is disallowed
// (open -> resolved directly is still forward, so it's allowed too — only
// a transition to an earlier stage, or to the same stage, is rejected).
var statusOrder = map[Status]int{StatusOpen: 0, StatusAcknowledged: 1, StatusResolved: 2}

// CanTransition reports whether moving from cur to next is a forward-only
// transition (strictly increasing stage, never a reversal or a no-op).
func CanTransition(cur, next Status) bool {
	curOrder, curOK := statusOrder[cur]
	nextOrder, nextOK := statusOrder[next]
	return curOK && nextOK && nextOrder > curOrder
}

// Entry is one lost-checkout row (R10), stored at
// merchants/{merchant_id}/control_ledger_entries/{entry_id} (a Mongo document
// carrying merchant_id as a field — see mongo_store.go's doc comment for
// why this isn't literally Firestore).
type Entry struct {
	EntryID    string `bson:"_id"`
	MerchantID string `bson:"merchant_id"`

	// EventID is the source event's own event_id (CheckoutFailedEvent /
	// CheckoutAbandonedEvent) — the idempotency key a unique index enforces
	// (R1/A1), independent of EntryID (a fresh ULID per successful insert).
	EventID string `bson:"event_id"`

	OccurredAt time.Time `bson:"occurred_at"`
	CheckoutID string    `bson:"checkout_id"`
	StoreID    string    `bson:"store_id"`
	Surface    string    `bson:"surface,omitempty"`
	Stage      string    `bson:"stage"`

	// FailureCode is the raw serving-plane signal: a checkout_service.go
	// SessionError.Code for a failed entry, or the literal "abandoned" for
	// an abandoned entry (there is no SessionError.Code for a timeout —
	// see classification.go).
	FailureCode string `bson:"failure_code"`
	Cause       Cause  `bson:"cause"`
	// RecommendedAction is merchant-readable and imperative (R5) — never
	// re-derive it at read time, it's computed once, alongside Cause, at
	// classification time.
	RecommendedAction string `bson:"recommended_action"`

	LostRevenueMinor int64  `bson:"lost_revenue_minor"`
	Currency         string `bson:"currency,omitempty"`
	// RevenueUnknown is true when no cart snapshot was available at the
	// point of failure (R8) — LostRevenueMinor is 0 in that case, never an
	// estimate.
	RevenueUnknown bool `bson:"revenue_unknown"`

	Status          Status    `bson:"status"`
	StatusChangedAt time.Time `bson:"status_changed_at"`
	CreatedAt       time.Time `bson:"created_at"`
}

// CauseBreakdown is one cause's contribution to a monthly rollup (R11).
type CauseBreakdown struct {
	Cause            Cause `bson:"cause"`
	Count            int64 `bson:"count"`
	LostRevenueMinor int64 `bson:"lost_revenue_minor"`
}

// Rollup is the monthly, per-currency aggregate at
// merchants/{merchant_id}/control_ledger_rollups/{yyyy_mm}__{currency} (R11). It is
// the only thing the read API sorts or totals from (R12) — never
// recomputed from raw entries at read time.
type Rollup struct {
	RollupID   string `bson:"_id"` // "{yyyy_mm}__{currency}"
	MerchantID string `bson:"merchant_id"`
	Month      string `bson:"month"` // "yyyy_mm"
	Currency   string `bson:"currency"`

	Causes                    []CauseBreakdown `bson:"causes"`
	TotalCount                int64            `bson:"total_count"`
	TotalLostRevenueMinor     int64            `bson:"total_lost_revenue_minor"`
	EntriesWithUnknownRevenue int64            `bson:"entries_with_unknown_revenue"`

	UpdatedAt time.Time `bson:"updated_at"`
}

// ListFilter is the read API's query filter for GET /ledger/entries (R13).
type ListFilter struct {
	Cause    Cause
	Status   Status
	StoreID  string
	Surface  string
	From, To *time.Time
}
