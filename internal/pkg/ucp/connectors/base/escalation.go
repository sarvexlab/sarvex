// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package base

import (
	"errors"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
)

// NewEscalationResponse constructs a CheckoutResponse with status requires_escalation.
//
// Enforces R2: continueURL must not be empty — a buyer cannot resume a purchase
// without a URL to redirect to.
//
// Enforces R3: at least one message must carry severity "requires_buyer_input" or
// "requires_buyer_review" so the platform knows why escalation is required and what
// action the buyer must take on the merchant site.
//
// Both violations are hard errors; callers must fail loudly rather than silently
// emit a non-conformant response.
func NewEscalationResponse(continueURL string, messages []model.Message) (*CheckoutResponse, error) {
	if continueURL == "" {
		return nil, errors.New("requires_escalation: continue_url must not be empty (R2)")
	}
	hasQualifying := false
	for _, m := range messages {
		if me, ok := m.AsError(); ok {
			if me.Severity.RequiresEscalation() {
				hasQualifying = true
				break
			}
		}
	}
	if !hasQualifying {
		return nil, errors.New("requires_escalation: at least one message must have severity requires_buyer_input or requires_buyer_review (R3)")
	}
	return &CheckoutResponse{
		Status:      CheckoutStatusRequiresEscalation,
		ContinueURL: continueURL,
		Messages:    messages,
	}, nil
}
