// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package surface

import (
	"context"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// Service is the single authority on surface enrollment.
type Service interface {
	// Registry returns every registered surface's descriptor.
	Registry(ctx context.Context) []domain.SurfaceDescriptor

	// List returns the store's position on every registered surface. A
	// surface the store has never touched reads as `available` — the
	// registry is the source of what exists, not the enrollment table.
	List(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) ([]domain.SurfaceEnrollment, error)

	// Get returns one surface's enrollment for one store.
	Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) (domain.SurfaceEnrollment, error)

	// Enroll starts enrollment and immediately evaluates prerequisites,
	// so the merchant sees what is outstanding in one round trip. Fails
	// with ErrStoreNotActive if the store has not reached active.
	Enroll(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) (domain.SurfaceEnrollment, error)

	// EvaluatePrerequisites re-runs every check on demand and advances or
	// regresses the enrollment accordingly (R10). Safe to call repeatedly.
	EvaluatePrerequisites(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) (domain.SurfaceEnrollment, error)

	// SetConfig replaces the merchant-supplied per-surface configuration
	// and re-evaluates prerequisites against it.
	SetConfig(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string, config map[string]string) (domain.SurfaceEnrollment, error)

	// Submit records that the merchant completed the surface's external
	// steps, moving enrollment into verifying.
	Submit(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) (domain.SurfaceEnrollment, error)

	// Withdraw removes enrollment. Activation state is untouched, and so
	// is every other surface (R9).
	Withdraw(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) error

	// Suspend marks a store suspended on one surface. Store activation and
	// all other surfaces are unaffected.
	Suspend(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID, reason string) error

	// IsEnrolled reports whether a store is enrolled on one surface. This
	// is the read every R13 call site uses in place of onboarding state:
	// feed generation, feed-freshness health, console gating.
	IsEnrolled(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) (bool, error)

	// OnStoreActivated auto-enrolls every AutoEnroll surface for a store
	// that just reached active. Idempotent.
	OnStoreActivated(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) error
}

// ActivationReader is the narrow read the service needs about activation:
// the store's UCP state, for the one gate in R9. Shaped as its own
// interface so this package depends on a single method rather than the
// whole activation service, and so the gate is trivially fakeable in tests.
type ActivationReader interface {
	Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Activation, error)
}

// ConnectorReader lists a store's connectors, of every type. Prerequisite
// checks that require a connector (a surface may require a connector type) read it from
// the CheckContext this fills.
type ConnectorReader interface {
	ListByStore(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) ([]domain.Connector, error)
}
