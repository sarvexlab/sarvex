// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package paymenthandler

// BraintreeConfig represents Braintree payment handler configuration.
// NOTE: This struct is based on the UCP framework template (environment + business_id).
// Braintree has not yet published an official UCP payment handler specification.
// Vendor-specific fields will be added once the spec is publicly available.
type BraintreeConfig struct {
	Environment string `json:"environment" validate:"required,oneof=sandbox production"`
	BusinessID  string `json:"business_id" validate:"required"`
	// TODO: Add Braintree-specific fields pending official UCP handler specification
}
