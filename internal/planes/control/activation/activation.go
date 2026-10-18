// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package activation owns the store UCP activation lifecycle: the states a
// store passes through as its *own* endpoints become agent-transactable.
//
// The whole of what this package knows about is the store: its connector,
// its profile, its signing keys, its conformance result, its health. It has
// no knowledge of feeds or any agent
// surface, and it must not acquire any — surface_isolation_test.go asserts
// that mechanically, by scanning this package's own source.
//
// Being live on a particular agent surface is a separate lifecycle
// (internal/planes/control/surface). The dependency between the two runs
// one way and lives entirely on the surface side: enrollment cannot
// progress unless the store is active. Nothing a surface does — enrolling,
// failing, being suspended, being withdrawn — reaches back into this
// package.
//
// This package replaces the Google-shaped half of the older journey state
// machine (internal/planes/control/journey), which conflated "our endpoints
// work" with "Google approved us". The journey machine remains as the
// operator-facing concierge funnel; activation is the product truth.
package activation

import (
	"fmt"
	"slices"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// Signal is a state-transition trigger. Each one names evidence that
// arrived — a connector paired, a profile served, a conformance run passed
// — never an intention.
type Signal string

const (
	// SignalConnectorLinked — a commerce connector completed pairing and
	// the catalog is syncing.
	SignalConnectorLinked Signal = "connector_linked"
	// SignalConnectorUnlinked — the merchant unlinked the commerce
	// connector. Regresses to draft from anywhere: with no backend there
	// is nothing to serve.
	SignalConnectorUnlinked Signal = "connector_unlinked"
	// SignalProfilePublished — /.well-known/ucp is serving from the
	// merchant domain with keys provisioned and capabilities declared.
	SignalProfilePublished Signal = "profile_published"
	// SignalProfileWithdrawn — the profile stopped serving. Falls back to
	// connector_linked; the connector is still fine.
	SignalProfileWithdrawn Signal = "profile_withdrawn"
	// SignalVerificationPassed — the conformance suite passed against the
	// store's own endpoints.
	SignalVerificationPassed Signal = "verification_passed"
	// SignalVerificationFailed — conformance regressed. From verified this
	// falls back to profile_published; from active it suspends, because an
	// active store is one agents are transacting against.
	SignalVerificationFailed Signal = "verification_failed"
	// SignalActivated — monitoring is running against verified endpoints.
	SignalActivated Signal = "activated"
	// SignalHealthCritical — health has been critical for a sustained
	// period. Suspends.
	SignalHealthCritical Signal = "health_critical"
	// SignalResumed — a suspended store cleared conformance again.
	SignalResumed Signal = "resumed"
)

// transition is one legal move.
type transition struct {
	from   domain.UCPState
	signal Signal
	to     domain.UCPState
}

// transitions is the complete activation state machine. Every legal move is
// here and nothing else is legal: unlike the journey machine this replaces,
// an unknown (state, signal) pair is an error, not a silent no-op. Skipping
// a state is impossible because no row skips one.
//
// Read down the happy path first — draft → connector_linked →
// profile_published → verified → active — then the regressions, then
// suspension. There is no row anywhere in this table that mentions a
// surface, and there cannot be: see surface_isolation_test.go.
var transitions = []transition{
	// ── Forward ──────────────────────────────────────────────────────────
	{domain.UCPStateDraft, SignalConnectorLinked, domain.UCPStateConnectorLinked},
	{domain.UCPStateConnectorLinked, SignalProfilePublished, domain.UCPStateProfilePublished},
	{domain.UCPStateProfilePublished, SignalVerificationPassed, domain.UCPStateVerified},
	{domain.UCPStateVerified, SignalActivated, domain.UCPStateActive},

	// ── Regressions ──────────────────────────────────────────────────────
	// A failed verification from verified means the endpoints changed
	// under us; the profile is still published, so that is where it lands.
	{domain.UCPStateVerified, SignalVerificationFailed, domain.UCPStateProfilePublished},
	{domain.UCPStateProfilePublished, SignalProfileWithdrawn, domain.UCPStateConnectorLinked},
	{domain.UCPStateVerified, SignalProfileWithdrawn, domain.UCPStateConnectorLinked},
	{domain.UCPStateActive, SignalProfileWithdrawn, domain.UCPStateConnectorLinked},

	// Unlinking the commerce connector removes the backend entirely, so
	// every state from connector_linked onward regresses to draft — the
	// same rule the journey machine applies for connector_disconnected.
	{domain.UCPStateConnectorLinked, SignalConnectorUnlinked, domain.UCPStateDraft},
	{domain.UCPStateProfilePublished, SignalConnectorUnlinked, domain.UCPStateDraft},
	{domain.UCPStateVerified, SignalConnectorUnlinked, domain.UCPStateDraft},
	{domain.UCPStateActive, SignalConnectorUnlinked, domain.UCPStateDraft},
	{domain.UCPStateSuspended, SignalConnectorUnlinked, domain.UCPStateDraft},

	// ── Suspension ───────────────────────────────────────────────────────
	// Reachable from verified and active only (R4): a store that never got
	// there has nothing to suspend.
	{domain.UCPStateVerified, SignalHealthCritical, domain.UCPStateSuspended},
	{domain.UCPStateActive, SignalHealthCritical, domain.UCPStateSuspended},
	{domain.UCPStateActive, SignalVerificationFailed, domain.UCPStateSuspended},
	{domain.UCPStateSuspended, SignalResumed, domain.UCPStateActive},
	{domain.UCPStateSuspended, SignalVerificationPassed, domain.UCPStateActive},
}

// order is the activation lifecycle as progression, used to render per-step
// detail and to decide which steps a store has already cleared. Kept
// separate from transitions, which describes legal moves rather than
// progress (it also carries the regressions). Suspended is deliberately
// absent: it is a condition, not a step on the way anywhere.
var order = []domain.UCPState{
	domain.UCPStateDraft,
	domain.UCPStateConnectorLinked,
	domain.UCPStateProfilePublished,
	domain.UCPStateVerified,
	domain.UCPStateActive,
}

// Order returns the activation lifecycle in progression order.
func Order() []domain.UCPState { return slices.Clone(order) }

// ErrInvalidTransition is returned when a signal does not name a legal move
// from the store's current state. Callers that expect duplicate signals
// (event consumers, converge-to-state passes) should check for it with
// errors.Is and treat it as a no-op rather than a failure.
type ErrInvalidTransition struct {
	From   domain.UCPState
	Signal Signal
}

func (e ErrInvalidTransition) Error() string {
	return fmt.Sprintf("activation: no transition from %q on signal %q", e.From, e.Signal)
}

// Next resolves the state a signal moves a store to. The second return is
// false when the pair names no legal move.
func Next(from domain.UCPState, sig Signal) (domain.UCPState, bool) {
	for _, t := range transitions {
		if t.from == from && t.signal == sig {
			return t.to, true
		}
	}
	return "", false
}

// IsTerminalSuccess reports whether a state is the lifecycle's completed
// condition. active is complete on its own: a store may sit here with zero
// surfaces enrolled indefinitely and nothing about that is degraded or
// unfinished (R3).
func IsTerminalSuccess(s domain.UCPState) bool { return s == domain.UCPStateActive }

// AllStates returns every activation state, progression order first and
// suspended last. Used by contract tests and the console's step renderer.
func AllStates() []domain.UCPState {
	return append(Order(), domain.UCPStateSuspended)
}

// AllSignals returns every signal the machine accepts.
func AllSignals() []Signal {
	return []Signal{
		SignalConnectorLinked,
		SignalConnectorUnlinked,
		SignalProfilePublished,
		SignalProfileWithdrawn,
		SignalVerificationPassed,
		SignalVerificationFailed,
		SignalActivated,
		SignalHealthCritical,
		SignalResumed,
	}
}

// blockingReason explains, in merchant-readable terms, what the store needs
// before it can leave its current state. Empty once the store is active.
//
// The strings here are i18n keys, not display copy — the console owns
// wording. They name store facts only.
func blockingReason(state domain.UCPState) string {
	switch state {
	case domain.UCPStateDraft:
		return "activation.blocking.connector"
	case domain.UCPStateConnectorLinked:
		return "activation.blocking.profile"
	case domain.UCPStateProfilePublished:
		return "activation.blocking.verification"
	case domain.UCPStateVerified:
		return "activation.blocking.monitoring"
	case domain.UCPStateSuspended:
		return "activation.blocking.suspended"
	default:
		return ""
	}
}
