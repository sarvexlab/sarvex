// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package channel

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound mirrors ledger.ErrNotFound's role for this package.
var ErrNotFound = errors.New("channel: not found")

// SessionEvent is one lifecycle-event fold applied to a Session by
// Store.FoldSessionEvent — the unit of idempotent, transactional work the
// Consumer performs per inbound bus message (A1).
type SessionEvent struct {
	// EventID dedups this fold against redelivery (A1) — the lifecycle
	// event's own event_id, or (for order_intent, which carries no
	// event_id) a value derived from its OrderID.
	EventID string

	SessionID  string
	MerchantID string
	StoreID    string
	Surface    Surface

	// AgentPlatform, StartedAt are only meaningful (and only applied) on
	// the initiating fold — see mongo_store.go.
	AgentPlatform string
	OccurredAt    time.Time

	// Kind selects which fields this fold touches; see SessionEventKind.
	Kind SessionEventKind

	// RevenueMinor/Currency are only set (and only applied) for
	// SessionEventCompleted.
	RevenueMinor int64
	Currency     string
}

// SessionEventKind is the closed set of fold operations a lifecycle event
// can apply to a Session.
type SessionEventKind string

const (
	// SessionEventInitiated creates the session (if absent) — the
	// session's true beginning.
	SessionEventInitiated SessionEventKind = "initiated"
	// SessionEventCartActivity marks ReachedCart and increments
	// RequestCount.
	SessionEventCartActivity SessionEventKind = "cart_activity"
	// SessionEventCheckoutReached marks ReachedCheckout (payment
	// submitted) and increments RequestCount.
	SessionEventCheckoutReached SessionEventKind = "checkout_reached"
	// SessionEventEscalated increments RequestCount only — no funnel
	// stage change (escalation already implies checkout was reached).
	SessionEventEscalated SessionEventKind = "escalated"
	// SessionEventFailed closes the session (EndedAt) without Completed.
	SessionEventFailed SessionEventKind = "failed"
	// SessionEventAbandoned closes the session (EndedAt) without
	// Completed — the serving-plane's own 30-minute-inactivity sweep
	// already produced this signal (R3's "inactivity timeout" close
	// condition is satisfied upstream; this package does not run its own
	// timer, see consumer.go's doc comment).
	SessionEventAbandoned SessionEventKind = "abandoned"
	// SessionEventCompleted closes the session (EndedAt), sets Completed
	// and RevenueMinor/Currency.
	SessionEventCompleted SessionEventKind = "completed"
)

// Store is the Channel Dashboard's persistence contract: sessions and the
// daily funnel/revenue rollup, folded atomically per event (mirrors
// ledger.Store's InsertEntryAndRollup — R6's reconciliation guarantee
// depends on both documents changing in the same transaction). The read
// API only ever calls the ListDailyAggregates/GetDailyAggregate methods —
// R14 (never scan raw sessions at read time).
type Store interface {
	// FoldSessionEvent applies ev to its session (creating it on
	// SessionEventInitiated, or on any other kind if this is the first
	// event this store has seen for that session_id — in which case the
	// session is created with Attribution=inferred, R2) and folds the
	// resulting funnel/revenue delta into the matching DailyAggregate, in
	// one transaction. A redelivered ev.EventID is a no-op (false,nil) —
	// A1's duplicate-safety contract.
	FoldSessionEvent(ctx context.Context, ev SessionEvent) (applied bool, err error)

	// RecordDiscoveryRequest increments DailyAggregate.DiscoveryRequests
	// for storeID/surface/date, deduped by eventID against redelivery.
	// Not part of the session transaction (R2's separate request-level
	// fallback metric).
	RecordDiscoveryRequest(ctx context.Context, eventID, merchantID, storeID string, surface Surface, occurredAt time.Time) error

	GetDailyAggregate(ctx context.Context, merchantID, storeID string, surface Surface, date string) (*DailyAggregate, error)
	// ListDailyAggregates returns every DailyAggregate for merchantID in
	// [from,to] (inclusive, yyyy_mm_dd). surface == "" lists every
	// surface; a non-empty surface filters to it.
	ListDailyAggregates(ctx context.Context, merchantID string, storeID string, surface Surface, from, to string) ([]DailyAggregate, error)

	// GetSession is exposed only for tests (A1/A2's assertions on session
	// shape) — never called by the read API (R14).
	GetSession(ctx context.Context, sessionID string) (*Session, error)
}
