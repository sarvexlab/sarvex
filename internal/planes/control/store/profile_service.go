// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// ErrProfileNotFound is returned when a store has no persisted profile yet.
var ErrProfileNotFound = errors.New("store profile not found")

// ProfileService manages store profile data (business facts, configuration, security material).
// Profiles are control-plane data owned by the merchant during onboarding and updated as needed.
type ProfileService struct {
	repo ProfileRepository
}

// ProfileRepository provides persistent storage for store profiles.
type ProfileRepository interface {
	// GetBusinessProfile retrieves a store's business profile.
	GetBusinessProfile(ctx context.Context, storeID domain.StoreID) (*domain.StoreBusinessProfile, error)
	// SaveBusinessProfile persists a store's business profile.
	SaveBusinessProfile(ctx context.Context, profile *domain.StoreBusinessProfile) error

	// GetConfiguration retrieves a store's configuration.
	GetConfiguration(ctx context.Context, storeID domain.StoreID) (*domain.StoreConfiguration, error)
	// SaveConfiguration persists a store's configuration.
	SaveConfiguration(ctx context.Context, config *domain.StoreConfiguration) error

	GetCapabilities(ctx context.Context) (catalogue.Catalogue, error)
}

// NewProfileService creates a profile service with a repository.
func NewProfileService(repo ProfileRepository) *ProfileService {
	return &ProfileService{repo: repo}
}

// GetCapabilities retrieves the capabilities for a store.
func (s *ProfileService) GetCapabilities(ctx context.Context) (catalogue.Catalogue, error) {
	return s.repo.GetCapabilities(ctx)
}

// GetBusinessProfile retrieves the business profile for a store.
func (s *ProfileService) GetBusinessProfile(ctx context.Context, storeID domain.StoreID) (*domain.StoreBusinessProfile, error) {
	profile, err := s.repo.GetBusinessProfile(ctx, storeID)
	if err != nil {
		return nil, fmt.Errorf("get business profile: %w", err)
	}
	return profile, nil
}

// SaveBusinessProfile persists the business profile for a store.
func (s *ProfileService) SaveBusinessProfile(ctx context.Context, profile *domain.StoreBusinessProfile) error {
	if profile == nil {
		return fmt.Errorf("profile cannot be nil")
	}
	if profile.StoreID == "" {
		return fmt.Errorf("store_id is required")
	}
	if profile.MerchantID == "" {
		return fmt.Errorf("merchant_id is required")
	}

	return s.repo.SaveBusinessProfile(ctx, profile)
}

// GetConfiguration retrieves the configuration for a store.
func (s *ProfileService) GetConfiguration(ctx context.Context, storeID domain.StoreID) (*domain.StoreConfiguration, error) {
	config, err := s.repo.GetConfiguration(ctx, storeID)
	if err != nil {
		return nil, fmt.Errorf("get configuration: %w", err)
	}
	return config, nil
}

// SaveConfiguration persists the configuration for a store.
func (s *ProfileService) SaveConfiguration(ctx context.Context, config *domain.StoreConfiguration) error {
	if config == nil {
		return fmt.Errorf("configuration cannot be nil")
	}
	if config.StoreID == "" {
		return fmt.Errorf("store_id is required")
	}
	if config.MerchantID == "" {
		return fmt.Errorf("merchant_id is required")
	}

	return s.repo.SaveConfiguration(ctx, config)
}
