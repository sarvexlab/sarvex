// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package base manages the lifecycle of per-merchant backend connector
// instances. It acts as the bridge between the gateway HTTP layer and the
// underlying connector implementations.
//
// Connectors are registered at startup (or lazily on first request) and
// cached for the lifetime of the process. The registry is safe for concurrent
// use.
package base

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/sarvexlab/sarvex/internal/pkg/domain"
	pkgucp "github.com/sarvexlab/sarvex/internal/pkg/ucp"
)

// CredentialsProvider resolves a store's decrypted platform credentials at
// call time. Connectors use it inside the store-API pull methods
// (FetchProduct, FetchCatalog, Ping, ...) so a credential rotation at
// pairing takes effect on the very next call, rather than being baked into
// MerchantConfig once at connector-construction time.
type CredentialsProvider interface {
	GetStoreCredentials(ctx context.Context, storeID string) (map[string]any, error)
}

// CredentialsFunc adapts a plain function into a CredentialsProvider.
type CredentialsFunc func(ctx context.Context, storeID string) (map[string]any, error)

// GetStoreCredentials implements CredentialsProvider.
func (f CredentialsFunc) GetStoreCredentials(ctx context.Context, storeID string) (map[string]any, error) {
	return f(ctx, storeID)
}

// MerchantConfig carries the information needed to instantiate a connector
// for a specific merchant. Populated from your merchant configuration store
// (Cloud SQL, etc.).
type MerchantConfig struct {
	MerchantID string
	StoreID    string
	Platform   domain.CommerceSystemType // "shopify" | "bigcommerce" | "magento" | "commercetools" | "woocommerce"
	Config     any                       // platform-specific config struct (e.g. shopify.MerchantConfig)

	// Credentials resolves this store's decrypted secrets fresh, on demand.
	// The checkout/catalog/cart/order methods may keep using Config as
	// today; the store-API pull methods MUST call Credentials on every
	// invocation rather than reading a value cached at construction time.
	Credentials CredentialsProvider
}

// ConnectorFactory is a function that creates a new connector for a merchant.
type ConnectorFactory func(cfg MerchantConfig, logger *slog.Logger) (Connector, error)

type connectorCacheKey struct {
	platform domain.CommerceSystemType
	storeID  string
}

// Registry holds connector factories and a per-(platform, storeID) cache.
type Registry struct {
	mu          sync.RWMutex
	factories   map[domain.CommerceSystemType]ConnectorFactory
	cache       map[connectorCacheKey]Connector
	credentials CredentialsProvider
	logger      *slog.Logger
}

// New creates an empty registry. credentials is shared by every constructed
// connector and is what the store-API pull methods call to resolve a
// store's secrets fresh on each invocation — pass a provider that always
// errors for a registry whose connectors must never make live pull calls
// (e.g. the serving plane, per architecture invariant §4.5.2).
func New(credentials CredentialsProvider, logger *slog.Logger) *Registry {
	return &Registry{
		factories:   make(map[domain.CommerceSystemType]ConnectorFactory),
		cache:       make(map[connectorCacheKey]Connector),
		credentials: credentials,
		logger:      logger,
	}
}

// RegisterFactory registers a connector factory for a platform. Call this once
// at startup for each supported platform.
func (r *Registry) RegisterFactory(platform domain.CommerceSystemType, factory ConnectorFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[platform] = factory
}

// Get returns the connector for an instance, constructing and caching it on first call.
func (r *Registry) Get(storeProvider pkgucp.StoreProvider) (Connector, error) {
	if storeProvider == nil {
		return nil, fmt.Errorf("registry: nil store provider")
	}
	key := connectorCacheKey{
		platform: storeProvider.Configuration().Platform,
		storeID:  storeProvider.Configuration().StoreID,
	}

	r.mu.RLock()
	cached, ok := r.cache[key]
	r.mu.RUnlock()
	if ok {
		return cached, nil
	}

	factory, ok := r.factories[key.platform]
	if !ok {
		return nil, fmt.Errorf("registry: no factory registered for platform %q", key.platform)
	}
	cfg := MerchantConfig{
		MerchantID:  storeProvider.Configuration().MerchantID,
		StoreID:     storeProvider.Configuration().StoreID,
		Platform:    storeProvider.Configuration().Platform,
		Config:      storeProvider.Configuration().Config,
		Credentials: r.credentials,
	}
	connector, err := factory(cfg, r.logger)
	if err != nil {
		return nil, fmt.Errorf("registry: create connector for merchant %s: %w", cfg.MerchantID, err)
	}

	r.mu.Lock()
	r.cache[key] = connector
	r.mu.Unlock()
	return connector, nil
}

// Set injects a pre-built connector for a specific (platform, storeID) pair.
// Use in tests to substitute a stub without touching factories.
func (r *Registry) Set(platform domain.CommerceSystemType, storeID string, connector Connector) {
	key := connectorCacheKey{platform: platform, storeID: storeID}
	r.mu.Lock()
	r.cache[key] = connector
	r.mu.Unlock()
}
