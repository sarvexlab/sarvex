// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"

	"github.com/samber/lo"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// MongoProfileRepository implements ProfileRepository using MongoDB.
// Stores profile data in control_store_profiles collection keyed by store_id.
type MongoProfileRepository struct {
	coll *mongo.Collection
	cat  catalogue.Catalogue
}

// NewMongoProfileRepository creates a MongoDB-backed profile repository.
func NewMongoProfileRepository(db *mongo.Database, cat catalogue.Catalogue, createIndexes bool) (*MongoProfileRepository, error) {
	coll := db.Collection("control_store_profiles")

	if createIndexes {
		// Create indexes: merchant_id for list queries, compound for cursor pagination, store_id for HMAC auth.
		indexModel := []mongo.IndexModel{
			{
				Keys: bson.D{{Key: "merchant_id", Value: 1}},
			},
			{
				Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "_id", Value: 1}},
			},
			{
				Keys: bson.D{{Key: "store_id", Value: 1}, {Key: "_id", Value: 1}},
			},
		}
		_, err := coll.Indexes().CreateMany(context.Background(), indexModel)
		if err != nil {
			return nil, fmt.Errorf("create indexes: %w", err)
		}
	}

	return &MongoProfileRepository{coll: coll, cat: cat}, nil
}

// --- Internal document types for MongoDB persistence ---

type storeProfileModel struct {
	ID                   string                       `bson:"_id"` // store_id
	StoreID              string                       `bson:"store_id"`
	MerchantID           string                       `bson:"merchant_id"`
	LegalName            string                       `bson:"legal_name"`
	SupportEmail         string                       `bson:"support_email"`
	StorefrontURL        string                       `bson:"storefront_url"`
	ReturnPolicyURL      string                       `bson:"return_policy_url"`
	FulfillmentRegions   []string                     `bson:"fulfillment_regions"`
	ShippingZones        map[string]shippingZoneModel `bson:"shipping_zones"`
	EnabledCapabilityIDs map[string][]string          `bson:"enabled_capability_ids"`
	UpdatedAt            time.Time                    `bson:"updated_at"`
}

type shippingZoneModel struct {
	ID    string      `bson:"id"`
	Name  string      `bson:"name"`
	Rates []rateModel `bson:"rates"`
}

type rateModel struct {
	Method string     `bson:"method"`
	Cost   moneyModel `bson:"cost"`
}

type moneyModel struct {
	AmountMinor int64  `bson:"amount_minor"`
	Currency    string `bson:"currency"`
}

// GetBusinessProfile retrieves a store's business profile.
func (m *MongoProfileRepository) GetBusinessProfile(ctx context.Context, storeID domain.StoreID) (*domain.StoreBusinessProfile, error) {

	var doc storeProfileModel
	err := m.coll.FindOne(ctx, bson.M{"store_id": string(storeID)}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrProfileNotFound
		}
		return nil, fmt.Errorf("fetch business profile: %w", err)
	}

	return &domain.StoreBusinessProfile{
		StoreID:         doc.StoreID,
		MerchantID:      doc.MerchantID,
		LegalName:       doc.LegalName,
		SupportEmail:    doc.SupportEmail,
		StorefrontURL:   doc.StorefrontURL,
		ReturnPolicyURL: doc.ReturnPolicyURL,
	}, nil
}

func (m *MongoProfileRepository) GetCapabilities(ctx context.Context) (catalogue.Catalogue, error) {
	return m.cat, nil
}

// SaveBusinessProfile persists a store's business profile.
func (m *MongoProfileRepository) SaveBusinessProfile(ctx context.Context, profile *domain.StoreBusinessProfile) error {
	// $set only the business-profile fields so configuration fields on the same
	// document are preserved.
	update := bson.M{
		"$set": bson.M{
			"store_id":          profile.StoreID,
			"merchant_id":       profile.MerchantID,
			"legal_name":        profile.LegalName,
			"support_email":     profile.SupportEmail,
			"storefront_url":    profile.StorefrontURL,
			"return_policy_url": profile.ReturnPolicyURL,
			"updated_at":        time.Now().UTC(),
		},
	}

	// Filter by store_id AND merchant_id so a merchant can never overwrite a store
	// profile owned by another merchant: a mismatched merchant produces no match and
	// the upsert insert collides with the existing _id (= store_id). Tenant isolation
	// per sarvex-v1-architecture.md A10.4 enforced atomically at the data layer.
	opts := options.UpdateOne().SetUpsert(true)
	_, err := m.coll.UpdateOne(ctx, bson.M{"_id": profile.StoreID, "merchant_id": profile.MerchantID}, update, opts)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			// A document with this _id (= store_id) exists under a different merchant.
			return ErrProfileNotFound
		}
		return fmt.Errorf("save business profile: %w", err)
	}

	return nil
}

// GetConfiguration retrieves a store's configuration.
func (m *MongoProfileRepository) GetConfiguration(ctx context.Context, storeID domain.StoreID) (*domain.StoreConfiguration, error) {

	var doc storeProfileModel
	err := m.coll.FindOne(ctx, bson.M{"store_id": string(storeID)}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrProfileNotFound
		}
		return nil, fmt.Errorf("fetch configuration: %w", err)
	}

	// Convert shipping zones
	shippingZones := lo.MapValues(doc.ShippingZones, func(zone shippingZoneModel, _ string) domain.ShippingZone {
		rates := lo.Map(zone.Rates, func(r rateModel, _ int) domain.Rate {
			return domain.Rate{
				Method: r.Method,
				Cost: domain.Money{
					AmountMinor: r.Cost.AmountMinor,
					Currency:    r.Cost.Currency,
				},
			}
		})
		return domain.ShippingZone{
			ID:    zone.ID,
			Name:  zone.Name,
			Rates: rates,
		}
	})

	// Build capabilities from catalogue
	capabilities := m.buildCapabilities(doc.EnabledCapabilityIDs)

	return &domain.StoreConfiguration{
		StoreID:            doc.StoreID,
		MerchantID:         doc.MerchantID,
		FulfillmentRegions: doc.FulfillmentRegions,
		ShippingZones:      shippingZones,
		Capabilities:       capabilities,
	}, nil
}

// SaveConfiguration persists a store's configuration.
func (m *MongoProfileRepository) SaveConfiguration(ctx context.Context, config *domain.StoreConfiguration) error {
	// Convert shipping zones
	shippingZoneModels := lo.MapValues(config.ShippingZones, func(zone domain.ShippingZone, _ string) shippingZoneModel {
		rates := lo.Map(zone.Rates, func(r domain.Rate, _ int) rateModel {
			return rateModel{
				Method: r.Method,
				Cost: moneyModel{
					AmountMinor: int64(r.Cost.AmountMinor),
					Currency:    r.Cost.Currency,
				},
			}
		})
		return shippingZoneModel{
			ID:    zone.ID,
			Name:  zone.Name,
			Rates: rates,
		}
	})

	// Extract enabled capability IDs
	enabledIDs := lo.OmitBy(
		lo.MapValues(config.Capabilities, func(caps []wire.Capability, _ string) []string {
			return lo.Map(caps, func(c wire.Capability, _ int) string { return c.ID })
		}),
		func(_ string, ids []string) bool { return len(ids) == 0 },
	)

	update := bson.M{
		"$set": bson.M{
			"store_id":               config.StoreID,
			"merchant_id":            config.MerchantID,
			"fulfillment_regions":    config.FulfillmentRegions,
			"shipping_zones":         shippingZoneModels,
			"enabled_capability_ids": enabledIDs,
			"updated_at":             time.Now().UTC(),
		},
	}

	// Scope by merchant_id as well as _id (= store_id) for tenant isolation (see SaveBusinessProfile).
	opts := options.UpdateOne().SetUpsert(true)
	_, err := m.coll.UpdateOne(ctx, bson.M{"_id": config.StoreID, "merchant_id": config.MerchantID}, update, opts)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrProfileNotFound
		}
		return fmt.Errorf("save configuration: %w", err)
	}

	return nil
}

// buildCapabilities retrieves full capability definitions from catalogue for enabled IDs.
func (m *MongoProfileRepository) buildCapabilities(enabledIDs map[string][]string) map[string][]wire.Capability {
	if m.cat == nil {
		return make(map[string][]wire.Capability)
	}

	result := make(map[string][]wire.Capability)

	// If no enabled capabilities specified, use all V1 capabilities from catalogue
	if len(enabledIDs) == 0 {
		v1Caps := m.cat.CapabilitiesForV1()
		for _, cap := range v1Caps {
			result[cap.ID] = []wire.Capability{cap}
		}
		return result
	}

	// Build a set of all enabled capability IDs
	enabledSet := make(map[string]bool)
	for _, capIDs := range enabledIDs {
		for _, capID := range capIDs {
			enabledSet[capID] = true
		}
	}

	// Get all V1 capabilities and filter by enabled set
	v1Caps := m.cat.CapabilitiesForV1()
	for _, cap := range v1Caps {
		if enabledSet[cap.ID] {
			result[cap.ID] = []wire.Capability{cap}
		}
	}

	return result
}
