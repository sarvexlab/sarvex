// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package monitoring

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgdomain "github.com/sarvexlab/sarvex/internal/pkg/domain"
	"github.com/sarvexlab/sarvex/internal/pkg/serving"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
	"github.com/sarvexlab/sarvex/internal/planes/control/catalog"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/store"
)

const testPlatform pkgdomain.CommerceSystemType = "test-platform"

// fakeStoreService satisfies store.Service with a single fixed store,
// enough for connectorConsistencySource's storeID -> (merchantID, platform)
// resolution.
type fakeStoreService struct {
	store.StubService
	byID map[domain.StoreID]domain.Store
}

func (f *fakeStoreService) GetByID(_ context.Context, storeID domain.StoreID) (domain.Store, error) {
	st, ok := f.byID[storeID]
	if !ok {
		return domain.Store{}, errors.New("store not found")
	}
	return st, nil
}

// fakeIdentity is a minimal ucp.StoreProvider carrying only the
// registry-lookup identity, mirroring sync/api/entry.go's
// connectorRegistryIdentity.
type fakeIdentity struct {
	merchantID, storeID string
	platform            pkgdomain.CommerceSystemType
}

func (f *fakeIdentity) Configuration() ucp.Configuration {
	return ucp.Configuration{MerchantID: f.merchantID, StoreID: f.storeID, Platform: f.platform}
}
func (f *fakeIdentity) StoreID() string                                   { return f.storeID }
func (f *fakeIdentity) Version() string                                   { return "" }
func (f *fakeIdentity) Capabilities() map[string][]wire.Capability        { return nil }
func (f *fakeIdentity) Services() map[string][]wire.Service               { return nil }
func (f *fakeIdentity) PaymentHandlers() map[string][]wire.PaymentHandler { return nil }
func (f *fakeIdentity) SigningKeys() []wire.JWK                           { return nil }
func (f *fakeIdentity) StorefrontURL() string                             { return "" }

// fakeConnector implements base.Connector, erroring on everything except
// FetchPricing/FetchInventory, which return configured fixtures.
type fakeConnector struct {
	pricing    *base.PricingEntity
	pricingErr error
	inventory  *base.InventoryEntity
	invErr     error
}

var errFakeConnectorNotImplemented = errors.New("fakeConnector: not implemented")

func (f *fakeConnector) CatalogSearch(context.Context, string, *base.CatalogSearchRequest) (*base.CatalogSearchResponse, error) {
	return nil, errFakeConnectorNotImplemented
}
func (f *fakeConnector) CatalogLookup(context.Context, string, string) (*base.CatalogLookupResponse, error) {
	return nil, errFakeConnectorNotImplemented
}
func (f *fakeConnector) UpdateProduct(context.Context, string, string, map[string]any) error {
	return errFakeConnectorNotImplemented
}
func (f *fakeConnector) CartCreate(context.Context, string, *base.CreateCartRequest) (*base.CartResponse, error) {
	return nil, errFakeConnectorNotImplemented
}
func (f *fakeConnector) CartGet(context.Context, string, string) (*base.CartResponse, error) {
	return nil, errFakeConnectorNotImplemented
}
func (f *fakeConnector) CartUpdate(context.Context, string, string, *base.UpdateCartRequest) (*base.CartResponse, error) {
	return nil, errFakeConnectorNotImplemented
}
func (f *fakeConnector) CheckoutCreate(context.Context, string, *base.CreateCheckoutRequest) (*base.CheckoutResponse, error) {
	return nil, errFakeConnectorNotImplemented
}
func (f *fakeConnector) CheckoutGet(context.Context, string, string) (*base.CheckoutResponse, error) {
	return nil, errFakeConnectorNotImplemented
}
func (f *fakeConnector) CheckoutUpdate(context.Context, string, string, *base.UpdateCheckoutRequest) (*base.CheckoutResponse, error) {
	return nil, errFakeConnectorNotImplemented
}
func (f *fakeConnector) CheckoutComplete(context.Context, string, string, *base.CompleteCheckoutRequest) (*base.CheckoutResponse, error) {
	return nil, errFakeConnectorNotImplemented
}
func (f *fakeConnector) CheckoutCancel(context.Context, string, string) (*base.CheckoutResponse, error) {
	return nil, errFakeConnectorNotImplemented
}
func (f *fakeConnector) CheckoutURL(context.Context, string, string) (string, error) {
	return "", errFakeConnectorNotImplemented
}
func (f *fakeConnector) OrderGet(context.Context, string, string) (*base.OrderEvent, error) {
	return nil, errFakeConnectorNotImplemented
}
func (f *fakeConnector) FetchProduct(context.Context, string, string) (*base.CatalogEntity, error) {
	return nil, errFakeConnectorNotImplemented
}
func (f *fakeConnector) FetchInventory(_ context.Context, _, _ string) (*base.InventoryEntity, error) {
	if f.invErr != nil {
		return nil, f.invErr
	}
	return f.inventory, nil
}
func (f *fakeConnector) FetchPricing(_ context.Context, _, _ string) (*base.PricingEntity, error) {
	if f.pricingErr != nil {
		return nil, f.pricingErr
	}
	return f.pricing, nil
}
func (f *fakeConnector) FetchCatalog(context.Context, string, int) ([]base.CatalogEntity, error) {
	return nil, errFakeConnectorNotImplemented
}
func (f *fakeConnector) Ping(context.Context, string) (base.HealthStatus, error) {
	return "", errFakeConnectorNotImplemented
}

var _ base.Connector = (*fakeConnector)(nil)

// fakeServingCache is a test double for ServingCacheReader.
type fakeServingCache struct {
	product *serving.CacheProduct
	err     error
}

func (f *fakeServingCache) GetProduct(_ context.Context, _, _ string) (*serving.CacheProduct, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.product, nil
}

// fakeProductRepo is a test double for catalog.ProductRepository.
type fakeProductRepo struct {
	products []*catalog.ProductModel
}

func (f *fakeProductRepo) Get(context.Context, domain.MerchantID, domain.StoreID, string) (*catalog.ProductModel, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeProductRepo) List(_ context.Context, _ domain.MerchantID, _ domain.StoreID, afterSKU string, limit int, _ *int, _ *bool) ([]*catalog.ProductModel, bool, error) {
	start := 0
	if afterSKU != "" {
		for i, p := range f.products {
			if p.SKU == afterSKU {
				start = i + 1
				break
			}
		}
	}
	end := start + limit
	hasMore := end < len(f.products)
	if end > len(f.products) {
		end = len(f.products)
	}
	if start > len(f.products) {
		return nil, false, nil
	}
	return f.products[start:end], hasMore, nil
}

func newTestSource(t *testing.T, st domain.Store, connector *fakeConnector, sc *fakeServingCache, products *fakeProductRepo) ConsistencySource {
	t.Helper()
	stores := &fakeStoreService{byID: map[domain.StoreID]domain.Store{st.StoreID: st}}
	provider := func(merchantID, storeID string) (ucp.StoreProvider, error) {
		return &fakeIdentity{merchantID: merchantID, storeID: storeID, platform: testPlatform}, nil
	}
	registry := base.New(base.CredentialsFunc(func(context.Context, string) (map[string]any, error) {
		return nil, nil
	}), slog.Default())
	registry.RegisterFactory(testPlatform, func(base.MerchantConfig, *slog.Logger) (base.Connector, error) {
		return connector, nil
	})
	return NewConnectorConsistencySource(stores, provider, registry, sc, products)
}

func TestConsistencySource_GetStorePrice_OK(t *testing.T) {
	st := domain.Store{StoreID: "store-1", MerchantID: "merch-1", Platform: domain.Platform(testPlatform)}
	connector := &fakeConnector{pricing: &base.PricingEntity{SKU: "sku-1", PriceMinor: 1999, Currency: "USD"}}
	source := newTestSource(t, st, connector, &fakeServingCache{}, &fakeProductRepo{})

	price, err := source.GetStorePrice(context.Background(), "store-1", "sku-1")
	require.NoError(t, err)
	assert.Equal(t, 1999, price.AmountMinor)
	assert.Equal(t, "USD", price.Currency)
}

func TestConsistencySource_GetStorePrice_UnknownStore(t *testing.T) {
	source := newTestSource(t, domain.Store{StoreID: "store-1", MerchantID: "merch-1", Platform: domain.Platform(testPlatform)}, &fakeConnector{}, &fakeServingCache{}, &fakeProductRepo{})

	_, err := source.GetStorePrice(context.Background(), "store-unknown", "sku-1")
	assert.Error(t, err)
}

func TestConsistencySource_GetServedPrice_OK(t *testing.T) {
	st := domain.Store{StoreID: "store-1", MerchantID: "merch-1", Platform: domain.Platform(testPlatform)}
	sc := &fakeServingCache{product: &serving.CacheProduct{SKU: "sku-1", Price: serving.Money{AmountMinor: 1999, Currency: "USD"}}}
	source := newTestSource(t, st, &fakeConnector{}, sc, &fakeProductRepo{})

	price, err := source.GetServedPrice(context.Background(), "store-1", "sku-1")
	require.NoError(t, err)
	assert.Equal(t, 1999, price.AmountMinor)
}

func TestConsistencySource_GetStockStatus_OK(t *testing.T) {
	st := domain.Store{StoreID: "store-1", MerchantID: "merch-1", Platform: domain.Platform(testPlatform)}
	qty := 5
	connector := &fakeConnector{inventory: &base.InventoryEntity{SKU: "sku-1", InStock: true, Quantity: &qty}}
	source := newTestSource(t, st, connector, &fakeServingCache{}, &fakeProductRepo{})

	stock, err := source.GetStockStatus(context.Background(), "store-1", "sku-1")
	require.NoError(t, err)
	assert.True(t, stock.InStock)
	require.NotNil(t, stock.Quantity)
	assert.Equal(t, 5, *stock.Quantity)
}

func TestConsistencySource_ListChangedProducts_PagesAndCaps(t *testing.T) {
	st := domain.Store{StoreID: "store-1", MerchantID: "merch-1", Platform: domain.Platform(testPlatform)}
	products := &fakeProductRepo{}
	for i := range 250 {
		products.products = append(products.products, &catalog.ProductModel{SKU: fmtSKU(i)})
	}
	source := newTestSource(t, st, &fakeConnector{}, &fakeServingCache{}, products)

	skus, err := source.ListChangedProducts(context.Background(), "store-1", time.Now())
	require.NoError(t, err)
	assert.Len(t, skus, 250)
}

func fmtSKU(i int) string {
	return "sku-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
}
