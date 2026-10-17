// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"

	"github.com/sarvexlab/sarvex/internal/pkg/jwkmanagement"
)

// signingKeyTTL is the validity window for a freshly provisioned store signing
// keypair. Matches the rotation TTL used by jwkmanagement.RotateKeypair.
const signingKeyTTL = 365 * 24 * time.Hour

// StoreService implements the Service interface.
// It manages store lifecycle (create, read, update, delete).
type StoreService struct {
	repo StoreRepository
	log  *slog.Logger
	keys KeyProvisioner
}

// GetByID implements [Service].
func (s *StoreService) GetByID(ctx context.Context, storeID domain.StoreID) (domain.Store, error) {
	model, err := s.repo.GetByD(ctx, storeID)
	if err != nil {
		return domain.Store{}, err
	}

	return model.ToDomain(), nil
}

// NewStoreService creates a store service with its dependencies.
func NewStoreService(repo StoreRepository,
	keys KeyProvisioner,
	log *slog.Logger) Service {
	return &StoreService{
		repo: repo,
		log:  log,
		keys: keys,
	}
}

// List returns stores for a merchant with pagination.
func (s *StoreService) List(ctx context.Context, merchantID domain.MerchantID, cursor string, limit int) (domain.ListOf[domain.Store], error) {
	models, hasMore, err := s.repo.ListByMerchant(ctx, merchantID, cursor, limit)
	if err != nil {
		return domain.ListOf[domain.Store]{}, fmt.Errorf("list stores: %w", err)
	}

	stores := lo.Map(models, func(m *StoreModel, _ int) domain.Store { return m.ToDomain() })

	var nextCursor *string
	if hasMore && len(models) > 0 {
		nextCursor = ptrString(models[len(models)-1].ID)
	}

	return domain.ListOf[domain.Store]{
		Data: stores,
		Page: domain.Page{
			Cursor:  nextCursor,
			HasMore: hasMore,
		},
	}, nil
}

// Get retrieves a store by ID.
func (s *StoreService) Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Store, error) {
	model, err := s.repo.Get(ctx, merchantID, storeID)
	if err != nil {
		return domain.Store{}, err
	}

	return model.ToDomain(), nil
}

// Create creates a new store for a merchant.
func (s *StoreService) Create(ctx context.Context, merchantID domain.MerchantID, displayName string, platform domain.Platform) (domain.Store, error) {
	storeID := domain.StoreID(uuid.New().String())
	now := time.Now()

	model := &StoreModel{
		ID:          string(storeID),
		MerchantID:  string(merchantID),
		Platform:    platform,
		DisplayName: displayName,
		Status:      domain.StoreStatusOnboarding,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.repo.Upsert(ctx, model); err != nil {
		return domain.Store{}, fmt.Errorf("create store: %w", err)
	}

	// Provision the store's UCP signing keypair before marking the connector
	// healthy, so a paired connector always has the keys it needs to serve a
	// verifiable profile. Idempotent: a retried pairing completion reuses the
	// existing active key.
	if _, err := s.EnsureSigningKeys(ctx, string(storeID)); err != nil {
		s.log.Error("failed to provision signing keys", "error", err, "store_id", storeID)
		return domain.Store{}, fmt.Errorf("provision signing keys: %w", err)
	}

	s.log.Info("store created", "merchant_id", merchantID, "store_id", storeID, "platform", platform)

	return model.ToDomain(), nil
}

// EnsureSigningKeys ensures the store has an active UCP signing keypair,
// generating one if absent, and returns the active key's id.
//
// Idempotent, so retried pairing completions do not mint duplicate keys — and
// so it can be called repeatedly to repair a store that has lost its keys.
// That matters because losing them is both possible and silent: the keys live
// wherever jwkmanagement is configured to put them (Redis by default), and
// MongoCatalogSource.activeSigningKeys deliberately renders a profile without
// signing_keys rather than failing a rebuild. A store in that state publishes
// a well-formed profile that no agent can verify a signature against, and
// nothing recovers on its own — provisioning used to happen once, at pairing,
// and never again.
func (s *StoreService) EnsureSigningKeys(ctx context.Context, storeID string) (string, error) {
	existing, err := s.keys.GetActiveKeypair(ctx, storeID)
	if err != nil {
		return "", fmt.Errorf("check active keypair: %w", err)
	}
	if existing != nil {
		return keypairID(existing), nil
	}
	generated, err := s.keys.GenerateKeypair(ctx, storeID, signingKeyTTL)
	if err != nil {
		return "", fmt.Errorf("generate keypair: %w", err)
	}
	s.log.Info("signing keypair provisioned", "store_id", storeID)
	return keypairID(generated), nil
}

// keypairID reads the published key id off a keypair, tolerating a nil JWK —
// the reference only; no key material crosses this boundary.
func keypairID(kp *jwkmanagement.JWKKeypair) string {
	if kp == nil || kp.JWK == nil {
		return ""
	}
	return kp.JWK.Kid
}

// Update updates a store's display name.
func (s *StoreService) Update(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, displayName string) (domain.Store, error) {
	model, err := s.repo.Get(ctx, merchantID, storeID)
	if err != nil {
		return domain.Store{}, err
	}

	model.DisplayName = displayName
	model.UpdatedAt = time.Now()

	if err := s.repo.Upsert(ctx, model); err != nil {
		return domain.Store{}, fmt.Errorf("update store: %w", err)
	}

	s.log.Info("store updated", "merchant_id", merchantID, "store_id", storeID)

	return model.ToDomain(), nil
}

// Delete removes a store.
func (s *StoreService) Delete(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) error {
	if err := s.repo.Delete(ctx, merchantID, storeID); err != nil {
		return err
	}

	s.log.Info("store deleted", "merchant_id", merchantID, "store_id", storeID)
	return nil
}

// LinkConnector updates a store's connector_id reference.
// This is called after connector pairing begins or completes.
func (s *StoreService) LinkConnector(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, connectorID string) error {
	model, err := s.repo.Get(ctx, merchantID, storeID)
	if err != nil {
		return fmt.Errorf("get store: %w", err)
	}

	model.ConnectorID = connectorID
	model.UpdatedAt = time.Now()

	if err := s.repo.Upsert(ctx, model); err != nil {
		return fmt.Errorf("link connector: %w", err)
	}

	s.log.Info("store linked to connector", "merchant_id", merchantID, "store_id", storeID, "connector_id", connectorID)
	return nil
}

func ptrString(s string) *string {
	return &s
}

var _ Service = (*StoreService)(nil)
