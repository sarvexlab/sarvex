// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package billing manages Stripe-backed subscription state and hosted links.
package billing

import (
	"context"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// Service is the provider-authoritative billing surface.
type Service interface {
	Get(ctx context.Context, id domain.MerchantID) (domain.Billing, error)
	// CreatePortalLink creates a Stripe Customer Portal session — plan
	// switching (self_serve is never a switch target) and cancellation
	// live here, alongside payment-method and invoice management. See
	// BillingService.CreatePortalLink's doc comment for why there is no
	// separate Checkout-based upgrade endpoint.
	CreatePortalLink(ctx context.Context, id domain.MerchantID) (domain.HostedURL, error)

	GetNotificationPreferences(ctx context.Context, id domain.MerchantID) (domain.NotificationPreferences, error)
	SetNotificationPreferences(ctx context.Context, id domain.MerchantID, prefs domain.NotificationPreferences) (domain.NotificationPreferences, error)

	// ProvisionTrial creates the Stripe customer and a trialing subscription on
	// the entry-level plan for a newly created merchant, and persists the
	// resulting billing record. Called once, synchronously, from account
	// creation — see account.TrialProvisioner.
	ProvisionTrial(ctx context.Context, id domain.MerchantID, email string) error

	// EnsurePortalConfiguration reconciles the Stripe Billing Portal
	// Configuration every portal session is scoped to, returning its ID.
	// Called once at control-plane startup.
	EnsurePortalConfiguration(ctx context.Context) (string, error)

	// HandleStripeWebhook verifies and processes a Stripe webhook request,
	// syncing subscription state (customer/subscription ID, plan, status)
	// back into the billing record. Returns ErrInvalidWebhookSignature if
	// sigHeader fails verification.
	HandleStripeWebhook(ctx context.Context, payload []byte, sigHeader string) error
}
