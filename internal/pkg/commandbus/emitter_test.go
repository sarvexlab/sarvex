// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package commandbus

// Topic names are asserted against the constants in internal/pkg/ucp, not
// against string literals. Those constants are the single source of truth
// for bus topic names (sarvex-v1-architecture.md), so a literal here is a second,
// silently-divergent copy — which is exactly what these assertions were
// before: they pinned "command.audit_requested" and
// "command.remediations_applied", names the emitter had long since stopped
// using.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/domain"
	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
)

type mockEventBus struct {
	publishedEvents []eventbus.DomainEvent
	publishErr      error
}

func (m *mockEventBus) Publish(ctx context.Context, event eventbus.DomainEvent) error {
	if m.publishErr != nil {
		return m.publishErr
	}
	m.publishedEvents = append(m.publishedEvents, event)
	return nil
}

func (m *mockEventBus) Subscribe(ctx context.Context, topic string, handler eventbus.EventHandler) (eventbus.Subscription, error) {
	return nil, nil
}

func (m *mockEventBus) Unsubscribe(ctx context.Context, sub eventbus.Subscription) error {
	return nil
}

func (m *mockEventBus) Close() error {
	return nil
}

func TestEventBusCommandEmitter_RequestAudit(t *testing.T) {
	mockBus := &mockEventBus{}
	emitter := NewEventBusCommandEmitter(mockBus)

	ctx := context.Background()
	merchantID := domain.MerchantID("merchant-123")
	jobID := "job-456"
	storeID := "store-789"

	err := emitter.RequestAudit(ctx, merchantID, storeID, jobID)

	require.NoError(t, err)
	assert.Equal(t, 1, len(mockBus.publishedEvents))

	evt := mockBus.publishedEvents[0]
	assert.Equal(t, ucp.CommandRequestAudit, evt.EventType())
	assert.Equal(t, "merchant-123", evt.MerchantID())
	assert.Equal(t, "store-789", evt.StoreID())
}

func TestEventBusCommandEmitter_EmitAuditRequested_PropagatesPublishError(t *testing.T) {
	mockBus := &mockEventBus{publishErr: assert.AnError}
	emitter := NewEventBusCommandEmitter(mockBus)

	ctx := context.Background()
	merchantID := domain.MerchantID("merchant-123")
	jobID := "job-456"
	storeID := "store-789"

	err := emitter.RequestAudit(ctx, merchantID, storeID, jobID)

	assert.Error(t, err)
	assert.Equal(t, assert.AnError, err)
}

func TestEventBusCommandEmitter_EmitRemediationsApplied(t *testing.T) {
	mockBus := &mockEventBus{}
	emitter := NewEventBusCommandEmitter(mockBus)

	ctx := context.Background()
	merchantID := domain.MerchantID("merchant-abc")
	writeBackID := "wb-789"
	remediationIDs := []string{"fix-1", "fix-2", "fix-3"}
	storeID := "store-789"

	err := emitter.ApplyRemediation(ctx, merchantID, storeID, writeBackID, remediationIDs)

	require.NoError(t, err)
	assert.Equal(t, 1, len(mockBus.publishedEvents))

	evt := mockBus.publishedEvents[0]
	assert.Equal(t, ucp.CommandApplyRemediations, evt.EventType())
	assert.Equal(t, "merchant-abc", evt.MerchantID())
	assert.Equal(t, "store-789", evt.StoreID())
}

func TestEventBusCommandEmitter_EmitRemediationsApplied_PropagatesPublishError(t *testing.T) {
	mockBus := &mockEventBus{publishErr: assert.AnError}
	emitter := NewEventBusCommandEmitter(mockBus)

	ctx := context.Background()
	merchantID := domain.MerchantID("merchant-abc")
	storeID := "store-789"
	writeBackID := "wb-789"
	remediationIDs := []string{"fix-1", "fix-2"}

	err := emitter.ApplyRemediation(ctx, merchantID, storeID, writeBackID, remediationIDs)

	assert.Error(t, err)
	assert.Equal(t, assert.AnError, err)
}

func TestAuditRequestedEvent_ImplementsDomainEvent(t *testing.T) {
	evt := NewAuditRequestedEvent("merchant-123", "store-789", "job-456")

	// Assert it can be used as a DomainEvent
	var _ eventbus.DomainEvent = evt

	assert.Equal(t, ucp.CommandRequestAudit, evt.EventType())
	assert.Equal(t, "merchant-123", evt.MerchantID())
	assert.NotNil(t, evt.OccurredAt())
	assert.NotEmpty(t, evt.Payload())
}

func TestRemediationsAppliedEvent_ImplementsDomainEvent(t *testing.T) {
	evt := NewRemediationsAppliedEvent("merchant-abc", "store-789", "wb-789", []string{"fix-1", "fix-2"})

	// Assert it can be used as a DomainEvent
	var _ eventbus.DomainEvent = evt

	assert.Equal(t, ucp.CommandApplyRemediations, evt.EventType())
	assert.Equal(t, "merchant-abc", evt.MerchantID())
	assert.Equal(t, "store-789", evt.StoreID())
	assert.NotNil(t, evt.OccurredAt())
	assert.NotEmpty(t, evt.Payload())
}
