// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/sarvexlab/sarvex/internal/pkg/domain"
	pkgucp "github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/validator"
)

// ValidatorFactory is a function signature that creates a new validator for a merchant.
type ValidatorFactory func(cfg validator.MerchantConfig, logger *slog.Logger) (validator.Validator, error)

// Registry manages the lifecycle of per-merchant validator instances.
// It acts as the bridge between the configuration HTTP layer and the
// underlying validator implementations.
//
// Validators are registered at startup and accessed on-demand.
// The registry is safe for concurrent use.
type Registry struct {
	mu        sync.RWMutex
	factories map[domain.CommerceSystemType]ValidatorFactory
	logger    *slog.Logger
}

// New creates a new validator registry.
func New(logger *slog.Logger) *Registry {
	if logger == nil {
		logger = slog.Default()
	}
	return &Registry{
		factories: make(map[domain.CommerceSystemType]ValidatorFactory),
		logger:    logger,
	}
}

// RegisterFactory registers a validator factory for a platform.
// Call this at startup for each supported platform.
func (r *Registry) RegisterFactory(platform domain.CommerceSystemType, factory ValidatorFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[platform] = factory
	r.logger.Debug("validator factory registered", "platform", platform)
}

// ValidateCredentials validates a raw credential map for the given platform at
// pairing time, before the credentials are encrypted and stored. It uses the
// registered factory to unmarshal and validate the map without requiring a full
// StoreProvider. storeID is used as the merchant context (merchantID is not
// available at pairing; the factory accepts it as an opaque identifier).
func (r *Registry) ValidateCredentials(ctx context.Context, platform domain.CommerceSystemType, storeID string, creds map[string]any) error {
	r.mu.RLock()
	factory, ok := r.factories[platform]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("no validator registered for platform %q", platform)
	}
	cfg := validator.MerchantConfig{
		MerchantID: storeID,
		StoreID:    storeID,
		Platform:   platform,
		Config:     creds,
	}
	v, err := factory(cfg, r.logger)
	if err != nil {
		return fmt.Errorf("invalid store credentials: %w", err)
	}
	result := v.Validate(ctx)
	if !result.IsValid {
		return fmt.Errorf("credential validation failed: %s", strings.Join(result.Errors, "; "))
	}
	return nil
}

// Get returns a validator for the given UCP instance.
// It looks up the registered factory for the instance's platform,
// builds a MerchantConfig from the instance, and calls the factory
// to create a new validator instance.
func (r *Registry) Get(instance pkgucp.StoreProvider) (validator.Validator, error) {
	r.mu.RLock()
	factory, ok := r.factories[instance.Configuration().Platform]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("registry: no factory registered for platform %q", instance.Configuration().Platform)
	}

	cfg := validator.MerchantConfig{
		MerchantID: instance.Configuration().MerchantID,
		StoreID:    instance.Configuration().StoreID,
		Platform:   instance.Configuration().Platform,
		Config:     instance.Configuration().Config,
	}

	validator, err := factory(cfg, r.logger)
	if err != nil {
		return nil, fmt.Errorf("registry: create validator for merchant %s: %w", cfg.MerchantID, err)
	}
	return validator, nil
}
