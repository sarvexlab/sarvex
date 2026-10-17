// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package paymenthandler

// StripeConfig represents Stripe payment handler configuration.
// NOTE: This struct is based on the UCP framework template (environment + business_id).
// Stripe has not yet published an official UCP payment handler specification.
// Vendor-specific fields will be added once the spec is publicly available.
//
// Idempotency: Connectors (WooCommerce, Magento) that use Stripe should inject
// the idempotency key from CompleteCheckoutRequest.IdempotencyKey as the
// Idempotency-Key header when calling Stripe's payment API. This ensures
// Stripe's own exactly-once semantics catch duplicate charge attempts.
type StripeConfig struct {
	Environment string `json:"environment" validate:"required,oneof=sandbox production"`
	BusinessID  string `json:"business_id" validate:"required"`
	// TODO: Add Stripe-specific fields pending official UCP handler specification
}
