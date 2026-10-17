// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"time"

	"github.com/samber/lo"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// ── Sync-plane-owned read models ─────────────────────────────────────────────
//
// The documents below live in the catalog store, whose single writer is the
// sync plane's mapping/scoring workers (architecture §4.3 "one writer per
// store"). The control plane has READ-ONLY access to these collections.
//
// TODO(G3): When the sync plane catalog store is built, confirm that the BSON
// field names, collection names, and compound _id scheme used here match what
// the scoring workers actually write. These models are defined here to unblock
// the control plane API; the sync plane is the authoritative schema owner.
// See catalog/commands.go for the G3 integration points on the write side.

// ReadinessModel is the catalog store document holding the store-level
// Agent Readiness Score. One document per store (_id = "merchant_id:store_id").
// Written by sync plane scoring workers; read here by the control plane.
type ReadinessModel struct {
	LastAuditAt  time.Time `bson:"last_audit_at"`
	UpdatedAt    time.Time `bson:"updated_at"`
	ID           string    `bson:"_id"`
	MerchantID   string    `bson:"merchant_id"`
	StoreID      string    `bson:"store_id"`
	Overall      int       `bson:"overall"`
	Completeness int       `bson:"completeness"`
	Consistency  int       `bson:"consistency"`
	AttrQuality  int       `bson:"attribute_quality"`
	DeliveryData int       `bson:"delivery_data"`
	ProductCount int       `bson:"product_count"`
}

func (m *ReadinessModel) ToDomain() domain.Readiness {
	return domain.Readiness{
		Overall: m.Overall,
		Dimensions: domain.ReadinessDimensions{
			Completeness:     m.Completeness,
			Consistency:      m.Consistency,
			AttributeQuality: m.AttrQuality,
			DeliveryData:     m.DeliveryData,
		},
		LastAuditAt:  m.LastAuditAt,
		ProductCount: m.ProductCount,
	}
}

// ProductIssueModel is the embedded issue on a product document.
type ProductIssueModel struct {
	IssueID               string  `bson:"issue_id"`
	Field                 string  `bson:"field"`
	Problem               string  `bson:"problem"`
	BlocksCheckout        bool    `bson:"blocks_checkout"`
	RevenueImpactMinor    *int64  `bson:"revenue_impact_minor,omitempty"`
	RevenueImpactCurrency *string `bson:"revenue_impact_currency,omitempty"`
	Remediable            string  `bson:"remediable"`
}

func (m *ProductIssueModel) ToDomain() domain.ProductIssue {
	issue := domain.ProductIssue{
		IssueID:        m.IssueID,
		Field:          m.Field,
		Problem:        domain.IssueProblem(m.Problem),
		BlocksCheckout: m.BlocksCheckout,
		Remediable:     domain.RemediableBy(m.Remediable),
	}
	if m.RevenueImpactMinor != nil && m.RevenueImpactCurrency != nil {
		issue.RevenueImpactMonthly = &domain.Money{
			AmountMinor: *m.RevenueImpactMinor,
			Currency:    *m.RevenueImpactCurrency,
		}
	}
	return issue
}

// AttributeMappingModel is the embedded attribute mapping on a product document.
type AttributeMappingModel struct {
	SourceAttribute string `bson:"source_attribute"`
	// bson stays "ucp_field": that is the persisted name sync writes, and
	// renaming the stored key would orphan every mapping already in Mongo.
	// Only the Go name and the JSON the API serves are corrected.
	CatalogAttribute string  `bson:"ucp_field"`
	Tier             string  `bson:"tier"`
	Confidence       float64 `bson:"confidence"`
	Value            string  `bson:"value"`
}

func (m *AttributeMappingModel) ToDomain() domain.AttributeMapping {
	return domain.AttributeMapping{
		SourceAttribute:  m.SourceAttribute,
		CatalogAttribute: m.CatalogAttribute,
		Tier:             domain.ResolutionTier(m.Tier),
		Confidence:       m.Confidence,
		Value:            m.Value,
	}
}

// ProductModel is the catalog store document for a scored product.
// _id = "<merchant_id>:<store_id>:<sku>" — compound key embedding tenant isolation.
// Written by sync plane scoring workers; read here by the control plane.
type ProductModel struct {
	UpdatedAt         time.Time               `bson:"updated_at"`
	ID                string                  `bson:"_id"`
	MerchantID        string                  `bson:"merchant_id"`
	StoreID           string                  `bson:"store_id"`
	SKU               string                  `bson:"sku"`
	Title             string                  `bson:"title"`
	AttributeMappings []AttributeMappingModel `bson:"attribute_mappings"`
	Issues            []ProductIssueModel     `bson:"issues"`
	Consistency       int                     `bson:"consistency"`
	Completeness      int                     `bson:"completeness"`
	AttrQuality       int                     `bson:"attribute_quality"`
	DeliveryData      int                     `bson:"delivery_data"`
	OpenIssueCount    int                     `bson:"open_issue_count"`
	Score             int                     `bson:"score"`
	HasBlockingIssue  bool                    `bson:"has_blocking_issue"`
}

func (m *ProductModel) ToSummary() domain.ProductSummary {
	return domain.ProductSummary{
		SKU:              m.SKU,
		Title:            m.Title,
		Score:            m.Score,
		HasBlockingIssue: m.HasBlockingIssue,
		OpenIssueCount:   m.OpenIssueCount,
	}
}

func (m *ProductModel) ToDetail() domain.ProductDetail {
	issues := lo.Map(m.Issues, func(iss ProductIssueModel, _ int) domain.ProductIssue { return iss.ToDomain() })
	mappings := lo.Map(m.AttributeMappings, func(am AttributeMappingModel, _ int) domain.AttributeMapping { return am.ToDomain() })
	return domain.ProductDetail{
		ProductSummary: m.ToSummary(),
		Dimensions: domain.ReadinessDimensions{
			Completeness:     m.Completeness,
			Consistency:      m.Consistency,
			AttributeQuality: m.AttrQuality,
			DeliveryData:     m.DeliveryData,
		},
		Issues:            issues,
		AttributeMappings: mappings,
	}
}

// RemediationModel is the control plane's read model of a remediation record in
// the sync plane's sync_catalog_remediations collection (written only by the
// sync plane — see RemediationRepository). Its bson tags must match the sync
// plane's sync.Remediation; the planes/scenarios contract test holds them
// together.
type RemediationModel struct {
	CreatedAt   time.Time `bson:"created_at"`
	UpdatedAt   time.Time `bson:"updated_at"`
	ToValue     string    `bson:"to_value"`
	SKU         string    `bson:"sku"`
	Field       string    `bson:"field"`
	FromValue   string    `bson:"from_value"`
	ID          string    `bson:"_id"`
	Type        string    `bson:"type"`
	Trust       string    `bson:"trust"`
	WriteBackID string    `bson:"write_back_id,omitempty"`
	StoreID     string    `bson:"store_id"`
	MerchantID  string    `bson:"merchant_id"`
	Applied     bool      `bson:"applied"`
}

func (m *RemediationModel) ToDomain() domain.Remediation {
	return domain.Remediation{
		RemediationID: m.ID,
		SKU:           m.SKU,
		Field:         m.Field,
		FromValue:     m.FromValue,
		ToValue:       m.ToValue,
		Type:          domain.RemediableBy(m.Type),
		Trust:         domain.RemediationTrust(m.Trust),
	}
}

// ── Control-plane-owned models ────────────────────────────────────────────────

// WriteBackModel is the audit log document recording a batch remediation
// application. Owned and written by the control plane (architecture §4.3
// audit log). Stores the prior state needed for revert.
type WriteBackModel struct {
	ID             string    `bson:"_id"`
	MerchantID     string    `bson:"merchant_id"`
	StoreID        string    `bson:"store_id"`
	RemediationIDs []string  `bson:"remediation_ids"`
	Reverted       bool      `bson:"reverted"`
	CreatedAt      time.Time `bson:"created_at"`
	UpdatedAt      time.Time `bson:"updated_at"`
}

// AuditJobModel records a triggered catalog re-scan requested by the control
// plane. Written here; consumed by the sync plane (G3) via the command bus.
//
// TODO(G3): Replace polling/insertion with real command emission once the sync
// plane command bus is wired. See commands.go for the emitter interface.
type AuditJobModel struct {
	ID         string    `bson:"_id"`
	MerchantID string    `bson:"merchant_id"`
	StoreID    string    `bson:"store_id"`
	Status     string    `bson:"status"`
	CreatedAt  time.Time `bson:"created_at"`
}
