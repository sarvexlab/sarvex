// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build scenarios

package scenarios_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/platform/woocommerce"
	"github.com/sarvexlabs/sarvex.cp/internal/testtenancy"
)

// TestScenario_TenantIsolation_Matrix extends
// TestScenario_Onboarding_TenantIsolation's 2-tenant/1-store precedent
// (onboarding_test.go) to a configurable N-tenant x M-store/merchant matrix
// (default 5x3 — see internal/testtenancy, overridable via
// TENANCY_TEST_TENANTS/TENANCY_TEST_STORES_PER_TENANT), driving the same
// real HTTP onboarding surface (account creation, store creation, connector
// pairing) rather than seeding fixtures directly, and asserting every
// cross-tenant boundary reads as not-found — never a distinct forbidden,
// which would confirm the resource's existence.
//
// Each tenant is checked against its cyclic successor (i+1 mod N) rather
// than all N-1 others, so every ordered pair (i, i+1) is covered once
// across the full run without an O(N^2) HTTP request count.
func TestScenario_TenantIsolation_Matrix(t *testing.T) {
	cfg := testtenancy.LoadConfig()
	tenants := testtenancy.Matrix(cfg)
	if len(tenants) < 2 {
		t.Fatal("isolation matrix needs at least 2 tenants")
	}

	storeIDsByTenant := make([][]domain.StoreID, len(tenants))
	for i, tn := range tenants {
		merchantID := domain.MerchantID(tn.MerchantID)
		token := testtenancy.Token(i)
		createAccount(t, merchantID, fmt.Sprintf("Tenancy Matrix Merchant %d", i))

		for j := range tn.StoreIDs {
			storeID := createStore(t, merchantID, woocommerce.PlatformName, fmt.Sprintf("Tenancy matrix store %d-%d", i, j), token)
			storeIDsByTenant[i] = append(storeIDsByTenant[i], storeID)
		}
	}

	for i := range tenants {
		ownToken := testtenancy.Token(i)
		otherToken := testtenancy.Token((i + 1) % len(tenants))

		for j, storeID := range storeIDsByTenant[i] {
			t.Run(fmt.Sprintf("tenant_%d_store_%d", i, j), func(t *testing.T) {
				t.Run("owner_can_read_store", func(t *testing.T) {
					resp := apiGet(t, "/v1/stores/"+string(storeID), ownToken)
					var body map[string]any
					decodeJSON(t, resp, http.StatusOK, &body)
					if body["store_id"] != string(storeID) {
						t.Fatalf("expected store_id %q, got %v", storeID, body["store_id"])
					}
				})

				t.Run("other_tenant_store_read_is_404", func(t *testing.T) {
					resp := apiGet(t, "/v1/stores/"+string(storeID), otherToken)
					var errResp map[string]any
					decodeJSON(t, resp, http.StatusNotFound, &errResp)
					if errResp["error"] == nil {
						t.Fatal("expected an error envelope on the 404 response")
					}
				})

				t.Run("other_tenant_pairing_attempt_is_404", func(t *testing.T) {
					resp := apiPost(t, "/v1/stores/"+string(storeID)+"/connectors/pairing", otherToken, map[string]any{
						"platform": "woocommerce",
					})
					var errResp map[string]any
					decodeJSON(t, resp, http.StatusNotFound, &errResp)
					if errResp["error"] == nil {
						t.Fatal("expected an error envelope on the 404 response")
					}
				})
			})
		}
	}
}
