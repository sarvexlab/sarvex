// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package activation

import (
	"context"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// Service is the single authority on a store's activation state. Nothing
// else may write it, and nothing else may infer it by joining other
// records.
type Service interface {
	// Get returns the store's activation record, including per-step detail
	// and the blocking reason for the step it is working toward.
	Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Activation, error)

	// Initialize creates the record at store-creation time in draft. Safe
	// to call more than once (idempotent).
	Initialize(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) error

	// HandleSignal applies a transition. A signal that names no legal move
	// from the current state returns ErrInvalidTransition and changes
	// nothing — callers driven by events (where duplicates are the normal
	// case) should treat that error as a no-op.
	HandleSignal(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, sig Signal) error

	// Verify runs the conformance suite against the store's own endpoints
	// and applies the resulting signal. Returns the state the store ended
	// in and the individual check results.
	Verify(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (VerificationReport, error)
}

// CheckResult is one conformance check's outcome.
type CheckResult struct {
	CheckID string `json:"check_id"`
	Passed  bool   `json:"passed"`
	Detail  string `json:"detail,omitempty"`
}

// VerificationReport is what a verification run produced and where it left
// the store.
type VerificationReport struct {
	Passed bool            `json:"passed"`
	State  domain.UCPState `json:"ucp_state"`
	Checks []CheckResult   `json:"checks"`
	RanAt  string          `json:"ran_at"`
}

// Verifier runs the conformance suite against one store's own serving-plane
// endpoints and reports per-check results.
//
// Deliberately narrow, and deliberately store-only: the implementation
// (endpointhealth's probe runner) already covers exactly the four checks R1
// names — profile reachable, signature roundtrip, negotiation handshake,
// catalog lookup — against Sarvex's own endpoints, with no external party
// involved. That is what makes `verified` reachable without any surface.
type Verifier interface {
	Verify(ctx context.Context, storeID domain.StoreID, platform domain.Platform) ([]CheckResult, error)
}

// StoreReader is the narrow read the service needs about the store itself:
// its platform, for addressing the verifier at the right endpoints.
// Shaped as its own small interface rather than taking store.Service so
// this package doesn't depend on the whole store surface.
type StoreReader interface {
	Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Store, error)
}
