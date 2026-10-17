// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package email

import (
	"context"
	"testing"
)

func TestMockClient_SendCapturesMessage(t *testing.T) {
	c := newMockClient()
	msg := Message{To: "a@example.com", From: "from@sarvex.example.com", Subject: "hi", HTML: "<p>hi</p>", Text: "hi"}

	if err := c.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	sent := c.Sent()
	if len(sent) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(sent))
	}
	if sent[0] != msg {
		t.Fatalf("captured message = %+v, want %+v", sent[0], msg)
	}
}

func TestMockClient_SendNeverErrors(t *testing.T) {
	c := newMockClient()
	for i := 0; i < 3; i++ {
		if err := c.Send(context.Background(), Message{To: "a@example.com"}); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}
	if len(c.Sent()) != 3 {
		t.Fatalf("expected 3 sent messages, got %d", len(c.Sent()))
	}
}
