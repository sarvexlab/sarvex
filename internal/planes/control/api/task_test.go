// Copyright 2026 Sarvex Labs
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

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/account"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/api"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/catalog"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/connector"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/journey"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/operator"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/task"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/validation"
)

// stubTaskService is a controllable task.Service for handler component tests
// that records the storeID it was called with, so the store_id query-param
// wiring can be asserted without a real repository.
type stubTaskService struct {
	tasks         []domain.Task
	lastStoreID   *domain.StoreID
	listErr       error
	getResult     domain.Task
	getErr        error
	lastGetID     string
	resolveResult domain.Task
	resolveErr    error
}

func (s *stubTaskService) List(_ context.Context, _ domain.MerchantID, storeID *domain.StoreID, _ string, _ int, _ *domain.TaskOwner, _ *domain.TaskState) (domain.ListOf[domain.Task], error) {
	s.lastStoreID = storeID
	if s.listErr != nil {
		return domain.ListOf[domain.Task]{}, s.listErr
	}
	return domain.ListOf[domain.Task]{Data: s.tasks, Page: domain.Page{HasMore: false}}, nil
}

func (s *stubTaskService) Get(_ context.Context, _ domain.MerchantID, taskID string) (domain.Task, error) {
	s.lastGetID = taskID
	if s.getErr != nil {
		return domain.Task{}, s.getErr
	}
	return s.getResult, nil
}

func (s *stubTaskService) Resolve(_ context.Context, _ domain.MerchantID, _ string, _ domain.ResolveTaskRequest) (domain.Task, error) {
	if s.resolveErr != nil {
		return domain.Task{}, s.resolveErr
	}
	return s.resolveResult, nil
}

func (s *stubTaskService) Complete(_ context.Context, _ domain.MerchantID, _ string, _ *string) (domain.Task, error) {
	if s.resolveErr != nil {
		return domain.Task{}, s.resolveErr
	}
	return s.resolveResult, nil
}

func (s *stubTaskService) Reopen(_ context.Context, _ domain.MerchantID, taskID string, _ *string) (domain.Task, error) {
	return domain.Task{TaskID: taskID, State: domain.TaskStateOpen}, nil
}
func (s *stubTaskService) Raise(_ context.Context, _ domain.MerchantID, _ task.RaiseTaskRequest) (domain.Task, error) {
	return domain.Task{}, task.ErrNotImplemented
}

func (s *stubTaskService) ListOpenByMerchants(_ context.Context, _ []domain.MerchantID) (map[domain.StoreID]domain.StoreTaskSummary, error) {
	return nil, task.ErrNotImplemented
}

func newTaskTestEcho(svc task.Service) *echo.Echo {
	deps := api.Dependencies{
		Account:       account.StubService{},
		Connector:     connector.StubService{},
		Catalog:       catalog.StubService{},
		Task:          svc,
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

// ── GET /tasks ──────────────────────────────────────────────────────────────

func TestListTasks_NoStoreFilter_PassesNilStoreID(t *testing.T) {
	svc := &stubTaskService{tasks: []domain.Task{{TaskID: "task-1"}}}
	e := newTaskTestEcho(svc)
	rec := do(e, http.MethodGet, "/v1/tasks", bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Nil(t, svc.lastStoreID)

	var got domain.ListOf[domain.Task]
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Len(t, got.Data, 1)
}

func TestListTasks_StoreFilter_PassesStoreID(t *testing.T) {
	svc := &stubTaskService{tasks: []domain.Task{{TaskID: "task-1", StoreID: "store-1"}}}
	e := newTaskTestEcho(svc)
	rec := do(e, http.MethodGet, "/v1/tasks?store_id=store-1", bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, svc.lastStoreID)
	assert.Equal(t, domain.StoreID("store-1"), *svc.lastStoreID)
}

func TestListTasks_Unauthorized(t *testing.T) {
	e := newTaskTestEcho(&stubTaskService{})
	rec := do(e, http.MethodGet, "/v1/tasks", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestListTasks_InternalError(t *testing.T) {
	e := newTaskTestEcho(&stubTaskService{listErr: errSentinel})
	rec := do(e, http.MethodGet, "/v1/tasks", bearer(merchantToken))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assertErrorEnvelope(t, rec)
}

// ── GET /tasks/{task_id} ────────────────────────────────────────────────────

func TestGetTask_IncludesRemediationsForReadinessTask(t *testing.T) {
	svc := &stubTaskService{getResult: domain.Task{
		TaskID: "task-1",
		Source: domain.TaskSourceReadiness,
		Remediations: []domain.Remediation{
			{RemediationID: "r-1", SKU: "sku-1", Field: "color", FromValue: "Rot", ToValue: "Red"},
		},
	}}
	e := newTaskTestEcho(svc)
	rec := do(e, http.MethodGet, "/v1/tasks/task-1", bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got domain.Task
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	require.Len(t, got.Remediations, 1)
	assert.Equal(t, "r-1", got.Remediations[0].RemediationID)
}

func TestGetTask_IncludesIssuesForReadinessTask(t *testing.T) {
	svc := &stubTaskService{getResult: domain.Task{
		TaskID: "task-1",
		Source: domain.TaskSourceReadiness,
		Issues: []domain.ProductIssue{
			{IssueID: "iss-1", Field: "description", Problem: domain.ProblemFreeText, Remediable: domain.RemediableByManual},
		},
	}}
	e := newTaskTestEcho(svc)
	rec := do(e, http.MethodGet, "/v1/tasks/task-1", bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got domain.Task
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	require.Len(t, got.Issues, 1)
	assert.Equal(t, "iss-1", got.Issues[0].IssueID)
	assert.Equal(t, domain.ProblemFreeText, got.Issues[0].Problem)
}

func TestGetTask_NotFound(t *testing.T) {
	svc := &stubTaskService{getErr: task.ErrNotFound}
	e := newTaskTestEcho(svc)
	rec := do(e, http.MethodGet, "/v1/tasks/missing", bearer(merchantToken))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assertErrorEnvelope(t, rec)
}

// Task IDs are a compound key ("source:store_id:source_ref") and routinely
// contain colons. The console's API client percent-encodes the ID
// (encodeURIComponent) when building the URL, which is unnecessary for a
// colon but not incorrect — the server must still decode it. Echo's router
// prefers the request's RawPath (still-escaped) over the decoded Path when
// they differ, so a naive c.Param read would silently receive
// "readiness%3Astore-1%3Async-1" instead of "readiness:store-1:sync-1" and
// every lookup would 404. Guard against regressing that decode.
func TestGetTask_DecodesPercentEncodedCompoundID(t *testing.T) {
	svc := &stubTaskService{getResult: domain.Task{TaskID: "readiness:store-1:sync-1"}}
	e := newTaskTestEcho(svc)
	rec := do(e, http.MethodGet, "/v1/tasks/readiness%3Astore-1%3Async-1", bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "readiness:store-1:sync-1", svc.lastGetID)
}

// ── POST /tasks/{task_id}/resolve ──────────────────────────────────────────

func TestResolveTask_NotFound(t *testing.T) {
	svc := &stubTaskService{resolveErr: task.ErrNotFound}
	e := newTaskTestEcho(svc)
	rec := do(e, http.MethodPost, "/v1/tasks/missing/resolve", bearer(merchantToken))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestResolveTask_NotResolvable(t *testing.T) {
	svc := &stubTaskService{resolveErr: task.ErrNotResolvable}
	e := newTaskTestEcho(svc)
	rec := do(e, http.MethodPost, "/v1/tasks/task-1/resolve", bearer(merchantToken))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assertErrorEnvelope(t, rec)
}
