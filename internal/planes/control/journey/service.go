// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package journey owns the per-merchant onboarding state machine.
// Only this package may read or advance journey state; nothing else infers it.
package journey

import (
	"context"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// Signal is an event-driven state transition trigger.
type Signal string

const (
	SignalConnectorCreated       Signal = "connector_created"        // Prospect → Connected
	SignalFirstSyncComplete      Signal = "first_sync_complete"      // Connected → Synced
	SignalReadinessThresholdMet  Signal = "readiness_threshold_met"  // Synced → Ready
	SignalReadinessThresholdLost Signal = "readiness_threshold_lost" // Ready → Synced
	// SignalStoreActivated fires when activation reports the store's own
	// UCP profile active (activation.UCPStateActive) — see
	// journey.Subscriber's activation subscription.
	SignalStoreActivated        Signal = "store_activated"        // Ready → Live
	SignalConnectorDisconnected Signal = "connector_disconnected" // any state → Prospect
)

// Service is the single authority on engagement state per store.
// Per sarvex-v1-architecture.md A10.4: engagement/validation data is per-store.
type Service interface {
	// Get returns the store's current journey state. OpenTaskCounts and
	// NextAction are computed from the task service; zero values are returned
	// if the task service is not yet implemented.
	Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Journey, error)

	// Initialize creates the journey record at store-creation time, setting
	// state to "connected". Safe to call more than once (idempotent).
	Initialize(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) error

	// HandleSignal processes an event-driven signal. If the (current state,
	// signal) pair maps to a valid transition, the state is advanced and
	// entered_state_at is reset. Invalid or already-transitioned signals are
	// silently ignored — callers must not treat them as errors.
	HandleSignal(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, sig Signal) error
}
