// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/testtenancy"
)

// TestTenantIsolation_Matrix extends TestAuth_TenantIsolation's 2-tenant/
// 1-store precedent (auth_test.go) to a configurable N-tenant x M-store/
// merchant matrix (default 5x3 — see internal/testtenancy, overridable via
// TENANCY_TEST_TENANTS/TENANCY_TEST_STORES_PER_TENANT), asserting every
// store/connector belonging to tenant i reads as not-found — never a
// distinct forbidden, which would confirm the resource's existence — for
// every other tenant's principal, and reads normally for its own.
//
// Each tenant is checked against exactly one other tenant (its cyclic
// successor, i+1 mod N) rather than all N-1 others: across the full run
// every ordered pair (i, i+1) is covered once, which is enough to catch a
// query missing its merchant_id filter (the F-007/F-010 failure class this
// guards against) without an O(N^2) request count.
func TestTenantIsolation_Matrix(t *testing.T) {
	cfg := testtenancy.LoadConfig()
	tenants := testtenancy.Matrix(cfg)
	require.Greater(t, len(tenants), 1, "isolation matrix needs at least 2 tenants")

	type seededStore struct {
		storeID     domain.StoreID
		connectorID string
	}
	byTenant := make([][]seededStore, len(tenants))
	for i, tn := range tenants {
		merchantID := domain.MerchantID(tn.MerchantID)
		for j := range tn.StoreIDs {
			storeID := seedStore(t, merchantID, fmt.Sprintf("tenancy-matrix store %d-%d", i, j), "woocommerce")
			connectorID := seedConnector(t, merchantID, storeID, "woocommerce", testHMACSecret)
			byTenant[i] = append(byTenant[i], seededStore{storeID: storeID, connectorID: connectorID})
		}
	}

	for i := range tenants {
		ownToken := testtenancy.Token(i)
		otherToken := testtenancy.Token((i + 1) % len(tenants))

		for j, s := range byTenant[i] {
			t.Run(fmt.Sprintf("tenant_%d_store_%d", i, j), func(t *testing.T) {
				t.Run("owner_can_read_store", func(t *testing.T) {
					resp := apiGet(t, "/v1/stores/"+string(s.storeID), ownToken)
					var body map[string]any
					decodeJSON(t, resp, http.StatusOK, &body)
					assert.Equal(t, string(s.storeID), body["store_id"])
				})

				t.Run("other_tenant_store_read_is_404", func(t *testing.T) {
					resp := apiGet(t, "/v1/stores/"+string(s.storeID), otherToken)
					assertErrorEnvelope(t, resp, http.StatusNotFound)
				})

				t.Run("other_tenant_connector_read_is_404", func(t *testing.T) {
					resp := apiGet(t, "/v1/connectors/"+s.connectorID, otherToken)
					assertErrorEnvelope(t, resp, http.StatusNotFound)
				})
			})
		}
	}
}
