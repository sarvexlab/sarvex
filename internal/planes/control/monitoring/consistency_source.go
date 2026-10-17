// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package monitoring

import (
	"context"
	"fmt"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/serving"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/planes/control/catalog"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/store"
)

// ServingCacheReader is the narrow read-only slice of serving cache
// this package needs — a single product lookup by store/sku. Deliberately
// not the full serving.ServingCache interface (Search/GetProfile are
// irrelevant here).
type ServingCacheReader interface {
	GetProduct(ctx context.Context, storeID, sku string) (*serving.CacheProduct, error)
}

// listChangedProductsLimit caps how many SKUs a single sweep checks. This is
// a periodic background job over a store's whole catalog, not a paginated
// user-facing API, so an explicit bound keeps one large catalog from making
// a sweep run unboundedly long.
const listChangedProductsLimit = 500

// connectorConsistencySource implements ConsistencySource against the live
// connector (store side, via base.Connector.FetchPricing/FetchInventory —
// the same store-API pull methods the sync plane's ingestion pipeline
// uses), the serving cache (served side), and the catalog store (change
// detection).
type connectorConsistencySource struct {
	stores       store.Service
	provider     ucp.StoreProviderByStoreIDFunc
	registry     *base.Registry
	servingCache ServingCacheReader
	products     catalog.ProductRepository
}

// NewConnectorConsistencySource constructs a ConsistencySource backed by
// live connector calls, the serving cache, and the catalog store.
func NewConnectorConsistencySource(
	stores store.Service,
	provider ucp.StoreProviderByStoreIDFunc,
	registry *base.Registry,
	servingCache ServingCacheReader,
	products catalog.ProductRepository,
) ConsistencySource {
	return &connectorConsistencySource{
		stores:       stores,
		provider:     provider,
		registry:     registry,
		servingCache: servingCache,
		products:     products,
	}
}

// resolveConnector resolves the base.Connector for storeID via the
// registry, the same pattern the sync plane's ingestion pipeline uses
// (internal/planes/sync/ingestion.go resolveConnector). Also returns the
// store record, since callers need MerchantID for catalog-store reads.
func (s *connectorConsistencySource) resolveConnector(ctx context.Context, storeID string) (base.Connector, domain.Store, error) {
	st, err := s.stores.GetByID(ctx, domain.StoreID(storeID))
	if err != nil {
		return nil, domain.Store{}, fmt.Errorf("resolve store %s: %w", storeID, err)
	}
	instance, err := s.provider(string(st.MerchantID), storeID)
	if err != nil {
		return nil, st, fmt.Errorf("resolve store provider: %w", err)
	}
	connector, err := s.registry.Get(instance)
	if err != nil {
		return nil, st, fmt.Errorf("resolve connector: %w", err)
	}
	return connector, st, nil
}

func (s *connectorConsistencySource) GetStorePrice(ctx context.Context, storeID, sku string) (*PriceData, error) {
	connector, _, err := s.resolveConnector(ctx, storeID)
	if err != nil {
		return nil, err
	}
	pricing, err := connector.FetchPricing(ctx, storeID, sku)
	if err != nil {
		return nil, fmt.Errorf("fetch store pricing: %w", err)
	}
	return &PriceData{AmountMinor: int(pricing.PriceMinor), Currency: pricing.Currency, UpdatedAt: time.Now().UTC()}, nil
}

func (s *connectorConsistencySource) GetServedPrice(ctx context.Context, storeID, sku string) (*PriceData, error) {
	product, err := s.servingCache.GetProduct(ctx, storeID, sku)
	if err != nil {
		return nil, fmt.Errorf("get served product: %w", err)
	}
	return &PriceData{AmountMinor: product.Price.AmountMinor, Currency: product.Price.Currency, UpdatedAt: time.Now().UTC()}, nil
}

func (s *connectorConsistencySource) GetStockStatus(ctx context.Context, storeID, sku string) (*StockData, error) {
	connector, _, err := s.resolveConnector(ctx, storeID)
	if err != nil {
		return nil, err
	}
	inv, err := connector.FetchInventory(ctx, storeID, sku)
	if err != nil {
		return nil, fmt.Errorf("fetch store inventory: %w", err)
	}
	return &StockData{InStock: inv.InStock, Quantity: inv.Quantity, UpdatedAt: time.Now().UTC()}, nil
}

// ListChangedProducts ignores since and returns up to listChangedProductsLimit
// SKUs from the catalog store: catalog.ProductRepository has no "changed
// since" index, so this is a full-catalog sweep — matching
// ReconciliationSweeper's own description ("full-catalog diffs"), which is
// what the reconciliation safety net is meant to run.
func (s *connectorConsistencySource) ListChangedProducts(ctx context.Context, storeID string, _ time.Time) ([]string, error) {
	st, err := s.stores.GetByID(ctx, domain.StoreID(storeID))
	if err != nil {
		return nil, fmt.Errorf("resolve store %s: %w", storeID, err)
	}

	var skus []string
	cursor := ""
	for len(skus) < listChangedProductsLimit {
		batch, hasMore, err := s.products.List(ctx, st.MerchantID, domain.StoreID(storeID), cursor, 100, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("list products: %w", err)
		}
		for _, p := range batch {
			skus = append(skus, p.SKU)
		}
		if !hasMore || len(batch) == 0 {
			break
		}
		cursor = batch[len(batch)-1].SKU
	}
	return skus, nil
}
