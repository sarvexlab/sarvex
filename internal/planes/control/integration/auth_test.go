// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAuth_BearerEnforcement verifies that every bearer-protected route returns
// 401 when the Authorization header is absent or contains an invalid token,
// and that the response is a well-formed error envelope.
func TestAuth_BearerEnforcement(t *testing.T) {
	protectedRoutes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/account"},
		{http.MethodPatch, "/v1/account"},
		{http.MethodGet, "/v1/account/members"},
		{http.MethodGet, "/v1/stores"},
		{http.MethodPost, "/v1/stores"},
		{http.MethodGet, "/v1/stores/any-store"},
		{http.MethodPatch, "/v1/stores/any-store"},
		{http.MethodDelete, "/v1/stores/any-store"},
		{http.MethodGet, "/v1/connectors"},
		{http.MethodGet, "/v1/connectors/any-connector"},
		{http.MethodDelete, "/v1/connectors/any-connector"},
		{http.MethodPost, "/v1/connectors/any-connector/resync"},
		{http.MethodGet, "/v1/tasks"},
		{http.MethodGet, "/v1/tasks/any-task"},
		{http.MethodPost, "/v1/tasks/any-task/resolve"},
		{http.MethodGet, "/v1/stores/any-store/catalog/readiness"},
		{http.MethodGet, "/v1/stores/any-store/catalog/products"},
		{http.MethodGet, "/v1/stores/any-store/journey"},
		{http.MethodGet, "/v1/stores/any-store/validation/status"},
		{http.MethodGet, "/v1/stores/any-store/presence"},
		{http.MethodGet, "/v1/stores/any-store/orders"},
		{http.MethodGet, "/v1/stores/any-store/health"},
		{http.MethodGet, "/v1/billing"},
		{http.MethodGet, "/v1/notifications/preferences"},
	}

	for _, tc := range protectedRoutes {
		tc := tc
		t.Run("no_token "+tc.method+" "+tc.path, func(t *testing.T) {
			resp := doRequest(t, tc.method, tc.path, "", nil)
			assertErrorEnvelope(t, resp, http.StatusUnauthorized)
		})
		t.Run("bad_token "+tc.method+" "+tc.path, func(t *testing.T) {
			resp := doRequest(t, tc.method, tc.path, "invalid-token-xyz", nil)
			assertErrorEnvelope(t, resp, http.StatusUnauthorized)
		})
	}
}

// Connector-ingest HMAC enforcement is not tested here: /connector/{signals,
// heartbeat,profile} are sync-plane routes, so driving them against the control
// plane's handler only ever proved that the control plane 404s. The equivalent
// assertions now run against the handler that actually serves them, in
// internal/planes/sync/api/handler_test.go (TestSyncRoutes_RequireHMAC and
// TestSyncRoutes_RejectInvalidSignature).

// TestAuth_OperatorRoutes verifies that operator-only endpoints return 403 for
// a merchant token and 401 for no token.
func TestAuth_OperatorRoutes(t *testing.T) {
	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/operator/board"},
		{http.MethodPost, "/v1/operator/manual-steps"},
		{http.MethodGet, "/v1/operator/audit-log"},
	}

	for _, tc := range routes {
		tc := tc
		t.Run("merchant_rejected "+tc.method+" "+tc.path, func(t *testing.T) {
			resp := doRequest(t, tc.method, tc.path, tokenMerchantA, nil)
			assertErrorEnvelope(t, resp, http.StatusForbidden)
		})
		t.Run("no_token "+tc.method+" "+tc.path, func(t *testing.T) {
			resp := doRequest(t, tc.method, tc.path, "", nil)
			assertErrorEnvelope(t, resp, http.StatusUnauthorized)
		})
	}
}

// TestAuth_TenantIsolation verifies that merchant A's resources are not visible
// to merchant B, even when both are authenticated.
func TestAuth_TenantIsolation(t *testing.T) {
	// Seed a store under merchant A.
	storeID := seedStore(t, merchantAID, "Isolated Store", "woocommerce")

	// Merchant A can see it.
	t.Run("owner_can_get_own_store", func(t *testing.T) {
		resp := apiGet(t, "/v1/stores/"+string(storeID), tokenMerchantA)
		var body map[string]any
		decodeJSON(t, resp, http.StatusOK, &body)
		assert.Equal(t, string(storeID), body["store_id"])
	})

	// Merchant B gets 404 (store not found for that merchant_id).
	t.Run("other_merchant_gets_404", func(t *testing.T) {
		resp := apiGet(t, "/v1/stores/"+string(storeID), tokenMerchantB)
		assertErrorEnvelope(t, resp, http.StatusNotFound)
	})

	// Connector seeded under merchant A is invisible to merchant B.
	connectorID := seedConnector(t, merchantAID, storeID, "woocommerce", testHMACSecret)
	t.Run("connector_tenant_isolation", func(t *testing.T) {
		resp := apiGet(t, "/v1/connectors/"+connectorID, tokenMerchantB)
		assertErrorEnvelope(t, resp, http.StatusNotFound)
	})
}

// TestAuth_OperatorElevation verifies that an operator can scope requests to a
// specific merchant via X-Operate-As.
func TestAuth_OperatorElevation(t *testing.T) {
	seedAccount(t, merchantAID, "Acme Corp")

	req, _ := doWithOperateAs(t, http.MethodGet, "/v1/account", tokenOperator, string(merchantAID))
	var body map[string]any
	decodeJSON(t, req, http.StatusOK, &body)
	assert.Equal(t, "Acme Corp", body["name"])
}

// ── internal helpers ───────────────────────────────────────────────────────

// doWithOperateAs sends a bearer-authenticated request with an X-Operate-As header.
func doWithOperateAs(t *testing.T, method, path, token, operateAs string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest(method, env.url(path), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Operate-As", operateAs)
	return http.DefaultClient.Do(req)
}
