// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package surface

import (
	"context"
	"errors"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// ErrNotFound is returned when a store has no enrollment record for a
// surface. A store with no record is `available`, not an error condition —
// the service translates this.
var ErrNotFound = errors.New("surface: enrollment not found")

// Repository persists enrollment records. Every read is scoped by
// (merchant_id, store_id) together (sarvex-v1-architecture.md A10.4).
type Repository interface {
	Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) (*EnrollmentModel, error)
	ListByStore(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) ([]*EnrollmentModel, error)
	Upsert(ctx context.Context, m *EnrollmentModel) error
	Delete(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) error
	// ListStoresBySurfaceState enumerates store ids enrolled on one surface
	// in one state, across tenants. Worker use only (artifact generation gating
	// asks "which stores are enrolled in this surface"); never reachable from
	// a merchant request path.
	ListStoresBySurfaceState(ctx context.Context, surfaceID string, state domain.SurfaceState) ([]string, error)
}
