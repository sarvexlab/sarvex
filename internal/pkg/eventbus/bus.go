// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package eventbus

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/ThreeDotsLabs/watermill/message/router/plugin"
)

// deadLetterTopic collects messages that still fail after the Retry
// middleware exhausts its attempts. Without this, a permanently-failing
// handler (e.g. a misconfigured dependency) NACKs forever: the backend
// redelivers immediately, the handler fails again, and the cycle repeats
// at whatever rate the backend allows, flooding the logs.
const deadLetterTopic = "eventbus.dead_letter"

// bus is the concrete EventBus implementation.
type bus struct {
	cfg     Config
	backend *backend

	// router is the Watermill message router that dispatches incoming messages
	// to registered handlers. Each Subscribe call adds a handler; each
	// Unsubscribe cancels that handler's context.
	router *message.Router

	// subs tracks live subscriptions keyed by ID for O(1) Unsubscribe.
	subs   map[string]*subscription
	subsMu sync.RWMutex

	// handlerCancels allows per-handler cancellation without stopping the router.
	handlerCancels   map[string]context.CancelFunc
	handlerCancelsMu sync.Mutex

	routerCtxCancel context.CancelFunc
}

// New creates and starts an EventBus using the provided configuration.
// The returned bus is ready to Publish and Subscribe immediately.
func New(cfg Config) (EventBus, error) {
	cfg.defaults()

	b, err := newBackend(cfg)
	if err != nil {
		return nil, err
	}

	r, err := message.NewRouter(
		message.RouterConfig{},
		cfg.Logger,
	)
	if err != nil {
		return nil, fmt.Errorf("eventbus: create router: %w", err)
	}

	r.AddPlugin(plugin.SignalsHandler)

	// Poison-queue middleware: messages that still fail after Retry exhausts
	// MaxRetries are published to deadLetterTopic and ACKed, rather than
	// NACKed back onto the stream for immediate, endless redelivery. It must
	// be the outermost middleware (added first) so it only sees the error
	// once Retry has given up.
	poisonMiddleware, err := middleware.PoisonQueue(b.publisher, b.topicName(cfg, deadLetterTopic))
	if err != nil {
		return nil, fmt.Errorf("eventbus: create poison queue: %w", err)
	}

	r.AddMiddleware(
		poisonMiddleware,
		middleware.Retry{
			MaxRetries:          5,
			Logger:              cfg.Logger,
			InitialInterval:     500 * time.Millisecond,
			Multiplier:          2,
			MaxInterval:         5 * time.Second,
			RandomizationFactor: 0,
		}.Middleware,
		middleware.Recoverer,
	)

	// CorrelationID propagates the message UUID through handler contexts,
	// useful for distributed tracing.
	r.AddMiddleware(middleware.CorrelationID)

	routerCtx, routerCancel := context.WithCancel(context.Background())

	eb := &bus{
		cfg:             cfg,
		backend:         b,
		router:          r,
		subs:            make(map[string]*subscription),
		handlerCancels:  make(map[string]context.CancelFunc),
		routerCtxCancel: routerCancel,
	}

	// Run the router in a background goroutine. It will not exit until
	// either its context is cancelled (Close) or it encounters a fatal error.
	go func() {
		if err := r.Run(routerCtx); err != nil {
			cfg.Logger.Error("eventbus: router exited", err, watermill.LogFields{})
		}
	}()

	// Block until the router is ready to accept handlers.
	<-r.Running()

	return eb, nil
}

// Publish serialises and sends an event to the backend.
func (b *bus) Publish(ctx context.Context, event DomainEvent) error {
	msg, err := marshalEvent(event)
	if err != nil {
		return err
	}

	topic := b.backend.topicName(b.cfg, event.EventType())

	if err := b.backend.publisher.Publish(topic, msg); err != nil {
		return fmt.Errorf("eventbus: publish to %q: %w", topic, err)
	}

	b.cfg.Logger.Debug("eventbus: published", watermill.LogFields{
		"topic":       topic,
		"message_id":  msg.UUID,
		"merchant_id": event.MerchantID(),
	})
	return nil
}

// Subscribe registers handler for the given topic. The handler runs in its
// own goroutine per message. Multiple subscriptions to the same topic in the
// same process are supported; each gets its own Watermill handler.
func (b *bus) Subscribe(ctx context.Context, topic string, handler EventHandler) (Subscription, error) {
	// Each handler gets a cancellable child context so it can be stopped
	// independently without shutting down the entire router.
	handlerCtx, cancel := context.WithCancel(ctx)

	sub := newSubscription(topic, cancel)
	handlerName := fmt.Sprintf("sub-%s", sub.ID())

	resolvedTopic := b.backend.topicName(b.cfg, topic)

	// AddHandler is thread-safe and can be called after the router is running.
	b.router.AddHandler(
		handlerName,
		resolvedTopic,
		b.backend.subscriber,
		// Handlers do not need to re-publish; we use a no-op publisher.
		"", // output topic (empty = no output)
		noPublisher{},
		func(msg *message.Message) ([]*message.Message, error) {
			// Respect handler cancellation.
			select {
			case <-handlerCtx.Done():
				msg.Nack()
				return nil, nil
			default:
			}

			evt, err := unmarshalEvent(msg)
			if err != nil {
				// Poison message: log and ACK to avoid infinite redelivery of
				// unparseable messages. The dead-letter strategy is to drop
				// unparseable messages rather than block the stream.
				b.cfg.Logger.Error("eventbus: unparseable message dropped", err, watermill.LogFields{
					"message_id": msg.UUID,
					"topic":      resolvedTopic,
				})
				msg.Ack()
				return nil, nil
			}

			if err := handler(msg.Context(), evt); err != nil {
				b.cfg.Logger.Error("eventbus: handler error (will NACK)", err, watermill.LogFields{
					"message_id":  msg.UUID,
					"topic":       resolvedTopic,
					"event_type":  evt.EventType(),
					"merchant_id": evt.MerchantID(),
				})
				return nil, err // returning error causes Watermill to NACK
			}

			return nil, nil // nil return causes Watermill to ACK
		},
	)

	b.subsMu.Lock()
	b.subs[sub.ID()] = sub
	b.subsMu.Unlock()

	b.handlerCancelsMu.Lock()
	b.handlerCancels[sub.ID()] = cancel
	b.handlerCancelsMu.Unlock()

	b.cfg.Logger.Info("eventbus: subscribed", watermill.LogFields{
		"topic":           topic,
		"subscription_id": sub.ID(),
		"consumer_group":  b.cfg.ConsumerGroup,
	})

	if err := b.router.RunHandlers(ctx); err != nil {
		b.subsMu.Lock()
		delete(b.subs, sub.ID())
		b.subsMu.Unlock()

		b.handlerCancelsMu.Lock()
		delete(b.handlerCancels, sub.ID())
		b.handlerCancelsMu.Unlock()

		cancel()
		return nil, fmt.Errorf("eventbus: run handlers: %w", err)
	}

	return sub, nil
}

// Unsubscribe stops the given subscription. In-flight messages currently
// being processed by the handler will complete before the handler exits.
func (b *bus) Unsubscribe(_ context.Context, sub Subscription) error {
	b.subsMu.Lock()
	_, ok := b.subs[sub.ID()]
	if ok {
		delete(b.subs, sub.ID())
	}
	b.subsMu.Unlock()

	if !ok {
		return fmt.Errorf("eventbus: subscription %q not found", sub.ID())
	}

	b.handlerCancelsMu.Lock()
	cancel, ok := b.handlerCancels[sub.ID()]
	if ok {
		cancel()
		delete(b.handlerCancels, sub.ID())
	}
	b.handlerCancelsMu.Unlock()

	b.cfg.Logger.Info("eventbus: unsubscribed", watermill.LogFields{
		"subscription_id": sub.ID(),
		"topic":           sub.Topic(),
	})
	return nil
}

// Close shuts the bus down gracefully, stopping all subscriptions and
// draining in-flight messages.
func (b *bus) Close() error {
	// Cancel all active subscriptions.
	b.handlerCancelsMu.Lock()
	for _, cancel := range b.handlerCancels {
		cancel()
	}
	b.handlerCancelsMu.Unlock()

	// Stop the router (triggers graceful drain).
	b.routerCtxCancel()

	// Close backend connections.
	return b.backend.close()
}

// noPublisher is a placeholder message.Publisher for handlers that only
// consume and do not need to forward messages downstream.
type noPublisher struct{}

func (noPublisher) Publish(_ string, _ ...*message.Message) error { return nil }
func (noPublisher) Close() error                                  { return nil }
