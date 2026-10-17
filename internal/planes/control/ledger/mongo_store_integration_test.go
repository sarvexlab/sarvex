// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package ledger

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// connectTestMongo requires a replica-set MongoDB (even a single-node one)
// at MONGO_URI — InsertEntryAndRollup uses a real multi-document
// transaction (R11), which a standalone mongod cannot run at all. See the
// PR description for the docker-compose change that turns local dev Mongo
// into a single-node replica set; make test-integration-control's existing
// MONGO_URI default points at it.
func connectTestMongo(t *testing.T) (*mongo.Client, *mongo.Database) {
	t.Helper()
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		t.Skip("MONGO_URI not set")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)

	db := client.Database("ledger_test")
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return client, db
}

func newTestEntry(eventID, merchantID, currency string, revenueMinor int64, cause Cause, occurredAt time.Time) Entry {
	return Entry{
		EntryID:           "led_" + eventID,
		MerchantID:        merchantID,
		EventID:           eventID,
		OccurredAt:        occurredAt,
		CheckoutID:        "chk_" + eventID,
		StoreID:           "store-1",
		Stage:             "create",
		FailureCode:       "CONNECTOR_CREATE_FAILED",
		Cause:             cause,
		RecommendedAction: "check your store",
		LostRevenueMinor:  revenueMinor,
		Currency:          currency,
		RevenueUnknown:    currency == "",
		Status:            StatusOpen,
		StatusChangedAt:   occurredAt,
		CreatedAt:         occurredAt,
	}
}

// TestMongoStore_Idempotency is A1 against real Mongo (the unit-level
// equivalent lives in consumer_test.go against a fake store): the same
// event_id inserted three times produces exactly one entry, with the
// rollup incremented once.
func TestMongoStore_Idempotency(t *testing.T) {
	client, db := connectTestMongo(t)
	store, err := NewMongoStore(client, db)
	require.NoError(t, err)

	occurredAt := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	entry := newTestEntry("evt-dup", "merch-1", "USD", 5000, CauseProfileMismatch, occurredAt)

	for i := range 3 {
		inserted, err := store.InsertEntryAndRollup(context.Background(), entry)
		require.NoError(t, err)
		if i == 0 {
			assert.True(t, inserted, "first delivery must insert")
		} else {
			assert.False(t, inserted, "redelivery must be a no-op, not a second entry")
		}
	}

	rollup, err := store.GetRollup(context.Background(), "merch-1", "2026_08", "USD")
	require.NoError(t, err)
	assert.Equal(t, int64(1), rollup.TotalCount)
	assert.Equal(t, int64(5000), rollup.TotalLostRevenueMinor)
}

// TestMongoStore_MultiCurrencyNeverSummed is A4/R9 against real Mongo:
// entries in different currencies produce separate rollups.
func TestMongoStore_MultiCurrencyNeverSummed(t *testing.T) {
	client, db := connectTestMongo(t)
	store, err := NewMongoStore(client, db)
	require.NoError(t, err)

	occurredAt := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	_, err = store.InsertEntryAndRollup(context.Background(), newTestEntry("evt-usd", "merch-1", "USD", 10000, CausePaymentHandler, occurredAt))
	require.NoError(t, err)
	_, err = store.InsertEntryAndRollup(context.Background(), newTestEntry("evt-aud", "merch-1", "AUD", 20000, CausePaymentHandler, occurredAt))
	require.NoError(t, err)

	usd, err := store.GetRollup(context.Background(), "merch-1", "2026_08", "USD")
	require.NoError(t, err)
	aud, err := store.GetRollup(context.Background(), "merch-1", "2026_08", "AUD")
	require.NoError(t, err)

	assert.Equal(t, int64(10000), usd.TotalLostRevenueMinor)
	assert.Equal(t, int64(20000), aud.TotalLostRevenueMinor)
}

// TestMongoStore_ListRollups covers GET /ledger/summary's no-currency-filter
// mode: every currency a merchant has a rollup for that month comes back,
// scoped to that merchant and month only, sorted by lost_revenue_minor
// descending (R14's money-first ordering applies here too), never summed.
func TestMongoStore_ListRollups(t *testing.T) {
	client, db := connectTestMongo(t)
	store, err := NewMongoStore(client, db)
	require.NoError(t, err)

	occurredAt := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	otherMonth := time.Date(2026, 7, 6, 10, 0, 0, 0, time.UTC)
	_, err = store.InsertEntryAndRollup(context.Background(), newTestEntry("evt-list-usd", "merch-list", "USD", 9000, CausePaymentHandler, occurredAt))
	require.NoError(t, err)
	_, err = store.InsertEntryAndRollup(context.Background(), newTestEntry("evt-list-aud", "merch-list", "AUD", 4000, CausePaymentHandler, occurredAt))
	require.NoError(t, err)
	// A different month and a different merchant must not leak into the list.
	_, err = store.InsertEntryAndRollup(context.Background(), newTestEntry("evt-list-old-month", "merch-list", "USD", 99999, CausePaymentHandler, otherMonth))
	require.NoError(t, err)
	_, err = store.InsertEntryAndRollup(context.Background(), newTestEntry("evt-list-other-merchant", "merch-other", "USD", 99999, CausePaymentHandler, occurredAt))
	require.NoError(t, err)

	rollups, err := store.ListRollups(context.Background(), "merch-list", "2026_08")
	require.NoError(t, err)
	require.Len(t, rollups, 2)
	assert.Equal(t, "USD", rollups[0].Currency, "USD (9000) must sort ahead of AUD (4000)")
	assert.Equal(t, int64(9000), rollups[0].TotalLostRevenueMinor)
	assert.Equal(t, "AUD", rollups[1].Currency)
	assert.Equal(t, int64(4000), rollups[1].TotalLostRevenueMinor)
}

// TestMongoStore_AbsentCartNeverEstimated is A4/R8: an entry built with no
// currency (the consumer's shape for "no cart snapshot") stores
// lost_revenue_minor=0 and revenue_unknown=true, and is folded into the
// rollup's entries_with_unknown_revenue count, not into any cause's
// lost_revenue_minor.
func TestMongoStore_AbsentCartNeverEstimated(t *testing.T) {
	client, db := connectTestMongo(t)
	store, err := NewMongoStore(client, db)
	require.NoError(t, err)

	occurredAt := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	entry := newTestEntry("evt-no-cart", "merch-1", "", 0, CauseUnclassified, occurredAt)
	_, err = store.InsertEntryAndRollup(context.Background(), entry)
	require.NoError(t, err)

	got, err := store.GetEntry(context.Background(), "merch-1", entry.EntryID)
	require.NoError(t, err)
	assert.True(t, got.RevenueUnknown)
	assert.Equal(t, int64(0), got.LostRevenueMinor)

	rollup, err := store.GetRollup(context.Background(), "merch-1", "2026_08", "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), rollup.EntriesWithUnknownRevenue)
}

// TestMongoStore_TransactionalConsistency is A5: an injected failure
// between the entry write and the rollup update leaves both unwritten —
// never divergent. The failure is injected via a MongoDB schema validator
// on control_ledger_rollups that rejects an unrecognized currency; foldIntoRollup's
// write inside the transaction hits that validator and errors, which must
// abort the whole transaction, including the entry insert that already
// happened moments before in the same call.
func TestMongoStore_TransactionalConsistency(t *testing.T) {
	client, db := connectTestMongo(t)
	store, err := NewMongoStore(client, db)
	require.NoError(t, err)

	// Force the validator after NewMongoStore has already created the
	// collection (CreateMany's index creation implicitly creates it) —
	// collMod applies a validator to an existing collection.
	err = db.RunCommand(context.Background(), bson.D{
		{Key: "collMod", Value: "control_ledger_rollups"},
		{Key: "validator", Value: bson.M{
			"$jsonSchema": bson.M{
				"properties": bson.M{
					"currency": bson.M{"enum": bson.A{"USD", "EUR", "AUD"}},
				},
			},
		}},
		{Key: "validationAction", Value: "error"},
	}).Err()
	require.NoError(t, err)

	occurredAt := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	badEntry := newTestEntry("evt-crash", "merch-1", "ZZZ", 7500, CausePaymentHandler, occurredAt)

	inserted, err := store.InsertEntryAndRollup(context.Background(), badEntry)
	assert.False(t, inserted)
	assert.Error(t, err, "the rollup write must fail validation and abort the transaction")

	_, getErr := store.GetEntry(context.Background(), "merch-1", badEntry.EntryID)
	assert.ErrorIs(t, getErr, ErrNotFound, "the entry insert must have been rolled back along with the failed rollup update — never divergent")

	_, rollupErr := store.GetRollup(context.Background(), "merch-1", "2026_08", "ZZZ")
	assert.ErrorIs(t, rollupErr, ErrNotFound, "the rollup must never have been created")

	// Retry with a valid currency for the same event_id: both must now
	// commit together (the other valid end state A5 allows).
	goodEntry := badEntry
	goodEntry.Currency = "USD"
	goodEntry.RevenueUnknown = false
	inserted, err = store.InsertEntryAndRollup(context.Background(), goodEntry)
	require.NoError(t, err)
	assert.True(t, inserted)

	got, err := store.GetEntry(context.Background(), "merch-1", goodEntry.EntryID)
	require.NoError(t, err)
	assert.Equal(t, "USD", got.Currency)

	rollup, err := store.GetRollup(context.Background(), "merch-1", "2026_08", "USD")
	require.NoError(t, err)
	assert.Equal(t, int64(1), rollup.TotalCount)
	assert.Equal(t, int64(7500), rollup.TotalLostRevenueMinor)
}

// TestMongoStore_StatusForwardOnly is A7 against real Mongo.
func TestMongoStore_StatusForwardOnly(t *testing.T) {
	client, db := connectTestMongo(t)
	store, err := NewMongoStore(client, db)
	require.NoError(t, err)

	occurredAt := time.Now().UTC()
	entry := newTestEntry("evt-status", "merch-1", "USD", 1000, CauseTimeout, occurredAt)
	_, err = store.InsertEntryAndRollup(context.Background(), entry)
	require.NoError(t, err)

	updated, err := store.UpdateEntryStatus(context.Background(), "merch-1", entry.EntryID, StatusAcknowledged, time.Now().UTC())
	require.NoError(t, err)
	assert.Equal(t, StatusAcknowledged, updated.Status)

	_, err = store.UpdateEntryStatus(context.Background(), "merch-1", entry.EntryID, StatusOpen, time.Now().UTC())
	assert.ErrorIs(t, err, ErrInvalidTransition)
}
