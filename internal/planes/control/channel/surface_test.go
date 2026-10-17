// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package channel_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/channel"
)

// TestSurfaceEnumExtension_NoMigrationRequired is R9: a brand-new Surface
// value this codebase has never defined (not one of the five named
// constants) must still fold into Session/DailyAggregate and be readable
// back correctly — proving Surface is stored as a plain string at the
// persistence layer with no fixed-enum schema constraint, so adding a
// future surface (R8's "must require no schema migration") is purely a Go
// constant addition, never a data migration.
func TestSurfaceEnumExtension_NoMigrationRequired(t *testing.T) {
	store := channel.NewInMemoryStoreForTest()
	ctx := context.Background()
	futureSurface := channel.Surface("anthropic_claude_shopping") // deliberately not one of the five defined constants

	applied, err := store.FoldSessionEvent(ctx, channel.SessionEvent{
		EventID: "evt-future-1", SessionID: "chk-future", MerchantID: "merch-1", StoreID: "store-1",
		Surface: futureSurface, OccurredAt: time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC), Kind: channel.SessionEventInitiated,
	})
	require.NoError(t, err)
	assert.True(t, applied)

	sess, err := store.GetSession(ctx, "chk-future")
	require.NoError(t, err)
	assert.Equal(t, futureSurface, sess.Surface)

	agg, err := store.GetDailyAggregate(ctx, "merch-1", "store-1", futureSurface, "2026_08_06")
	require.NoError(t, err)
	assert.Equal(t, int64(1), agg.Sessions)
	assert.Equal(t, futureSurface, agg.Surface)

	all, err := store.ListDailyAggregates(ctx, "merch-1", "store-1", "", "2026_08_01", "2026_08_31")
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, futureSurface, all[0].Surface)
}

// TestComputeFunnelRates_ZeroSafe is R7: a zero denominator yields a zero
// rate, never a division-by-zero panic or an estimate.
func TestComputeFunnelRates_ZeroSafe(t *testing.T) {
	rates := channel.ComputeFunnelRates(channel.DailyAggregate{})
	assert.Equal(t, 0.0, rates.CartRate)
	assert.Equal(t, 0.0, rates.CheckoutRate)
	assert.Equal(t, 0.0, rates.CompletionRate)
	assert.Equal(t, int64(0), rates.AOVMinor)
}
