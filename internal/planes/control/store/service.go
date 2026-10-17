// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/jwkmanagement"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// Service manages store lifecycle.
type Service interface {
	List(ctx context.Context, merchantID domain.MerchantID, cursor string, limit int) (domain.ListOf[domain.Store], error)
	Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Store, error)
	GetByID(ctx context.Context, storeID domain.StoreID) (domain.Store, error)
	Create(ctx context.Context, merchantID domain.MerchantID, displayName string, platform domain.Platform) (domain.Store, error)
	Update(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, displayName string) (domain.Store, error)
	Delete(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) error
	LinkConnector(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, connectorID string) error

	// EnsureSigningKeys provisions the store's UCP signing keypair if it is
	// absent and returns the active key id. Idempotent, and safe to call
	// repeatedly — it is what makes key provisioning self-healing rather than
	// a one-shot at pairing.
	EnsureSigningKeys(ctx context.Context, storeID string) (string, error)
}

// KeyProvisioner provisions a store's UCP signing keypair at pairing time.
// It is the narrow slice of [jwkmanagement.JWKManagementService] the connector
// service needs: read the active keypair (for idempotency) and generate a new
// one. jwkmanagement is the single source of truth for signing keys — the
// public half it holds is what the serving plane publishes in the profile, and
// the private half is what the serving signer uses, so the two always match.
type KeyProvisioner interface {
	GetActiveKeypair(ctx context.Context, storeID string) (*jwkmanagement.JWKKeypair, error)
	GenerateKeypair(ctx context.Context, storeID string, expiresIn time.Duration) (*jwkmanagement.JWKKeypair, error)
}
