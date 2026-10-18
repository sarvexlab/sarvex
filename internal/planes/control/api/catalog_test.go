// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/account"
	"github.com/sarvexlab/sarvex/internal/planes/control/api"
	"github.com/sarvexlab/sarvex/internal/planes/control/catalog"
	"github.com/sarvexlab/sarvex/internal/planes/control/connector"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/journey"
	"github.com/sarvexlab/sarvex/internal/planes/control/operator"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
	"github.com/sarvexlab/sarvex/internal/planes/control/validation"
)

// stubCatalogService is a controllable catalog.Service for handler component tests.
type stubCatalogService struct {
	readiness    domain.Readiness
	products     []domain.ProductSummary
	product      domain.ProductDetail
	remediations []domain.Remediation
	job          domain.JobAccepted
	applyResult  domain.ApplyRemediationsResponse
	revertResult domain.RevertResponse

	readinessErr    error
	listProductsErr error
	getProductErr   error
	triggerAuditErr error
	listRemsErr     error
	applyErr        error
	revertErr       error
}

func (s *stubCatalogService) GetReadiness(_ context.Context, _ domain.MerchantID, _ domain.StoreID) (domain.Readiness, error) {
	return s.readiness, s.readinessErr
}

func (s *stubCatalogService) ListProducts(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ string, _ int, _ *int, _ *bool) (domain.ListOf[domain.ProductSummary], error) {
	return domain.ListOf[domain.ProductSummary]{Data: s.products, Page: domain.Page{HasMore: false}}, s.listProductsErr
}

func (s *stubCatalogService) GetProduct(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ string) (domain.ProductDetail, error) {
	return s.product, s.getProductErr
}

func (s *stubCatalogService) TriggerAudit(_ context.Context, _ domain.MerchantID, _ domain.StoreID) (domain.JobAccepted, error) {
	return s.job, s.triggerAuditErr
}

func (s *stubCatalogService) ListRemediations(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ string, _ int, _ *string) (domain.ListOf[domain.Remediation], error) {
	return domain.ListOf[domain.Remediation]{Data: s.remediations, Page: domain.Page{HasMore: false}}, s.listRemsErr
}

func (s *stubCatalogService) ApplyRemediations(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ domain.ApplyRemediationsRequest) (domain.ApplyRemediationsResponse, error) {
	return s.applyResult, s.applyErr
}

func (s *stubCatalogService) RevertWriteBack(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ string) (domain.RevertResponse, error) {
	return s.revertResult, s.revertErr
}

func (s *stubCatalogService) ListRemediationsForSKU(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ string) ([]domain.Remediation, error) {
	return s.remediations, s.listRemsErr
}

func (s *stubCatalogService) ListIssuesForSKU(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ string) ([]domain.ProductIssue, error) {
	return nil, nil
}

func newCatalogTestEcho(svc catalog.Service) *echo.Echo {
	deps := api.Dependencies{
		Account:       account.StubService{},
		Connector:     connector.StubService{},
		Catalog:       svc,
		Task:          task.StubService{},
		Journey:       journey.StubService{},
		Billing:       activeBillingStub{},
		Operator:      operator.StubService{},
		Validation:    validation.StubService{},
		ResolveBearer: testResolver,
		ResolveHMAC: func(c echo.Context, sig string) (domain.Principal, error) {
			return domain.Principal{MerchantID: "test"}, nil
		},
	}
	h := api.New(deps)
	e := echo.New()
	_ = h.SetupRoutes("/v1", e.Group("/v1"))
	return e
}

func doJSONCatalog(e *echo.Echo, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// ── GET /catalog/readiness ────────────────────────────────────────────────────

func TestGetReadiness_OK(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{
		readiness: domain.Readiness{
			Overall:      75,
			ProductCount: 100,
			Dimensions:   domain.ReadinessDimensions{Completeness: 80},
		},
	})

	rec := do(e, http.MethodGet, "/v1/stores/store-1/catalog/readiness", bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got domain.Readiness
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, 75, got.Overall)
	assert.Equal(t, 100, got.ProductCount)
	assert.Equal(t, 80, got.Dimensions.Completeness)
}

func TestGetReadiness_ZeroWhenNotScored(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{readiness: domain.Readiness{}})

	rec := do(e, http.MethodGet, "/v1/stores/store-1/catalog/readiness", bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got domain.Readiness
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Zero(t, got.Overall)
}

func TestGetReadiness_Unauthorized(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{})
	rec := do(e, http.MethodGet, "/v1/stores/store-1/catalog/readiness", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestGetReadiness_InternalError(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{readinessErr: errSentinel})
	rec := do(e, http.MethodGet, "/v1/stores/store-1/catalog/readiness", bearer(merchantToken))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assertErrorEnvelope(t, rec)
}

// ── GET /catalog/products ─────────────────────────────────────────────────────

func TestListProducts_OK(t *testing.T) {
	products := []domain.ProductSummary{
		{SKU: "sku-a", Title: "Widget", Score: 80, HasBlockingIssue: false},
		{SKU: "sku-b", Title: "Gadget", Score: 60, HasBlockingIssue: true, OpenIssueCount: 2},
	}
	e := newCatalogTestEcho(&stubCatalogService{products: products})

	rec := do(e, http.MethodGet, "/v1/stores/store-1/catalog/products", bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got domain.ListOf[domain.ProductSummary]
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Len(t, got.Data, 2)
	assert.Equal(t, "sku-a", got.Data[0].SKU)
	assert.Equal(t, 80, got.Data[0].Score)
}

func TestListProducts_Empty(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{})
	rec := do(e, http.MethodGet, "/v1/stores/store-1/catalog/products", bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got domain.ListOf[domain.ProductSummary]
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Empty(t, got.Data)
}

func TestListProducts_Unauthorized(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{})
	rec := do(e, http.MethodGet, "/v1/stores/store-1/catalog/products", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestListProducts_InternalError(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{listProductsErr: errSentinel})
	rec := do(e, http.MethodGet, "/v1/stores/store-1/catalog/products", bearer(merchantToken))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assertErrorEnvelope(t, rec)
}

// ── GET /catalog/products/:sku ────────────────────────────────────────────────

func TestGetProduct_OK(t *testing.T) {
	detail := domain.ProductDetail{
		ProductSummary: domain.ProductSummary{SKU: "sku-a", Title: "Widget", Score: 75},
		Dimensions:     domain.ReadinessDimensions{Completeness: 80},
	}
	e := newCatalogTestEcho(&stubCatalogService{product: detail})

	rec := do(e, http.MethodGet, "/v1/stores/store-1/catalog/products/sku-a", bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got domain.ProductDetail
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "sku-a", got.SKU)
	assert.Equal(t, 75, got.Score)
	assert.Equal(t, 80, got.Dimensions.Completeness)
}

func TestGetProduct_NotFound(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{getProductErr: catalog.ErrNotFound})
	rec := do(e, http.MethodGet, "/v1/stores/store-1/catalog/products/sku-missing", bearer(merchantToken))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestGetProduct_Unauthorized(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{})
	rec := do(e, http.MethodGet, "/v1/stores/store-1/catalog/products/sku-a", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestGetProduct_InternalError(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{getProductErr: errSentinel})
	rec := do(e, http.MethodGet, "/v1/stores/store-1/catalog/products/sku-a", bearer(merchantToken))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assertErrorEnvelope(t, rec)
}

// ── POST /catalog/audits ──────────────────────────────────────────────────────

func TestTriggerAudit_OK(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{
		job: domain.JobAccepted{JobID: "job-1", Status: domain.JobQueued},
	})

	rec := do(e, http.MethodPost, "/v1/stores/store-1/catalog/audits", bearer(merchantToken))
	assert.Equal(t, http.StatusAccepted, rec.Code)

	var got domain.JobAccepted
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "job-1", got.JobID)
	assert.Equal(t, domain.JobQueued, got.Status)
}

func TestTriggerAudit_Unauthorized(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{})
	rec := do(e, http.MethodPost, "/v1/stores/store-1/catalog/audits", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestTriggerAudit_InternalError(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{triggerAuditErr: errSentinel})
	rec := do(e, http.MethodPost, "/v1/stores/store-1/catalog/audits", bearer(merchantToken))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assertErrorEnvelope(t, rec)
}

// ── GET /remediations ─────────────────────────────────────────────────────────

func TestListRemediations_OK(t *testing.T) {
	rems := []domain.Remediation{
		{RemediationID: "r-1", SKU: "sku-a", Field: "title", Type: "rule", Trust: "high"},
		{RemediationID: "r-2", SKU: "sku-b", Field: "desc", Type: "rule", Trust: "low"},
	}
	e := newCatalogTestEcho(&stubCatalogService{remediations: rems})

	rec := do(e, http.MethodGet, "/v1/stores/store-1/catalog/remediations", bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got domain.ListOf[domain.Remediation]
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Len(t, got.Data, 2)
	assert.Equal(t, "r-1", got.Data[0].RemediationID)
}

func TestListRemediations_Unauthorized(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{})
	rec := do(e, http.MethodGet, "/v1/stores/store-1/catalog/remediations", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestListRemediations_InternalError(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{listRemsErr: errSentinel})
	rec := do(e, http.MethodGet, "/v1/stores/store-1/catalog/remediations", bearer(merchantToken))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assertErrorEnvelope(t, rec)
}

// ── POST /remediations/apply ──────────────────────────────────────────────────

func TestApplyRemediations_OK(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{
		applyResult: domain.ApplyRemediationsResponse{Applied: 2, WriteBackIDs: []string{"wb-1"}},
	})

	body := map[string]any{"remediation_ids": []string{"r-1", "r-2"}}
	rec := doJSONCatalog(e, http.MethodPost, "/v1/stores/store-1/catalog/remediations/apply", body, bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got domain.ApplyRemediationsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, 2, got.Applied)
	assert.Equal(t, []string{"wb-1"}, got.WriteBackIDs)
}

func TestApplyRemediations_EmptyIDs(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{})

	body := map[string]any{"remediation_ids": []string{}}
	rec := doJSONCatalog(e, http.MethodPost, "/v1/stores/store-1/catalog/remediations/apply", body, bearer(merchantToken))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestApplyRemediations_InvalidJSON(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{})

	req := httptest.NewRequest(http.MethodPost, "/v1/stores/store-1/catalog/remediations/apply", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+merchantToken)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestApplyRemediations_NotFound(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{applyErr: catalog.ErrNotFound})

	body := map[string]any{"remediation_ids": []string{"r-missing"}}
	rec := doJSONCatalog(e, http.MethodPost, "/v1/stores/store-1/catalog/remediations/apply", body, bearer(merchantToken))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestApplyRemediations_AlreadyApplied(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{applyErr: catalog.ErrRemediationApplied})

	body := map[string]any{"remediation_ids": []string{"r-1"}}
	rec := doJSONCatalog(e, http.MethodPost, "/v1/stores/store-1/catalog/remediations/apply", body, bearer(merchantToken))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestApplyRemediations_Unauthorized(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{})

	body := map[string]any{"remediation_ids": []string{"r-1"}}
	rec := doJSONCatalog(e, http.MethodPost, "/v1/stores/store-1/catalog/remediations/apply", body, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestApplyRemediations_InternalError(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{applyErr: errSentinel})

	body := map[string]any{"remediation_ids": []string{"r-1"}}
	rec := doJSONCatalog(e, http.MethodPost, "/v1/stores/store-1/catalog/remediations/apply", body, bearer(merchantToken))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assertErrorEnvelope(t, rec)
}

// ── POST /write-backs/:write_back_id/revert ───────────────────────────────────

func TestRevertWriteBack_OK(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{revertResult: domain.RevertResponse{Reverted: true}})

	rec := do(e, http.MethodPost, "/v1/stores/store-1/catalog/write-backs/wb-1/revert", bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got domain.RevertResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.True(t, got.Reverted)
}

func TestRevertWriteBack_NotFound(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{revertErr: catalog.ErrWriteBackNotFound})
	rec := do(e, http.MethodPost, "/v1/stores/store-1/catalog/write-backs/wb-missing/revert", bearer(merchantToken))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestRevertWriteBack_Unauthorized(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{})
	rec := do(e, http.MethodPost, "/v1/stores/store-1/catalog/write-backs/wb-1/revert", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestRevertWriteBack_InternalError(t *testing.T) {
	e := newCatalogTestEcho(&stubCatalogService{revertErr: errSentinel})
	rec := do(e, http.MethodPost, "/v1/stores/store-1/catalog/write-backs/wb-1/revert", bearer(merchantToken))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assertErrorEnvelope(t, rec)
}
