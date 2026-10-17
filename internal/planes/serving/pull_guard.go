// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"fmt"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// errServingPull is returned by every store-API pull method on a connector
// the serving plane holds.
func errServingPull(method, storeID string) error {
	return fmt.Errorf(
		"serving plane must not call store-API pull method %s (store %s): the serving plane reads only the serving cache — architecture invariant §4.5.2",
		method, storeID,
	)
}

// blockPullConnector wraps a connector so the serving plane keeps the live
// checkout calls it is entitled to make and loses the store-API pull calls it
// is not.
//
// The split is base.Connector's own: everything under its "Store-API pull
// (sync-plane only — never call from the serving plane)" heading errors here;
// catalog reads, cart, checkout and order lookups pass through.
//
// This guard used to live on the credentials provider, which always errored.
// That is a blunter instrument than it looks: base.Registry hands one
// provider to the whole connector, so refusing credentials refused
// CheckoutCreate as surely as FetchCatalog, and the serving plane could not
// complete a checkout at all. Guarding the methods themselves keeps the
// invariant enforced at runtime — an accidental pull still fails loudly,
// naming the method and the invariant — without taking the permitted call
// down with it.
type blockPullConnector struct {
	base.Connector
	storeID string
}

func (c blockPullConnector) FetchProduct(_ context.Context, storeID string, _ string) (*base.CatalogEntity, error) {
	return nil, errServingPull("FetchProduct", storeID)
}

func (c blockPullConnector) FetchInventory(_ context.Context, storeID string, _ string) (*base.InventoryEntity, error) {
	return nil, errServingPull("FetchInventory", storeID)
}

func (c blockPullConnector) FetchPricing(_ context.Context, storeID string, _ string) (*base.PricingEntity, error) {
	return nil, errServingPull("FetchPricing", storeID)
}

func (c blockPullConnector) FetchCatalog(_ context.Context, storeID string, _ int) ([]base.CatalogEntity, error) {
	return nil, errServingPull("FetchCatalog", storeID)
}

func (c blockPullConnector) Ping(_ context.Context, storeID string) (base.HealthStatus, error) {
	return "", errServingPull("Ping", storeID)
}

// UpdateProduct writes back to the merchant's catalog. Not under the pull
// heading, but a write the sync plane owns (it applies rule-based
// remediations); the serving plane has no business calling it either.
func (c blockPullConnector) UpdateProduct(_ context.Context, storeID string, _ string, _ map[string]any) error {
	return errServingPull("UpdateProduct", storeID)
}

var _ base.Connector = blockPullConnector{}
