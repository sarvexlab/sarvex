// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// CacheWriter provides write access to the serving cache for snapshot swaps.
type CacheWriter interface {
	// SwapSnapshot atomically replaces the snapshot for a merchant.
	SwapSnapshot(ctx context.Context, snapshot *CacheSnapshot) error

	// MarkPendingChange records that the store's catalog changed and the
	// served snapshot has not caught up yet. Cleared by the next successful
	// SwapSnapshot; read by Watermark to decide staleness.
	MarkPendingChange(ctx context.Context, storeID string) error
}

// CatalogSource provides product and business-fact data for cache rebuilding and store profile management.
// Reads from the catalog store (sync plane authority for products, control plane authority for profiles).
// Per sarvex-v1-architecture.md A10.4: serving data is partitioned by store_id, each store owned by one merchant.
type CatalogSource interface {
	// GetStoreProfile returns the business profile and store configuration for a store (legal entity, shipping, signing keys, etc).
	GetStoreProfile(ctx context.Context, storeID string) (*StoreProfileData, error)
	// SaveStoreProfile persists a store profile (called by control plane during onboarding or updates).
	SaveStoreProfile(ctx context.Context, profile *StoreProfileData) error
	// ListProductsByStore returns all products for a store from the catalog store.
	ListProductsByStore(ctx context.Context, storeID string) ([]*ProductData, error)
}

// StoreProfileData holds store configuration and business facts used to build the serving-cache profile.
// Each store is owned by one merchant; per sarvex-v1-architecture.md A10.4 tenant isolation.
type StoreProfileData struct {
	StoreID            string
	Platform           string                       // "shopify" | "bigcommerce" | "magento" | "commercetools" | "woocommerce"
	MerchantID         string                       // denormalized for backward compat
	LegalName          string                       // From merchant account
	SupportEmail       string                       // From merchant account
	StorefrontURL      string                       // Store's public domain
	FulfillmentRegions []string                     // Store-specific fulfillment regions
	ShippingZones      map[string]ShippingZone      // Zones keyed by zone ID
	Capabilities       map[string][]wire.Capability // Capabilities enabled for this store
	SigningKeys        []wire.JWK                   // Store's signing keys

	// PaymentHandlers is the merchant's payment-handler choice for this store,
	// keyed by handler name (reverse-domain). Nil means the store has no
	// payment configuration at all — a distinct state from an empty one, which
	// the merchant set deliberately (see ProfileRenderer.buildPaymentHandlers).
	PaymentHandlers map[string]PaymentHandlerSetting
}

// PaymentHandlerSetting is one handler's merchant-supplied state: whether it is
// enabled and its public parameters. Secrets never appear here — the control
// plane refuses parameters a handler does not declare public.
type PaymentHandlerSetting struct {
	Enabled bool
	Params  map[string]string
}

// ProductData is the catalog-store representation of a product.
type ProductData struct {
	SKU           string
	Title         string
	Description   string
	PriceMinor    int // Price in minor units
	Currency      string
	InStock       bool
	Quantity      *int
	Attributes    map[string]string
	LastScoreTime time.Time
}

// CacheRebuilder watches for catalog changes and atomically refreshes the serving cache.
// Uses ProfileRenderer for catalogue-driven profile generation.
type CacheRebuilder struct {
	cache           CacheWriter
	catalogSource   CatalogSource
	profileRenderer *ProfileRenderer
	eventPublisher  eventbus.EventBus
	logger          *slog.Logger
}

// NewCacheRebuilder creates a cache rebuilder with catalogue-driven profile rendering.
func NewCacheRebuilder(
	cache CacheWriter,
	catalogSource CatalogSource,
	cat catalogue.Catalogue,
	servingOrigin string,
	eventPublisher eventbus.EventBus,
	logger *slog.Logger,
) *CacheRebuilder {
	// Initialize profile renderer with the catalogue for spec-compliant UCP
	// profiles. servingOrigin is what lets each rendered profile declare where
	// its services actually answer.
	profileRenderer := NewProfileRenderer(cat, servingOrigin, logger)

	return &CacheRebuilder{
		cache:           cache,
		catalogSource:   catalogSource,
		profileRenderer: profileRenderer,
		eventPublisher:  eventPublisher,
		logger:          logger,
	}
}

// WithProfileRenderer sets the profile renderer (for testing or custom rendering).
func (r *CacheRebuilder) WithProfileRenderer(pr *ProfileRenderer) *CacheRebuilder {
	r.profileRenderer = pr
	return r
}

// RebuildSnapshot rebuilds and atomically swaps a store's serving cache snapshot.
// Per sarvex-v1-architecture.md A10.4: serving data is partitioned by store_id, each store owned by one merchant.
// Called by the event listener when a rebuild is requested (e.g., after catalog change).
func (r *CacheRebuilder) RebuildSnapshot(ctx context.Context, storeID string) error {
	start := time.Now()

	// Fetch store profile and products from catalog store.
	profile, err := r.catalogSource.GetStoreProfile(ctx, storeID)
	if err != nil {
		r.logger.Error("failed to get store profile", "error", err, "store_id", storeID)
		return fmt.Errorf("get store profile: %w", err)
	}

	products, err := r.catalogSource.ListProductsByStore(ctx, storeID)
	if err != nil {
		r.logger.Error("failed to list products", "error", err, "store_id", storeID)
		return fmt.Errorf("list products: %w", err)
	}

	// Build cache snapshot for this store.
	snapshot := r.buildSnapshot(storeID, profile, products)

	// Atomically swap the snapshot.
	if err := r.cache.SwapSnapshot(ctx, snapshot); err != nil {
		if r.logger != nil {
			r.logger.Error("failed to swap snapshot", "error", err, "store_id", storeID, "product_count", len(products))
		}
		return fmt.Errorf("swap snapshot: %w", err)
	}

	elapsed := time.Since(start)
	if r.logger != nil {
		r.logger.Info("snapshot rebuilt and swapped", "store_id", storeID, "merchant_id", profile.MerchantID, "product_count", len(products), "elapsed_ms", elapsed.Milliseconds())
	}

	// Emit snapshot-swapped event (informational, for journey and monitoring).
	event := &SnapshotSwappedEvent{
		StoreID_:     storeID,
		MerchantID_:  profile.MerchantID,
		ProductCount: len(products),
		BuiltAt_:     snapshot.BuiltAt,
		SourceSync_:  snapshot.SourceSync,
		EmittedAt_:   time.Now().UTC(),
	}

	if err := r.publishSnapshotSwapped(ctx, event); err != nil {
		r.logger.Error("failed to publish snapshot-swapped event", "error", err, "store_id", storeID)
		// Log but don't fail the rebuild — the snapshot is already swapped.
	}

	return nil
}

// buildSnapshot constructs a CacheSnapshot from catalog data.
// Per sarvex-v1-architecture.md A10.4: each snapshot is keyed by store_id with merchant_id denormalized for compat.
func (r *CacheRebuilder) buildSnapshot(
	storeID string,
	profile *StoreProfileData,
	products []*ProductData,
) *CacheSnapshot {
	now := time.Now().UTC()

	// Determine the source sync watermark as the latest product score time.
	var sourceSync time.Time
	for _, prod := range products {
		if prod.LastScoreTime.After(sourceSync) {
			sourceSync = prod.LastScoreTime
		}
	}
	if sourceSync.IsZero() {
		sourceSync = now
	}

	// Build product map for cache.
	productMap := make(map[string]*CacheProduct)
	for _, prod := range products {
		productMap[prod.SKU] = &CacheProduct{
			SKU:           prod.SKU,
			Title:         prod.Title,
			Description:   prod.Description,
			Price:         Money{AmountMinor: prod.PriceMinor, Currency: prod.Currency},
			Stock:         StockLevel{InStock: prod.InStock, Quantity: prod.Quantity},
			Attributes:    prod.Attributes,
			LastScoreTime: prod.LastScoreTime,
		}
	}

	// Build cache profile using catalogue-driven profile renderer.
	// ProfileRenderer uses the catalogue to filter capabilities by V1 implementation status
	// and build spec-compliant services/capabilities/payment handlers.
	cacheProfile := r.profileRenderer.RenderProfile(storeID, profile.MerchantID, profile)

	return &CacheSnapshot{
		StoreID:    storeID,
		MerchantID: profile.MerchantID, // denormalized for backward compat
		BuiltAt:    now,
		SourceSync: sourceSync,
		Profile:    cacheProfile,
		Products:   productMap,
		Shipping:   &CacheShippingConfig{Zones: profile.ShippingZones},
	}
}

// publishSnapshotSwapped emits the snapshot-swapped event to the event bus.
func (r *CacheRebuilder) publishSnapshotSwapped(ctx context.Context, event *SnapshotSwappedEvent) error {
	return r.eventPublisher.Publish(ctx, event)
}
