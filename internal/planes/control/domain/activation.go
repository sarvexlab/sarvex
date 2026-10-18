// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package domain

import "time"

// UCPState is where a store sits in its own UCP activation lifecycle: the
// property of the store's *own* endpoints being served, declared, signed,
// verified, and monitored.
//
// Nothing in this lifecycle references feeds or any agent surface. A store becoming agent-transactable is a fact
// about its endpoints; being distributed through a particular surface is a
// separate, optional, repeatable enrollment (see SurfaceState). The two are
// deliberately independent — the only dependency runs one way, and only at
// the surface's end: enrollment cannot progress past `available` unless the
// store is UCPStateActive.
type UCPState string

const (
	// UCPStateDraft — the store record exists; nothing is connected yet.
	UCPStateDraft UCPState = "draft"
	// UCPStateConnectorLinked — a commerce connector is paired and the
	// catalog is syncing.
	UCPStateConnectorLinked UCPState = "connector_linked"
	// UCPStateProfilePublished — /.well-known/ucp serves from the merchant
	// domain with services.endpoint pointing at Sarvex, capabilities
	// declared, and signing keys provisioned.
	UCPStateProfilePublished UCPState = "profile_published"
	// UCPStateVerified — the conformance suite passes against the store's
	// own endpoints (see activation.Verifier).
	UCPStateVerified UCPState = "verified"
	// UCPStateActive — endpoints live and monitored; the store is
	// agent-transactable. Terminal success state, and independently
	// meaningful: a store may sit here with zero surfaces enrolled
	// indefinitely and that is a supported, complete condition.
	UCPStateActive UCPState = "active"
	// UCPStateSuspended — conformance regressed or health has been critical
	// for a sustained period. Store-level and independent of any surface.
	UCPStateSuspended UCPState = "suspended"
)

// ActivationStep is one stage of the activation lifecycle rendered for the
// console: what it is, whether the store has cleared it, and — when it
// hasn't — why not.
type ActivationStep struct {
	State UCPState `json:"state"`
	// Reached is true once the store has occupied this state at least once.
	Reached   bool       `json:"reached"`
	ReachedAt *time.Time `json:"reached_at,omitempty"`
	// Blocking, when non-empty, explains what is preventing entry. Only
	// populated for the step the store is currently working toward.
	Blocking string `json:"blocking,omitempty"`
}

// ActivationHistoryEntry records a state the store occupied and when it was
// entered. Append-only; entries are never edited or removed.
type ActivationHistoryEntry struct {
	State     UCPState  `json:"state"`
	EnteredAt time.Time `json:"entered_at"`
}

// VerificationCheck is one conformance check's outcome.
type VerificationCheck struct {
	CheckID string `json:"check_id"`
	Passed  bool   `json:"passed"`
	Detail  string `json:"detail,omitempty"`
}

// VerificationReport is what a verification run found and where it left the
// store. Returned by POST .../activation/verify, and kept on the activation
// record as LastVerification: verification runs as part of pre-validation
// (validation.Orchestrator.RunValidation), so the console needs to read the
// checks back from the record rather than only from the response of a call
// it didn't make.
type VerificationReport struct {
	Passed bool                `json:"passed"`
	State  UCPState            `json:"ucp_state"`
	Checks []VerificationCheck `json:"checks"`
	RanAt  string              `json:"ran_at"`
}

// Activation is a store's full activation position, as served by
// GET /stores/{store_id}/activation.
type Activation struct {
	StoreID      StoreID  `json:"store_id"`
	UCPState     UCPState `json:"ucp_state"`
	Capabilities []string `json:"capabilities"`
	ProfileURL   string   `json:"profile_url,omitempty"`
	// SigningKeyID references the store's active keypair in jwkmanagement —
	// the reference only. No key material crosses this boundary.
	SigningKeyID    string     `json:"signing_key_id,omitempty"`
	VerifiedAt      *time.Time `json:"verified_at,omitempty"`
	SuspendedReason string     `json:"suspension_reason,omitempty"`
	EnteredStateAt  time.Time  `json:"entered_state_at"`
	// LastVerification is the most recent verification run's result, absent
	// until the store has been verified once.
	LastVerification *VerificationReport      `json:"last_verification,omitempty"`
	Steps            []ActivationStep         `json:"steps"`
	History          []ActivationHistoryEntry `json:"history"`
}
