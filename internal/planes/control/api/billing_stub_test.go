// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"

	"github.com/sarvexlab/sarvex/internal/planes/control/billing"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// activeBillingStub is billing.StubService with Get overridden to report an
// active subscription, so handler tests exercising mutating routes aren't
// blocked by requireActiveBilling's billing_read_only gate. Every other
// method still returns ErrNotImplemented — no test in this package outside
// billing_test.go exercises those.
type activeBillingStub struct {
	billing.StubService
}

func (activeBillingStub) Get(_ context.Context, _ domain.MerchantID) (domain.Billing, error) {
	return domain.Billing{Status: domain.BillingActive}, nil
}
