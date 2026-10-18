// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package ucp

import (
	"context"
)

// ProfileSource retrieves the current UCP profile bytes for a merchant.
// This interface bridges the control plane (which needs profiles) to the serving
// plane (which caches them). Implemented by serving cache adapter; stubbed here
// for now until G2 wires the real serving cache connection.
type ProfileSource interface {
	// GetProfile returns the rendered UCP profile for the store, with etag for caching.
	// If the profile is not yet available, returns nil and no error so the caller
	// can degrade to last-known-good.
	GetProfile(ctx context.Context, storeID string) (body []byte, etag string, err error)
	GetStructuredProfile(ctx context.Context, storeID string) (provider StoreProvider, err error)
}

// StubProfileSource is a no-op implementation used where no profile source is
// needed (planes that do not serve the profile doorway).
type StubProfileSource struct{}

func (s *StubProfileSource) GetProfile(ctx context.Context, storeID string) ([]byte, string, error) {
	return nil, "", nil
}

func (s *StubProfileSource) GetStructuredProfile(ctx context.Context, storeID string) (StoreProvider, error) {
	return nil, nil
}

var _ ProfileSource = (*StubProfileSource)(nil)
