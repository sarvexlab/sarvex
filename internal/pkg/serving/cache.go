// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned when the cache holds no entry for the key — for a
// store, no snapshot has been built yet. There is exactly one: every caller,
// in whichever plane, matches it with errors.Is.
var ErrNotFound = errors.New("serving: not found")

// ErrInvalidCursor is returned when a caller supplies a pagination cursor
// the cache cannot interpret. It is a client error, not a server one: the
// handler maps it to 400 rather than silently restarting from the first
// page, which is what a discarded parse error used to do.
var ErrInvalidCursor = errors.New("serving: invalid pagination cursor")

// ServingCache is the read-only interface to the per-store snapshot.
type ServingCache interface {
	// GetProduct returns product data with full details.
	GetProduct(ctx context.Context, storeID, sku string) (*CacheProduct, error)
	// Search returns paginated product list.
	Search(ctx context.Context, storeID, query string, limit int, cursor string) (*CacheSearchResult, error)
	// GetProfile returns the business profile for the store.
	GetProfile(ctx context.Context, storeID string) (*CacheProfile, error)
	// Watermark returns snapshot freshness info.
	Watermark(ctx context.Context, storeID string) (*CacheWatermark, error)
}

// CacheSnapshot is the atomic unit written by the sync plane (cache rebuilder).
type CacheSnapshot struct {
	StoreID    string
	MerchantID string                   // denormalized for backward compat
	BuiltAt    time.Time                // When sync plane built this snapshot
	SourceSync time.Time                // Timestamp of the last successful catalog/pricing sync
	Profile    *CacheProfile            // /.well-known/ucp response
	Products   map[string]*CacheProduct // SKU → product
	Shipping   *CacheShippingConfig     // Shipping rules + zones
}

// CacheSearchResult is the response for catalog search.
type CacheSearchResult struct {
	Items   []*CacheProduct `json:"items"`
	Cursor  *string         `json:"cursor,omitempty"`
	HasMore bool            `json:"has_more"`
}

// CacheShippingConfig holds shipping rules and zones.
type CacheShippingConfig struct {
	Zones map[string]ShippingZone `json:"zones"`
}

// ShippingZone defines shipping rates for a region.
type ShippingZone struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Rates []Rate `json:"rates"`
}

// Rate is a shipping rate.
type Rate struct {
	Method string `json:"method"`
	Cost   Money  `json:"cost"`
}
