// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package catalog_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgdomain "github.com/sarvexlabs/sarvex.cp/internal/pkg/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/catalog"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// ── Fake repositories ─────────────────────────────────────────────────────────

type fakeReadinessRepo struct {
	data map[string]*catalog.ReadinessModel
}

func newFakeReadinessRepo() *fakeReadinessRepo {
	return &fakeReadinessRepo{data: make(map[string]*catalog.ReadinessModel)}
}

func (f *fakeReadinessRepo) Get(_ context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (*catalog.ReadinessModel, error) {
	key := string(merchantID) + ":" + string(storeID)
	m, ok := f.data[key]
	if !ok {
		return nil, catalog.ErrNotFound
	}
	return m, nil
}

type fakeProductRepo struct {
	data []*catalog.ProductModel
}

func (f *fakeProductRepo) Get(_ context.Context, merchantID domain.MerchantID, storeID domain.StoreID, sku string) (*catalog.ProductModel, error) {
	for _, p := range f.data {
		if p.MerchantID == string(merchantID) && p.StoreID == string(storeID) && p.SKU == sku {
			return p, nil
		}
	}
	return nil, catalog.ErrNotFound
}

func (f *fakeProductRepo) List(_ context.Context, merchantID domain.MerchantID, storeID domain.StoreID, afterSKU string, limit int, minScore *int, hasBlockingIssue *bool) ([]*catalog.ProductModel, bool, error) {
	var out []*catalog.ProductModel
	for _, p := range f.data {
		if p.MerchantID != string(merchantID) || p.StoreID != string(storeID) {
			continue
		}
		if afterSKU != "" && p.SKU <= afterSKU {
			continue
		}
		if minScore != nil && p.Score < *minScore {
			continue
		}
		if hasBlockingIssue != nil && p.HasBlockingIssue != *hasBlockingIssue {
			continue
		}
		out = append(out, p)
	}
	// stable order by SKU
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].SKU < out[j-1].SKU; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore, nil
}

type fakeRemediationRepo struct {
	data []*catalog.RemediationModel
}

// Insert seeds the fake, standing in for the sync plane (the real repository
// is read-only).
func (f *fakeRemediationRepo) Insert(_ context.Context, m *catalog.RemediationModel) error {
	f.data = append(f.data, m)
	return nil
}

func (f *fakeRemediationRepo) List(_ context.Context, merchantID domain.MerchantID, storeID domain.StoreID, afterID string, limit int, remType *string) ([]*catalog.RemediationModel, bool, error) {
	var out []*catalog.RemediationModel
	for _, r := range f.data {
		if r.MerchantID != string(merchantID) || r.StoreID != string(storeID) {
			continue
		}
		if afterID != "" && r.ID <= afterID {
			continue
		}
		if remType != nil && r.Type != *remType {
			continue
		}
		out = append(out, r)
	}
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore, nil
}

func (f *fakeRemediationRepo) GetByIDs(_ context.Context, merchantID domain.MerchantID, storeID domain.StoreID, ids []string) ([]*catalog.RemediationModel, error) {
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	var out []*catalog.RemediationModel
	for _, r := range f.data {
		if r.MerchantID == string(merchantID) && r.StoreID == string(storeID) && idSet[r.ID] {
			out = append(out, r)
		}
	}
	if len(out) != len(ids) {
		return nil, catalog.ErrNotFound
	}
	return out, nil
}

func (f *fakeRemediationRepo) ListUnappliedBySKU(_ context.Context, merchantID domain.MerchantID, storeID domain.StoreID, sku string) ([]*catalog.RemediationModel, error) {
	var out []*catalog.RemediationModel
	for _, r := range f.data {
		if r.MerchantID == string(merchantID) && r.StoreID == string(storeID) && r.SKU == sku && !r.Applied {
			out = append(out, r)
		}
	}
	return out, nil
}

type fakeWriteBackRepo struct {
	data map[string]*catalog.WriteBackModel
}

func newFakeWriteBackRepo() *fakeWriteBackRepo {
	return &fakeWriteBackRepo{data: make(map[string]*catalog.WriteBackModel)}
}

func (f *fakeWriteBackRepo) Get(_ context.Context, merchantID domain.MerchantID, storeID domain.StoreID, writeBackID string) (*catalog.WriteBackModel, error) {
	m, ok := f.data[writeBackID]
	if !ok || m.MerchantID != string(merchantID) || m.StoreID != string(storeID) {
		return nil, catalog.ErrWriteBackNotFound
	}
	return m, nil
}

func (f *fakeWriteBackRepo) Insert(_ context.Context, m *catalog.WriteBackModel) error {
	f.data[m.ID] = m
	return nil
}

func (f *fakeWriteBackRepo) MarkReverted(_ context.Context, merchantID domain.MerchantID, storeID domain.StoreID, writeBackID string) error {
	m, ok := f.data[writeBackID]
	if !ok || m.MerchantID != string(merchantID) || m.StoreID != string(storeID) {
		return catalog.ErrWriteBackNotFound
	}
	m.Reverted = true
	return nil
}

type fakeAuditJobRepo struct {
	inserted []*catalog.AuditJobModel
}

func (f *fakeAuditJobRepo) Insert(_ context.Context, m *catalog.AuditJobModel) error {
	f.inserted = append(f.inserted, m)
	return nil
}

type fakeCommandEmitter struct {
	auditRequested       []string // job IDs
	remediationsApplied  []string // write_back IDs
	remediationsReverted []string // write_back IDs
}

func (f *fakeCommandEmitter) RequestAudit(_ context.Context, _ pkgdomain.MerchantID, storeID, jobID string) error {
	f.auditRequested = append(f.auditRequested, jobID)
	return nil
}

func (f *fakeCommandEmitter) ApplyRemediation(_ context.Context, _ pkgdomain.MerchantID, storeID, writeBackID string, _ []string) error {
	f.remediationsApplied = append(f.remediationsApplied, writeBackID)
	return nil
}

func (f *fakeCommandEmitter) RevertRemediation(_ context.Context, _ pkgdomain.MerchantID, _, writeBackID string, _ []string) error {
	f.remediationsReverted = append(f.remediationsReverted, writeBackID)
	return nil
}

// ── Constructor helper ────────────────────────────────────────────────────────

func newService(
	readiness *fakeReadinessRepo,
	products *fakeProductRepo,
	remediations *fakeRemediationRepo,
	writeBacks *fakeWriteBackRepo,
	audits *fakeAuditJobRepo,
) catalog.Service {
	return catalog.NewCatalogService(readiness, products, remediations, writeBacks, audits, &fakeCommandEmitter{})
}

// ── GetReadiness ──────────────────────────────────────────────────────────────

func TestCatalogService_GetReadiness_ZeroWhenAbsent(t *testing.T) {
	svc := newService(newFakeReadinessRepo(), &fakeProductRepo{}, &fakeRemediationRepo{}, newFakeWriteBackRepo(), &fakeAuditJobRepo{})

	r, err := svc.GetReadiness(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"))
	require.NoError(t, err)
	assert.Zero(t, r.Overall)
	assert.Zero(t, r.ProductCount)
}

func TestCatalogService_GetReadiness_ReturnsSavedScore(t *testing.T) {
	repo := newFakeReadinessRepo()
	repo.data["merch-1:store-1"] = &catalog.ReadinessModel{
		MerchantID:   "merch-1",
		StoreID:      "store-1",
		Overall:      72,
		Completeness: 80,
		Consistency:  70,
		AttrQuality:  65,
		DeliveryData: 75,
		ProductCount: 120,
		LastAuditAt:  time.Now().UTC(),
	}

	svc := newService(repo, &fakeProductRepo{}, &fakeRemediationRepo{}, newFakeWriteBackRepo(), &fakeAuditJobRepo{})

	r, err := svc.GetReadiness(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"))
	require.NoError(t, err)
	assert.Equal(t, 72, r.Overall)
	assert.Equal(t, 80, r.Dimensions.Completeness)
	assert.Equal(t, 120, r.ProductCount)
}

// ── ListProducts ──────────────────────────────────────────────────────────────

func TestCatalogService_ListProducts_Empty(t *testing.T) {
	svc := newService(newFakeReadinessRepo(), &fakeProductRepo{}, &fakeRemediationRepo{}, newFakeWriteBackRepo(), &fakeAuditJobRepo{})

	result, err := svc.ListProducts(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), "", 20, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, result.Data)
	assert.False(t, result.Page.HasMore)
}

func TestCatalogService_ListProducts_ReturnsMerchantProducts(t *testing.T) {
	products := &fakeProductRepo{data: []*catalog.ProductModel{
		{MerchantID: "merch-1",
			StoreID: "store-1", SKU: "sku-a", Title: "A", Score: 80},
		{MerchantID: "merch-1",
			StoreID: "store-1", SKU: "sku-b", Title: "B", Score: 60},
		{MerchantID: "merch-2",
			StoreID: "store-2", SKU: "sku-c", Title: "C", Score: 90},
	}}
	svc := newService(newFakeReadinessRepo(), products, &fakeRemediationRepo{}, newFakeWriteBackRepo(), &fakeAuditJobRepo{})

	result, err := svc.ListProducts(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), "", 20, nil, nil)
	require.NoError(t, err)
	assert.Len(t, result.Data, 2)
	assert.Equal(t, "sku-a", result.Data[0].SKU)
	assert.Equal(t, "sku-b", result.Data[1].SKU)
}

func TestCatalogService_ListProducts_Pagination(t *testing.T) {
	products := &fakeProductRepo{data: []*catalog.ProductModel{
		{MerchantID: "merch-1",
			StoreID: "store-1", SKU: "sku-a", Score: 80},
		{MerchantID: "merch-1",
			StoreID: "store-1", SKU: "sku-b", Score: 70},
		{MerchantID: "merch-1",
			StoreID: "store-1", SKU: "sku-c", Score: 60},
	}}
	svc := newService(newFakeReadinessRepo(), products, &fakeRemediationRepo{}, newFakeWriteBackRepo(), &fakeAuditJobRepo{})

	page1, err := svc.ListProducts(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), "", 2, nil, nil)
	require.NoError(t, err)
	assert.Len(t, page1.Data, 2)
	assert.True(t, page1.Page.HasMore)
	require.NotNil(t, page1.Page.Cursor)

	page2, err := svc.ListProducts(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), *page1.Page.Cursor, 2, nil, nil)
	require.NoError(t, err)
	assert.Len(t, page2.Data, 1)
	assert.False(t, page2.Page.HasMore)
	assert.Equal(t, "sku-c", page2.Data[0].SKU)
}

func TestCatalogService_ListProducts_MinScoreFilter(t *testing.T) {
	products := &fakeProductRepo{data: []*catalog.ProductModel{
		{MerchantID: "merch-1",
			StoreID: "store-1", SKU: "sku-a", Score: 80},
		{MerchantID: "merch-1",
			StoreID: "store-1", SKU: "sku-b", Score: 40},
	}}
	svc := newService(newFakeReadinessRepo(), products, &fakeRemediationRepo{}, newFakeWriteBackRepo(), &fakeAuditJobRepo{})

	minScore := 60
	result, err := svc.ListProducts(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), "", 20, &minScore, nil)
	require.NoError(t, err)
	assert.Len(t, result.Data, 1)
	assert.Equal(t, "sku-a", result.Data[0].SKU)
}

func TestCatalogService_ListProducts_BlockingIssueFilter(t *testing.T) {
	products := &fakeProductRepo{data: []*catalog.ProductModel{
		{MerchantID: "merch-1",
			StoreID: "store-1", SKU: "sku-a", HasBlockingIssue: true},
		{MerchantID: "merch-1",
			StoreID: "store-1", SKU: "sku-b", HasBlockingIssue: false},
	}}
	svc := newService(newFakeReadinessRepo(), products, &fakeRemediationRepo{}, newFakeWriteBackRepo(), &fakeAuditJobRepo{})

	blocking := true
	result, err := svc.ListProducts(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), "", 20, nil, &blocking)
	require.NoError(t, err)
	assert.Len(t, result.Data, 1)
	assert.Equal(t, "sku-a", result.Data[0].SKU)
}

func TestCatalogService_ListProducts_LimitClamped(t *testing.T) {
	svc := newService(newFakeReadinessRepo(), &fakeProductRepo{}, &fakeRemediationRepo{}, newFakeWriteBackRepo(), &fakeAuditJobRepo{})

	_, err := svc.ListProducts(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), "", 0, nil, nil)
	require.NoError(t, err)

	_, err = svc.ListProducts(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), "", 200, nil, nil)
	require.NoError(t, err)
}

// ── GetProduct ────────────────────────────────────────────────────────────────

func TestCatalogService_GetProduct_NotFound(t *testing.T) {
	svc := newService(newFakeReadinessRepo(), &fakeProductRepo{}, &fakeRemediationRepo{}, newFakeWriteBackRepo(), &fakeAuditJobRepo{})

	_, err := svc.GetProduct(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), "sku-missing")
	assert.ErrorIs(t, err, catalog.ErrNotFound)
}

func TestCatalogService_GetProduct_Found(t *testing.T) {
	currency := "USD"
	amount := int64(500)
	products := &fakeProductRepo{data: []*catalog.ProductModel{
		{
			MerchantID:       "merch-1",
			StoreID:          "store-1",
			SKU:              "sku-a",
			Title:            "Widget",
			Score:            75,
			HasBlockingIssue: true,
			OpenIssueCount:   2,
			Completeness:     80,
			Consistency:      70,
			AttrQuality:      65,
			DeliveryData:     85,
			Issues: []catalog.ProductIssueModel{
				{
					IssueID:               "iss-1",
					Field:                 "description",
					Problem:               "missing",
					BlocksCheckout:        true,
					RevenueImpactMinor:    &amount,
					RevenueImpactCurrency: &currency,
					Remediable:            "rule",
				},
			},
			AttributeMappings: []catalog.AttributeMappingModel{
				{SourceAttribute: "weight", CatalogAttribute: "shipping_weight", Tier: "rule", Confidence: 1.0},
			},
		},
	}}
	svc := newService(newFakeReadinessRepo(), products, &fakeRemediationRepo{}, newFakeWriteBackRepo(), &fakeAuditJobRepo{})

	detail, err := svc.GetProduct(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), "sku-a")
	require.NoError(t, err)
	assert.Equal(t, "sku-a", detail.SKU)
	assert.Equal(t, "Widget", detail.Title)
	assert.Equal(t, 75, detail.Score)
	assert.True(t, detail.HasBlockingIssue)
	assert.Equal(t, 80, detail.Dimensions.Completeness)
	require.Len(t, detail.Issues, 1)
	assert.Equal(t, "iss-1", detail.Issues[0].IssueID)
	assert.Equal(t, domain.IssueProblem("missing"), detail.Issues[0].Problem)
	require.NotNil(t, detail.Issues[0].RevenueImpactMonthly)
	assert.Equal(t, int64(500), detail.Issues[0].RevenueImpactMonthly.AmountMinor)
	require.Len(t, detail.AttributeMappings, 1)
	assert.Equal(t, "weight", detail.AttributeMappings[0].SourceAttribute)
}

// ── ListIssuesForSKU ──────────────────────────────────────────────────────────

func TestCatalogService_ListIssuesForSKU_ReturnsOnlyManualIssues(t *testing.T) {
	products := &fakeProductRepo{data: []*catalog.ProductModel{
		{
			MerchantID: "merch-1",
			StoreID:    "store-1",
			SKU:        "sku-a",
			Issues: []catalog.ProductIssueModel{
				{IssueID: "iss-1", Field: "title", Problem: "missing", Remediable: "rule"},
				{IssueID: "iss-2", Field: "description", Problem: "free_text", Remediable: "manual"},
			},
		},
	}}
	svc := newService(newFakeReadinessRepo(), products, &fakeRemediationRepo{}, newFakeWriteBackRepo(), &fakeAuditJobRepo{})

	issues, err := svc.ListIssuesForSKU(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), "sku-a")
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "iss-2", issues[0].IssueID)
	assert.Equal(t, domain.RemediableByManual, issues[0].Remediable)
}

func TestCatalogService_ListIssuesForSKU_NotFound(t *testing.T) {
	svc := newService(newFakeReadinessRepo(), &fakeProductRepo{}, &fakeRemediationRepo{}, newFakeWriteBackRepo(), &fakeAuditJobRepo{})

	_, err := svc.ListIssuesForSKU(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), "sku-missing")
	assert.ErrorIs(t, err, catalog.ErrNotFound)
}

// ── TriggerAudit ─────────────────────────────────────────────────────────────

func TestCatalogService_TriggerAudit_ReturnsJobAccepted(t *testing.T) {
	audits := &fakeAuditJobRepo{}
	svc := newService(newFakeReadinessRepo(), &fakeProductRepo{}, &fakeRemediationRepo{}, newFakeWriteBackRepo(), audits)

	job, err := svc.TriggerAudit(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"))
	require.NoError(t, err)
	assert.NotEmpty(t, job.JobID)
	assert.Equal(t, domain.JobQueued, job.Status)

	require.Len(t, audits.inserted, 1)
	assert.Equal(t, job.JobID, audits.inserted[0].ID)
	assert.Equal(t, "merch-1", audits.inserted[0].MerchantID)
	assert.Equal(t, string(domain.JobQueued), audits.inserted[0].Status)
}

// ── ListRemediations ──────────────────────────────────────────────────────────

func TestCatalogService_ListRemediations_Empty(t *testing.T) {
	svc := newService(newFakeReadinessRepo(), &fakeProductRepo{}, &fakeRemediationRepo{}, newFakeWriteBackRepo(), &fakeAuditJobRepo{})

	result, err := svc.ListRemediations(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), "", 20, nil)
	require.NoError(t, err)
	assert.Empty(t, result.Data)
	assert.False(t, result.Page.HasMore)
}

func TestCatalogService_ListRemediations_TypeFilter(t *testing.T) {
	rems := &fakeRemediationRepo{data: []*catalog.RemediationModel{
		{ID: "r-1", MerchantID: "merch-1",
			StoreID: "store-1", SKU: "sku-a", Field: "title", Type: "rule", Trust: "high"},
		{ID: "r-2", MerchantID: "merch-1",
			StoreID: "store-1", SKU: "sku-b", Field: "desc", Type: "manual", Trust: "low"},
	}}
	svc := newService(newFakeReadinessRepo(), &fakeProductRepo{}, rems, newFakeWriteBackRepo(), &fakeAuditJobRepo{})

	remType := "rule"
	result, err := svc.ListRemediations(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), "", 20, &remType)
	require.NoError(t, err)
	assert.Len(t, result.Data, 1)
	assert.Equal(t, "r-1", result.Data[0].RemediationID)
}

func TestCatalogService_ListRemediations_Pagination(t *testing.T) {
	rems := &fakeRemediationRepo{data: []*catalog.RemediationModel{
		{ID: "r-1", MerchantID: "merch-1", StoreID: "store-1"},
		{ID: "r-2", MerchantID: "merch-1", StoreID: "store-1"},
		{ID: "r-3", MerchantID: "merch-1", StoreID: "store-1"},
	}}
	svc := newService(newFakeReadinessRepo(), &fakeProductRepo{}, rems, newFakeWriteBackRepo(), &fakeAuditJobRepo{})

	page1, err := svc.ListRemediations(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), "", 2, nil)
	require.NoError(t, err)
	assert.Len(t, page1.Data, 2)
	assert.True(t, page1.Page.HasMore)
	require.NotNil(t, page1.Page.Cursor)

	page2, err := svc.ListRemediations(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), *page1.Page.Cursor, 2, nil)
	require.NoError(t, err)
	assert.Len(t, page2.Data, 1)
	assert.False(t, page2.Page.HasMore)
}

// ── ApplyRemediations ─────────────────────────────────────────────────────────

func TestCatalogService_ApplyRemediations_Success(t *testing.T) {
	rems := &fakeRemediationRepo{data: []*catalog.RemediationModel{
		{ID: "r-1", MerchantID: "merch-1",
			StoreID: "store-1", SKU: "sku-a", Field: "title", FromValue: "old", ToValue: "new", Type: "rule", Trust: "high"},
		{ID: "r-2", MerchantID: "merch-1",
			StoreID: "store-1", SKU: "sku-b", Field: "desc", FromValue: "x", ToValue: "y", Type: "rule", Trust: "low"},
	}}
	writeBacks := newFakeWriteBackRepo()
	svc := newService(newFakeReadinessRepo(), &fakeProductRepo{}, rems, writeBacks, &fakeAuditJobRepo{})

	result, err := svc.ApplyRemediations(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), domain.ApplyRemediationsRequest{
		RemediationIDs: []string{"r-1", "r-2"},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, result.Applied)
	require.Len(t, result.WriteBackIDs, 1)

	wbID := result.WriteBackIDs[0]
	assert.NotEmpty(t, wbID)

	wb, ok := writeBacks.data[wbID]
	require.True(t, ok)
	assert.Equal(t, "merch-1", wb.MerchantID)
	assert.ElementsMatch(t, []string{"r-1", "r-2"}, wb.RemediationIDs)
	assert.False(t, wb.Reverted)
}

func TestCatalogService_ApplyRemediations_AlreadyApplied(t *testing.T) {
	rems := &fakeRemediationRepo{data: []*catalog.RemediationModel{
		{ID: "r-1", MerchantID: "merch-1",
			StoreID: "store-1", Applied: true, WriteBackID: "wb-old"},
	}}
	svc := newService(newFakeReadinessRepo(), &fakeProductRepo{}, rems, newFakeWriteBackRepo(), &fakeAuditJobRepo{})

	_, err := svc.ApplyRemediations(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), domain.ApplyRemediationsRequest{
		RemediationIDs: []string{"r-1"},
	})
	assert.ErrorIs(t, err, catalog.ErrRemediationApplied)
}

func TestCatalogService_ApplyRemediations_NotFound(t *testing.T) {
	svc := newService(newFakeReadinessRepo(), &fakeProductRepo{}, &fakeRemediationRepo{}, newFakeWriteBackRepo(), &fakeAuditJobRepo{})

	_, err := svc.ApplyRemediations(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), domain.ApplyRemediationsRequest{
		RemediationIDs: []string{"r-missing"},
	})
	assert.ErrorIs(t, err, catalog.ErrNotFound)
}

// ── RevertWriteBack ───────────────────────────────────────────────────────────

func TestCatalogService_RevertWriteBack_Success(t *testing.T) {
	writeBacks := newFakeWriteBackRepo()
	writeBacks.data["wb-1"] = &catalog.WriteBackModel{
		ID:             "wb-1",
		MerchantID:     "merch-1",
		StoreID:        "store-1",
		RemediationIDs: []string{"r-1"},
		Reverted:       false,
	}
	emitter := &fakeCommandEmitter{}
	svc := catalog.NewCatalogService(newFakeReadinessRepo(), &fakeProductRepo{}, &fakeRemediationRepo{}, writeBacks, &fakeAuditJobRepo{}, emitter)

	result, err := svc.RevertWriteBack(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), "wb-1")
	require.NoError(t, err)
	assert.True(t, result.Reverted)
	assert.True(t, writeBacks.data["wb-1"].Reverted)
	// The sync plane is told to undo the fixes; marking the record alone
	// used to be all a revert did.
	assert.Equal(t, []string{"wb-1"}, emitter.remediationsReverted)
}

func TestCatalogService_RevertWriteBack_AlreadyReverted(t *testing.T) {
	writeBacks := newFakeWriteBackRepo()
	writeBacks.data["wb-1"] = &catalog.WriteBackModel{
		ID:         "wb-1",
		MerchantID: "merch-1",
		StoreID:    "store-1",
		Reverted:   true,
	}
	svc := newService(newFakeReadinessRepo(), &fakeProductRepo{}, &fakeRemediationRepo{}, writeBacks, &fakeAuditJobRepo{})

	result, err := svc.RevertWriteBack(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), "wb-1")
	require.NoError(t, err)
	assert.True(t, result.Reverted)
}

func TestCatalogService_RevertWriteBack_NotFound(t *testing.T) {
	svc := newService(newFakeReadinessRepo(), &fakeProductRepo{}, &fakeRemediationRepo{}, newFakeWriteBackRepo(), &fakeAuditJobRepo{})

	_, err := svc.RevertWriteBack(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-1"), "wb-missing")
	assert.ErrorIs(t, err, catalog.ErrWriteBackNotFound)
}

func TestCatalogService_RevertWriteBack_WrongMerchant(t *testing.T) {
	writeBacks := newFakeWriteBackRepo()
	writeBacks.data["wb-1"] = &catalog.WriteBackModel{
		ID:         "wb-1",
		MerchantID: "merch-1",
		StoreID:    "store-1",
	}
	svc := newService(newFakeReadinessRepo(), &fakeProductRepo{}, &fakeRemediationRepo{}, writeBacks, &fakeAuditJobRepo{})

	// Try to revert with wrong store
	_, err := svc.RevertWriteBack(context.Background(), domain.MerchantID("merch-1"), domain.StoreID("store-2"), "wb-1")
	assert.ErrorIs(t, err, catalog.ErrWriteBackNotFound)
}
