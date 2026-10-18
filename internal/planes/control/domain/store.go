// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package domain

import "time"

// Store represents a single store owned by a merchant.
// Each store has its own connector, catalog, serving cache, profile, and sessions.
type Store struct {
	StoreID        StoreID     `json:"store_id"`
	MerchantID     MerchantID  `json:"merchant_id"`
	ConnectorID    string      `json:"connector_id"`
	Platform       Platform    `json:"platform"`
	DisplayName    string      `json:"display_name"`
	Status         StoreStatus `json:"status"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
	LastSyncAt     *time.Time  `json:"last_sync_at,omitempty"`
	ReadinessScore *float32    `json:"readiness_score,omitempty"`
}

// StoreStatus describes the operational state of a store.
type StoreStatus string

const (
	StoreStatusOnboarding StoreStatus = "onboarding"
	StoreStatusActive     StoreStatus = "active"
	StoreStatusSuspended  StoreStatus = "suspended"
	StoreStatusArchived   StoreStatus = "archived"
)
