// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package natsjetstream provides the NATS JetStream backend for
// internal/pkg/eventbus — chosen because JetStream superclusters/leaf
// nodes federate across regions and cloud providers natively (see
// deployments/selfhost/scripts/bootstrap-nats-leaf.sh).
//
// It is kept out of the base eventbus package so that a consumer that only
// needs the EventBus interface is not forced to pull in
// github.com/nats-io/nats.go. Anything that actually needs the bus to work
// at runtime must blank-import this package so its init() registers the backend factory:
//
//	import _ "github.com/sarvexlab/sarvex/internal/pkg/eventbus/natsjetstream"
package natsjetstream

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	wmnats "github.com/ThreeDotsLabs/watermill-nats/v2/pkg/nats"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/nats-io/nats.go"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
)

func init() {
	eventbus.RegisterNATSBackend(newNATSBackend)
}

// newNATSBackend builds a Watermill publisher and subscriber backed by NATS
// JetStream. Each topic maps to a JetStream stream/subject (dots are valid
// NATS subject-hierarchy separators, so event_types.go's dotted topic names
// — e.g. "control.connector.resync_requested" — need no translation);
// durable consumer names cannot contain dots, so the durable calculator
// below substitutes underscores there only ("%s--%s" of ConsumerGroup and
// topic).
//
// Stream names cannot contain dots either, and the Watermill NATS library
// names an auto-provisioned stream after the raw topic — which fails for
// every dotted topic. So the library's AutoProvision is left off and
// streamEnsurer creates one stream per topic (sanitized name, the topic as
// its only subject) before the first publish or subscribe on it.
func newNATSBackend(cfg eventbus.Config) (message.Publisher, message.Subscriber, error) {
	pub, err := wmnats.NewPublisher(
		wmnats.PublisherConfig{
			URL: cfg.NATS.URL,
			JetStream: wmnats.JetStreamConfig{
				TrackMsgID: true, // Nats-Msg-Id = message UUID, dedupes retried publishes
			},
		},
		cfg.Logger,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("eventbus/natsjetstream: publisher: %w", err)
	}

	sub, err := wmnats.NewSubscriber(
		wmnats.SubscriberConfig{
			URL:              cfg.NATS.URL,
			QueueGroupPrefix: cfg.ConsumerGroup,
			SubscribersCount: 1,
			JetStream: wmnats.JetStreamConfig{
				DurablePrefix: cfg.ConsumerGroup,
				// A bare DurablePrefix would collide across topics (the
				// same durable name would be reused for every Subscribe
				// call in this ConsumerGroup) — combine it with the topic.
				DurableCalculator: func(prefix, topic string) string {
					return prefix + "--" + strings.ReplaceAll(topic, ".", "_")
				},
			},
		},
		cfg.Logger,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("eventbus/natsjetstream: subscriber: %w", err)
	}

	ensurer, err := newStreamEnsurer(cfg.NATS.URL)
	if err != nil {
		return nil, nil, err
	}

	return &ensuringPublisher{Publisher: pub, ensurer: ensurer},
		&ensuringSubscriber{Subscriber: sub, ensurer: ensurer}, nil
}

// streamName maps a topic to a valid JetStream stream name.
func streamName(topic string) string {
	return strings.NewReplacer(".", "_", "*", "_", ">", "_", " ", "_", "/", "_").Replace(topic)
}

// streamEnsurer lazily creates the JetStream stream backing each topic.
type streamEnsurer struct {
	conn *nats.Conn
	js   nats.JetStreamManager

	mu   sync.Mutex
	done map[string]struct{}
}

func newStreamEnsurer(url string) (*streamEnsurer, error) {
	conn, err := nats.Connect(url, nats.Name("sarvex-eventbus-streams"))
	if err != nil {
		return nil, fmt.Errorf("eventbus/natsjetstream: connect: %w", err)
	}
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("eventbus/natsjetstream: jetstream: %w", err)
	}
	return &streamEnsurer{conn: conn, js: js, done: map[string]struct{}{}}, nil
}

func (e *streamEnsurer) ensure(topic string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.done[topic]; ok {
		return nil
	}
	name := streamName(topic)
	if _, err := e.js.StreamInfo(name); err != nil {
		if !errors.Is(err, nats.ErrStreamNotFound) {
			return fmt.Errorf("eventbus/natsjetstream: stream %q: %w", name, err)
		}
		// Another instance may create it first; that is fine.
		if _, err := e.js.AddStream(&nats.StreamConfig{Name: name, Subjects: []string{topic}}); err != nil &&
			!errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
			return fmt.Errorf("eventbus/natsjetstream: create stream %q: %w", name, err)
		}
	}
	e.done[topic] = struct{}{}
	return nil
}

func (e *streamEnsurer) close() { e.conn.Close() }

type ensuringPublisher struct {
	message.Publisher
	ensurer *streamEnsurer
}

func (p *ensuringPublisher) Publish(topic string, messages ...*message.Message) error {
	if err := p.ensurer.ensure(topic); err != nil {
		return err
	}
	return p.Publisher.Publish(topic, messages...)
}

func (p *ensuringPublisher) Close() error {
	err := p.Publisher.Close()
	p.ensurer.close()
	return err
}

type ensuringSubscriber struct {
	message.Subscriber
	ensurer *streamEnsurer
}

func (s *ensuringSubscriber) Subscribe(ctx context.Context, topic string) (<-chan *message.Message, error) {
	if err := s.ensurer.ensure(topic); err != nil {
		return nil, err
	}
	return s.Subscriber.Subscribe(ctx, topic)
}

func (s *ensuringSubscriber) Close() error {
	err := s.Subscriber.Close()
	s.ensurer.close()
	return err
}
