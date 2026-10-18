// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/api"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/operator"
	"github.com/sarvexlab/sarvex/internal/planes/control/validation"
)

// TestGetValidationStatus_OK tests successful retrieval of validation status.
func TestGetValidationStatus_OK(t *testing.T) {
	mockSvc := &mockValidationService{
		status: domain.ValidationStatus{
			PreValidation:     domain.PreValidationNotRun,
			TargetSpecVersion: "1.0",
		},
	}

	deps := newTestDeps()
	deps.Validation = mockSvc

	h := newTestHandlerWithDeps(deps)
	e := h.echo
	mb := bearer(merchantToken)

	rec := do(e, http.MethodGet, "/v1/stores/store-1/validation/status", mb)
	assert.Equal(t, http.StatusOK, rec.Code)

	var status domain.ValidationStatus
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&status))
	assert.Equal(t, domain.PreValidationNotRun, status.PreValidation)
}

// TestGetValidationStatus_Unauthorized tests that auth is required.
func TestGetValidationStatus_Unauthorized(t *testing.T) {
	_, e := newTestHandler()

	rec := do(e, http.MethodGet, "/v1/stores/store-1/validation/status", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestListValidationRuns_OK tests successful retrieval of validation runs.
func TestListValidationRuns_OK(t *testing.T) {
	mockSvc := &mockValidationService{
		runs: domain.ListOf[domain.ConformanceRecord]{
			Data: make([]domain.ConformanceRecord, 0),
		},
	}

	deps := newTestDeps()
	deps.Validation = mockSvc

	h := newTestHandlerWithDeps(deps)
	e := h.echo
	mb := bearer(merchantToken)

	rec := do(e, http.MethodGet, "/v1/stores/store-1/validation/runs", mb)
	assert.Equal(t, http.StatusOK, rec.Code)

	var runs domain.ListOf[domain.ConformanceRecord]
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&runs))
	assert.NotNil(t, runs.Data)
}

// TestListValidationRuns_Unauthorized tests that auth is required.
func TestListValidationRuns_Unauthorized(t *testing.T) {
	_, e := newTestHandler()

	rec := do(e, http.MethodGet, "/v1/stores/store-1/validation/runs", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestTriggerValidationRun_OK tests successful validation run trigger.
func TestTriggerValidationRun_OK(t *testing.T) {
	mockSvc := &mockValidationService{
		jobAccepted: domain.JobAccepted{
			JobID:  "run-xyz",
			Status: "completed",
		},
	}

	deps := newTestDeps()
	deps.Validation = mockSvc

	h := newTestHandlerWithDeps(deps)
	e := h.echo
	mb := bearer(merchantToken)

	rec := do(e, http.MethodPost, "/v1/stores/store-1/validation/runs", mb)
	assert.Equal(t, http.StatusAccepted, rec.Code)

	var job domain.JobAccepted
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&job))
	assert.NotEmpty(t, job.JobID)
	assert.NotEmpty(t, job.Status)
}

// TestTriggerValidationRun_Unauthorized tests that auth is required.
func TestTriggerValidationRun_Unauthorized(t *testing.T) {
	_, e := newTestHandler()

	rec := do(e, http.MethodPost, "/v1/stores/store-1/validation/runs", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestTriggerValidationRun_WithMockService tests trigger with a custom mock service.
func TestTriggerValidationRun_WithMockService(t *testing.T) {
	mockSvc := &mockValidationService{
		jobAccepted: domain.JobAccepted{
			JobID:  "run-123",
			Status: "queued",
		},
	}

	deps := newTestDeps()
	deps.Validation = mockSvc

	h := newTestHandlerWithDeps(deps)
	e := h.echo
	mb := bearer(merchantToken)

	rec := do(e, http.MethodPost, "/v1/stores/store-1/validation/runs", mb)
	assert.Equal(t, http.StatusAccepted, rec.Code)

	var job domain.JobAccepted
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&job))
	assert.Equal(t, "run-123", job.JobID)
}

// mockValidationService is a test double for validation.Service.
type mockValidationService struct {
	status       domain.ValidationStatus
	statusErr    error
	runs         domain.ListOf[domain.ConformanceRecord]
	runsErr      error
	jobAccepted  domain.JobAccepted
	jobAcceptErr error
}

func (m *mockValidationService) GetStatus(ctx context.Context, id domain.MerchantID, storeID domain.StoreID) (domain.ValidationStatus, error) {
	if m.statusErr != nil {
		return domain.ValidationStatus{}, m.statusErr
	}
	return m.status, nil
}

func (m *mockValidationService) TriggerRun(ctx context.Context, id domain.MerchantID, storeID domain.StoreID) (domain.JobAccepted, error) {
	if m.jobAcceptErr != nil {
		return domain.JobAccepted{}, m.jobAcceptErr
	}
	return m.jobAccepted, nil
}

func (m *mockValidationService) ListRuns(ctx context.Context, id domain.MerchantID, storeID domain.StoreID, cursor string, limit int) (domain.ListOf[domain.ConformanceRecord], error) {
	if m.runsErr != nil {
		return domain.ListOf[domain.ConformanceRecord]{}, m.runsErr
	}
	return m.runs, nil
}

var _ validation.Service = (*mockValidationService)(nil)

// Helper to create deps
func newTestDeps() api.Dependencies {
	deps := api.Dependencies{
		Validation:    validation.StubService{},
		Billing:       activeBillingStub{},
		ResolveBearer: testResolver,
		ResolveHMAC: func(c echo.Context, sig string) (domain.Principal, error) {
			return domain.Principal{MerchantID: "test"}, nil
		},
	}
	return deps
}

// Helper to create handler with custom deps
func newTestHandlerWithDeps(deps api.Dependencies) *testHandler {
	h := api.New(deps)
	e := echo.New()
	g := e.Group("/v1")
	_ = h.SetupRoutes("/v1", g)
	return &testHandler{h: h, echo: e}
}

type testHandler struct {
	h    api.Handler
	echo *echo.Echo
}

// ── Operator Console Endpoint Tests ──────────────────────────────────────

// TestGetPresence_OK tests successful retrieval of presence.
func TestGetPresence_OK(t *testing.T) {
	mockSvc := &mockOperatorService{
		presence: domain.Presence{
			Summary:          "Healthy",
			ProductCount:     100,
			Serving:          domain.ServingStateActive,
			ProfileReachable: true,
		},
	}

	deps := newTestDeps()
	deps.Operator = mockSvc

	h := newTestHandlerWithDeps(deps)
	e := h.echo
	mb := bearer(merchantToken)

	rec := do(e, http.MethodGet, "/v1/stores/store-1/presence", mb)
	assert.Equal(t, http.StatusOK, rec.Code)

	var presence domain.Presence
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&presence))
	assert.True(t, presence.ProfileReachable)
}

// TestGetPresence_Unauthorized tests that auth is required.
func TestGetPresence_Unauthorized(t *testing.T) {
	_, e := newTestHandler()

	rec := do(e, http.MethodGet, "/v1/stores/store-1/presence", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestGetManifest_OK tests successful retrieval of manifest.
func TestGetManifest_OK(t *testing.T) {
	mockSvc := &mockOperatorService{
		manifest: domain.ManifestView{
			Manifest: json.RawMessage(`{"spec_version": "1.0"}`),
			Cache: domain.ManifestCache{
				Stale: false,
			},
		},
	}

	deps := newTestDeps()
	deps.Operator = mockSvc

	h := newTestHandlerWithDeps(deps)
	e := h.echo
	mb := bearer(merchantToken)

	rec := do(e, http.MethodGet, "/v1/stores/store-1/presence/manifest", mb)
	assert.Equal(t, http.StatusOK, rec.Code)

	var manifest domain.ManifestView
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&manifest))
	assert.NotNil(t, manifest.Manifest)
}

// TestGetManifest_CrossTenantIs404 asserts the handler maps
// operator.ErrNotFound (returned when the store doesn't belong to the
// caller's merchant, per GetManifest's StoreOwnershipChecker) to 404, not
// a 500 — matching this API's "cross-tenant reads as not-found, never
// forbidden or an internal error" convention.
func TestGetManifest_CrossTenantIs404(t *testing.T) {
	mockSvc := &mockOperatorService{manifestErr: operator.ErrNotFound}

	deps := newTestDeps()
	deps.Operator = mockSvc

	h := newTestHandlerWithDeps(deps)
	e := h.echo
	mb := bearer(merchantToken)

	rec := do(e, http.MethodGet, "/v1/stores/store-1/presence/manifest", mb)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestRebuildCache_OK tests successful cache rebuild trigger.
func TestRebuildCache_OK(t *testing.T) {
	mockSvc := &mockOperatorService{
		jobAccepted: domain.JobAccepted{
			JobID:  "rebuild-xyz",
			Status: "queued",
		},
	}

	deps := newTestDeps()
	deps.Operator = mockSvc

	h := newTestHandlerWithDeps(deps)
	e := h.echo
	mb := bearer(merchantToken)

	rec := do(e, http.MethodPost, "/v1/stores/store-1/presence/rebuild", mb)
	assert.Equal(t, http.StatusAccepted, rec.Code)

	var job domain.JobAccepted
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&job))
	assert.NotEmpty(t, job.JobID)
}

// TestListOrders_OK tests successful retrieval of orders.
func TestListOrders_OK(t *testing.T) {
	mockSvc := &mockOperatorService{
		orders: domain.ListOf[domain.Order]{
			Data: []domain.Order{
				{OrderID: "ord-1", Status: domain.OrderCreated},
			},
		},
	}

	deps := newTestDeps()
	deps.Operator = mockSvc

	h := newTestHandlerWithDeps(deps)
	e := h.echo
	mb := bearer(merchantToken)

	rec := do(e, http.MethodGet, "/v1/stores/store-1/orders", mb)
	assert.Equal(t, http.StatusOK, rec.Code)

	var orders domain.ListOf[domain.Order]
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&orders))
	assert.Equal(t, 1, len(orders.Data))
}

// TestGetOrder_OK tests successful retrieval of a specific order.
func TestGetOrder_OK(t *testing.T) {
	mockSvc := &mockOperatorService{
		order: domain.Order{
			OrderID: "ord-1",
			Status:  domain.OrderCreated,
		},
	}

	deps := newTestDeps()
	deps.Operator = mockSvc

	h := newTestHandlerWithDeps(deps)
	e := h.echo
	mb := bearer(merchantToken)

	rec := do(e, http.MethodGet, "/v1/stores/store-1/orders/ord-1", mb)
	assert.Equal(t, http.StatusOK, rec.Code)

	var order domain.Order
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&order))
	assert.Equal(t, "ord-1", order.OrderID)
}

// TestGetOrder_Unauthorized tests that auth is required.
func TestGetOrder_Unauthorized(t *testing.T) {
	_, e := newTestHandler()

	rec := do(e, http.MethodGet, "/v1/stores/store-1/orders/ord-1", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestGetOrder_NotFound tests that a missing order maps to 404, not 500.
func TestGetOrder_NotFound(t *testing.T) {
	mockSvc := &mockOperatorService{
		orderErr: operator.ErrOrderNotFound,
	}

	deps := newTestDeps()
	deps.Operator = mockSvc

	h := newTestHandlerWithDeps(deps)
	e := h.echo
	mb := bearer(merchantToken)

	rec := do(e, http.MethodGet, "/v1/stores/store-1/orders/ord-missing", mb)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestGetHealth_OK tests successful retrieval of health.
func TestGetHealth_OK(t *testing.T) {
	mockSvc := &mockOperatorService{
		health: domain.Health{
			Statements: []domain.HealthStatement{
				{
					Kind:    domain.HealthConsistency,
					OK:      true,
					Message: "All prices match",
				},
			},
		},
	}

	deps := newTestDeps()
	deps.Operator = mockSvc

	h := newTestHandlerWithDeps(deps)
	e := h.echo
	mb := bearer(merchantToken)

	rec := do(e, http.MethodGet, "/v1/stores/store-1/health", mb)
	assert.Equal(t, http.StatusOK, rec.Code)

	var health domain.Health
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&health))
	assert.Equal(t, 1, len(health.Statements))
}

// TestListConsistencyFindings_OK tests successful retrieval of findings.
func TestListConsistencyFindings_OK(t *testing.T) {
	mockSvc := &mockOperatorService{
		findings: domain.ListOf[domain.ConsistencyFinding]{
			Data: make([]domain.ConsistencyFinding, 0),
		},
	}

	deps := newTestDeps()
	deps.Operator = mockSvc

	h := newTestHandlerWithDeps(deps)
	e := h.echo
	mb := bearer(merchantToken)

	rec := do(e, http.MethodGet, "/v1/stores/store-1/health/consistency", mb)
	assert.Equal(t, http.StatusOK, rec.Code)

	var findings domain.ListOf[domain.ConsistencyFinding]
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&findings))
	assert.NotNil(t, findings.Data)
}

// mockOperatorService is a test double for operator.Service.
type mockOperatorService struct {
	presence     domain.Presence
	presenceErr  error
	manifest     domain.ManifestView
	manifestErr  error
	jobAccepted  domain.JobAccepted
	jobAcceptErr error
	orders       domain.ListOf[domain.Order]
	ordersErr    error
	order        domain.Order
	orderErr     error
	health       domain.Health
	healthErr    error
	findings     domain.ListOf[domain.ConsistencyFinding]
	findingsErr  error
}

func (m *mockOperatorService) Board(ctx context.Context, state *domain.JourneyState, attention *bool, cursor string, limit int) (domain.ListOf[domain.BoardCard], error) {
	return domain.ListOf[domain.BoardCard]{}, nil
}

func (m *mockOperatorService) CaptureManualStep(ctx context.Context, operatorID string, targetMerchant domain.MerchantID, step domain.ManualStep) (domain.ManualStep, error) {
	return domain.ManualStep{}, nil
}

func (m *mockOperatorService) ListManualSteps(ctx context.Context, disposition *domain.ManualStepDisposition, cursor string, limit int) (domain.ListOf[domain.ManualStep], error) {
	return domain.ListOf[domain.ManualStep]{}, nil
}

func (m *mockOperatorService) RetagManualStep(ctx context.Context, operatorID string, stepID string, disposition domain.ManualStepDisposition) error {
	return nil
}

func (m *mockOperatorService) ListManualStepsFiltered(ctx context.Context, stage *domain.EngagementStage, disposition *domain.ManualStepDisposition, cursor string, limit int) (domain.ListOf[domain.ManualStep], error) {
	return domain.ListOf[domain.ManualStep]{}, nil
}

func (m *mockOperatorService) BulkRetagManualSteps(ctx context.Context, operatorID string, stepIDs []string, disposition domain.ManualStepDisposition) error {
	return nil
}

func (m *mockOperatorService) TimelineMetrics(ctx context.Context, merchantID *domain.MerchantID, startDate, endDate string) ([]domain.TimelineDatapoint, error) {
	return nil, nil
}

func (m *mockOperatorService) AuditLog(ctx context.Context, targetMerchant *domain.MerchantID, cursor string, limit int) (domain.ListOf[domain.AuditEntry], error) {
	return domain.ListOf[domain.AuditEntry]{}, nil
}

func (m *mockOperatorService) GetPresence(ctx context.Context, id domain.MerchantID, storeID domain.StoreID) (domain.Presence, error) {
	if m.presenceErr != nil {
		return domain.Presence{}, m.presenceErr
	}
	return m.presence, nil
}

func (m *mockOperatorService) GetManifest(ctx context.Context, id domain.MerchantID, storeID domain.StoreID) (domain.ManifestView, error) {
	if m.manifestErr != nil {
		return domain.ManifestView{}, m.manifestErr
	}
	return m.manifest, nil
}

func (m *mockOperatorService) RebuildCache(ctx context.Context, id domain.MerchantID, storeID domain.StoreID) (domain.JobAccepted, error) {
	if m.jobAcceptErr != nil {
		return domain.JobAccepted{}, m.jobAcceptErr
	}
	return m.jobAccepted, nil
}

func (m *mockOperatorService) ListOrders(ctx context.Context, id domain.MerchantID, storeID domain.StoreID, cursor string, limit int, status *domain.OrderStatus) (domain.ListOf[domain.Order], error) {
	if m.ordersErr != nil {
		return domain.ListOf[domain.Order]{}, m.ordersErr
	}
	return m.orders, nil
}

func (m *mockOperatorService) GetOrder(ctx context.Context, id domain.MerchantID, storeID domain.StoreID, orderID string) (domain.Order, error) {
	if m.orderErr != nil {
		return domain.Order{}, m.orderErr
	}
	return m.order, nil
}

func (m *mockOperatorService) GetHealth(ctx context.Context, id domain.MerchantID, storeID domain.StoreID) (domain.Health, error) {
	if m.healthErr != nil {
		return domain.Health{}, m.healthErr
	}
	return m.health, nil
}

func (m *mockOperatorService) ListConsistencyFindings(ctx context.Context, id domain.MerchantID, storeID domain.StoreID, cursor string, limit int, severity *domain.Severity) (domain.ListOf[domain.ConsistencyFinding], error) {
	if m.findingsErr != nil {
		return domain.ListOf[domain.ConsistencyFinding]{}, m.findingsErr
	}
	return m.findings, nil
}

func (m *mockOperatorService) ActOnBehalf(ctx context.Context, operatorID string, targetMerchant domain.MerchantID, action string, params map[string]any) error {
	return nil
}
