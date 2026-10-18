// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package eventbus

import (
	"context"
)

// NoopEventBus satisfies EventBus with no-op implementations.
// Used in control plane until the real event bus is available.
type NoopEventBus struct{}

func (NoopEventBus) Publish(_ context.Context, _ DomainEvent) error {
	return nil
}

func (NoopEventBus) Subscribe(_ context.Context, _ string, _ EventHandler) (Subscription, error) {
	return &noopSubscription{}, nil
}

func (NoopEventBus) Unsubscribe(_ context.Context, _ Subscription) error {
	return nil
}

func (NoopEventBus) Close() error {
	return nil
}

var _ EventBus = NoopEventBus{}

type noopSubscription struct{}

func (n *noopSubscription) Topic() string {
	return ""
}

func (n *noopSubscription) ID() string {
	return ""
}

func (n *noopSubscription) Close() error {
	return nil
}

var _ Subscription = (*noopSubscription)(nil)
