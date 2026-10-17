// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"context"
	"log/slog"
	"net/url"

	ucpvalidator "github.com/sarvexlab/sarvex/internal/pkg/ucp/validator"
	"github.com/sarvexlab/sarvex/internal/platform/woocommerce"
)

// Validator is the WooCommerce implementation of configuration.Validator.
// One Validator instance handles validation for exactly one merchant.
// Thread-safe for concurrent use after construction.
type Validator struct {
	cfg    woocommerce.MerchantConfig
	logger *slog.Logger
}

// NewValidator creates and validates a new WooCommerce validator for one merchant.
func NewValidator(cfg woocommerce.MerchantConfig, logger *slog.Logger) (*Validator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("validator", "woocommerce", "merchant_id", cfg.MerchantID)

	return &Validator{
		cfg:    cfg,
		logger: logger,
	}, nil
}

// Validate validates the WooCommerce configuration.
func (v *Validator) Validate(ctx context.Context) ucpvalidator.ValidationResult {
	result := ucpvalidator.ValidationResult{
		IsValid:  true,
		Errors:   []string{},
		Warnings: []string{},
	}

	// Validate required fields
	if err := v.cfg.Validate(); err != nil {
		result.IsValid = false
		result.Errors = append(result.Errors, err.Error())
		return result
	}

	// Validate store URL format
	if !isValidStoreURL(v.cfg.StoreBaseURL) {
		result.IsValid = false
		result.Errors = append(result.Errors, "store_base_url must be a valid URL (e.g., https://mystore.com)")
	}

	// Validate consumer key/secret length
	if len(v.cfg.ConsumerKey) < 10 {
		result.IsValid = false
		result.Errors = append(result.Errors, "consumer_key must be at least 10 characters")
	}
	if len(v.cfg.ConsumerSecret) < 10 {
		result.IsValid = false
		result.Errors = append(result.Errors, "consumer_secret must be at least 10 characters")
	}

	// Presence is enforced by cfg.Validate() above; only format is checked here.
	if !isValidCurrencyCode(v.cfg.StoreCurrency) {
		result.Warnings = append(result.Warnings, "store_currency appears to be an invalid currency code")
	}

	return result
}

// Compile-time interface satisfaction check
var _ ucpvalidator.Validator = (*Validator)(nil)

// Helper functions

func isValidStoreURL(storeURL string) bool {
	if storeURL == "" {
		return false
	}
	parsedURL, err := url.Parse(storeURL)
	if err != nil {
		return false
	}
	return parsedURL.Scheme != "" && parsedURL.Host != ""
}

func isValidCurrencyCode(code string) bool {
	// Simple check for valid ISO 4217 currency codes (3 uppercase letters)
	if len(code) != 3 {
		return false
	}
	for _, c := range code {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}
