// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package woocommerce

import (
	"encoding/json"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/domain"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/configs"
)

// MerchantConfig carries WooCommerce merchant-specific configuration.
type MerchantConfig struct {
	MerchantID      string                    `json:"merchant_id"`
	StoreID         string                    `json:"store_id"`
	StoreBaseURL    string                    `json:"store_base_url"`  // https://mystore.com
	ConsumerKey     string                    `json:"consumer_key"`    // REST API v3 OAuth consumer key
	ConsumerSecret  string                    `json:"consumer_secret"` // REST API v3 OAuth consumer secret
	APIVersion      string                    `json:"api_version"`     // REST API version, default "v3"
	ProfileCacheTTL time.Duration             `json:"profile_cache_ttl"`
	RequestTimeout  time.Duration             `json:"request_timeout"`
	MaxRetries      int                       `json:"max_retries"`
	APIOverrideBase string                    `json:"api_override_base"` // Override base URL for testing
	Platform        domain.CommerceSystemType `json:"platform"`          // Schema-specific platform identifier
	StoreCurrency   string                    `json:"store_currency"`    // USD, EUR, etc. (required, no default — see Validate)
}

const (
	DefaultProfileCacheTTL = 5 * time.Minute
	DefaultRequestTimeout  = 10 * time.Second
	DefaultMaxRetries      = 3
)

// Validate ensures required fields are populated.
func (cfg MerchantConfig) Validate() error {
	// if cfg.MerchantID == "" {
	// 	return errMissingField("merchant_id")
	// }
	// if cfg.StoreID == "" {
	// 	return errMissingField("store_id")
	// }
	if cfg.StoreBaseURL == "" {
		return errMissingField("store_base_url")
	}
	if cfg.ConsumerKey == "" {
		return errMissingField("consumer_key")
	}
	if cfg.ConsumerSecret == "" {
		return errMissingField("consumer_secret")
	}
	if cfg.StoreCurrency == "" {
		return errMissingField("store_currency")
	}
	return nil
}

// WithDefaults fills in default values for optional fields.
func (cfg MerchantConfig) WithDefaults() MerchantConfig {
	if cfg.APIVersion == "" {
		cfg.APIVersion = "v3"
	}
	if cfg.ProfileCacheTTL == 0 {
		cfg.ProfileCacheTTL = DefaultProfileCacheTTL
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = DefaultRequestTimeout
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	cfg.Platform = PlatformName
	return cfg
}

// errMissingField constructs a validation error for a missing required field.
func errMissingField(field string) error {
	return configs.NewMissingFieldError("woocommerce", field)
}

// Schema returns the JSON Schema for MerchantConfig.
// Used for validation and API documentation.
func (cfg *MerchantConfig) Schema() json.RawMessage {
	return []byte(`{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "title": "MerchantConfig",
  "description": "WooCommerce merchant credentials and settings for UCP instance",
  "properties": {
    "merchant_id": {
      "type": "string",
      "description": "Sarvex-internal tenant identifier for routing and logging",
      "x-ignored": true
    },
    "store_base_url": {
      "type": "string",
      "description": "Public storefront URL, e.g. 'https://mystore.com'",
      "format": "uri"
    },
    "consumer_key": {
      "type": "string",
      "description": "WooCommerce REST API v3 OAuth consumer key",
      "x-secret": true
    },
    "consumer_secret": {
      "type": "string",
      "description": "WooCommerce REST API v3 OAuth consumer secret",
      "x-secret": true
    },
    "webhook_secret": {
      "type": "string",
      "description": "Webhook signature verification key",
      "x-secret": true
    },
    "store_currency": {
      "type": "string",
      "description": "ISO 4217 currency code the store operates in",
      "pattern": "^[A-Z]{3}$"
    },
    "store_code": {
      "type": "string",
      "description": "Store code for consistency (unused for WooCommerce)"
    },
    "api_version": {
      "type": "string",
      "description": "WooCommerce REST API version",
      "default": "v3"
    },
    "api_override_base": {
      "type": "string",
      "description": "Override base URL for testing purposes",
      "format": "uri"
    },
    "max_retries": {
      "type": "integer",
      "description": "Number of retry attempts on transient failures",
      "default": 3,
      "minimum": 0
    }
  },
  "required": ["merchant_id", "store_base_url", "consumer_key", "consumer_secret", "webhook_secret", "store_currency"],
  "additionalProperties": false
}`)
}
