// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package domain

// MerchantID is the opaque per-tenant identifier resolved from the auth principal.
// It is never sourced from a URL parameter. Partitions billing, identity, team, notification prefs.
type MerchantID string

// StoreID is the per-store identifier for a catalog/serving partition.
// Each store owns one connector. Partitions catalog, serving cache, sessions, profile, pairing, validation.
type StoreID string

// Principal is the resolved identity attached to every authenticated request.
type Principal struct {
	MerchantID  MerchantID `json:"merchant_id"`
	StoreID     StoreID    `json:"store_id,omitempty"`     // Set by HMAC auth for connector requests
	ConnectorID string     `json:"connector_id,omitempty"` // Set by HMAC auth for connector requests
}
