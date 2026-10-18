// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package validation orchestrates pre-validation, sandbox, and production runs
// and maintains per-merchant conformance records pinned to a spec version.
package validation

import (
	"context"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// Service drives validation runs and exposes conformance records.
// All methods are scoped to a specific store (per sarvex-v1-architecture.md A10.4: catalog/validation data is per-store).
type Service interface {
	GetStatus(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.ValidationStatus, error)
	TriggerRun(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.JobAccepted, error)
	ListRuns(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, cursor string, limit int) (domain.ListOf[domain.ConformanceRecord], error)
}
