// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
)

// rebuildRequestedPayload is the body of serving.rebuild.requested, matching
// the shape internal/planes/sync/mapping.go's requestRebuild already
// publishes (and internal/planes/sync/service.go's
// handleServingRebuildRequested already consumes generically) — "manual" is
// the existing reason used for remediation-triggered rebuilds; RebuildCache
// uses "operator_requested" so a forced console rebuild is distinguishable
// in logs.
type rebuildRequestedPayload struct {
	Reason   string `json:"reason"`
	Priority bool   `json:"priority"`
}

// newDomainEvent builds an eventbus.DomainEvent for events this package
// publishes (RebuildCache). Redeclared per-package rather than shared across
// planes, matching the existing convention in control/connector's
// connector_service.go.
func newDomainEvent(eventType, merchantID, storeID string, payload []byte) eventbus.DomainEvent {
	return &domainEvent{
		eventType:  eventType,
		merchantID: merchantID,
		storeID:    storeID,
		occurredAt: time.Now().UTC(),
		payload:    payload,
	}
}

type domainEvent struct {
	eventType  string
	merchantID string
	storeID    string
	occurredAt time.Time
	payload    []byte
}

func (e *domainEvent) EventType() string     { return e.eventType }
func (e *domainEvent) MerchantID() string    { return e.merchantID }
func (e *domainEvent) StoreID() string       { return e.storeID }
func (e *domainEvent) OccurredAt() time.Time { return e.occurredAt }
func (e *domainEvent) Payload() []byte       { return e.payload }
