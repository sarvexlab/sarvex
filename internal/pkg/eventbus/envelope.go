// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package eventbus

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
)

// envelope is the wire format stored inside a Watermill message payload.
// It carries all Event fields so that receivers can reconstruct a full Event
// without needing out-of-band metadata.
type envelope struct {
	EventTypeField  string          `json:"event_type"`
	StoreIDField    string          `json:"store_id"`
	MerchantIDField string          `json:"merchant_id"` // denormalized for backward compat and scope resolution
	OccurredAtField time.Time       `json:"occurred_at"`
	PayloadField    json.RawMessage `json:"payload"`
}

// marshalEvent encodes an Event into a Watermill *message.Message.
// The message UUID is a fresh UUID so Watermill's deduplication logic works correctly.
// StoreID is the Watermill partitioning key for per-store ordering.
func marshalEvent(evt DomainEvent) (*message.Message, error) {
	env := envelope{
		EventTypeField:  evt.EventType(),
		StoreIDField:    evt.StoreID(),
		MerchantIDField: evt.MerchantID(),
		OccurredAtField: evt.OccurredAt(),
		PayloadField:    evt.Payload(),
	}

	raw, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("eventbus: marshal event envelope: %w", err)
	}

	msg := message.NewMessage(uuid.New().String(), raw)
	// Store the event type and store_id in metadata so the router can inspect them without
	// decoding the full payload. store_id is the Watermill partitioning key.
	msg.Metadata.Set("event_type", evt.EventType())
	msg.Metadata.Set("store_id", evt.StoreID())
	msg.Metadata.Set("merchant_id", evt.MerchantID())

	return msg, nil
}

// unmarshalEvent decodes a Watermill message back into an envelopedEvent.
func unmarshalEvent(msg *message.Message) (DomainEvent, error) {
	var env envelope
	if err := json.Unmarshal(msg.Payload, &env); err != nil {
		return nil, fmt.Errorf("eventbus: unmarshal event envelope: %w", err)
	}
	return &envelopedEvent{env: env}, nil
}

// envelopedEvent implements DomainEvent from a decoded envelope.
type envelopedEvent struct {
	env envelope
}

func (e *envelopedEvent) EventType() string     { return e.env.EventTypeField }
func (e *envelopedEvent) StoreID() string       { return e.env.StoreIDField }
func (e *envelopedEvent) MerchantID() string    { return e.env.MerchantIDField }
func (e *envelopedEvent) OccurredAt() time.Time { return e.env.OccurredAtField }
func (e *envelopedEvent) Payload() []byte       { return []byte(e.env.PayloadField) }
