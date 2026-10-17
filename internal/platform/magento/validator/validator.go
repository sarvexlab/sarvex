// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"context"
	"log/slog"
	"net/url"

	ucpvalidator "github.com/sarvexlab/sarvex/internal/pkg/ucp/validator"
	"github.com/sarvexlab/sarvex/internal/platform/magento"
)

// Validator is the Magento implementation of configuration.Validator.
type Validator struct {
	cfg    magento.MerchantConfig
	logger *slog.Logger
}

// NewValidator creates and validates a new Magento validator for one merchant.
func NewValidator(cfg magento.MerchantConfig, logger *slog.Logger) (*Validator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("validator", "magento", "merchant_id", cfg.MerchantID)

	return &Validator{
		cfg:    cfg,
		logger: logger,
	}, nil
}

// Validate validates the Magento configuration.
func (v *Validator) Validate(ctx context.Context) ucpvalidator.ValidationResult {
	result := ucpvalidator.ValidationResult{
		IsValid:  true,
		Errors:   []string{},
		Warnings: []string{},
	}

	if err := v.cfg.Validate(); err != nil {
		result.IsValid = false
		result.Errors = append(result.Errors, err.Error())
		return result
	}

	// Validate base URL format
	if !isValidStoreURL(v.cfg.BaseURL) {
		result.IsValid = false
		result.Errors = append(result.Errors, "base_url must be a valid URL")
	}

	// Validate admin user and password length
	if len(v.cfg.AdminUser) < 3 {
		result.IsValid = false
		result.Errors = append(result.Errors, "admin_user must be at least 3 characters")
	}
	if len(v.cfg.AdminPassword) < 6 {
		result.IsValid = false
		result.Errors = append(result.Errors, "admin_password must be at least 6 characters")
	}

	// Validate optional store currency if provided
	if v.cfg.StoreCurrency != "" && !isValidCurrencyCode(v.cfg.StoreCurrency) {
		result.Warnings = append(result.Warnings, "store_currency appears to be an invalid currency code")
	}

	return result
}

var _ ucpvalidator.Validator = (*Validator)(nil)

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
