// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

const (
	merchantToken = "merchant-token"
	staffToken    = "staff-token"
	operatorToken = "operator-token"
)

// errSentinel is a generic non-nil error used to trigger internal error paths.
var errSentinel = errors.New("unexpected internal error")

// testResolver is the PrincipalResolver used in all handler tests.
// "operator-token" → operator role; any other non-empty token → merchant owner.
func testResolver(token string) (domain.Principal, error) {
	switch token {
	case operatorToken:
		return domain.Principal{
			UserID:     "op-1",
			Email:      "ops@sarvex.example.com",
			Role:       domain.RoleOperator,
			MerchantID: "",
		}, nil
	case staffToken:
		return domain.Principal{
			UserID:     "u-2",
			Email:      "staff@example.com",
			Role:       domain.RoleStaff,
			MerchantID: "merch-1",
		}, nil
	case "":
		return domain.Principal{}, errors.New("no token")
	default:
		return domain.Principal{
			UserID:     "u-1",
			Email:      "merchant@example.com",
			Role:       domain.RoleOwner,
			MerchantID: "merch-1",
		}, nil
	}
}

func newTestHandler() (api.Handler, *echo.Echo) {
	deps := api.Dependencies{
		Account:       account.StubService{},
		Connector:     connector.StubService{},
		Catalog:       catalog.StubService{},
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
	g := e.Group("/v1")
	_ = h.SetupRoutes("/v1", g)
	return h, e
}

func do(e *echo.Echo, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func assertErrorEnvelope(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var env domain.ErrorEnvelope
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&env))
	assert.NotEmpty(t, env.Error.Code, "error.code must be present")
	assert.NotEmpty(t, env.Error.Message, "error.message must be present")
}

// TestRoutes_StubHandlersReturn501 verifies every registered route is reachable
// (returns 501, not 404) when called with valid credentials.
func TestRoutes_StubHandlersReturn501(t *testing.T) {
	_, e := newTestHandler()
	mb := bearer(merchantToken)

	routes := []struct {
		method  string
		path    string
		headers map[string]string
	}{
		// Connectors
		{http.MethodGet, "/v1/connectors", mb},
		{http.MethodGet, "/v1/connectors/c1", mb},
		{http.MethodDelete, "/v1/connectors/c1", mb},
		{http.MethodPost, "/v1/connectors/c1/resync", mb},
		// Connector *ingest* (POST /connector/signals, /connector/heartbeat,
		// GET /connector/profile) is not served here: those three HMAC routes
		// belong to the sync plane (CLAUDE.md §5) and are tested in
		// internal/planes/sync/api. The /connectors/* routes above are the
		// merchant-facing console reads, which do live here.
		// Catalog routes are implemented; see catalog_test.go.
		// Tasks
		{http.MethodGet, "/v1/tasks", mb},
		{http.MethodGet, "/v1/tasks/t1", mb},
		{http.MethodPost, "/v1/tasks/t1/resolve", mb},
		// Journey is implemented; see journey_test.go (or api/task.go).
		// Validation routes are implemented; see validation_handlers tests.
		// Presence/Orders/Health routes are implemented; see validation_handlers_test.go (GetPresence, ListOrders, GetHealth, etc.).
		// Billing and notification routes are implemented; see billing_test.go.
		// Operator routes are implemented; see operator_test.go (requires setup not in this test).
	}

	for _, tc := range routes {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := do(e, tc.method, tc.path, tc.headers)
			assert.Equal(t, http.StatusNotImplemented, rec.Code,
				"expected 501 (stub), got %d — route may not be registered", rec.Code)
			assertErrorEnvelope(t, rec)
		})
	}
}

// TestRoutes_BearerProtected verifies that auth-required routes reject requests
// with no Authorization header with 401.
func TestRoutes_BearerProtected(t *testing.T) {
	_, e := newTestHandler()

	protectedRoutes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/account"},
		{http.MethodPatch, "/v1/account"},
		{http.MethodGet, "/v1/account/members"},
		{http.MethodPost, "/v1/account/members"},
		{http.MethodGet, "/v1/connectors"},
		{http.MethodGet, "/v1/connectors/c1"},
		{http.MethodDelete, "/v1/connectors/c1"},
		{http.MethodPost, "/v1/connectors/c1/resync"},
		{http.MethodGet, "/v1/stores/store-1/catalog/readiness"},
		{http.MethodGet, "/v1/tasks"},
		{http.MethodGet, "/v1/stores/store-1/journey"},
		{http.MethodGet, "/v1/stores/store-1/validation/status"},
		{http.MethodGet, "/v1/stores/store-1/presence"},
		{http.MethodGet, "/v1/stores/store-1/orders"},
		{http.MethodGet, "/v1/stores/store-1/health"},
	}

	for _, tc := range protectedRoutes {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := do(e, tc.method, tc.path, nil)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assertErrorEnvelope(t, rec)
		})
	}
}

// HMAC-protected routes are the sync plane's connector-ingest edge, not
// this one. The test that used to sit here asserted 401 on
// /v1/connector/{signals,heartbeat,profile} against the control-plane
// handler, which does not register them — so it was really asserting 404 ==
// 401. It now lives where those routes actually are: see
// TestSyncRoutes_RequireHMAC in internal/planes/sync/api/handler_test.go.

// TestRoutes_OperatorOnly verifies that operator routes reject merchant tokens
// with 403.
func TestRoutes_OperatorOnly(t *testing.T) {
	_, e := newTestHandler()

	operatorRoutes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/operator/board"},
		{http.MethodPost, "/v1/operator/manual-steps"},
		{http.MethodGet, "/v1/operator/audit-log"},
	}

	for _, tc := range operatorRoutes {
		t.Run("merchant_token_"+tc.method+" "+tc.path, func(t *testing.T) {
			rec := do(e, tc.method, tc.path, bearer(merchantToken))
			assert.Equal(t, http.StatusForbidden, rec.Code)
			assertErrorEnvelope(t, rec)
		})
		t.Run("no_token_"+tc.method+" "+tc.path, func(t *testing.T) {
			rec := do(e, tc.method, tc.path, nil)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assertErrorEnvelope(t, rec)
		})
	}
}

// TestRoutes_OwnerOnly verifies that owner-only self-serve routes reject a
// staff principal with 403.
func TestRoutes_OwnerOnly(t *testing.T) {
	_, e := newTestHandler()

	ownerRoutes := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/v1/account/members"},
	}

	for _, tc := range ownerRoutes {
		t.Run("staff_token_"+tc.method+" "+tc.path, func(t *testing.T) {
			rec := do(e, tc.method, tc.path, bearer(staffToken))
			assert.Equal(t, http.StatusForbidden, rec.Code)
			assertErrorEnvelope(t, rec)
		})
		t.Run("no_token_"+tc.method+" "+tc.path, func(t *testing.T) {
			rec := do(e, tc.method, tc.path, nil)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assertErrorEnvelope(t, rec)
		})
	}
}
