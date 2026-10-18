// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package billing

import "errors"

var (
	// ErrNotFound is returned when no billing record exists for a merchant.
	ErrNotFound = errors.New("billing record not found")
	// ErrNoStripeCustomer is returned when a portal link is requested but the
	// merchant has not yet completed a Stripe checkout.
	ErrNoStripeCustomer = errors.New("no stripe customer for merchant")
	// ErrUnknownPlan is returned when a requested plan has no configured Stripe price.
	ErrUnknownPlan = errors.New("unknown plan")
	// ErrInvalidWebhookSignature is returned when a Stripe webhook request's
	// Stripe-Signature header fails verification against the configured secret.
	ErrInvalidWebhookSignature = errors.New("invalid stripe webhook signature")
)
