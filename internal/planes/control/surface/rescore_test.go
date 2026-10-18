// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package surface_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/surface"
)

// recordingBus records published event types; nothing else on the bus is used.
type recordingBus struct {
	eventbus.EventBus
	mu     sync.Mutex
	events []string
}

func (b *recordingBus) Publish(_ context.Context, e eventbus.DomainEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e.EventType())
	return nil
}

func (b *recordingBus) count(topic string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, e := range b.events {
		if e == topic {
			n++
		}
	}
	return n
}

// The sync plane scopes a store's attributes and scoring rules by the surfaces
// it is on. Joining or leaving a surface must therefore ask it to re-score —
// once per crossing of the available boundary, and not for moves within it.
func TestEnrollmentRequestsCatalogRescoreOnlyWhenTheSurfaceSetChanges(t *testing.T) {
	registry, err := surface.NewRegistry(namedRegistration("surface_a"))
	require.NoError(t, err)
	bus := &recordingBus{}
	svc := surface.NewSurfaceService(registry, newMemRepo(), &stubActivation{state: domain.UCPStateActive}, stubConnectors{}, bus, nil)
	ctx := context.Background()

	_, err = svc.Enroll(ctx, testMerchant, testStore, "surface_a")
	require.NoError(t, err)
	assert.Equal(t, 1, bus.count(ucp.CommandRequestAudit), "available -> enrolled changes the surface set: one rescore request")
	assert.Greater(t, bus.count(ucp.EventStoreSurfaceStateChanged), 0)

	before := bus.count(ucp.CommandRequestAudit)
	require.NoError(t, svc.Suspend(ctx, testMerchant, testStore, "surface_a", "paused"))
	assert.Equal(t, before, bus.count(ucp.CommandRequestAudit), "enrolled -> suspended stays within the set: no rescore")

	require.NoError(t, svc.Withdraw(ctx, testMerchant, testStore, "surface_a"))
	assert.Equal(t, before+1, bus.count(ucp.CommandRequestAudit), "-> available leaves the set: one rescore request")
}
