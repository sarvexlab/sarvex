// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package channel_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/planes/control/channel"
)

// TestReconciliation_SessionRevenueMatchesDailyAggregateRevenue is A2/R6:
// for a generated stream of several sessions (some completed, some not),
// the sum of RevenueMinor across completed Session documents must equal
// DailyAggregate.RevenueMinor exactly — one truth, two views, built from
// the same fold in the same transaction (fold.go's foldSessionEvent is
// shared by both the session upsert and the daily delta, so this is a
// structural guarantee, not a coincidence — this test is what would catch
// a regression that breaks that structure).
func TestReconciliation_SessionRevenueMatchesDailyAggregateRevenue(t *testing.T) {
	store := channel.NewInMemoryStoreForTest()
	_, bus := newTestConsumer(t, store)
	ctx := context.Background()
	day := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)

	checkouts := []struct {
		id           string
		revenueMinor int64
		completes    bool
	}{
		{"chk-a", 1500, true},
		{"chk-b", 2500, true},
		{"chk-c", 999, false}, // fails before completion — contributes no revenue
		{"chk-d", 4200, true},
	}

	for i, co := range checkouts {
		require.NoError(t, bus.handlers[ucp.EventCheckoutInitiated](ctx, fakeEvent{
			eventType: ucp.EventCheckoutInitiated, occurredAt: day,
			payload: mustJSON(t, map[string]any{
				"event_id": "init-" + co.id, "checkout_id": co.id, "store_id": "store-1", "merchant_id": "merch-1",
				"surface": "google_gemini", "occurred_at": day,
			}),
		}))
		if co.completes {
			require.NoError(t, bus.handlers[ucp.EventOrderIntent](ctx, fakeEvent{
				eventType: ucp.EventOrderIntent, occurredAt: day.Add(time.Minute),
				payload: mustJSON(t, map[string]any{
					"checkout_session_id": co.id, "order_id": "ord-" + co.id, "store_id": "store-1", "merchant_id": "merch-1",
					"total": map[string]any{"amount_minor": co.revenueMinor, "currency": "USD"},
				}),
			}))
		} else {
			require.NoError(t, bus.handlers[ucp.EventCheckoutFailed](ctx, fakeEvent{
				eventType: ucp.EventCheckoutFailed, occurredAt: day.Add(time.Minute),
				payload: mustJSON(t, map[string]any{
					"event_id": "fail-" + co.id, "checkout_id": co.id, "store_id": "store-1", "merchant_id": "merch-1",
					"surface": "google_gemini", "occurred_at": day.Add(time.Minute),
				}),
			}))
		}
		_ = i
	}

	var sessionRevenueSum int64
	for _, co := range checkouts {
		sess, err := store.GetSession(ctx, co.id)
		require.NoError(t, err)
		sessionRevenueSum += sess.RevenueMinor
		assert.Equal(t, co.completes, sess.Completed)
	}

	agg, err := store.GetDailyAggregate(ctx, "merch-1", "store-1", channel.SurfaceGoogleGemini, "2026_08_06")
	require.NoError(t, err)

	assert.Equal(t, sessionRevenueSum, agg.RevenueMinor, "session-level revenue must reconcile exactly with the daily aggregate")
	assert.Equal(t, int64(1500+2500+4200), agg.RevenueMinor)
	assert.Equal(t, int64(3), agg.CheckoutsCompleted)
	assert.Equal(t, int64(4), agg.Sessions)
}

// TestReconciliation_AbandonedSessionClosesWithoutRevenue covers A1's
// "inactivity close" case: the serving plane's own 30-minute-inactivity
// sweep (internal/planes/serving.AbandonmentSweeper) emits
// serving.checkout.abandoned once a non-terminal session goes quiet — this
// package does not run its own timer (see consumer.go's doc comment); it
// only needs to close the session and contribute zero revenue when that
// signal arrives.
func TestReconciliation_AbandonedSessionClosesWithoutRevenue(t *testing.T) {
	store := channel.NewInMemoryStoreForTest()
	_, bus := newTestConsumer(t, store)
	ctx := context.Background()
	day := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)

	require.NoError(t, bus.handlers[ucp.EventCheckoutInitiated](ctx, fakeEvent{
		eventType: ucp.EventCheckoutInitiated, occurredAt: day,
		payload: mustJSON(t, map[string]any{
			"event_id": "e1", "checkout_id": "chk-abandoned", "store_id": "store-1", "merchant_id": "merch-1",
			"surface": "google_gemini", "occurred_at": day,
		}),
	}))
	require.NoError(t, bus.handlers[ucp.EventCheckoutAbandoned](ctx, fakeEvent{
		eventType: ucp.EventCheckoutAbandoned, occurredAt: day.Add(30 * time.Minute),
		payload: mustJSON(t, map[string]any{
			"event_id": "e2", "checkout_id": "chk-abandoned", "store_id": "store-1", "merchant_id": "merch-1",
			"surface": "google_gemini", "occurred_at": day.Add(30 * time.Minute),
		}),
	}))

	sess, err := store.GetSession(ctx, "chk-abandoned")
	require.NoError(t, err)
	assert.False(t, sess.Completed)
	assert.Equal(t, int64(0), sess.RevenueMinor)
	require.NotNil(t, sess.EndedAt)
	assert.Equal(t, day.Add(30*time.Minute), *sess.EndedAt)

	agg, err := store.GetDailyAggregate(ctx, "merch-1", "store-1", channel.SurfaceGoogleGemini, "2026_08_06")
	require.NoError(t, err)
	assert.Equal(t, int64(0), agg.CheckoutsCompleted)
	assert.Equal(t, int64(1), agg.Sessions)
}
