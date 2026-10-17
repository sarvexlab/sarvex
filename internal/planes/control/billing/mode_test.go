// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package billing

import "testing"

func TestNewClient_DefaultsToMock(t *testing.T) {
	c, err := NewClient(ClientConfig{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, ok := c.(*mockStripeClient); !ok {
		t.Fatalf("expected *mockStripeClient for empty mode, got %T", c)
	}
}

func TestNewClient_Mock(t *testing.T) {
	c, err := NewClient(ClientConfig{Mode: ModeMock})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, ok := c.(*mockStripeClient); !ok {
		t.Fatalf("expected *mockStripeClient, got %T", c)
	}
}

func TestNewClient_Stripe_RequiresSecretKey(t *testing.T) {
	if _, err := NewClient(ClientConfig{Mode: ModeStripe}); err == nil {
		t.Fatal("expected error when StripeSecretKey is empty in stripe mode")
	}
}

func TestNewClient_Stripe_WithSecretKey(t *testing.T) {
	c, err := NewClient(ClientConfig{Mode: ModeStripe, StripeSecretKey: "sk_test_key"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, ok := c.(*stripeClientImpl); !ok {
		t.Fatalf("expected *stripeClientImpl, got %T", c)
	}
}

func TestNewClient_UnknownMode(t *testing.T) {
	if _, err := NewClient(ClientConfig{Mode: "carrier-pigeon"}); err == nil {
		t.Fatal("expected error for unknown mode")
	}
}
