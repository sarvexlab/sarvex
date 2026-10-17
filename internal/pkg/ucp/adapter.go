// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package ucp

import (
	"context"
	"errors"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// ErrStoreProfileNotFound is returned by a StoreProviderFunc when the store
// has no cached/published profile yet (e.g. paired but not yet imported, or
// an unknown store_id). Distinguished from other errors so callers like
// negotiation.WithStoreConfigProvider can respond 404 rather than 500 — the
// serving plane's discovery/read paths never call live (sarvex-v1-architecture.md invariant
// #2), so an unpopulated cache is an expected, well-defined state, not a
// server failure.
var ErrStoreProfileNotFound = errors.New("ucp: store profile not found")

type StoreProvider interface {
	StoreID() string

	// Version returns the UCP protocol version this instance supports (YYYY-MM-DD).
	Version() string

	// UCPServices returns the services map for the business profile.
	Services() map[string][]wire.Service

	// UCPCapabilities returns the capabilities map as defined in the business profile.
	Capabilities() map[string][]wire.Capability

	// UCPPaymentHandlers returns the payment handlers map.
	PaymentHandlers() map[string][]wire.PaymentHandler

	// UCPSigningKeys returns the JWK signing keys published by this business.
	SigningKeys() []wire.JWK

	// UCPStorefrontURL returns the merchant's public storefront URL for continue_url in error responses.
	// Returns empty string if not available.
	StorefrontURL() string

	Configuration() Configuration
}

type StoreProviderFunc func(ctx context.Context, storeID string) (StoreProvider, error)

type StoreProviderByStoreIDFunc func(merchantID, storeID string) (StoreProvider, error)

// BusinessProfileFromStoreProfile maps a UCPInstanceProvider to a wire.Profile
// for use by the negotiation engine.
func BusinessProfileFromStoreProfile(stroreProfile StoreProvider) *wire.Profile {
	return &wire.Profile{
		UCP: wire.UCP{
			Version:         wire.Version(stroreProfile.Version()),
			Services:        stroreProfile.Services(),
			Capabilities:    stroreProfile.Capabilities(),
			PaymentHandlers: stroreProfile.PaymentHandlers(),
		},
		Keys: stroreProfile.SigningKeys(),
	}
}
