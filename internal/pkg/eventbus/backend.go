// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package eventbus

import (
	"fmt"

	"github.com/ThreeDotsLabs/watermill/message"
)

// backend groups a Watermill publisher and subscriber pair.
type backend struct {
	publisher  message.Publisher
	subscriber message.Subscriber
}

// NATSBackendFactory constructs the Watermill publisher/subscriber pair
// backed by NATS JetStream. It is an indirection so that a consumer that
// only needs the EventBus interface is not forced to pull in
// github.com/nats-io/nats.go. A consumer that needs the bus to actually
// work at runtime must blank-import eventbus/natsjetstream so this factory
// gets registered.
type NATSBackendFactory func(cfg Config) (message.Publisher, message.Subscriber, error)

var natsBackendFactory NATSBackendFactory

// RegisterNATSBackend installs the NATS JetStream backend factory. Called
// from eventbus/natsjetstream's init().
func RegisterNATSBackend(f NATSBackendFactory) {
	natsBackendFactory = f
}

// newBackend constructs the NATS JetStream publisher/subscriber pair.
func newBackend(cfg Config) (*backend, error) {
	if natsBackendFactory == nil {
		return nil, fmt.Errorf("eventbus: the NATS JetStream backend is not registered; blank-import github.com/sarvexlab/sarvex/internal/pkg/eventbus/natsjetstream")
	}
	pub, sub, err := natsBackendFactory(cfg)
	if err != nil {
		return nil, err
	}
	return &backend{publisher: pub, subscriber: sub}, nil
}

// topicName applies the configured TopicPrefix to a raw topic string.
func (b *backend) topicName(cfg Config, raw string) string {
	if cfg.TopicPrefix != "" {
		return cfg.TopicPrefix + raw
	}
	return raw
}

// close shuts down both publisher and subscriber.
func (b *backend) close() error {
	pubErr := b.publisher.Close()
	subErr := b.subscriber.Close()
	if pubErr != nil {
		return pubErr
	}
	return subErr
}
