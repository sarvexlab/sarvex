// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// CacheProduct is the serving-plane projection of a product.
type CacheProduct struct {
	SKU           string            `json:"sku"`
	Title         string            `json:"title"`
	Description   string            `json:"description"`
	Price         Money             `json:"price"` // Current price in serving cache
	Stock         StockLevel        `json:"stock"` // Current stock status
	Attributes    map[string]string `json:"attributes"`
	LastScoreTime time.Time         `json:"last_score_time"`
}

// CacheProfile is the per-store UCP business profile, matching ucp.dev specification.
// This is the canonical representation stored in Redis and returned to agents at /.well-known/ucp.
// Per spec: https://ucp.dev/latest/specification/overview/#business-profile
type CacheProfile struct {
	// StoreID is the internal store identifier (for cache management, not in spec response).
	StoreID string `json:"-"`
	// MerchantID is the merchant identifier (for audit, not in spec response).
	MerchantID string `json:"-"`

	StorefrontURL string `json:"-"` // Store's public domain

	Platform string `json:"-"` // "shopify" | "bigcommerce" | "magento" | "commercetools" | "woocommerce"

	// UCP is the spec-compliant UCP block returned to agents.
	UCP UCPBlockCache `json:"ucp"`
	// SigningKeys/Keys are JWK format public keys for signature verification —
	// mutually exclusive per rendered generation, never both populated on the
	// same CacheProfile. SigningKeys is the 2026-04-08 field name; Keys
	// replaces it from the 2026-08-25 generation on (the sole canonical name
	// there — see internal/spec/v20260825/wire/profile.go's doc comment).
	// ProfileRenderer.RenderProfile picks the one matching UCP.Version.
	SigningKeys []SigningKeyCache `json:"signing_keys,omitempty"`
	Keys        []SigningKeyCache `json:"keys,omitempty"`
}

// UCPBlockCache is the spec-compliant UCP block per https://ucp.dev/2026-04-08/specification/overview/
// Uses catalogue types directly as they are the authoritative UCP spec definitions.
type UCPBlockCache struct {
	Version         string                           `json:"version"`          // YYYY-MM-DD UCP protocol version
	Services        map[string][]wire.Service        `json:"services"`         // Service definitions (from catalogue)
	Capabilities    map[string][]wire.Capability     `json:"capabilities"`     // Capability declarations (from catalogue)
	PaymentHandlers map[string][]wire.PaymentHandler `json:"payment_handlers"` // Payment handlers (from catalogue)
}

// SigningKeyCache is a JWK format signing key.
type SigningKeyCache struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Crv string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
	Use string `json:"use,omitempty"`
	Alg string `json:"alg,omitempty"`
}

// CacheWatermark tracks freshness for responses.
// CacheWatermark reports how far behind the merchant's store the served
// snapshot is.
//
// Staleness is "a change was signalled and we have not rebuilt since", not
// "the newest product is old". It used to be the latter — IsStale was
// time.Since(SourceSync) > 5m, where SourceSync is the newest product's
// UpdatedAt — which inverts in the normal case: a store whose catalog has not
// changed in an hour has maximally *accurate* cached data, and that rule
// called it maximally stale. Every quiet store was permanently stale, and the
// checkout freshness guard fails a session on this flag, so a merchant whose
// catalog had been steady for five minutes could not complete a sale.
type CacheWatermark struct {
	BuiltAt    time.Time `json:"built_at"`
	SourceSync time.Time `json:"source_sync"`
	// PendingSince is when the oldest not-yet-rebuilt change was signalled,
	// zero when the snapshot is level with the store.
	PendingSince time.Time `json:"pending_since,omitempty"`
	// IsStale reports that a signalled change has gone un-rebuilt for longer
	// than the freshness window.
	IsStale bool `json:"is_stale"`
	// Age is how long the snapshot has been behind — zero when it is level,
	// however long ago it was built.
	Age time.Duration `json:"age_ms"`
}

// Money represents a monetary amount in minor units + currency.
type Money struct {
	AmountMinor int    `json:"amount_minor"`
	Currency    string `json:"currency"`
}

// StockLevel represents product stock status.
type StockLevel struct {
	InStock  bool `json:"in_stock"`
	Quantity *int `json:"quantity,omitempty"`
}

// ConvertJWKsToSigningKeyCache converts UCP JWKs to cache SigningKeyCache format.
// This is used by both the profile renderer and cache rebuilder.
func ConvertJWKsToSigningKeyCache(jwks []wire.JWK) []SigningKeyCache {
	keys := make([]SigningKeyCache, len(jwks))
	for i, jwk := range jwks {
		keys[i] = SigningKeyCache{
			Kid: jwk.Kid,
			Kty: jwk.Kty,
			Crv: jwk.Crv,
			X:   jwk.X,
			Y:   jwk.Y,
			Use: jwk.Use,
			Alg: jwk.Alg,
		}
	}
	return keys
}
