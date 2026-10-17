// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package activation

import (
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
)

// StateChangedPayload is the event payload for
// control.store.activation_state_changed.
//
// It carries both ends of the move, not just the new state: health,
// analytics and the surface service all care about *direction* — entering
// active is what auto-enrolls direct_agent, leaving it is what a consumer
// reacts to — and reconstructing that from a bare new-state event would
// mean every consumer keeping its own copy of the previous value.
type StateChangedPayload struct {
	StoreID   string `json:"store_id"`
	FromState string `json:"from_state"`
	ToState   string `json:"to_state"`
	Signal    string `json:"signal"`
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
