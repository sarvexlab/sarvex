// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrProductNotFound is returned when a product does not exist.
	ErrProductNotFound = errors.New("sync: product not found")
	// ErrConcurrentUpdate is returned when concurrent writes are detected.
	ErrConcurrentUpdate = errors.New("sync: concurrent update detected")
)

// CatalogStore manages the canonical catalog for a store (one writer per store).
// All operations are keyed by store_id for per-store partitioning; store_id
// is never derived from a URL, it is passed in by the caller (the sync
// pipeline for writes, the readiness API / rebuilder for reads).
//
// The underlying document _id is "<merchant_id>:<store_id>:<sku>" (matching
// control/catalog.ProductModel's id scheme), but Get/Delete take only
// (storeID, sku) — store_id already determines merchant_id 1:1, so the
// implementation resolves identity via the store_id+sku index rather than
// requiring merchant_id on every call.
type CatalogStore interface {
	// UpsertProduct stores or updates a product. Atomic. product.MerchantID,
	// product.StoreID, and product.SKU must be set; the implementation
	// derives the storage key from them.
	UpsertProduct(ctx context.Context, product *CatalogProduct) error
	// DeleteProduct removes a product (soft or hard per store implementation).
	DeleteProduct(ctx context.Context, storeID, sku string) error
	// GetProduct retrieves a product by SKU.
	GetProduct(ctx context.Context, storeID, sku string) (*CatalogProduct, error)
	// ListByStore returns all products for a store (paginated).
	ListByStore(ctx context.Context, storeID string, limit int, cursor string) ([]*CatalogProduct, *string, error)
	// LatestUpdateTime returns the most recent product update timestamp for a store.
	LatestUpdateTime(ctx context.Context, storeID string) (time.Time, error)
	// ListStoreIDs returns the distinct store_ids with at least one catalog
	// record, i.e. every store that has completed at least one catalog
	// import. Used by the reconciliation sweeper's scheduler to discover
	// which stores to sweep; it is an internal maintenance enumeration
	// (store identities only, no product/merchant data), not a tenant-facing
	// read, so it does not cross the merchant/store tenancy boundary any
	// read path relies on.
	ListStoreIDs(ctx context.Context) ([]string, error)
}
