// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package email

import (
	"context"
	"log/slog"
	"sync"
)

// mockClient logs messages instead of sending them and keeps the most recent
// ones in memory, so local/test environments can run the real send path
// without an outbound network call.
type mockClient struct {
	mu   sync.Mutex
	sent []Message
}

func newMockClient() *mockClient {
	return &mockClient{}
}

func (c *mockClient) Send(_ context.Context, msg Message) error {
	c.mu.Lock()
	c.sent = append(c.sent, msg)
	c.mu.Unlock()

	slog.Info("email (mock mode, not sent)",
		"to", msg.To,
		"from", msg.From,
		"subject", msg.Subject,
	)
	return nil
}

// Sent returns the messages sent so far, oldest first. Test helper.
func (c *mockClient) Sent() []Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Message, len(c.sent))
	copy(out, c.sent)
	return out
}
