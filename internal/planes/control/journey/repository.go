// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package journey

import (
	"context"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// Repository is the persistence interface for journey state records.
// The single writer is JourneyService; every other component consumes events.
type Repository interface {
	// Get returns ErrNotFound when no record exists for the store owned by
	// merchantID. merchantID is part of the query, not a post-hoc filter — a
	// store_id belonging to a different merchant must be indistinguishable
	// from a nonexistent one (sarvex-v1-architecture.md A10.4: no cross-merchant query).
	Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (*JourneyModel, error)
	// Upsert creates or replaces the journey document.
	Upsert(ctx context.Context, m *JourneyModel) error
}
