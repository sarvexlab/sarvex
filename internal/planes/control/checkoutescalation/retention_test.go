// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package checkoutescalation

import (
	"context"
	"testing"
	"time"
)

// TestRetention_ExpiresAtSetOnceTerminal_AndBackfilledOntoTimeline is A5.
//
// The actual purge is MongoDB's own TTL background thread
// (SetExpireAfterSeconds(0) on expires_at — mongo_sessions.go /
// mongo_timeline.go), which runs periodically and can't be exercised by a
// fast unit test without a live Mongo instance and a real wait. What this
// test verifies is the precondition for that purge to work correctly: once
// a session goes terminal, expires_at is set on the parent document and
// backfilled onto every existing timeline entry, so the whole session
// (not just the parent doc) expires atomically 30 days after terminal_at.
// The TTL index itself, and actual expiry, are exercised by
// make test-integration against real Mongo.
func TestRetention_ExpiresAtSetOnceTerminal_AndBackfilledOntoTimeline(t *testing.T) {
	sessions, timeline := newFakeSessionRepo(), newFakeTimelineRepo()
	p := NewProjector(sessions, timeline, nil, 30*24*time.Hour, discardLog())
	ctx := context.Background()
	t0 := time.Now().UTC()

	must(t, p.handleSessionInitiated(ctx, sessionInitiatedEvent(t, "ev-1", t0)))
	must(t, p.handleCartUpdated(ctx, cartUpdatedEvent(t, "ev-2", t0.Add(time.Minute))))

	// While active, no expiry — a session must never expire mid-flow.
	s, err := sessions.Get(ctx, "merchant-1", testCheckoutID)
	must(t, err)
	if s.ExpiresAt != nil {
		t.Fatalf("expected no expires_at while the session is still active, got %v", s.ExpiresAt)
	}
	entries, _, err := timeline.List(ctx, testCheckoutID, 0, 100)
	must(t, err)
	for _, e := range entries {
		if e.ExpiresAt != nil {
			t.Fatalf("expected no expires_at on timeline entries while the session is still active, got %v on seq %d", e.ExpiresAt, e.Seq)
		}
	}

	terminalAt := t0.Add(2 * time.Minute)
	must(t, p.handleFailed(ctx, failedEvent(t, "ev-3", terminalAt, "ORDER_CREATION_FAILED")))

	s, err = sessions.Get(ctx, "merchant-1", testCheckoutID)
	must(t, err)
	if s.ExpiresAt == nil {
		t.Fatalf("expected expires_at to be set once terminal")
	}
	wantExpiry := terminalAt.Add(30 * 24 * time.Hour)
	if !s.ExpiresAt.Equal(wantExpiry) {
		t.Errorf("expires_at = %v, want terminal_at + 30d = %v", s.ExpiresAt, wantExpiry)
	}

	entries, _, err = timeline.List(ctx, testCheckoutID, 0, 100)
	must(t, err)
	if len(entries) != 3 {
		t.Fatalf("expected 3 timeline entries, got %d", len(entries))
	}
	for _, e := range entries {
		if e.ExpiresAt == nil {
			t.Errorf("expected expires_at backfilled onto timeline seq %d (event_type %s), got nil", e.Seq, e.EventType)
			continue
		}
		if !e.ExpiresAt.Equal(wantExpiry) {
			t.Errorf("timeline seq %d expires_at = %v, want %v — the whole timeline must expire atomically with its parent", e.Seq, e.ExpiresAt, wantExpiry)
		}
	}
}

// TestRetention_AbandonedSessionAlsoGetsExpiry verifies the sweep's
// abandonment path (a time-derived terminal transition, not an event) sets
// expires_at with the same terminal_at + retention rule.
func TestRetention_AbandonedSessionAlsoGetsExpiry(t *testing.T) {
	sessions := newFakeSessionRepo()
	sessions.sessions["checkout-1"] = &Session{
		CheckoutID: "checkout-1", MerchantID: "merchant-1", StoreID: "store-1",
		State: StateCartActive, LastActivityAt: time.Now().UTC().Add(-25 * time.Hour),
	}
	sweeper := NewSweeper(sessions, SweepConfig{AbandonmentThreshold: 24 * time.Hour, Retention: 30 * 24 * time.Hour}, discardLog())
	sweeper.sweepAbandoned(context.Background())

	s, err := sessions.Get(context.Background(), "merchant-1", "checkout-1")
	must(t, err)
	if s.TerminalAt == nil || s.ExpiresAt == nil {
		t.Fatalf("expected terminal_at and expires_at to be set on abandonment, got %+v", s)
	}
	wantExpiry := s.TerminalAt.Add(30 * 24 * time.Hour)
	if !s.ExpiresAt.Equal(wantExpiry) {
		t.Errorf("expires_at = %v, want terminal_at + 30d = %v", s.ExpiresAt, wantExpiry)
	}
}
