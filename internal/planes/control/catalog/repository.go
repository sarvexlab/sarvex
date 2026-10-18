// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// ReadinessRepository reads the store-level readiness score from the catalog
// store. The catalog store is owned by the sync plane (architecture §4.3);
// this interface is READ-ONLY from the control plane's perspective.
//
// TODO(G3): Confirm collection name matches what sync plane scoring workers
// write to. See model.go for the expected document shape (ReadinessModel).
type ReadinessRepository interface {
	// Get returns ErrNotFound when no readiness record exists for the store.
	// A missing record is valid — the store has not yet been scored.
	Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (*ReadinessModel, error)
}

// ProductRepository reads scored product records from the catalog store.
// The catalog store is owned by the sync plane; this interface is READ-ONLY.
//
// TODO(G3): Confirm collection name and _id scheme ("<merchant_id>:<store_id>:<sku>")
// match what scoring workers write. Indexes on {merchant_id, store_id, sku} and
// {merchant_id, store_id, score} must exist for List to be efficient.
type ProductRepository interface {
	// Get returns ErrNotFound when no product record exists for the given SKU.
	Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, sku string) (*ProductModel, error)

	// List returns up to limit products for the store ordered by SKU.
	// afterSKU is the cursor (pass "" for the first page).
	// minScore and hasBlockingIssue filter the result when non-nil.
	// hasMore is true when more records exist beyond the returned slice.
	List(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, afterSKU string, limit int, minScore *int, hasBlockingIssue *bool) ([]*ProductModel, bool, error)
}

// RemediationRepository reads rule-based remediations. The records live in the
// sync plane's sync_catalog_remediations collection: the sync plane is their
// only writer (its score stage creates and retires them, its apply-remediations
// handler marks them applied), and the control plane reads them by collection
// name, read-only (sarvex-v1-architecture.md A10.1, §2.3). Applying remediations is a command to
// the sync plane (CommandEmitter.ApplyRemediation), never a write here.
type RemediationRepository interface {
	// List returns up to limit remediations for the store ordered by ID.
	// afterID is the cursor (pass "" for the first page).
	// remType filters by remediation type when non-nil ("rule" | "manual").
	// hasMore is true when more records exist beyond the returned slice.
	List(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, afterID string, limit int, remType *string) ([]*RemediationModel, bool, error)

	// GetByIDs returns the remediation records for the given IDs, all scoped
	// to the store. Returns ErrNotFound if any ID is missing or belongs to
	// a different store.
	GetByIDs(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, ids []string) ([]*RemediationModel, error)

	// ListUnappliedBySKU returns the not-yet-applied remediations for a single
	// product, used to attach typed drill-in payload to a readiness-sourced
	// task (task.source_ref == sku).
	ListUnappliedBySKU(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, sku string) ([]*RemediationModel, error)
}

// WriteBackRepository persists write-back records in the audit log.
// The control plane OWNS this store (architecture §4.3 audit log).
type WriteBackRepository interface {
	// Get returns ErrWriteBackNotFound when no record exists for the given ID
	// scoped to the store.
	Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, writeBackID string) (*WriteBackModel, error)

	// Insert persists a new write-back record.
	Insert(ctx context.Context, m *WriteBackModel) error

	// MarkReverted sets reverted=true on the write-back record.
	MarkReverted(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, writeBackID string) error
}

// AuditJobRepository persists triggered catalog audit jobs.
// Written by the control plane; polled or watched by the sync plane (G3).
//
// TODO(G3): Once the sync plane command bus is wired, jobs may be emitted
// directly as events rather than stored in this collection. Retain the
// collection as a durable record of audit requests either way.
type AuditJobRepository interface {
	// Insert persists a new audit job with status "queued".
	Insert(ctx context.Context, m *AuditJobModel) error
}
