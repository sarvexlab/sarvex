// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// CatalogProduct is the normalised, platform-independent product entity —
// the canonical shape every later Domain 2 stage (mapping, scoring,
// consistency, the cache rebuilder) reads and writes. It is persisted in the
// catalog store (catalog_mongo.go), keyed per store: _id is
// "<merchant_id>:<store_id>:<sku>". MerchantID/StoreID/SKU are denormalized
// top-level fields so every read is scoped without decoding the _id.
//
// Field/collection naming is reconciled with the control plane's read model
// (internal/planes/control/catalog.ProductModel): this is the sync-plane
// write side of that G3 TODO. Where a field maps 1:1 onto an already-scored
// value (Score, HasBlockingIssue), the existing Go field keeps its name and
// only the bson tag changes, so the mapping/scoring pipeline that already
// populates them needs no changes. The structured fields control's schema
// expects but nothing here computes yet (AttributeMappings, per-dimension
// scores, Issues) are added as empty scaffolding for Domain 2a to populate;
// this file contains no mapping or scoring logic itself.
type CatalogProduct struct {
	ID         string `bson:"_id" json:"id"` // "<merchant_id>:<store_id>:<sku>"
	StoreID    string `bson:"store_id" json:"store_id"`
	MerchantID string `bson:"merchant_id" json:"merchant_id"`
	SKU        string `bson:"sku" json:"sku"`

	// Canonical product fields, independent of any platform's raw schema.
	Title       string      `bson:"title" json:"title"`
	Description string      `bson:"description" json:"description"`
	Category    string      `bson:"category" json:"category"`
	Brand       string      `bson:"brand" json:"brand"`
	Images      []string    `bson:"images" json:"images"`
	Identifiers Identifiers `bson:"identifiers" json:"identifiers"`

	// Price facet: integer minor units + currency, never floats.
	PriceMinor int64  `bson:"price_minor" json:"price_minor"`
	Currency   string `bson:"currency" json:"currency"`

	// Inventory facet.
	InStock  bool `bson:"in_stock" json:"in_stock"`
	Quantity *int `bson:"quantity,omitempty" json:"quantity,omitempty"`

	RawAttributes map[string]string `bson:"raw_attributes" json:"raw_attributes"`
	MappedFields  map[string]string `bson:"mapped_fields" json:"mapped_fields"`

	// Attribute mappings and readiness — structure only here; populated by
	// the mapping/scoring stages (Domain 2a), not by catalog sync.
	AttributeMappings []AttributeMapping `bson:"attribute_mappings" json:"attribute_mappings"`
	Completeness      int                `bson:"completeness" json:"completeness"`
	Consistency       int                `bson:"consistency" json:"consistency"`
	AttributeQuality  int                `bson:"attribute_quality" json:"attribute_quality"`
	DeliveryData      int                `bson:"delivery_data" json:"delivery_data"`
	Issues            []ProductIssue     `bson:"issues" json:"issues"`
	OpenIssueCount    int                `bson:"open_issue_count" json:"open_issue_count"`

	// CatalogueVersion pins the readiness slot (Completeness..Issues above)
	// to the UCP definitions catalogue version the score stage ran against
	// (sarvex-v1-architecture.md A10.6: "spec version is data, not code").
	CatalogueVersion string `bson:"catalogue_version" json:"catalogue_version"`

	// ScoredForSurfaces are the surface ids whose scoped rules were part of the
	// score (sorted; empty = core only). A product whose store has since
	// enrolled on, or left, a surface differs from the store's current set and
	// is due a rescore.
	ScoredForSurfaces []string `bson:"scored_for_surfaces,omitempty" json:"scored_for_surfaces,omitempty"`

	// ReadinessScore/BlocksCheckout/Findings are the pre-existing scoring
	// pipeline's working fields (scoring.go, consistency_engine.go). Retagged
	// onto the control-plane's wire names (score, has_blocking_issue) so the
	// same values are readable there; the Go field names are unchanged so
	// none of the existing scoring/mapping code needed to change.
	ReadinessScore int        `bson:"score" json:"score"`
	BlocksCheckout bool       `bson:"has_blocking_issue" json:"has_blocking_issue"`
	Findings       []*Finding `bson:"findings" json:"findings"`

	UpdatedAt time.Time  `bson:"updated_at" json:"updated_at"`
	DeletedAt *time.Time `bson:"deleted_at,omitempty" json:"deleted_at,omitempty"`
}

// applyEntity copies the store's authoritative fields from a connector entity
// onto the product in place. Every path that pulls a product from the store
// (live signal, bulk import, sweep correction) goes through it, so none of
// them can forget a field the score stage reads. Mapping and scoring fields
// are left untouched.
func (p *CatalogProduct) applyEntity(e base.CatalogEntity) {
	p.Title = e.Name
	p.Description = e.Description
	p.Category = e.Category
	p.Images = e.Images
	p.PriceMinor = e.PriceMinor
	p.Currency = e.Currency
	p.InStock = e.InStock
	p.Quantity = e.Quantity
	if e.RawAttributes != nil {
		p.RawAttributes = e.RawAttributes
	}
}

// Identifiers holds cross-catalog product identifiers.
type Identifiers struct {
	GTIN string `bson:"gtin,omitempty" json:"gtin,omitempty"`
	MPN  string `bson:"mpn,omitempty" json:"mpn,omitempty"`
}

// AttributeMapping records how a raw merchant attribute resolved onto a
// canonical catalog attribute (a merchandising attribute, not a UCP protocol
// field — see control/domain.AttributeMapping).
// Mirrors control/catalog.AttributeMappingModel field-for-field. Populated by
// the resolve stage (resolve.go), one entry per catalogue target field.
type AttributeMapping struct {
	SourceAttribute string `bson:"source_attribute" json:"source_attribute"`
	// bson stays "ucp_field" so already-persisted documents keep resolving;
	// only the Go name and the JSON name are corrected.
	CatalogAttribute string  `bson:"ucp_field" json:"catalog_attribute"`
	Tier             string  `bson:"tier" json:"tier"` // "rule" | "learned" | "unresolved"
	Confidence       float64 `bson:"confidence" json:"confidence"`
	Value            string  `bson:"value" json:"value"`
}

// ProductIssue is a single actionable readiness finding on a product.
// Mirrors control/catalog.ProductIssueModel field-for-field.
type ProductIssue struct {
	IssueID               string  `bson:"issue_id" json:"issue_id"`
	Field                 string  `bson:"field" json:"field"`
	Problem               string  `bson:"problem" json:"problem"`     // missing|free_text|non_standard_enum|inconsistent
	Dimension             string  `bson:"dimension" json:"dimension"` // completeness|consistency|attribute_quality|delivery_data
	WhyItMatters          string  `bson:"why_it_matters" json:"why_it_matters"`
	BlocksCheckout        bool    `bson:"blocks_checkout" json:"blocks_checkout"`
	Remediable            string  `bson:"remediable" json:"remediable"` // rule|manual
	RevenueImpactMinor    *int64  `bson:"revenue_impact_minor,omitempty" json:"revenue_impact_minor,omitempty"`
	RevenueImpactCurrency *string `bson:"revenue_impact_currency,omitempty" json:"revenue_impact_currency,omitempty"`
}
