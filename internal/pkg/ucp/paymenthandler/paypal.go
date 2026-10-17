// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package paymenthandler

// PayPalConfig represents PayPal payment handler configuration.
// NOTE: This struct is based on the UCP framework template (environment + business_id).
// PayPal has not yet published an official UCP payment handler specification.
// Vendor-specific fields will be added once the spec is publicly available.
type PayPalConfig struct {
	Environment string `json:"environment" validate:"required,oneof=sandbox production"`
	BusinessID  string `json:"business_id" validate:"required"`
	// TODO: Add PayPal-specific fields pending official UCP handler specification
}
