// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wire"
)

// CacheProfileSource is the concrete ucp.ProfileSource backed by the serving
// cache. It feeds the plugin profile doorway (GET /connector/profile): the
// control plane reads the last-known-good rendered profile from the cache and
// hands it to the plugin, which serves it verbatim at /.well-known/ucp.
//
// Per CLAUDE.md §8, the doorway must never fail: a missing snapshot returns no
// body (the plugin then serves its own last-known-good copy) rather than an
// error.
type CacheProfileSource struct {
	cache ServingCache
}

// NewCacheProfileSource creates a ProfileSource that renders bytes from the
// serving cache.
func NewCacheProfileSource(cache ServingCache) *CacheProfileSource {
	return &CacheProfileSource{cache: cache}
}

// GetProfile returns the marshalled UCP business profile for the store plus a
// content-derived etag. The bytes match what the serving plane returns at the
// profile endpoint (the CacheProfile JSON shape). When no snapshot exists yet
// it returns (nil, "", nil) so the doorway degrades to last-known-good.
func (s *CacheProfileSource) GetProfile(ctx context.Context, storeID string) ([]byte, string, error) {
	prof, err := s.cache.GetProfile(ctx, storeID)
	if errors.Is(err, ErrNotFound) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("get profile from cache: %w", err)
	}

	body, err := json.Marshal(prof)
	if err != nil {
		return nil, "", fmt.Errorf("marshal profile: %w", err)
	}

	// Content hash so identical profiles yield identical etags, enabling the
	// 304 path in ConnectorService.FetchProfile.
	etag := fmt.Sprintf("%q", fmt.Sprintf("%x", sha256.Sum256(body)))

	return body, etag, nil
}

func (s *CacheProfileSource) GetStructuredProfile(ctx context.Context, storeID string) (ucp.StoreProvider, error) {
	prof, err := s.cache.GetProfile(ctx, storeID)
	if errors.Is(err, ErrNotFound) {
		return nil, ucp.ErrStoreProfileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get profile from cache: %w", err)
	}
	return &connectorInstanceProvider{
		profile: prof,
	}, nil
}

type connectorInstanceProvider struct {
	profile *CacheProfile
}

// Capabilities implements [ucp.StoreProvider].
func (s *connectorInstanceProvider) Capabilities() map[string][]wire.Capability {
	// Every member is kept — notably extends, which negotiation's
	// intersection uses to prune an extension whose parent the platform does
	// not share (UCP intersection step 3).
	capabilities := make(map[string][]wire.Capability, len(s.profile.UCP.Capabilities))
	for name, capabilityList := range s.profile.UCP.Capabilities {
		capabilities[name] = slices.Clone(capabilityList)
	}
	return capabilities
}

// Configuration implements [ucp.StoreProvider].
func (s *connectorInstanceProvider) Configuration() ucp.Configuration {
	return ucp.Configuration{
		StoreID:    string(s.profile.StoreID),
		MerchantID: string(s.profile.MerchantID),
		Platform:   domain.CommerceSystemType(s.profile.Platform),
		Config:     nil, //TODO: where to get this - should be passed to conectors
	}
}

// PaymentHandlers implements [ucp.StoreProvider].
func (s *connectorInstanceProvider) PaymentHandlers() map[string][]wire.PaymentHandler {
	paymentHandlers := make(map[string][]wire.PaymentHandler)
	for _, handlers := range s.profile.UCP.PaymentHandlers {
		for _, handler := range handlers {
			paymentHandlers[handler.ID] = append(paymentHandlers[handler.ID], wire.PaymentHandler{
				ID:      handler.ID,
				Schema:  handler.Schema,
				Spec:    handler.Spec,
				Version: handler.Version,
				Config:  nil, //TODO: should we include config here? or is it only for the merchant to know?
			})
		}
	}
	return paymentHandlers
}

// Services implements [ucp.StoreProvider].
func (s *connectorInstanceProvider) Services() map[string][]wire.Service {
	services := make(map[string][]wire.Service)
	for _, svcs := range s.profile.UCP.Services {
		for _, service := range svcs {
			services[service.ID] = append(services[service.ID], service)
		}
	}
	return services
}

// SigningKeys implements [ucp.StoreProvider].
//
// CacheProfile.Keys and .SigningKeys are mutually exclusive per rendered
// generation (see CacheProfile's doc comment) — Keys is preferred since
// ProfileRenderer has published it for every generation from 2026-08-25 on;
// SigningKeys is the fallback for a store's snapshot from before that
// rollout, until its next rebuild.
func (s *connectorInstanceProvider) SigningKeys() []wire.JWK {
	src := s.profile.Keys
	if len(src) == 0 {
		src = s.profile.SigningKeys
	}
	keys := make([]wire.JWK, len(src))
	for i, key := range src {
		keys[i] = wire.JWK{
			Kid: key.Kid,
			Kty: key.Kty,
			Crv: key.Crv,
			X:   key.X,
			Y:   key.Y,
			Use: key.Use,
			Alg: key.Alg,
		}
	}
	return keys
}

// StorefrontURL implements [ucp.StoreProvider].
func (s *connectorInstanceProvider) StorefrontURL() string {
	return s.profile.StorefrontURL
}

// Version implements [ucp.StoreProvider].
func (s *connectorInstanceProvider) Version() string {
	return s.profile.UCP.Version
}

// StoreID implements [ucp.StoreProvider].
func (s *connectorInstanceProvider) StoreID() string {
	return s.profile.StoreID
}

var _ ucp.ProfileSource = (*CacheProfileSource)(nil)
