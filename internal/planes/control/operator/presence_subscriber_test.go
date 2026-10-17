// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package operator_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/planes/control/operator"
)

// fakeSnapshotSwappedEvent is a minimal eventbus.DomainEvent for
// serving.snapshot.swapped, matching serving.SnapshotSwappedEvent's wire
// shape without importing the serving package's event type.
type fakeSnapshotSwappedEvent struct {
	storeID    string
	merchantID string
	payload    []byte
}

func (e *fakeSnapshotSwappedEvent) EventType() string     { return "serving.snapshot.swapped" }
func (e *fakeSnapshotSwappedEvent) StoreID() string       { return e.storeID }
func (e *fakeSnapshotSwappedEvent) MerchantID() string    { return e.merchantID }
func (e *fakeSnapshotSwappedEvent) OccurredAt() time.Time { return time.Now().UTC() }
func (e *fakeSnapshotSwappedEvent) Payload() []byte       { return e.payload }

var _ eventbus.DomainEvent = (*fakeSnapshotSwappedEvent)(nil)

func TestPresenceSubscriber_Handle_UpsertsRecord(t *testing.T) {
	repo := &fakePresenceRepo{}
	sub := operator.NewPresenceSubscriber(repo, &fakeOperatorEventBus{}, nil)

	builtAt := time.Now().UTC()
	sourceSync := builtAt.Add(-2 * time.Minute)
	payload, err := json.Marshal(map[string]any{
		"store_id":      "store-1",
		"merchant_id":   "merch-1",
		"product_count": 17,
		"built_at":      builtAt,
		"source_sync":   sourceSync,
	})
	require.NoError(t, err)

	event := &fakeSnapshotSwappedEvent{storeID: "store-1", merchantID: "merch-1", payload: payload}

	require.NoError(t, sub.Handle(context.Background(), event))

	rec, err := repo.Get(context.Background(), "merch-1", "store-1")
	require.NoError(t, err)
	assert.Equal(t, 17, rec.ProductCount)
	assert.Equal(t, "merch-1", rec.MerchantID)
}

func TestPresenceSubscriber_Handle_MalformedPayloadIsNonFatal(t *testing.T) {
	repo := &fakePresenceRepo{}
	sub := operator.NewPresenceSubscriber(repo, &fakeOperatorEventBus{}, nil)

	event := &fakeSnapshotSwappedEvent{storeID: "store-1", merchantID: "merch-1", payload: []byte("not json")}

	err := sub.Handle(context.Background(), event)
	assert.NoError(t, err, "a malformed payload must be dropped, not retried forever")

	_, err = repo.Get(context.Background(), "merch-1", "store-1")
	assert.ErrorIs(t, err, operator.ErrPresenceNotFound)
}
