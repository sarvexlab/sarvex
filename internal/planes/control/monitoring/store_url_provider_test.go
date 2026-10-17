// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package monitoring

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/store"
)

type fakeProfileReader struct {
	profile *domain.StoreBusinessProfile
	err     error
}

func (f *fakeProfileReader) GetBusinessProfile(context.Context, domain.StoreID) (*domain.StoreBusinessProfile, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.profile, nil
}

func TestProfileStoreURLProvider_OK(t *testing.T) {
	provider := NewProfileStoreURLProvider(&fakeProfileReader{profile: &domain.StoreBusinessProfile{
		StoreID: "store-1", StorefrontURL: "https://example.com",
	}})

	url, err := provider.GetStorefrontURL(context.Background(), "store-1")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com", url)
}

func TestProfileStoreURLProvider_NoProfileYet(t *testing.T) {
	provider := NewProfileStoreURLProvider(&fakeProfileReader{err: store.ErrProfileNotFound})

	url, err := provider.GetStorefrontURL(context.Background(), "store-1")
	require.NoError(t, err)
	assert.Empty(t, url)
}

func TestProfileStoreURLProvider_OtherError(t *testing.T) {
	provider := NewProfileStoreURLProvider(&fakeProfileReader{err: errors.New("mongo down")})

	_, err := provider.GetStorefrontURL(context.Background(), "store-1")
	assert.Error(t, err)
}
