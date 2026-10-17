// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package checkoutescalation is the backend for the Checkout Escalation
// Console: a read-only projection of checkout sessions built from the
// checkout lifecycle events (internal/planes/serving/checkout_lifecycle_events.go),
// a per-session append-only timeline, and a rule-based diagnosis layer.
//
// This package is a projection, not a second source of truth. Redis
// session state (internal/planes/serving) remains authoritative for the
// serving plane. Nothing here writes to Redis, calls the serving plane, or
// otherwise influences checkout behavior — state transitions are derived
// from consumed events only (plus one time-based sweep for abandonment/
// stalled-escalation detection, which only ever writes to this package's
// own collections). See doc.go for the import-boundary test that enforces
// this.
package checkoutescalation

import "time"

// State is the session-projection's own state enum — coarser than
// serving.SessionState, and including states (escalated, awaiting_payment,
// abandoned) that have no equivalent there because Redis never needed to
// distinguish them.
type State string

const (
	StateInitiated       State = "initiated"
	StateCartActive      State = "cart_active"
	StateEscalated       State = "escalated"
	StateAwaitingPayment State = "awaiting_payment"
	StateCompleted       State = "completed"
	StateFailed          State = "failed"
	StateAbandoned       State = "abandoned"
)

// Terminal reports whether s is one of the three states a session never
// transitions out of. The terminal-state guard (projection.go) uses this
// to refuse to let a late/out-of-order non-terminal event regress a
// terminal session (R3).
func (s State) Terminal() bool {
	return s == StateCompleted || s == StateFailed || s == StateAbandoned
}

// CartSummary mirrors serving.CartSummary — duplicated rather than
// imported so this control-plane package has no import path to the
// serving plane at all (see doc.go).
type CartSummary struct {
	Currency   string `bson:"currency"`
	TotalMinor int64  `bson:"total_minor"`
	ItemCount  int    `bson:"item_count"`
}

// Escalation is the session's escalation sub-document, present once the
// session has been escalated at least once.
type Escalation struct {
	Reason            string     `bson:"reason"`
	EscalatedAt       time.Time  `bson:"escalated_at"`
	ContinueURLIssued bool       `bson:"continue_url_issued"`
	ReturnedAt        *time.Time `bson:"returned_at,omitempty"`
	Resolved          bool       `bson:"resolved"`
}

// Failure is the session's failure sub-document, present once the session
// has reached state=failed.
type Failure struct {
	Code  string `bson:"code"`
	Cause string `bson:"cause"`
	Stage string `bson:"stage"`
}

// Diagnosis is computed once (R10, diagnosis.go) and stored on the session
// document — never recomputed at read time.
type Diagnosis struct {
	StageReached         string  `bson:"stage_reached"`
	Cause                string  `bson:"cause"`
	PlainLanguageSummary string  `bson:"plain_language_summary"`
	RecommendedAction    string  `bson:"recommended_action"`
	Evidence             []int64 `bson:"evidence"` // timeline seq values supporting the conclusion
}

// Session is the projection's parent document, one per checkout
// (_id = checkout_id).
type Session struct {
	CheckoutID     string      `bson:"_id"`
	MerchantID     string      `bson:"merchant_id"`
	StoreID        string      `bson:"store_id"`
	Surface        string      `bson:"surface,omitempty"`
	AgentPlatform  string      `bson:"agent_platform,omitempty"`
	State          State       `bson:"state"`
	CurrentStage   string      `bson:"current_stage"`
	StartedAt      time.Time   `bson:"started_at"`
	LastActivityAt time.Time   `bson:"last_activity_at"`
	TerminalAt     *time.Time  `bson:"terminal_at,omitempty"`
	Cart           CartSummary `bson:"cart_summary"`
	Escalation     *Escalation `bson:"escalation,omitempty"`
	Failure        *Failure    `bson:"failure,omitempty"`
	Diagnosis      *Diagnosis  `bson:"diagnosis,omitempty"`
	EventCount     int64       `bson:"event_count"`
	UpdatedAt      time.Time   `bson:"updated_at"`
	// ExpiresAt is set once the session goes terminal (terminal_at +
	// retention) and drives the TTL index (mongo_sessions.go). Absent
	// while the session is still active — it never expires until it
	// terminates.
	ExpiresAt *time.Time `bson:"expires_at,omitempty"`
}

// TimelineEntry is one row of a session's append-only, replay-only
// timeline (R5/R7 — reconstruct and display, never re-execute).
type TimelineEntry struct {
	CheckoutID string         `bson:"checkout_id"`
	Seq        int64          `bson:"seq"`
	EventID    string         `bson:"event_id"` // dedup key (R3); see projection.go
	OccurredAt time.Time      `bson:"occurred_at"`
	EventType  string         `bson:"event_type"`
	Stage      string         `bson:"stage"`
	Payload    map[string]any `bson:"payload"` // redacted at write time (R6) — see redact.go
	ExpiresAt  *time.Time     `bson:"expires_at,omitempty"`
}
