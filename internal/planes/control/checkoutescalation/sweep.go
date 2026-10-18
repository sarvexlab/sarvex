// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package checkoutescalation

import (
	"context"
	"log/slog"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/checkoutcause"
)

// SweepConfig configures Sweeper's two time-derived transitions. Both are
// signals no event on the bus ever produces — nothing marks "the buyer
// went quiet" or "the escalation window closed" — so they're detected by
// periodically re-evaluating the projection's own state against the clock,
// following internal/planes/control/monitoring.Scheduler's plain-ticker
// pattern (no leader election: duplicate sweep passes are idempotent —
// MarkAbandoned/SetDiagnosis are themselves guarded/overwrite-safe — the
// same rationale that scheduler's own doc comment gives for skipping
// leader election).
type SweepConfig struct {
	// Interval between sweep passes.
	Interval time.Duration
	// AbandonmentThreshold: a non-terminal session with no activity for
	// longer than this is marked abandoned.
	AbandonmentThreshold time.Duration
	// EscalationStalledThreshold (R9, default 60min): an escalated session
	// with continue_url_issued and no returned_at for longer than this is
	// flagged escalation_stalled. Deliberately much shorter than
	// AbandonmentThreshold — a buyer who was actively handed off and never
	// came back is a stronger, faster signal than generic inactivity (R9:
	// "the single highest-value signal in this feature").
	EscalationStalledThreshold time.Duration
	// Retention: passed through to MarkAbandoned so the abandoned session
	// (and its timeline) still expires 30 days after terminal_at, same as
	// every other terminal path.
	Retention time.Duration
	// BatchLimit bounds how many sessions one sweep pass processes per
	// query, so a large backlog can't make a single pass unbounded.
	BatchLimit int
}

// DefaultSweepConfig returns the production defaults.
func DefaultSweepConfig() SweepConfig {
	return SweepConfig{
		Interval:                   5 * time.Minute,
		AbandonmentThreshold:       24 * time.Hour,
		EscalationStalledThreshold: 60 * time.Minute,
		Retention:                  30 * 24 * time.Hour,
		BatchLimit:                 500,
	}
}

// Sweeper periodically flags abandoned sessions and stalled escalations.
type Sweeper struct {
	sessions SessionRepository
	config   SweepConfig
	log      *slog.Logger
	// now is the clock Sweeper reads to compute thresholds — a field
	// rather than a bare time.Now() call so a test can pin an exact
	// instant and assert the R9 threshold boundary deterministically
	// (age == threshold flags; age == threshold-1s does not), instead of
	// depending on how much wall-clock time elapses between test setup
	// and the sweep running.
	now func() time.Time
}

// NewSweeper constructs a Sweeper. A zero-value config field falls back to
// DefaultSweepConfig's value for that field.
func NewSweeper(sessions SessionRepository, config SweepConfig, log *slog.Logger) *Sweeper {
	if log == nil {
		log = slog.Default()
	}
	d := DefaultSweepConfig()
	if config.Interval <= 0 {
		config.Interval = d.Interval
	}
	if config.AbandonmentThreshold <= 0 {
		config.AbandonmentThreshold = d.AbandonmentThreshold
	}
	if config.EscalationStalledThreshold <= 0 {
		config.EscalationStalledThreshold = d.EscalationStalledThreshold
	}
	if config.Retention <= 0 {
		config.Retention = d.Retention
	}
	if config.BatchLimit <= 0 {
		config.BatchLimit = d.BatchLimit
	}
	return &Sweeper{sessions: sessions, config: config, log: log, now: time.Now}
}

// Run blocks, running an immediate pass and then ticking on config.Interval,
// until ctx is cancelled.
func (s *Sweeper) Run(ctx context.Context) {
	s.runOnce(ctx)
	ticker := time.NewTicker(s.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

func (s *Sweeper) runOnce(ctx context.Context) {
	s.sweepAbandoned(ctx)
	s.sweepStalledEscalations(ctx)
}

// sweepAbandoned finds every non-terminal session with no activity for
// longer than AbandonmentThreshold and marks it abandoned (R1's state
// enum). Evidence is the session's own event_count: since every timeline
// insert and its matching session-doc increment happen in lockstep
// (projection.go), event_count always equals the highest seq the session
// has — the last thing that actually happened.
func (s *Sweeper) sweepAbandoned(ctx context.Context) {
	now := s.now().UTC()
	cutoff := now.Add(-s.config.AbandonmentThreshold)
	sessions, err := s.sessions.ListStaleActive(ctx, cutoff, s.config.BatchLimit)
	if err != nil {
		s.log.Error("checkoutescalation: sweep abandoned query failed", "error", err)
		return
	}
	for _, sess := range sessions {
		diag := Diagnose(checkoutcause.SignalBuyerAbandoned, sess.CurrentStage, []int64{sess.EventCount})
		if err := s.sessions.MarkAbandoned(ctx, sess.CheckoutID, now, diag, s.config.Retention); err != nil {
			s.log.Error("checkoutescalation: mark abandoned failed", "error", err, "checkout_id", sess.CheckoutID)
		}
	}
}

// sweepStalledEscalations flags R9's signal: an escalated session where
// continue_url was issued and no further activity was observed
// (escalation.returned_at is the proxy — see mongo_sessions.go's ApplyEvent
// doc) for longer than EscalationStalledThreshold. Distinct from
// abandonment: state stays "escalated" (the buyer was handed off
// somewhere specific, not just gone quiet) — only the diagnosis is set.
func (s *Sweeper) sweepStalledEscalations(ctx context.Context) {
	cutoff := s.now().UTC().Add(-s.config.EscalationStalledThreshold)
	sessions, err := s.sessions.ListStalledEscalations(ctx, cutoff, s.config.BatchLimit)
	if err != nil {
		s.log.Error("checkoutescalation: sweep stalled-escalation query failed", "error", err)
		return
	}
	for _, sess := range sessions {
		diag := Diagnose(checkoutcause.SignalEscalationStalled, sess.CurrentStage, []int64{sess.EventCount})
		if err := s.sessions.SetDiagnosis(ctx, sess.CheckoutID, diag); err != nil {
			s.log.Error("checkoutescalation: set stalled-escalation diagnosis failed", "error", err, "checkout_id", sess.CheckoutID)
		}
	}
}
