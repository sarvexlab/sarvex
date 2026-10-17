// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package email

import "testing"

func TestNewClient_DefaultsToMock(t *testing.T) {
	c, err := NewClient(Config{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, ok := c.(*mockClient); !ok {
		t.Fatalf("expected *mockClient for empty mode, got %T", c)
	}
}

func TestNewClient_Mock(t *testing.T) {
	c, err := NewClient(Config{Mode: ModeMock})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, ok := c.(*mockClient); !ok {
		t.Fatalf("expected *mockClient, got %T", c)
	}
}

func TestNewClient_Resend_RequiresAPIKey(t *testing.T) {
	if _, err := NewClient(Config{Mode: ModeResend}); err == nil {
		t.Fatal("expected error when ResendAPIKey is empty in resend mode")
	}
}

func TestNewClient_Resend_WithAPIKey(t *testing.T) {
	c, err := NewClient(Config{Mode: ModeResend, ResendAPIKey: "re_test_key"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, ok := c.(*resendClient); !ok {
		t.Fatalf("expected *resendClient, got %T", c)
	}
}

func TestNewClient_UnknownMode(t *testing.T) {
	if _, err := NewClient(Config{Mode: "carrier-pigeon"}); err == nil {
		t.Fatal("expected error for unknown mode")
	}
}
