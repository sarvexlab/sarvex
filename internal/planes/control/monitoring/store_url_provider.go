// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package monitoring

import (
	"context"
	"errors"
	"fmt"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/store"
)

// ProfileReader is the narrow read-only slice of store.ProfileService this
// package needs — a single store's business profile, for its StorefrontURL.
type ProfileReader interface {
	GetBusinessProfile(ctx context.Context, storeID domain.StoreID) (*domain.StoreBusinessProfile, error)
}

// profileStoreURLProvider implements StoreURLProvider from the store
// profile service's StorefrontURL field (set by the merchant via
// PUT /stores/:store_id/profile).
type profileStoreURLProvider struct {
	profiles ProfileReader
}

// NewProfileStoreURLProvider constructs a StoreURLProvider backed by the
// control plane's store profile service.
func NewProfileStoreURLProvider(profiles ProfileReader) StoreURLProvider {
	return &profileStoreURLProvider{profiles: profiles}
}

// GetStorefrontURL returns "" (not an error) when the store has no saved
// profile yet or no domain set — probeProfileEdge already treats an empty
// URL as "deferred pending storefront URL configuration" rather than a
// failure.
func (p *profileStoreURLProvider) GetStorefrontURL(ctx context.Context, storeID string) (string, error) {
	profile, err := p.profiles.GetBusinessProfile(ctx, domain.StoreID(storeID))
	if errors.Is(err, store.ErrProfileNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get business profile: %w", err)
	}
	return profile.StorefrontURL, nil
}
