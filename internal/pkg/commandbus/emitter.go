// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package commandbus

import (
	"context"

	"github.com/sarvexlab/sarvex/internal/pkg/domain"
	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
)

// CommandEmitter sends cross-plane commands from the control plane to the sync plane.
// Implementations emit commands as events to the EventBus for cross-plane coordination.
type CommandEmitter interface {
	// RequestAudit tells the sync plane to re-score this merchant's catalog.
	RequestAudit(ctx context.Context, merchantID domain.MerchantID, storeID string, jobID string) error

	// ApplyRemediation tells the sync plane to apply the given write-back:
	// push ToValue fixes to product records, re-score, and write back to the store.
	ApplyRemediation(ctx context.Context, merchantID domain.MerchantID, storeID string, writeBackID string, remediationIDs []string) error

	// RevertRemediation tells the sync plane to undo the given write-back:
	// restore each applied record's prior value on the product and in the
	// store, reopen the record, and re-score.
	RevertRemediation(ctx context.Context, merchantID domain.MerchantID, storeID string, writeBackID string, remediationIDs []string) error
}

// EventBusCommandEmitter implements CommandEmitter by emitting commands as events
// to the EventBus. This allows the sync plane to subscribe to commands and act on them.
// Non-blocking: publish is fire-and-forget (errors returned synchronously to caller).
type EventBusCommandEmitter struct {
	bus eventbus.EventBus
}

// NewEventBusCommandEmitter creates a command emitter backed by an EventBus.
func NewEventBusCommandEmitter(bus eventbus.EventBus) CommandEmitter {
	return &EventBusCommandEmitter{bus: bus}
}

// RequestAudit tells the sync plane to re-score this merchant's catalog.
// Publishes ucp.CommandRequestAudit to the event bus.
func (e *EventBusCommandEmitter) RequestAudit(ctx context.Context, merchantID domain.MerchantID, storeID string, jobID string) error {
	evt := NewAuditRequestedEvent(string(merchantID), storeID, jobID)
	return e.bus.Publish(ctx, evt)
}

// ApplyRemediation tells the sync plane to apply the given write-back:
// push ToValue fixes to product records, re-score, and write back to the store.
// Publishes ucp.CommandApplyRemediations to the event bus.
func (e *EventBusCommandEmitter) ApplyRemediation(ctx context.Context, merchantID domain.MerchantID, storeID string, writeBackID string, remediationIDs []string) error {
	evt := NewRemediationsAppliedEvent(string(merchantID), storeID, writeBackID, remediationIDs)
	return e.bus.Publish(ctx, evt)
}

// RevertRemediation publishes ucp.CommandRevertRemediations.
func (e *EventBusCommandEmitter) RevertRemediation(ctx context.Context, merchantID domain.MerchantID, storeID string, writeBackID string, remediationIDs []string) error {
	evt := NewRemediationsRevertedEvent(string(merchantID), storeID, writeBackID, remediationIDs)
	return e.bus.Publish(ctx, evt)
}

var _ CommandEmitter = (*EventBusCommandEmitter)(nil)
