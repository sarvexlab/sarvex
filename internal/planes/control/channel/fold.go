// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package channel

import "time"

// dailyDelta is the funnel/revenue change one FoldSessionEvent fold
// contributes to a DailyAggregate — computed by diffing the session's
// before/after state so a redelivered or stage-repeating event (e.g. two
// cart_updated events) never double-counts a stage transition.
type dailyDelta struct {
	sessions, checkoutsInitiated, sessionsInferred int64
	reachedCart, reachedCheckout, completed        int64
	revenueMinor                                   int64
}

func (d dailyDelta) isZero() bool {
	return d == dailyDelta{}
}

// foldSessionEvent is the pure fold logic FoldSessionEvent implementations
// apply: given a session's current state (nil if it doesn't exist yet)
// and an inbound event, it returns the session's next state and the
// DailyAggregate delta that transition contributes — or duplicate=true if
// ev.EventID has already been folded (A1's idempotency contract).
// Store-independent (no I/O) so both mongoStore and tests share one
// implementation instead of two that could drift apart — see
// consumer_test.go's fakeStore.
func foldSessionEvent(existing *Session, ev SessionEvent, now time.Time) (updated Session, delta dailyDelta, duplicate bool) {
	var before Session
	notFound := existing == nil
	if !notFound {
		before = *existing
		for _, id := range existing.ProcessedEventIDs {
			if id == ev.EventID {
				return Session{}, dailyDelta{}, true
			}
		}
		updated = *existing
		updated.ProcessedEventIDs = append(append([]string{}, existing.ProcessedEventIDs...), ev.EventID)
		if ev.Kind == SessionEventInitiated && existing.Attribution == AttributionInferred {
			// The true initiating event arrived late — upgrade the
			// reconstructed session to Confirmed and correct started_at,
			// but keep it in the daily bucket it was already counted
			// into (Date unchanged) to avoid a cross-bucket move.
			updated.Attribution = AttributionConfirmed
			updated.StartedAt = ev.OccurredAt
		}
	} else {
		attribution := AttributionConfirmed
		if ev.Kind != SessionEventInitiated {
			// R2: this worker's first observation for this session_id
			// isn't the initiating event — out-of-order delivery, or the
			// initiating event predates this worker's subscription. The
			// correlation (session_id) is still explicit and reliable;
			// what's reconstructed is the session's early state
			// (started_at, request_count), so it's honestly marked
			// inferred rather than exact.
			attribution = AttributionInferred
		}
		updated = Session{
			SessionID:         ev.SessionID,
			MerchantID:        ev.MerchantID,
			StoreID:           ev.StoreID,
			Surface:           ev.Surface,
			AgentPlatform:     ev.AgentPlatform,
			StartedAt:         ev.OccurredAt,
			Attribution:       attribution,
			Date:              DateKey(ev.OccurredAt),
			ProcessedEventIDs: []string{ev.EventID},
			CreatedAt:         now,
		}
	}
	updated.RequestCount++
	updated.UpdatedAt = now

	switch ev.Kind {
	case SessionEventCartActivity:
		updated.ReachedCart = true
	case SessionEventCheckoutReached:
		updated.ReachedCheckout = true
	case SessionEventFailed, SessionEventAbandoned:
		t := ev.OccurredAt
		updated.EndedAt = &t
	case SessionEventCompleted:
		t := ev.OccurredAt
		updated.EndedAt = &t
		updated.Completed = true
		updated.RevenueMinor = ev.RevenueMinor
		updated.Currency = ev.Currency
	case SessionEventInitiated, SessionEventEscalated:
		// No additional stage change.
	}

	if notFound {
		delta.sessions = 1
		delta.checkoutsInitiated = 1
		if updated.Attribution == AttributionInferred {
			delta.sessionsInferred = 1
		}
	} else if ev.Kind == SessionEventInitiated && before.Attribution == AttributionInferred && updated.Attribution == AttributionConfirmed {
		delta.sessionsInferred = -1
	}
	if updated.ReachedCart && !before.ReachedCart {
		delta.reachedCart = 1
	}
	if updated.ReachedCheckout && !before.ReachedCheckout {
		delta.reachedCheckout = 1
	}
	if updated.Completed && !before.Completed {
		delta.completed = 1
		delta.revenueMinor = updated.RevenueMinor
	}

	return updated, delta, false
}
