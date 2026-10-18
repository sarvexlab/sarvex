// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package channel

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// connectTestMongo requires a replica-set MongoDB (even a single-node one)
// at MONGO_URI — FoldSessionEvent uses a real multi-document transaction,
// which a standalone mongod cannot run at all. Mirrors
// internal/planes/control/ledger/mongo_store_integration_test.go's
// identical helper.
func connectTestMongo(t *testing.T) (*mongo.Client, *mongo.Database) {
	t.Helper()
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		t.Skip("MONGO_URI not set")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)

	db := client.Database("channel_test")
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return client, db
}

// TestMongoStore_FoldSessionEvent_TransactionalReconciliation is A2/R6
// against real MongoDB: the session upsert and the daily-aggregate delta
// commit atomically, and duplicate event_id delivery is a true no-op
// (unlike the in-memory test double, this exercises the real
// mongo.Client.WithTransaction retry path).
func TestMongoStore_FoldSessionEvent_TransactionalReconciliation(t *testing.T) {
	client, db := connectTestMongo(t)
	store, err := NewMongoStore(client, db)
	require.NoError(t, err)
	ctx := context.Background()
	day := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	initiated := SessionEvent{
		EventID: "evt-1", SessionID: "chk-int-1", MerchantID: "merch-1", StoreID: "store-1",
		Surface: SurfaceGoogleGemini, OccurredAt: day, Kind: SessionEventInitiated,
	}
	applied, err := store.FoldSessionEvent(ctx, initiated)
	require.NoError(t, err)
	assert.True(t, applied)

	// Redelivery must be a no-op.
	applied, err = store.FoldSessionEvent(ctx, initiated)
	require.NoError(t, err)
	assert.False(t, applied)

	completed := SessionEvent{
		EventID: "order_intent:ord-1", SessionID: "chk-int-1", MerchantID: "merch-1", StoreID: "store-1",
		OccurredAt: day.Add(time.Minute), Kind: SessionEventCompleted, RevenueMinor: 12345, Currency: "USD",
	}
	applied, err = store.FoldSessionEvent(ctx, completed)
	require.NoError(t, err)
	assert.True(t, applied)

	sess, err := store.GetSession(ctx, "chk-int-1")
	require.NoError(t, err)
	assert.True(t, sess.Completed)
	assert.Equal(t, int64(12345), sess.RevenueMinor)

	agg, err := store.GetDailyAggregate(ctx, "merch-1", "store-1", SurfaceGoogleGemini, "2026_08_06")
	require.NoError(t, err)
	assert.Equal(t, int64(1), agg.Sessions, "duplicate delivery must not double-count")
	assert.Equal(t, int64(1), agg.CheckoutsCompleted)
	assert.Equal(t, sess.RevenueMinor, agg.RevenueMinor, "session and daily aggregate revenue must reconcile exactly")
}
