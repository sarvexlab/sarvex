// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"log/slog"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
)

// This file implements R4: a scheduled sweep, independent of the checkout
// request path, that flags a checkout session abandoned once it has sat in
// a non-terminal state with no activity for AbandonmentThreshold. It reads
// only from the serving plane's own session store (Redis in production,
// the in-memory fallback in tests) — never the checkoutescalation
// package's separate, control-plane-side abandonment heuristic (a
// different mechanism entirely, operating on that package's own
// projection with its own threshold; see checkout_lifecycle_events.go's
// package doc comment on how the two contracts relate).

// DefaultAbandonmentSweepInterval is how often the sweep re-scans the
// session store.
const DefaultAbandonmentSweepInterval = 5 * time.Minute

// DefaultAbandonmentThreshold is R4's "no activity for 30 minutes"
// constant, configurable via AbandonmentSweepConfig.AbandonmentThreshold.
const DefaultAbandonmentThreshold = 30 * time.Minute

// defaultAbandonmentBatchLimit bounds how many sessions one sweep pass
// processes, so a large backlog can't make a single pass unbounded.
const defaultAbandonmentBatchLimit = 500

// AbandonmentSweepConfig configures AbandonmentSweeper.
type AbandonmentSweepConfig struct {
	// Interval between sweep passes.
	Interval time.Duration
	// AbandonmentThreshold: a non-terminal session with no activity for
	// longer than this is marked abandoned (R4, default 30 minutes).
	AbandonmentThreshold time.Duration
	// BatchLimit bounds how many sessions one sweep pass processes.
	BatchLimit int
}

// DefaultAbandonmentSweepConfig returns the production defaults.
func DefaultAbandonmentSweepConfig() AbandonmentSweepConfig {
	return AbandonmentSweepConfig{
		Interval:             DefaultAbandonmentSweepInterval,
		AbandonmentThreshold: DefaultAbandonmentThreshold,
		BatchLimit:           defaultAbandonmentBatchLimit,
	}
}

// AbandonmentSweeper periodically scans the session store for sessions
// that satisfy R4's abandonment condition and emits
// serving.checkout.abandoned exactly once per session. Single-process:
// like internal/planes/control/monitoring.Scheduler and
// internal/planes/control/checkoutescalation.Sweeper, it runs with no
// leader election — duplicate sweep passes are idempotent (marking a
// session already marked is a no-op read-side skip; see markAndEmit),
// which is the same rationale those two give for skipping leader
// election, so running one serving-plane replica or several doesn't
// double-emit.
type AbandonmentSweeper struct {
	sessions SessionStore
	events   eventbus.EventBus
	config   AbandonmentSweepConfig
	log      *slog.Logger
	// now is the clock the sweeper reads to compute the cutoff — a field
	// rather than a bare time.Now() call so a test can pin an exact
	// instant and assert the threshold boundary deterministically.
	now func() time.Time
}

// NewAbandonmentSweeper constructs an AbandonmentSweeper. A zero-value
// config field falls back to DefaultAbandonmentSweepConfig's value for
// that field.
func NewAbandonmentSweeper(sessions SessionStore, events eventbus.EventBus, config AbandonmentSweepConfig, log *slog.Logger) *AbandonmentSweeper {
	if log == nil {
		log = slog.Default()
	}
	d := DefaultAbandonmentSweepConfig()
	if config.Interval <= 0 {
		config.Interval = d.Interval
	}
	if config.AbandonmentThreshold <= 0 {
		config.AbandonmentThreshold = d.AbandonmentThreshold
	}
	if config.BatchLimit <= 0 {
		config.BatchLimit = d.BatchLimit
	}
	return &AbandonmentSweeper{sessions: sessions, events: events, config: config, log: log, now: time.Now}
}

// Run blocks, running an immediate pass and then ticking on
// config.Interval, until ctx is cancelled.
func (a *AbandonmentSweeper) Run(ctx context.Context) {
	a.runOnce(ctx)
	ticker := time.NewTicker(a.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.runOnce(ctx)
		}
	}
}

func (a *AbandonmentSweeper) runOnce(ctx context.Context) {
	cutoff := a.now().UTC().Add(-a.config.AbandonmentThreshold)
	candidates, err := a.sessions.ListAbandonmentCandidates(ctx, cutoff, a.config.BatchLimit)
	if err != nil {
		a.log.Error("abandonment sweep: list candidates failed", "error", err)
		return
	}
	for _, session := range candidates {
		a.markAndEmit(ctx, session)
	}
}

// markAndEmit marks session as notified (persisted, R4's "mark the
// session") *before* emitting, so a crash between the two leaves the
// session unmarked — re-swept and re-attempted next pass — rather than
// marked-but-never-emitted, which would silently and permanently swallow
// the event. This is what makes ListAbandonmentCandidates' read side (it
// skips AbandonedNotifiedAt != nil) sufficient to guarantee at most one
// emission per session even across concurrent/duplicate sweep passes.
func (a *AbandonmentSweeper) markAndEmit(ctx context.Context, session *CheckoutSession) {
	now := a.now().UTC()
	marked := *session
	marked.AbandonedNotifiedAt = &now
	if err := a.sessions.Update(ctx, &marked); err != nil {
		a.log.Error("abandonment sweep: mark session failed", "error", err, "session_id", session.SessionID)
		return
	}

	ev := &CheckoutAbandonedEvent{
		EventVersion: eventContractVersion,
		EventID:      newEventID(),
		OccurredAt_:  now,
		MerchantID_:  session.MerchantID,
		StoreID_:     session.StoreID,
		Surface:      Surface(session.Surface),
		CheckoutID:   session.SessionID,
		// Stage is the last SessionState the session reached before it went
		// quiet (only Incomplete/ReadyForComplete are reachable here — see
		// Terminal()) — the Lost Revenue Ledger uses it to distinguish
		// ordinary top-of-funnel drop-off from a session that stalled right
		// before the final synchronous complete call.
		Stage: string(session.State),
		Cart:  lastKnownOrFreshSnapshot(session),
	}
	publishLifecycleEventV1(a.events, ev, a.log, session.SessionID)
}
