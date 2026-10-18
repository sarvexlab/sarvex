// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// stubCatalogSource provides test data (implements CatalogSource for per-store testing).
type stubCatalogSource struct {
	profile  *StoreProfileData
	products []*ProductData
	err      error
}

func (s *stubCatalogSource) GetStoreProfile(ctx context.Context, storeID string) (*StoreProfileData, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.profile, nil
}

func (s *stubCatalogSource) SaveStoreProfile(ctx context.Context, profile *StoreProfileData) error {
	if s.err != nil {
		return s.err
	}
	s.profile = profile
	return nil
}

func (s *stubCatalogSource) ListProductsByStore(ctx context.Context, storeID string) ([]*ProductData, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.products, nil
}

// stubRedisCache captures swapped snapshots.
type stubRedisCache struct {
	swappedSnapshots []*CacheSnapshot
	pendingMarked    []string
}

func (s *stubRedisCache) MarkPendingChange(_ context.Context, storeID string) error {
	s.pendingMarked = append(s.pendingMarked, storeID)
	return nil
}

func (s *stubRedisCache) SwapSnapshot(ctx context.Context, snapshot *CacheSnapshot) error {
	s.swappedSnapshots = append(s.swappedSnapshots, snapshot)
	return nil
}

// stubEventBus captures published events.
type stubEventBus struct {
	publishedEvents []eventbus.DomainEvent
}

func (s *stubEventBus) Publish(ctx context.Context, event eventbus.DomainEvent) error {
	s.publishedEvents = append(s.publishedEvents, event)
	return nil
}

func (s *stubEventBus) Subscribe(ctx context.Context, topic string, handler eventbus.EventHandler) (eventbus.Subscription, error) {
	return nil, nil
}

func (s *stubEventBus) Unsubscribe(ctx context.Context, sub eventbus.Subscription) error {
	return nil
}

func (s *stubEventBus) Close() error {
	return nil
}

// testCatalogue loads the catalogue for testing
func testCatalogue(t *testing.T) catalogue.Catalogue {
	cat, err := catalogue.LoadCatalogue(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	require.NotNil(t, cat)
	return cat
}

func TestCacheRebuilderPopulatesWebhookSigningKID(t *testing.T) {
	cache := &stubRedisCache{}
	eventBus := &stubEventBus{}

	signingKID := "sig-key-2026-01"
	source := &stubCatalogSource{
		profile: &StoreProfileData{
			StoreID:       "store1",
			MerchantID:    "merchant1",
			LegalName:     "Test Store",
			SupportEmail:  "support@example.com",
			StorefrontURL: "https://store.example.com",
			Capabilities:  map[string][]wire.Capability{},
			SigningKeys: []wire.JWK{
				{
					Kid: signingKID,
					X:   "x_coord",
					Y:   "y_coord",
				},
			},
		},
		products: []*ProductData{},
	}

	rebuilder := NewCacheRebuilder(cache, source, testCatalogue(t), "https://serving.test", eventBus, slog.New(slog.NewTextHandler(io.Discard, nil)))
	err := rebuilder.RebuildSnapshot(context.Background(), "store1")
	require.NoError(t, err)

	require.Equal(t, 1, len(cache.swappedSnapshots))
	snapshot := cache.swappedSnapshots[0]
	// Rendered at specall.Default().Latest() (2026-08-25 today): keys, not the legacy signing_keys.
	require.GreaterOrEqual(t, len(snapshot.Profile.Keys), 1)
	assert.Equal(t, signingKID, snapshot.Profile.Keys[0].Kid)
}

func TestCacheRebuilderPopulatesShippingZones(t *testing.T) {
	cache := &stubRedisCache{}
	eventBus := &stubEventBus{}

	shippingZones := map[string]ShippingZone{
		"us-standard": {
			ID:   "us-standard",
			Name: "United States Standard",
			Rates: []Rate{
				{
					Method: "ground",
					Cost:   Money{AmountMinor: 500, Currency: "USD"},
				},
			},
		},
		"us-express": {
			ID:   "us-express",
			Name: "United States Express",
			Rates: []Rate{
				{
					Method: "express",
					Cost:   Money{AmountMinor: 1500, Currency: "USD"},
				},
			},
		},
	}

	source := &stubCatalogSource{
		profile: &StoreProfileData{
			StoreID:       "store1",
			MerchantID:    "merchant1",
			LegalName:     "Test Store",
			SupportEmail:  "support@example.com",
			StorefrontURL: "https://store.example.com",
			ShippingZones: shippingZones,
			Capabilities:  map[string][]wire.Capability{},
			SigningKeys:   []wire.JWK{},
		},
		products: []*ProductData{},
	}

	rebuilder := NewCacheRebuilder(cache, source, testCatalogue(t), "https://serving.test", eventBus, slog.New(slog.NewTextHandler(io.Discard, nil)))
	err := rebuilder.RebuildSnapshot(context.Background(), "store1")
	require.NoError(t, err)

	require.Equal(t, 1, len(cache.swappedSnapshots))
	snapshot := cache.swappedSnapshots[0]
	assert.Equal(t, 2, len(snapshot.Shipping.Zones))
	assert.Equal(t, "United States Standard", snapshot.Shipping.Zones["us-standard"].Name)
	assert.Equal(t, "United States Express", snapshot.Shipping.Zones["us-express"].Name)
}

func TestCacheRebuilderPopulatesAllMerchantConfig(t *testing.T) {
	cache := &stubRedisCache{}
	eventBus := &stubEventBus{}

	signingKID := "key-2026"
	shippingZones := map[string]ShippingZone{
		"zone1": {
			ID:   "zone1",
			Name: "Zone 1",
			Rates: []Rate{
				{Method: "standard", Cost: Money{AmountMinor: 500, Currency: "USD"}},
			},
		},
	}

	source := &stubCatalogSource{
		profile: &StoreProfileData{
			StoreID:       "store1",
			MerchantID:    "merchant1",
			LegalName:     "Test Store",
			SupportEmail:  "support@example.com",
			StorefrontURL: "https://store.example.com",
			ShippingZones: shippingZones,
			Capabilities:  map[string][]wire.Capability{},
			SigningKeys: []wire.JWK{
				{Kid: signingKID, X: "x", Y: "y"},
			},
		},
		products: []*ProductData{
			{
				SKU:           "SKU001",
				Title:         "Product 1",
				Description:   "A great product",
				PriceMinor:    2999,
				Currency:      "USD",
				InStock:       true,
				Quantity:      intPtr(100),
				Attributes:    map[string]string{"color": "red"},
				LastScoreTime: time.Now().UTC(),
			},
		},
	}

	rebuilder := NewCacheRebuilder(cache, source, testCatalogue(t), "https://serving.test", eventBus, slog.New(slog.NewTextHandler(io.Discard, nil)))
	err := rebuilder.RebuildSnapshot(context.Background(), "store1")
	require.NoError(t, err)

	require.Equal(t, 1, len(cache.swappedSnapshots))
	snapshot := cache.swappedSnapshots[0]

	// Verify all config is populated. Rendered at specall.Default().Latest() (2026-08-25 today):
	// keys, not the legacy signing_keys.
	require.GreaterOrEqual(t, len(snapshot.Profile.Keys), 1)
	assert.Equal(t, signingKID, snapshot.Profile.Keys[0].Kid)
	assert.Equal(t, 1, len(snapshot.Shipping.Zones))
	assert.Equal(t, "Zone 1", snapshot.Shipping.Zones["zone1"].Name)
	assert.Equal(t, 1, len(snapshot.Products))
}

func intPtr(i int) *int {
	return &i
}
