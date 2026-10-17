// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package billing

import (
	"fmt"
	"net/http"
)

// Mode selects the Stripe transport backend.
type Mode string

const (
	// ModeMock fabricates hosted checkout/portal URLs instead of calling
	// Stripe (dev/test) — no outbound network call, no Stripe account needed.
	ModeMock Mode = "mock"

	// ModeStripe creates real Stripe Checkout Sessions and Billing Portal
	// sessions via the Stripe API.
	ModeStripe Mode = "stripe"
)

// ClientConfig configures the Stripe transport.
type ClientConfig struct {
	// Mode selects the backend (ModeMock or ModeStripe). Defaults to ModeMock if empty.
	Mode Mode

	// StripeSecretKey authenticates against the Stripe API. Required in ModeStripe.
	StripeSecretKey string
}

// NewClient creates a StripeClient for the configured mode. Defaults to
// ModeMock if Mode is empty, so an unconfigured environment fails safe
// (fabricates URLs, never calls Stripe) rather than hitting the live Stripe
// API with an empty key.
func NewClient(cfg ClientConfig) (StripeClient, error) {
	return NewClientWithHTTPClient(cfg, nil)
}

// NewClientWithHTTPClient creates a StripeClient with an explicit HTTP client.
// If httpClient is nil, the Stripe SDK uses its default. Pass an httpClient with
// explicit timeouts to enforce bounded latency.
func NewClientWithHTTPClient(cfg ClientConfig, httpClient *http.Client) (StripeClient, error) {
	if cfg.Mode == "" {
		cfg.Mode = ModeMock
	}

	switch cfg.Mode {
	case ModeMock:
		return newMockStripeClient(), nil
	case ModeStripe:
		if cfg.StripeSecretKey == "" {
			return nil, fmt.Errorf("STRIPE_SECRET_KEY is required in stripe mode")
		}
		return newStripeClientImpl(cfg.StripeSecretKey, httpClient), nil
	default:
		return nil, fmt.Errorf("unknown billing mode: %s", cfg.Mode)
	}
}
