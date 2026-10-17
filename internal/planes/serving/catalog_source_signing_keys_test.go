// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/jwkmanagement"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/keypair"
	"github.com/stretchr/testify/assert"
)

type fakeSigningKeySource struct {
	kp  *jwkmanagement.JWKKeypair
	err error
}

func (f fakeSigningKeySource) GetActiveKeypair(_ context.Context, _ string) (*jwkmanagement.JWKKeypair, error) {
	return f.kp, f.err
}

func newCatalogSource(keys SigningKeySource) *MongoCatalogSource {
	return &MongoCatalogSource{keys: keys, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestActiveSigningKeys_ReturnsActivePublicJWK(t *testing.T) {
	src := newCatalogSource(fakeSigningKeySource{
		kp: &jwkmanagement.JWKKeypair{
			StoreID: "store-1",
			Status:  "active",
			JWK:     &keypair.JWKModel{Kid: "kid-1", Kty: "EC", Crv: "P-256", X: "x", Y: "y", Use: "sig", Alg: "ES256"},
		},
	})

	keys := src.activeSigningKeys(context.Background(), "store-1")

	assert.Len(t, keys, 1)
	assert.Equal(t, "kid-1", keys[0].Kid)
	assert.Equal(t, "EC", keys[0].Kty)
	assert.Equal(t, "P-256", keys[0].Crv)
}

func TestActiveSigningKeys_EmptyWhenNoActiveKey(t *testing.T) {
	src := newCatalogSource(fakeSigningKeySource{kp: nil})
	assert.Empty(t, src.activeSigningKeys(context.Background(), "store-1"))
}

func TestActiveSigningKeys_EmptyOnError(t *testing.T) {
	src := newCatalogSource(fakeSigningKeySource{err: errors.New("key store down")})
	assert.Empty(t, src.activeSigningKeys(context.Background(), "store-1"))
}

func TestActiveSigningKeys_EmptyWhenNoKeySource(t *testing.T) {
	src := newCatalogSource(nil)
	assert.Empty(t, src.activeSigningKeys(context.Background(), "store-1"))
}
