// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package task

import (
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
)

// taskUpdatedPayload is the event payload for control.task.updated. Thin
// signal only — consumers (the console-realtime hub) decide relevance and
// fan it out to WebSocket clients as a cue to re-fetch the task, not as an
// authoritative copy of it.
type taskUpdatedPayload struct {
	TaskID  string `json:"task_id"`
	StoreID string `json:"store_id"`
	State   string `json:"state"`
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
