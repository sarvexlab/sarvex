// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package magento

import (
	"encoding/json"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/configs"
)

// MerchantConfig holds the merchant-specific Magento configuration.
type MerchantConfig struct {
	MerchantID        string                    `json:"merchant_id"`
	StoreID           string                    `json:"store_id"`
	BaseURL           string                    `json:"base_url"` // e.g., "https://mystore.magentosite.cloud"
	AdminUser         string                    `json:"admin_user"`
	AdminPassword     string                    `json:"admin_password"`
	StoreCode         string                    `json:"store_code"` // e.g., "default"
	StoreCurrency     string                    `json:"store_currency"`
	PaymentMethodCode string                    `json:"payment_method_code"` // e.g., "checkmo" for check/money order
	WebhookSecret     string                    `json:"webhook_secret"`
	ProfileCacheTTL   time.Duration             `json:"profile_cache_ttl"`
	TokenTTL          time.Duration             `json:"token_ttl"`
	TokenTTLBuffer    time.Duration             `json:"token_ttl_buffer"` // Buffer before token expires to refresh early
	RequestTimeout    time.Duration             `json:"request_timeout"`
	MaxRetries        int                       `json:"max_retries"`
	APIOverrideBase   string                    `json:"api_override_base"` // For testing against mock servers
	Platform          domain.CommerceSystemType `json:"platform"`          // Schema-specific platform identifier
}

const (
	DefaultProfileCacheTTL = 5 * time.Minute
	DefaultTokenTTL        = 4 * time.Hour
	DefaultTokenTTLBuffer  = 60 * time.Second
	DefaultRequestTimeout  = 30 * time.Second
	DefaultMaxRetries      = 3
)

// Validate validates the required fields in the configuration.
func (cfg MerchantConfig) Validate() error {
	if cfg.MerchantID == "" {
		return errMissingField("merchant_id")
	}
	if cfg.BaseURL == "" {
		return errMissingField("base_url")
	}
	if cfg.AdminUser == "" {
		return errMissingField("admin_user")
	}
	if cfg.AdminPassword == "" {
		return errMissingField("admin_password")
	}
	if cfg.StoreCode == "" {
		return errMissingField("store_code")
	}
	if cfg.StoreCurrency == "" {
		return errMissingField("store_currency")
	}
	if cfg.WebhookSecret == "" {
		return errMissingField("webhook_secret")
	}
	return nil
}

// WithDefaults returns a new config with defaults filled in for zero values.
func (cfg MerchantConfig) WithDefaults() MerchantConfig {
	if cfg.ProfileCacheTTL == 0 {
		cfg.ProfileCacheTTL = DefaultProfileCacheTTL
	}
	if cfg.TokenTTL == 0 {
		cfg.TokenTTL = DefaultTokenTTL
	}
	if cfg.TokenTTLBuffer == 0 {
		cfg.TokenTTLBuffer = DefaultTokenTTLBuffer
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = DefaultRequestTimeout
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	if cfg.PaymentMethodCode == "" {
		cfg.PaymentMethodCode = "checkmo"
	}
	cfg.Platform = PlatformName
	return cfg
}

// errMissingField constructs a validation error for a missing required field.
func errMissingField(field string) error {
	return configs.NewMissingFieldError("magento", field)
}

// Schema returns the JSON Schema for MerchantConfig.
// Used for validation and API documentation.
func (cfg *MerchantConfig) Schema() json.RawMessage {
	return []byte(`{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "title": "MerchantConfig",
  "description": "Magento merchant credentials and settings for UCP instance",
  "properties": {
    "merchant_id": {
      "type": "string",
      "description": "Sarvex-internal tenant identifier for routing and logging",
	  "x-ignored": true
    },
    "base_url": {
      "type": "string",
      "description": "Magento instance base URL, e.g. 'https://mystore.magentosite.cloud'",
      "format": "uri"
    },
    "admin_user": {
      "type": "string",
      "description": "Magento admin API username"
    },
    "admin_password": {
      "type": "string",
      "description": "Magento admin API password",
	  "x-secret": true
    },
    "store_code": {
      "type": "string",
      "description": "Magento store code, e.g. 'default'"
    },
    "store_currency": {
      "type": "string",
      "description": "ISO 4217 currency code the store operates in",
      "pattern": "^[A-Z]{3}$"
    },
    "payment_method_code": {
      "type": "string",
      "description": "Magento payment method code, e.g. 'checkmo' for check/money order",
      "default": "checkmo"
    },
    "webhook_secret": {
      "type": "string",
      "description": "Webhook signature verification secret",
      "x-secret": true
    },
    "api_override_base": {
      "type": "string",
      "description": "Override base URL for testing against mock servers",
      "format": "uri"
    },
    "max_retries": {
      "type": "integer",
      "description": "Number of retry attempts on transient failures",
      "default": 3,
      "minimum": 0
    }
  },
  "required": ["merchant_id", "base_url", "admin_user", "admin_password", "store_code", "store_currency", "webhook_secret"],
  "additionalProperties": false
}`)
}
