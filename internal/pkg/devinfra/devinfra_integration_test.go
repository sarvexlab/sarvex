// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package devinfra_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/devinfra"
	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	_ "github.com/sarvexlab/sarvex/internal/pkg/eventbus/natsjetstream"
	"github.com/sarvexlab/sarvex/internal/pkg/jwkmanagement"
	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
)

// These tests run against the real NATS JetStream and Vault from
// deployments/docker-compose.yaml (`make infra-up`) — the same services a
// self-hosted deployment runs.

type testEvent struct {
	merchantID string
	payload    []byte
}

func (e testEvent) EventType() string     { return "devinfra.test" }
func (e testEvent) StoreID() string       { return "store-1" }
func (e testEvent) MerchantID() string    { return e.merchantID }
func (e testEvent) OccurredAt() time.Time { return time.Now().UTC() }
func (e testEvent) Payload() []byte       { return e.payload }

func TestEventBus_NATSRoundTrip(t *testing.T) {
	runID := uuid.NewString()
	bus, err := eventbus.New(eventbus.Config{
		ConsumerGroup: "devinfra_" + runID,
		TopicPrefix:   "devinfra_" + runID + ".",
		NATS:          eventbus.NATSConfig{URL: devinfra.NATSURL()},
	})
	require.NoError(t, err)
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	got := make(chan string, 1)
	_, err = bus.Subscribe(ctx, "devinfra.test", func(_ context.Context, ev eventbus.DomainEvent) error {
		got <- ev.MerchantID() + ":" + string(ev.Payload())
		return nil
	})
	require.NoError(t, err)

	require.NoError(t, bus.Publish(ctx, testEvent{merchantID: "m-1", payload: []byte(`{"ok":true}`)}))

	select {
	case v := <-got:
		require.Equal(t, `m-1:{"ok":true}`, v)
	case <-ctx.Done():
		t.Fatal("event was not delivered through NATS JetStream")
	}
}

func TestSecretManagement_VaultRoundTrip(t *testing.T) {
	svc, err := secretmanagement.New(context.Background(), devinfra.SecretConfig())
	require.NoError(t, err)

	ref, err := svc.Encrypt(context.Background(), map[string]any{"consumer_key": "ck_123", "consumer_secret": "cs_456"})
	require.NoError(t, err)

	// The reference is what Mongo stores; it must survive a JSON round trip.
	raw, err := json.Marshal(ref)
	require.NoError(t, err)
	var back secretmanagement.EncryptedConfig
	require.NoError(t, json.Unmarshal(raw, &back))

	got, err := svc.Decrypt(context.Background(), &back)
	require.NoError(t, err)
	require.Equal(t, "ck_123", got["consumer_key"])
	require.Equal(t, "cs_456", got["consumer_secret"])
}

func TestJWKManagement_VaultTransitWrapsSigningKey(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	svc, err := jwkmanagement.NewFromConfig(devinfra.JWKConfig(addr), slog.Default())
	require.NoError(t, err)

	storeID := "devinfra-" + uuid.NewString()
	made, err := svc.GenerateKeypair(context.Background(), storeID, time.Hour)
	require.NoError(t, err)

	active, err := svc.GetActiveKeypair(context.Background(), storeID)
	require.NoError(t, err)
	require.NotNil(t, active.PrivateKey, "private key must decrypt through Vault Transit")
	require.Equal(t, made.JWK.Kid, active.JWK.Kid)
}

// The planes depend on two delivery semantics: different consumer groups each
// receive a copy (fan-out), and instances sharing a group split the load
// (competing consumers).
func TestEventBus_FanOutAndCompetingConsumers(t *testing.T) {
	runID := uuid.NewString()
	newBus := func(group string) eventbus.EventBus {
		b, err := eventbus.New(eventbus.Config{
			ConsumerGroup: group + "_" + runID,
			TopicPrefix:   "devinfra_" + runID + ".",
			NATS:          eventbus.NATSConfig{URL: devinfra.NATSURL()},
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = b.Close() })
		return b
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var mu sync.Mutex
	counts := map[string]int{}
	handler := func(name string) eventbus.EventHandler {
		return func(context.Context, eventbus.DomainEvent) error {
			mu.Lock()
			counts[name]++
			mu.Unlock()
			return nil
		}
	}

	a1, a2, b1, pub := newBus("group_a"), newBus("group_a"), newBus("group_b"), newBus("publisher")
	// a1 and a2 are two instances of the same group; b1 is another group.
	_, err := a1.Subscribe(ctx, "devinfra.fanout", handler("a"))
	require.NoError(t, err)
	_, err = a2.Subscribe(ctx, "devinfra.fanout", handler("a"))
	require.NoError(t, err)
	_, err = b1.Subscribe(ctx, "devinfra.fanout", handler("b"))
	require.NoError(t, err)

	const n = 10
	for i := 0; i < n; i++ {
		require.NoError(t, pub.Publish(ctx, fanoutEvent{}))
	}

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return counts["a"] == n && counts["b"] == n
	}, 20*time.Second, 100*time.Millisecond, "each group should receive every event exactly once; got %v", counts)
}

type fanoutEvent struct{}

func (fanoutEvent) EventType() string     { return "devinfra.fanout" }
func (fanoutEvent) StoreID() string       { return "store-1" }
func (fanoutEvent) MerchantID() string    { return "m-1" }
func (fanoutEvent) OccurredAt() time.Time { return time.Now().UTC() }
func (fanoutEvent) Payload() []byte       { return []byte(`{}`) }
