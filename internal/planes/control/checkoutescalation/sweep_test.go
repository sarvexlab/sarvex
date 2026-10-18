// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package checkoutescalation

import (
	"context"
	"testing"
	"time"
)

func escalatedSession(id string, escalatedAt time.Time) *Session {
	return &Session{
		CheckoutID: id, MerchantID: "merchant-1", StoreID: "store-1",
		State: StateEscalated, CurrentStage: "escalated", EventCount: 2,
		LastActivityAt: escalatedAt,
		Escalation: &Escalation{
			Reason: "3ds", EscalatedAt: escalatedAt, ContinueURLIssued: true,
		},
	}
}

// TestSweeper_EscalationStalled_ExactlyAtThreshold is R9/A4: stalled
// escalation must be detected exactly at the threshold and not before —
// tested in isolation from abandonment, since R9 calls this out as the
// single highest-value signal in the feature.
func TestSweeper_EscalationStalled_ExactlyAtThreshold(t *testing.T) {
	const threshold = 60 * time.Minute
	now := time.Now().UTC()

	tests := []struct {
		name        string
		escalatedAt time.Time
		wantFlagged bool
	}{
		{"1 second before threshold: not yet stalled", now.Add(-threshold + time.Second), false},
		{"exactly at threshold: stalled", now.Add(-threshold), true},
		{"1 second past threshold: stalled", now.Add(-threshold - time.Second), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessions := newFakeSessionRepo()
			sessions.sessions["checkout-1"] = escalatedSession("checkout-1", tt.escalatedAt)

			sweeper := NewSweeper(sessions, SweepConfig{EscalationStalledThreshold: threshold}, discardLog())
			sweeper.now = func() time.Time { return now } // pin the clock so the boundary is exact, not dependent on test-execution latency
			sweeper.sweepStalledEscalations(context.Background())

			s, err := sessions.Get(context.Background(), "merchant-1", "checkout-1")
			must(t, err)

			flagged := s.Diagnosis != nil && s.Diagnosis.Cause == "escalation_stalled"
			if flagged != tt.wantFlagged {
				t.Errorf("escalated_at=%v: flagged=%v, want %v", tt.escalatedAt, flagged, tt.wantFlagged)
			}
			// R9: stalled escalation is a diagnosis, not a state change —
			// the session must stay "escalated" either way.
			if s.State != StateEscalated {
				t.Errorf("expected state to remain escalated, got %q", s.State)
			}
		})
	}
}

// TestSweeper_EscalationStalled_DistinctFromAbandonment verifies the two
// signals don't bleed into each other: a returned escalation is never
// flagged stalled no matter how old, and a stalled escalation is not
// simultaneously marked abandoned by the same sweep pass.
func TestSweeper_EscalationStalled_DistinctFromAbandonment(t *testing.T) {
	oldEscalation := escalatedSession("checkout-1", time.Now().UTC().Add(-3*time.Hour))
	returnedAt := oldEscalation.Escalation.EscalatedAt.Add(time.Minute)
	oldEscalation.Escalation.ReturnedAt = &returnedAt

	sessions := newFakeSessionRepo()
	sessions.sessions["checkout-1"] = oldEscalation

	sweeper := NewSweeper(sessions, SweepConfig{
		EscalationStalledThreshold: 60 * time.Minute,
		AbandonmentThreshold:       24 * time.Hour,
	}, discardLog())
	sweeper.runOnce(context.Background())

	s, err := sessions.Get(context.Background(), "merchant-1", "checkout-1")
	must(t, err)
	if s.Diagnosis != nil && s.Diagnosis.Cause == "escalation_stalled" {
		t.Errorf("a returned escalation must never be flagged stalled")
	}
	if s.State == StateAbandoned {
		t.Errorf("a 3-hour-old escalated session with only a 60min stalled-threshold and 24h abandonment-threshold must not be abandoned yet")
	}
}

func TestSweeper_Abandonment(t *testing.T) {
	sessions := newFakeSessionRepo()
	sessions.sessions["checkout-1"] = &Session{
		CheckoutID: "checkout-1", MerchantID: "merchant-1", StoreID: "store-1",
		State: StateCartActive, CurrentStage: "cart_active", EventCount: 2,
		LastActivityAt: time.Now().UTC().Add(-25 * time.Hour),
	}
	sweeper := NewSweeper(sessions, SweepConfig{AbandonmentThreshold: 24 * time.Hour}, discardLog())
	sweeper.sweepAbandoned(context.Background())

	s, err := sessions.Get(context.Background(), "merchant-1", "checkout-1")
	must(t, err)
	if s.State != StateAbandoned {
		t.Errorf("expected state abandoned, got %q", s.State)
	}
	if s.Diagnosis == nil || s.Diagnosis.Cause != "buyer_abandoned" {
		t.Errorf("expected buyer_abandoned diagnosis, got %+v", s.Diagnosis)
	}
	if s.TerminalAt == nil || s.ExpiresAt == nil {
		t.Errorf("expected terminal_at/expires_at to be set on abandonment")
	}
}

func TestSweeper_DoesNotTouchAlreadyTerminalSessions(t *testing.T) {
	sessions := newFakeSessionRepo()
	terminalAt := time.Now().UTC().Add(-48 * time.Hour)
	sessions.sessions["checkout-1"] = &Session{
		CheckoutID: "checkout-1", MerchantID: "merchant-1", StoreID: "store-1",
		State: StateCompleted, EventCount: 4, LastActivityAt: terminalAt, TerminalAt: &terminalAt,
	}
	sweeper := NewSweeper(sessions, SweepConfig{AbandonmentThreshold: 24 * time.Hour}, discardLog())
	sweeper.runOnce(context.Background())

	s, err := sessions.Get(context.Background(), "merchant-1", "checkout-1")
	must(t, err)
	if s.State != StateCompleted {
		t.Errorf("sweep must never touch an already-terminal session, got state %q", s.State)
	}
}
