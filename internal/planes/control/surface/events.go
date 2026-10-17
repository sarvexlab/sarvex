// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package surface

import (
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
)

// StateChangedPayload is the event payload for
// control.store.surface_state_changed.
//
// SurfaceID is what makes this event useful to a consumer that cares about
// exactly one surface: an artifact worker gates on its own surface and ignores
// every other surface's transitions without having to load anything.
type StateChangedPayload struct {
	StoreID   string `json:"store_id"`
	SurfaceID string `json:"surface_id"`
	FromState string `json:"from_state"`
	ToState   string `json:"to_state"`
	Event     string `json:"event"`
}

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
