// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package email sends transactional email (member invites, task notifications,
// general control-plane notifications) via Resend in production, or logs
// in-memory in local/test environments. It owns rendering (branded HTML +
// text templates) and transport; callers work with the typed Sender API,
// not raw messages.
package email

import (
	"context"
	"fmt"
)

// Mode selects the transport backend.
type Mode string

const (
	// ModeMock logs the message instead of sending it (dev/test).
	ModeMock Mode = "mock"

	// ModeResend sends the message via the Resend API.
	ModeResend Mode = "resend"
)

// Message is a rendered, transport-ready email.
type Message struct {
	To       string
	From     string
	FromName string
	Subject  string
	HTML     string
	Text     string
}

// Client sends a rendered Message. Implementations are the mock (local/test)
// and Resend (production) transports; construct one via NewClient.
type Client interface {
	Send(ctx context.Context, msg Message) error
}

// Config configures the email transport.
type Config struct {
	// Mode selects the backend (ModeMock or ModeResend). Defaults to ModeMock if empty.
	Mode Mode

	// ResendAPIKey authenticates against the Resend API. Required in ModeResend.
	ResendAPIKey string
}

// NewClient creates a Client for the configured mode. Defaults to ModeMock if
// Mode is empty, so an unconfigured environment fails safe (logs, never sends)
// rather than failing closed.
func NewClient(cfg Config) (Client, error) {
	if cfg.Mode == "" {
		cfg.Mode = ModeMock
	}

	switch cfg.Mode {
	case ModeMock:
		return newMockClient(), nil
	case ModeResend:
		return newResendClient(cfg)
	default:
		return nil, fmt.Errorf("unknown email mode: %s", cfg.Mode)
	}
}
