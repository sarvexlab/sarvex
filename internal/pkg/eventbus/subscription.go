// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package eventbus

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// subscription is the concrete implementation of Subscription.
type subscription struct {
	id     string
	topic  string
	cancel context.CancelFunc // cancels this subscription's handler goroutine
}

func newSubscription(topic string, cancel context.CancelFunc) *subscription {
	return &subscription{
		id:     fmt.Sprintf("%s-%s", topic, uuid.New().String()),
		topic:  topic,
		cancel: cancel,
	}
}

func (s *subscription) Topic() string { return s.topic }
func (s *subscription) ID() string    { return s.id }

// Close stops this subscription by cancelling its context.
func (s *subscription) Close() error {
	s.cancel()
	return nil
}
