// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"context"
	"log/slog"
	"strings"

	ucpvalidator "github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/validator"
	"github.com/sarvexlabs/sarvex.cp/internal/platform/frozen/shopify"
)

// Validator is the Shopify implementation of configuration.Validator.
// One Validator instance handles validation for exactly one merchant.
// Thread-safe for concurrent use after construction.
type Validator struct {
	cfg    shopify.MerchantConfig
	logger *slog.Logger
}

// NewValidator creates and validates a new Shopify validator for one merchant.
func NewValidator(cfg shopify.MerchantConfig, logger *slog.Logger) (*Validator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("validator", "shopify", "merchant_id", cfg.MerchantID)

	return &Validator{
		cfg:    cfg,
		logger: logger,
	}, nil
}

// Validate validates the Shopify configuration.
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

	// Validate shop domain format
	if !isValidShopDomain(v.cfg.ShopDomain) {
		result.IsValid = false
		result.Errors = append(result.Errors, "shop_domain must be in format 'myshop.myshopify.com'")
	}

	// Validate token formats
	if !isValidStorefrontToken(v.cfg.StorefrontAccessToken) {
		result.Warnings = append(result.Warnings, "storefront_access_token format looks invalid")
	}
	if !isValidAdminToken(v.cfg.AdminAccessToken) {
		result.Warnings = append(result.Warnings, "admin_access_token format looks invalid")
	}

	// Validate webhook secret length
	if len(v.cfg.WebhookSecret) < 16 {
		result.IsValid = false
		result.Errors = append(result.Errors, "webhook_secret must be at least 16 characters")
	}

	// Validate optional store currency if provided
	if v.cfg.StoreCurrency != "" && !isValidCurrencyCode(v.cfg.StoreCurrency) {
		result.Warnings = append(result.Warnings, "store_currency appears to be an invalid currency code")
	}

	return result
}

// Compile-time interface satisfaction check
var _ ucpvalidator.Validator = (*Validator)(nil)

// Helper functions

func isValidShopDomain(domain string) bool {
	if domain == "" {
		return false
	}
	return strings.HasSuffix(domain, ".myshopify.com")
}

func isValidStorefrontToken(token string) bool {
	// Shopify storefront tokens are typically 32 character hex strings
	if len(token) < 20 {
		return false
	}
	return true
}

func isValidAdminToken(token string) bool {
	// Shopify admin tokens typically start with "shpat_" and are at least 32 chars
	if len(token) < 10 {
		return false
	}
	return true
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
