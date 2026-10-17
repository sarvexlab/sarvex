// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package surface

import (
	"fmt"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// Event is an enrollment-transition trigger.
type Event string

const (
	// EventEnroll — the merchant asked to enroll (or an auto-enroll
	// surface fired). Moves off available.
	EventEnroll Event = "enroll"
	// EventPrerequisitesMet — every prerequisite check passes.
	EventPrerequisitesMet Event = "prerequisites_met"
	// EventPrerequisitesFailed — a check that was passing now fails.
	EventPrerequisitesFailed Event = "prerequisites_failed"
	// EventSubmitted — the merchant completed the surface's external steps.
	EventSubmitted Event = "submitted"
	// EventVerifying — Sarvex started watching for the verification signal.
	EventVerifying Event = "verifying"
	// EventVerified — the verification signal arrived.
	EventVerified Event = "verified"
	// EventSuspended — the surface suspended or withdrew the store. Note
	// what this does *not* do: it never touches activation state, and it
	// never touches another surface (R9).
	EventSuspended Event = "suspended"
	// EventResumed — the surface reinstated the store.
	EventResumed Event = "resumed"
	// EventWithdrawn — the merchant withdrew enrollment. Returns to
	// available; activation is untouched.
	EventWithdrawn Event = "withdrawn"
)

type enrollmentTransition struct {
	from  domain.SurfaceState
	event Event
	to    domain.SurfaceState
}

// enrollmentTransitions is the complete per-store, per-surface state
// machine (R8). As with activation, an unmatched pair is an error rather
// than a silent no-op.
//
// Nothing here reads another surface's state or the store's activation
// state: the active-store gate is enforced once, at the service boundary
// (see Service.transition), because it is a precondition on *starting*
// rather than a state in this machine.
var enrollmentTransitions = []enrollmentTransition{
	{domain.SurfaceAvailable, EventEnroll, domain.SurfacePrerequisitesPending},

	{domain.SurfacePrerequisitesPending, EventPrerequisitesMet, domain.SurfaceConfigured},
	{domain.SurfaceConfigured, EventPrerequisitesFailed, domain.SurfacePrerequisitesPending},

	{domain.SurfaceConfigured, EventSubmitted, domain.SurfaceSubmitted},
	{domain.SurfaceSubmitted, EventVerifying, domain.SurfaceVerifying},
	// A surface with no external steps goes straight from configured to
	// verifying — there is nothing for the merchant to do in between.
	{domain.SurfaceConfigured, EventVerifying, domain.SurfaceVerifying},
	{domain.SurfaceVerifying, EventVerified, domain.SurfaceEnrolled},
	// An auto-enroll surface (direct_agent) has no prerequisites, no
	// external steps and no artifacts: reaching configured is the whole of
	// its admission, so it verifies immediately.
	{domain.SurfaceConfigured, EventVerified, domain.SurfaceEnrolled},

	{domain.SurfaceEnrolled, EventSuspended, domain.SurfaceSuspended},
	{domain.SurfaceVerifying, EventSuspended, domain.SurfaceSuspended},
	{domain.SurfaceSubmitted, EventSuspended, domain.SurfaceSuspended},
	{domain.SurfaceSuspended, EventResumed, domain.SurfaceEnrolled},

	// Withdrawal is reachable from every non-available state and always
	// lands back at available: the surface is still in the registry, the
	// store is simply not enrolled in it.
	{domain.SurfacePrerequisitesPending, EventWithdrawn, domain.SurfaceAvailable},
	{domain.SurfaceConfigured, EventWithdrawn, domain.SurfaceAvailable},
	{domain.SurfaceSubmitted, EventWithdrawn, domain.SurfaceAvailable},
	{domain.SurfaceVerifying, EventWithdrawn, domain.SurfaceAvailable},
	{domain.SurfaceEnrolled, EventWithdrawn, domain.SurfaceAvailable},
	{domain.SurfaceSuspended, EventWithdrawn, domain.SurfaceAvailable},
}

// ErrInvalidTransition is returned when an event names no legal move from
// the enrollment's current state.
type ErrInvalidTransition struct {
	SurfaceID string
	From      domain.SurfaceState
	Event     Event
}

func (e ErrInvalidTransition) Error() string {
	return fmt.Sprintf("surface %s: no transition from %q on event %q", e.SurfaceID, e.From, e.Event)
}

// ErrStoreNotActive is returned when enrollment is asked to progress past
// available for a store that has not reached UCPStateActive. This is the
// only cross-lifecycle dependency in the system (R9).
type ErrStoreNotActive struct {
	SurfaceID string
	UCPState  domain.UCPState
}

func (e ErrStoreNotActive) Error() string {
	return fmt.Sprintf("surface %s: store is %q, must be active to enroll", e.SurfaceID, e.UCPState)
}

// nextEnrollmentState resolves the state an event moves an enrollment to.
func nextEnrollmentState(from domain.SurfaceState, ev Event) (domain.SurfaceState, bool) {
	for _, t := range enrollmentTransitions {
		if t.from == from && t.event == ev {
			return t.to, true
		}
	}
	return "", false
}
