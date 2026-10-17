// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package base_test

import (
	"strings"
	"testing"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/commerce/model"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/connectors/base"
)

// ─── C4: requires_escalation without continue_url is unconstructable ──────────

func TestNewEscalationResponse_RejectsEmptyContinueURL(t *testing.T) {
	_, err := base.NewEscalationResponse("", []model.Message{
		model.NewErrorMessage(model.MessageError{
			Code:     "test",
			Content:  "test",
			Severity: model.MessageSeverityRequiresBuyerInput,
		}),
	})
	if err == nil {
		t.Fatal("expected error when continue_url is empty, got nil")
	}
	if !strings.Contains(err.Error(), "continue_url") {
		t.Fatalf("error message should mention continue_url, got: %v", err)
	}
}

func TestNewEscalationResponse_RejectsNoMessages(t *testing.T) {
	_, err := base.NewEscalationResponse("https://shop.example.com/checkout", nil)
	if err == nil {
		t.Fatal("expected error when messages slice is nil, got nil")
	}
}

func TestNewEscalationResponse_RejectsOnlyRecoverableMessages(t *testing.T) {
	_, err := base.NewEscalationResponse("https://shop.example.com/checkout", []model.Message{
		model.NewErrorMessage(model.MessageError{
			Code:     "payment_failed",
			Content:  "card declined",
			Severity: model.MessageSeverityRecoverable,
		}),
	})
	if err == nil {
		t.Fatal("expected error when no qualifying severity present, got nil")
	}
}

func TestNewEscalationResponse_RejectsOnlyUnrecoverableMessages(t *testing.T) {
	_, err := base.NewEscalationResponse("https://shop.example.com/checkout", []model.Message{
		model.NewErrorMessage(model.MessageError{
			Code:     "item_unavailable",
			Content:  "item no longer available",
			Severity: model.MessageSeverityUnrecoverable,
		}),
	})
	if err == nil {
		t.Fatal("expected error when only unrecoverable severity present, got nil")
	}
}

func TestNewEscalationResponse_AcceptsRequiresBuyerInput(t *testing.T) {
	resp, err := base.NewEscalationResponse("https://shop.example.com/checkout", []model.Message{
		model.NewErrorMessage(model.MessageError{
			Code:     "payment_step_up",
			Content:  "3DS authentication required",
			Severity: model.MessageSeverityRequiresBuyerInput,
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != base.CheckoutStatusRequiresEscalation {
		t.Fatalf("expected status %q, got %q", base.CheckoutStatusRequiresEscalation, resp.Status)
	}
	if resp.ContinueURL != "https://shop.example.com/checkout" {
		t.Fatalf("expected ContinueURL %q, got %q", "https://shop.example.com/checkout", resp.ContinueURL)
	}
	if len(resp.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(resp.Messages))
	}
}

func TestNewEscalationResponse_AcceptsRequiresBuyerReview(t *testing.T) {
	resp, err := base.NewEscalationResponse("https://shop.example.com/checkout", []model.Message{
		model.NewErrorMessage(model.MessageError{
			Code:     "buyer_review_required",
			Content:  "please review your order",
			Severity: model.MessageSeverityRequiresBuyerReview,
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != base.CheckoutStatusRequiresEscalation {
		t.Fatalf("expected status %q, got %q", base.CheckoutStatusRequiresEscalation, resp.Status)
	}
}

func TestNewEscalationResponse_AcceptsMixedMessages(t *testing.T) {
	_, err := base.NewEscalationResponse("https://shop.example.com/checkout", []model.Message{
		model.NewErrorMessage(model.MessageError{
			Code:     "other",
			Content:  "other",
			Severity: model.MessageSeverityRecoverable,
		}),
		model.NewErrorMessage(model.MessageError{
			Code:     "step_up",
			Content:  "3DS",
			Severity: model.MessageSeverityRequiresBuyerInput,
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
