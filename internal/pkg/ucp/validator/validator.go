// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"context"

	"github.com/sarvexlab/sarvex/internal/pkg/domain"
)

// ValidationResult represents the result of a configuration validation.
type ValidationResult struct {
	IsValid  bool
	Errors   []string
	Warnings []string
}

// Validator defines the interface for validating UCP instance configurations
// for a specific commerce platform (Shopify, WooCommerce, etc.)
// Implementations MUST be safe for concurrent use.
type Validator interface {
	// Validate validates the entire configuration for the UCP instance.
	// It checks required fields, data types, and field ranges.
	// Returns a ValidationResult with all errors and warnings found.
	Validate(ctx context.Context) ValidationResult
}

// MerchantConfig carries the information needed to instantiate a validator.
// The Config field contains the platform-specific configuration.
type MerchantConfig struct {
	MerchantID string
	StoreID    string
	Platform   domain.CommerceSystemType
	Config     any // platform-specific config struct
}
