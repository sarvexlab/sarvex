// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/pkg/jwkmanagement"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// SigningKeySource provides a store's active public signing key for profile
// rendering. jwkmanagement is the single source of truth: the same active
// keypair whose private half the serving signer uses is published here, so the
// profile's signing_keys[] always verify against the signatures Sarvex emits.
type SigningKeySource interface {
	GetActiveKeypair(ctx context.Context, storeID string) (*jwkmanagement.JWKKeypair, error)
}

// mongoStoreProfileDoc represents the store profile stored in MongoDB.
// Stored in control_store_profiles collection, keyed by store_id.
// Capability definitions come from the catalogue; this stores which are enabled.
type mongoStoreProfileDoc struct {
	ID                   string                       `bson:"_id"`
	Platform             string                       `bson:"platform"` // "shopify" | "bigcommerce" | "magento" | "commercetools" | "woocommerce"
	StoreID              string                       `bson:"store_id"`
	MerchantID           string                       `bson:"merchant_id"`
	LegalName            string                       `bson:"legal_name"`
	SupportEmail         string                       `bson:"support_email"`
	StorefrontURL        string                       `bson:"storefront_url"`
	FulfillmentRegions   []string                     `bson:"fulfillment_regions"`
	ShippingZones        map[string]mongoShippingZone `bson:"shipping_zones"`
	EnabledCapabilityIDs map[string][]string          `bson:"enabled_capability_ids"` // Map of service_id -> capability_ids
	UpdatedAt            time.Time                    `bson:"updated_at"`
}

type mongoShippingZone struct {
	ID    string      `bson:"id"`
	Name  string      `bson:"name"`
	Rates []mongoRate `bson:"rates"`
}

type mongoRate struct {
	Method string     `bson:"method"`
	Cost   mongoMoney `bson:"cost"`
}

type mongoMoney struct {
	AmountMinor int64  `bson:"amount_minor"`
	Currency    string `bson:"currency"`
}

// MongoCatalogSource reads store profiles from MongoDB control_store_profiles
// and products from sync_catalog_products (via sync plane).
// Capability definitions come from the authoritative catalogue (v1.DefaultCatalogue).
// Per sarvex-v1-architecture.md A10.4: serving data is partitioned by store_id, each store owned by one merchant.
type MongoCatalogSource struct {
	db   *mongo.Database
	cat  catalogue.Catalogue
	keys SigningKeySource
	log  *slog.Logger
}

// NewMongoCatalogSource creates a MongoDB-backed catalog source.
// The catalogue is the authoritative source for UCP definitions; signing keys
// are sourced from keys (jwkmanagement), not from the stored profile document.
func NewMongoCatalogSource(db *mongo.Database, cat catalogue.Catalogue, keys SigningKeySource, log *slog.Logger) *MongoCatalogSource {
	return &MongoCatalogSource{
		db:   db,
		cat:  cat,
		keys: keys,
		log:  log,
	}
}

// activeSigningKeys returns the store's active public signing key as published
// JWKs. Returns an empty slice (never an error) when no active key exists or the
// key store is briefly unreachable — the profile still renders, degraded, rather
// than failing the rebuild.
func (m *MongoCatalogSource) activeSigningKeys(ctx context.Context, storeID string) []wire.JWK {
	if m.keys == nil {
		return []wire.JWK{}
	}
	kp, err := m.keys.GetActiveKeypair(ctx, storeID)
	if err != nil {
		m.log.Error("failed to read active signing keypair", "error", err, "store_id", storeID)
		return []wire.JWK{}
	}
	if kp == nil || kp.JWK == nil {
		m.log.Warn("no active signing keypair for store", "store_id", storeID)
		return []wire.JWK{}
	}
	return jwkmanagement.PublicWireJWKs(kp)
}

// GetStoreProfile reads store profile from MongoDB control_store_profiles collection.
// Capability definitions are fetched from the authoritative catalogue, not stored in MongoDB.
// Returns a minimal bootstrap profile if not found (will be populated during onboarding).
func (m *MongoCatalogSource) GetStoreProfile(ctx context.Context, storeID string) (*StoreProfileData, error) {
	coll := m.db.Collection("control_store_profiles")

	var doc mongoStoreProfileDoc
	err := coll.FindOne(ctx, bson.M{"store_id": storeID}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			m.log.Warn("store profile not found", "store_id", storeID)
			// Return a minimal profile for bootstrap (will be populated during onboarding)
			return &StoreProfileData{
				StoreID:      storeID,
				Platform:     m.storePlatform(ctx, storeID),
				MerchantID:   "", // Will be populated from event context
				LegalName:    "",
				Capabilities: make(map[string][]wire.Capability),
				SigningKeys:  m.activeSigningKeys(ctx, storeID),
			}, nil
		}
		return nil, fmt.Errorf("fetch store profile: %w", err)
	}

	// Convert shipping zones
	shippingZones := make(map[string]ShippingZone)
	for id, zone := range doc.ShippingZones {
		rates := make([]Rate, len(zone.Rates))
		for i, r := range zone.Rates {
			rates[i] = Rate{
				Method: r.Method,
				Cost: Money{
					AmountMinor: int(r.Cost.AmountMinor),
					Currency:    r.Cost.Currency,
				},
			}
		}
		shippingZones[id] = ShippingZone{
			ID:    zone.ID,
			Name:  zone.Name,
			Rates: rates,
		}
	}

	// Signing keys come from jwkmanagement (single source of truth), not from
	// the stored profile document — so the published key matches the signer's.
	signingKeys := m.activeSigningKeys(ctx, storeID)

	// Build capabilities from catalogue using enabled capability IDs from MongoDB.
	// MongoDB stores which capabilities are enabled (IDs only).
	// The catalogue is the authoritative source for capability definitions.
	capabilities := m.buildEnabledCapabilities(doc.EnabledCapabilityIDs)

	platform := doc.Platform
	if platform == "" {
		platform = m.storePlatform(ctx, storeID)
	}

	return &StoreProfileData{
		PaymentHandlers:    m.paymentHandlers(ctx, doc.StoreID, doc.MerchantID),
		StoreID:            doc.StoreID,
		Platform:           platform,
		MerchantID:         doc.MerchantID,
		LegalName:          doc.LegalName,
		SupportEmail:       doc.SupportEmail,
		StorefrontURL:      doc.StorefrontURL,
		FulfillmentRegions: doc.FulfillmentRegions,
		ShippingZones:      shippingZones,
		Capabilities:       capabilities,
		SigningKeys:        signingKeys,
	}, nil
}

// paymentHandlers reads the merchant's payment-handler choice for the store from
// control_payment_configs (owned and written by the control plane; this is a
// read-only cross-plane read by collection name, sarvex-v1-architecture.md A10.1). Fenced by
// both the store and its merchant. Returns nil when the store has no payment
// configuration, or when it cannot be read — a rebuild is never worth failing
// over it, and nil renders the legacy default rather than dropping every
// handler on a transient error.
func (m *MongoCatalogSource) paymentHandlers(ctx context.Context, storeID, merchantID string) map[string]PaymentHandlerSetting {
	var doc struct {
		Handlers map[string]struct {
			Enabled bool              `bson:"enabled"`
			Config  map[string]string `bson:"config"`
		} `bson:"handlers"`
	}
	err := m.db.Collection("control_payment_configs").
		FindOne(ctx, bson.M{"_id": storeID, "merchant_id": merchantID}).Decode(&doc)
	if err != nil {
		if !errors.Is(err, mongo.ErrNoDocuments) {
			m.log.Warn("payment config lookup failed", "store_id", storeID, "error", err)
		}
		return nil
	}
	out := make(map[string]PaymentHandlerSetting, len(doc.Handlers))
	for name, h := range doc.Handlers {
		out[name] = PaymentHandlerSetting{Enabled: h.Enabled, Params: h.Config}
	}
	return out
}

// storePlatform reads the store's commerce platform from control_stores.
//
// mongoStoreProfileDoc declares a platform field, but nothing writes it — the
// platform lives on the store record, which the control plane owns. Every
// serving route is addressed /ucp/{platform}/{store_id}, so without it the
// rendered profile declares an endpoint with an empty path segment
// (/ucp//store-id), which matches nothing and fails the profile_reachable
// probe on an endpoint mismatch.
//
// Read-only cross-plane read by collection name, the pattern sarvex-v1-architecture.md A10.1
// sanctions and the same one GetStoreProfile already uses for
// control_store_profiles. Returns "" on any error: the caller falls back to
// the profile document's own value, and a rebuild is never worth failing over
// a lookup that the next rebuild will retry.
func (m *MongoCatalogSource) storePlatform(ctx context.Context, storeID string) string {
	var doc struct {
		Platform string `bson:"platform"`
	}
	if err := m.db.Collection("control_stores").FindOne(ctx, bson.M{"_id": storeID}).Decode(&doc); err != nil {
		m.log.Warn("store platform lookup failed", "store_id", storeID, "error", err)
		return ""
	}
	return doc.Platform
}

// buildEnabledCapabilities retrieves full capability definitions from the catalogue
// for the enabled capability IDs stored in MongoDB.
// The catalogue is the authoritative source of truth for capability definitions.
// Returns a map of capability_id -> capability list from the catalogue (filtered by enabled IDs).
func (m *MongoCatalogSource) buildEnabledCapabilities(enabledIDs map[string][]string) map[string][]wire.Capability {
	if m.cat == nil {
		m.log.Warn("catalogue not available, returning empty capabilities")
		return make(map[string][]wire.Capability)
	}

	result := make(map[string][]wire.Capability)

	// If no enabled capabilities specified, use all V1 capabilities from catalogue
	if len(enabledIDs) == 0 {
		m.log.Debug("no enabled capability IDs specified, using all V1 capabilities")
		v1Caps := m.cat.CapabilitiesForV1()
		for _, cap := range v1Caps {
			result[cap.ID] = []wire.Capability{cap}
		}
		return result
	}

	// Build a set of all enabled capability IDs from MongoDB
	enabledSet := make(map[string]bool)
	for _, capIDs := range enabledIDs {
		for _, capID := range capIDs {
			enabledSet[capID] = true
		}
	}

	// Get all V1 capabilities from catalogue and filter by enabled set
	v1Caps := m.cat.CapabilitiesForV1()
	for _, cap := range v1Caps {
		if enabledSet[cap.ID] {
			result[cap.ID] = []wire.Capability{cap}
		}
	}

	m.log.Debug("built enabled capabilities from catalogue",
		"enabled_count", len(result),
		"total_v1_count", len(v1Caps))

	return result
}

// SaveStoreProfile persists a store profile to MongoDB.
// Called by the control plane during onboarding or when updating store configuration.
// Upserts the profile document (creates if not exists, updates if exists).
func (m *MongoCatalogSource) SaveStoreProfile(ctx context.Context, profile *StoreProfileData) error {
	if profile == nil {
		return fmt.Errorf("profile cannot be nil")
	}

	coll := m.db.Collection("control_store_profiles")

	// Convert StoreProfileData back to mongoStoreProfileDoc for persistence
	doc := mongoStoreProfileDoc{
		ID:                 profile.StoreID,
		Platform:           profile.Platform,
		StoreID:            profile.StoreID,
		MerchantID:         profile.MerchantID,
		LegalName:          profile.LegalName,
		SupportEmail:       profile.SupportEmail,
		StorefrontURL:      profile.StorefrontURL,
		FulfillmentRegions: profile.FulfillmentRegions,
		UpdatedAt:          time.Now().UTC(),
	}

	// Convert shipping zones
	doc.ShippingZones = make(map[string]mongoShippingZone)
	for id, zone := range profile.ShippingZones {
		rates := make([]mongoRate, len(zone.Rates))
		for i, rate := range zone.Rates {
			rates[i] = mongoRate{
				Method: rate.Method,
				Cost: mongoMoney{
					AmountMinor: int64(rate.Cost.AmountMinor),
					Currency:    rate.Cost.Currency,
				},
			}
		}
		doc.ShippingZones[id] = mongoShippingZone{
			ID:    zone.ID,
			Name:  zone.Name,
			Rates: rates,
		}
	}

	// Signing keys are not persisted here — they are sourced from jwkmanagement
	// (single source of truth) when the profile is read.

	// Convert enabled capabilities (store only IDs, not full definitions)
	// Extract enabled capability IDs from the profile's capabilities map
	doc.EnabledCapabilityIDs = make(map[string][]string)
	for serviceID, caps := range profile.Capabilities {
		capIDs := make([]string, len(caps))
		for i, cap := range caps {
			capIDs[i] = cap.ID
		}
		if len(capIDs) > 0 {
			doc.EnabledCapabilityIDs[serviceID] = capIDs
		}
	}

	// Upsert the document (create if not exists, update if exists)
	opts := options.UpdateOne().SetUpsert(true)
	_, err := coll.UpdateOne(ctx, bson.M{"store_id": profile.StoreID}, bson.M{"$set": doc}, opts)
	if err != nil {
		return fmt.Errorf("save store profile: %w", err)
	}

	m.log.Info("store profile saved", "store_id", profile.StoreID, "merchant_id", profile.MerchantID)
	return nil
}

// ListProductsByStore reads products from the sync_catalog_products collection.
// The serving plane consumes products built by the sync plane (sync plane is authority).
// Excludes soft-deleted products (deleted_at field exists).
func (m *MongoCatalogSource) ListProductsByStore(ctx context.Context, storeID string) ([]*ProductData, error) {
	coll := m.db.Collection("sync_catalog_products")

	// Query for all products for this store that are not deleted
	filter := bson.M{
		"store_id":   storeID,
		"deleted_at": bson.M{"$exists": false},
	}

	opts := options.Find().SetBatchSize(1000)
	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("fetch products: %w", err)
	}
	defer cursor.Close(ctx)

	var products []*ProductData

	// Decode sync_catalog_products documents and convert to ProductData. Field
	// names/types mirror sync.CatalogProduct (internal/planes/sync/product.go),
	// the sync plane's writer for this collection.
	type syncProduct struct {
		SKU           string            `bson:"sku"`
		Title         string            `bson:"title"`
		Description   string            `bson:"description"`
		PriceMinor    int64             `bson:"price_minor"`
		Currency      string            `bson:"currency"`
		InStock       bool              `bson:"in_stock"`
		Quantity      *int              `bson:"quantity"`
		RawAttributes map[string]string `bson:"raw_attributes"`
		UpdatedAt     time.Time         `bson:"updated_at"`
	}

	// Fetch all documents
	var syncProducts []syncProduct
	if err = cursor.All(ctx, &syncProducts); err != nil {
		return nil, fmt.Errorf("decode products: %w", err)
	}

	// Convert to ProductData
	for _, syncProd := range syncProducts {
		products = append(products, &ProductData{
			SKU:           syncProd.SKU,
			Title:         syncProd.Title,
			Description:   syncProd.Description,
			PriceMinor:    int(syncProd.PriceMinor),
			Currency:      syncProd.Currency,
			InStock:       syncProd.InStock,
			Quantity:      syncProd.Quantity,
			Attributes:    syncProd.RawAttributes,
			LastScoreTime: syncProd.UpdatedAt,
		})
	}

	m.log.Debug("loaded products from sync_catalog_products",
		"store_id", storeID,
		"product_count", len(products))

	return products, nil
}
