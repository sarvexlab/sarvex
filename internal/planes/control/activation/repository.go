// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package activation

import (
	"context"
	"errors"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// ErrNotFound is returned when no activation record exists for a store.
var ErrNotFound = errors.New("activation: not found")

// Repository persists activation records. Reads are always scoped by
// (merchant_id, store_id) together — a store_id alone is never a sufficient
// key (CLAUDE.md invariant 4).
type Repository interface {
	Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (*ActivationModel, error)
	Upsert(ctx context.Context, m *ActivationModel) error
	// ListByState enumerates activation records in one state across all
	// tenants. Operator/worker use only — never reachable from a merchant
	// request path.
	ListByState(ctx context.Context, state domain.UCPState) ([]*ActivationModel, error)
	// Delete removes a store's activation record. Used by the migration's
	// rollback path only.
	Delete(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) error
}
