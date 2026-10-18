// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package webbaseline

import "context"

// Store persists BaselineDay documents — the sync plane is the single
// writer (R11); the control-plane read API (internal/planes/control/api)
// reads the same collection directly, as
// internal/planes/control/surface.NewMongoSyncedArtifactReader does for a
// sync-owned collection.
type Store interface {
	Upsert(ctx context.Context, day BaselineDay) error
}

// StoreRef is the minimal per-store identity Syncer needs to iterate every
// store it should sync a baseline for.
type StoreRef struct {
	MerchantID string
	StoreID    string
	Platform   string
}

// StoreLister enumerates every store across every merchant — a system-
// wide, cross-tenant scheduler read, same category as
// internal/planes/control/monitoring.StoreLister (which this mirrors).
type StoreLister interface {
	ListAllStores(ctx context.Context, afterID string, limit int) ([]StoreRef, bool, error)
}
