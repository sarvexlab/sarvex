// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package eventbus provides a publisher/subscriber event bus backed by
// Watermill and NATS JetStream — local development, CI and production all
// run the same backend. It guarantees at-least-once delivery per
// subscription.
package eventbus

import (
	"context"
	"time"
)

// DomainEvent is the message contract every domain event must satisfy.
type DomainEvent interface {
	// EventID() string
	// AggregateID() string
	// EventType returns the logical name of the event, used as the routing topic.
	// Example: "merchant.created", "payment.processed"
	EventType() string

	// StoreID partitions the event for per-store ordering and causality.
	// The event bus uses this as the ordering key (Watermill partitioning key).
	StoreID() string

	// MerchantID scopes the event to the merchant (denormalized for query and scope resolution).
	MerchantID() string

	// OccurredAt is the wall-clock time the event happened in the domain.
	OccurredAt() time.Time

	// Payload is the JSON-serialised body of the event.
	Payload() []byte
}

// EventHandler is the function signature subscribers must implement.
// Returning a non-nil error causes the message to be NACKed and redelivered
// (at-least-once guarantee). Returning nil ACKs the message.
type EventHandler func(ctx context.Context, event DomainEvent) error

// Subscription is an opaque handle returned by Subscribe.
// Pass it to Unsubscribe to stop receiving events.
type Subscription interface {
	// Topic returns the event type this subscription listens on.
	Topic() string

	// ID is a unique identifier for this subscription within the bus.
	ID() string

	// Close stops this subscription. Equivalent to calling Unsubscribe.
	Close() error
}

// EventBus is the primary interface for publishing and subscribing to events.
// A single EventBus instance may be used as both publisher and subscriber.
type EventBus interface {
	// Publish sends an event to all subscribers of that event's EventType.
	// The call blocks until the backend has accepted the message.
	Publish(ctx context.Context, event DomainEvent) error

	// Subscribe registers handler to receive events of the given topic.
	// The handler is called in its own goroutine for each message.
	// Multiple calls with the same topic create independent subscribers;
	// use the same consumerGroup across process instances to share load.
	Subscribe(ctx context.Context, topic string, handler EventHandler) (Subscription, error)

	// Unsubscribe stops the given subscription from receiving further events.
	Unsubscribe(ctx context.Context, sub Subscription) error

	// Close shuts down the bus, draining in-flight messages gracefully.
	Close() error
}
