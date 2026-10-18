// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package domain

import "time"

// PaymentHandlerConfig represents which payment handlers are enabled for a store.
// Partitioned by store_id; each store manages its own handler configuration.
type PaymentHandlerConfig struct {
	StoreID   StoreID          `json:"store_id"`
	Handlers  []EnabledHandler `json:"handlers"`
	UpdatedAt time.Time        `json:"updated_at"`
	UpdatedBy string           `json:"updated_by"` // user email or system identifier
}

// EnabledHandler represents an enabled payment handler with optional store-specific config.
type EnabledHandler struct {
	HandlerID string            `json:"handler_id"` // e.g., "google_pay"
	Enabled   bool              `json:"enabled"`
	Config    map[string]string `json:"config,omitempty"` // handler-specific settings (API keys, merchant IDs, etc.)
}
