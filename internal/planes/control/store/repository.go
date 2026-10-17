// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// StoreRepository is the persistence interface for store records.
// Stores are owned by a merchant (tenant isolation via merchant_id).
type StoreRepository interface {
	// Get returns ErrNotFound when no record exists for the given merchant and store ids.
	Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (*StoreModel, error)

	GetByD(ctx context.Context, storeID domain.StoreID) (*StoreModel, error)

	// ListByMerchant returns up to limit stores for the merchant, ordered by store_id.
	// afterID is the cursor (use "" for the first page); hasMore is true when more records exist.
	ListByMerchant(ctx context.Context, merchantID domain.MerchantID, afterID string, limit int) ([]*StoreModel, bool, error)

	// ListAll returns up to limit stores across every merchant, ordered by
	// store_id. Cross-tenant by design: for internal background jobs only
	// (e.g. the monitoring scheduler) — never expose this through a
	// tenant-facing API (architecture §4.5 invariant 4 — no cross-merchant
	// query on tenant read paths). afterID is the cursor (use "" for the
	// first page); hasMore is true when more records exist.
	ListAll(ctx context.Context, afterID string, limit int) ([]*StoreModel, bool, error)

	// Upsert creates or replaces the store document.
	Upsert(ctx context.Context, m *StoreModel) error

	// Delete removes the store record. Returns ErrNotFound if not found.
	Delete(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) error
}
