// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package shopify

import (
	"encoding/json"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/configs"
)

// MerchantConfig holds all per-merchant Shopify credentials and settings.
// This struct is populated from merchant configuration storage (e.g. Cloud SQL
// or Vault) and passed to NewConnector.
//
// Required fields are marked; optional fields have sensible defaults.
type MerchantConfig struct {
	// MerchantID is the Sarvex-internal tenant identifier. Used as a routing
	// key and for logging/tracing — not sent to Shopify.
	MerchantID string `json:"merchant_id"`

	// ShopDomain is the myshopify.com hostname, e.g. "my-store.myshopify.com".
	// Required.
	ShopDomain string `json:"shop_domain"`

	// StorefrontAccessToken is the public Storefront API token.
	// Scoped to storefront read operations (products, cart, checkout).
	// Required.
	StorefrontAccessToken string `json:"storefront_access_token"`

	// AdminAccessToken is the private Admin API token.
	// Required for order write-back and webhook registration.
	// Required.
	AdminAccessToken string `json:"admin_access_token"`

	// WebhookSecret is the shared HMAC-SHA256 secret used to verify inbound
	// webhooks. Required for webhook handling.
	WebhookSecret string `json:"webhook_secret"`

	// StorefrontAPIVersion is the Shopify API version string, e.g. "2025-01".
	// Defaults to DefaultStorefrontAPIVersion when empty.
	StorefrontAPIVersion string `json:"storefront_api_version,omitempty"`

	// AdminAPIVersion is the Admin REST API version string, e.g. "2025-01".
	// Defaults to DefaultAdminAPIVersion when empty.
	AdminAPIVersion string `json:"admin_api_version,omitempty"`

	// ShopID is the numeric Shopify shop identifier returned by the Admin API.
	// Used in the Shop Pay payment handler config.
	// Optional; populated lazily on first profile build if empty.
	ShopID string `json:"shop_id,omitempty"`

	// ShopPayEnabled controls whether the Shop Pay payment handler is included
	// in the UCP business profile. Defaults to true.
	ShopPayEnabled bool `json:"shop_pay_enabled"`

	// StoreCurrency is the ISO 4217 currency code the store operates in.
	// Defaults to "USD" when empty.
	StoreCurrency string `json:"store_currency,omitempty"`

	// StoreBaseURL is the public storefront URL for continue_url construction.
	// e.g. "https://my-store.com". Falls back to ShopDomain when empty.
	StoreBaseURL string `json:"store_base_url,omitempty"`

	// ProfileCacheTTL controls how long the built business profile is cached
	// in-memory. Defaults to DefaultProfileCacheTTL.
	ProfileCacheTTL time.Duration `json:"profile_cache_ttl,omitempty"`

	// RequestTimeout is the per-request timeout for Shopify API calls.
	// Defaults to DefaultRequestTimeout.
	RequestTimeout time.Duration `json:"request_timeout,omitempty"`

	// MaxRetries is the number of retry attempts on transient failures (429, 503).
	// Defaults to DefaultMaxRetries.
	MaxRetries int `json:"max_retries,omitempty"`
}

const (
	DefaultStorefrontAPIVersion = "2025-01"
	DefaultAdminAPIVersion      = "2025-01"
	DefaultProfileCacheTTL      = 5 * time.Minute
	DefaultRequestTimeout       = 10 * time.Second
	DefaultMaxRetries           = 3
)

// WithDefaults returns a copy of cfg with zero-value optional fields populated.
func (cfg MerchantConfig) WithDefaults() MerchantConfig {
	if cfg.StorefrontAPIVersion == "" {
		cfg.StorefrontAPIVersion = DefaultStorefrontAPIVersion
	}
	if cfg.AdminAPIVersion == "" {
		cfg.AdminAPIVersion = DefaultAdminAPIVersion
	}
	if cfg.StoreCurrency == "" {
		cfg.StoreCurrency = "USD"
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
	if cfg.StoreBaseURL == "" {
		cfg.StoreBaseURL = "https://" + cfg.ShopDomain
	}
	return cfg
}

// Validate checks that all required fields are present.
func (cfg *MerchantConfig) Validate() error {
	switch {
	case cfg.MerchantID == "":
		return errMissingField("merchant_id")
	case cfg.ShopDomain == "":
		return errMissingField("shop_domain")
	case cfg.StorefrontAccessToken == "":
		return errMissingField("storefront_access_token")
	case cfg.AdminAccessToken == "":
		return errMissingField("admin_access_token")
	}
	return nil
}

// errMissingField constructs a validation error for a missing required field.
func errMissingField(field string) error {
	return configs.NewMissingFieldError("shopify", field)
}

// Schema returns the JSON Schema for MerchantConfig.
// Used for validation and API documentation.
func (cfg *MerchantConfig) Schema() json.RawMessage {
	return []byte(`{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "title": "MerchantConfig",
  "description": "Shopify merchant credentials and settings for UCP instance",
  "properties": {
    "merchant_id": {
      "type": "string",
      "description": "Sarvex-internal tenant identifier for routing and logging",
	  "x-ignored": true,
	  "x-instruction": "This field is not sent to Shopify; it is used internally by Sarvex for routing and logging purposes."
    },
    "shop_domain": {
      "type": "string",
      "description": "Myshopify.com hostname, e.g. 'my-store.myshopify.com'",
	  "x-instruction": "This is the unique identifier for the Shopify store and is used to construct API URLs. It should not include 'https://'."
    },
    "storefront_access_token": {
      "type": "string",
      "description": "Public Storefront API token for storefront operations",
	  "x-secret": true,
	  "x-instruction": "This token is used for public storefront API operations and should be kept secure."
    },
    "admin_access_token": {
      "type": "string",
      "description": "Private Admin API token for order write-back and webhook registration",
	  "x-secret": true,
	  "x-instruction": "This token is used for private admin API operations and should be kept secure."
    },
    "webhook_secret": {
      "type": "string",
      "description": "Shared HMAC-SHA256 secret for webhook verification",
	  "x-secret": true,
	  "x-instruction": "This secret is used to verify the authenticity of incoming webhook requests."
    },
    "storefront_api_version": {
      "type": "string",
      "description": "Shopify Storefront API version, e.g. '2025-01'",
      "default": "2025-01",
      "x-instruction": "This is the version of the Shopify Storefront API to use for storefront operations."
    },
    "admin_api_version": {
      "type": "string",
      "description": "Shopify Admin REST API version, e.g. '2025-01'",
      "default": "2025-01",
      "x-instruction": "This is the version of the Shopify Admin REST API to use for admin operations."
    },
    "shop_id": {
      "type": "string",
      "description": "Numeric Shopify shop identifier (populated lazily on first profile build)",
	  "x-instruction": "This field is populated lazily on first profile build by calling the Admin API. It is used in the Shop Pay payment handler config."
    },
    "shop_pay_enabled": {
      "type": "boolean",
      "description": "Whether to include Shop Pay payment handler",
      "default": true,
	  "x-instruction": "Set to false to exclude the Shop Pay payment handler from the UCP business profile."
    },
    "store_currency": {
      "type": "string",
      "description": "ISO 4217 currency code the store operates in",
      "default": "USD",
      "pattern": "^[A-Z]{3}$",
	  "x-instruction": "This is the currency code used for price display and transactions."
    },
    "store_base_url": {
      "type": "string",
      "description": "Public storefront URL for continue_url construction, e.g. 'https://my-store.com'",
      "format": "uri",
	  "x-instruction": "This URL is used for constructing continue_url parameters. If empty, it defaults to 'https://' + shop_domain."
    },
    "max_retries": {
      "type": "integer",
      "description": "Number of retry attempts on transient failures (429, 503)",
      "default": 3,
      "minimum": 0,
	  "x-instruction": "Set this to control how many times the connector will retry API calls on transient failures such as rate limiting (429) or service unavailable (503)."
    }
  },
  "required": ["merchant_id", "shop_domain", "storefront_access_token", "admin_access_token", "webhook_secret"],
  "additionalProperties": false
}`)
}
