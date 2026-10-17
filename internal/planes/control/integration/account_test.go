// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAccount_Get verifies GET /account returns the account for the calling merchant
// and 404 when no account has been created yet.
func TestAccount_Get(t *testing.T) {
	t.Run("not_found_when_unseeded", func(t *testing.T) {
		// Use a token whose merchant has no seeded account in this test run.
		// merchantBID is used by store tests but no account is ever seeded for it.
		resp := apiGet(t, "/v1/account", tokenMerchantB)
		assertErrorEnvelope(t, resp, http.StatusNotFound)
	})

	t.Run("returns_account_when_seeded", func(t *testing.T) {
		seedAccount(t, merchantAID, "Acme Corp")

		resp := apiGet(t, "/v1/account", tokenMerchantA)
		var result map[string]any
		decodeJSON(t, resp, http.StatusOK, &result)
		assert.Equal(t, "Acme Corp", result["name"])
	})
}

// TestAccount_Update verifies PATCH /account updates and persists the account.
func TestAccount_Update(t *testing.T) {
	seedAccount(t, merchantAID, "Before Update")

	update := map[string]any{
		"name":         "After Update",
		"timezone":     "Europe/London",
		"country_code": "GB",
	}
	resp := apiPatch(t, "/v1/account", tokenMerchantA, update)
	var result map[string]any
	decodeJSON(t, resp, http.StatusOK, &result)
	assert.Equal(t, "After Update", result["name"])
	assert.Equal(t, "Europe/London", result["timezone"])

	// Verify it persisted.
	resp2 := apiGet(t, "/v1/account", tokenMerchantA)
	var result2 map[string]any
	decodeJSON(t, resp2, http.StatusOK, &result2)
	assert.Equal(t, "After Update", result2["name"])
}

// TestAccount_MerchantIDIsCallersOwn verifies that the merchant_id in the
// GET /account response is the caller's own, resolved from the bearer claim.
//
// The account schema in sarvex-v1-control-plane.yaml declares merchant_id, so
// returning it is the contract, not a leak: /account is the caller's *own*
// account and the id is already theirs. Invariant §2.4 constrains where the id
// is *read from* — server-side, from the Kinde claim, never a URL path or query
// parameter — not whether a merchant may see their own identifier. The
// tenant-isolation assertion that matters is that another merchant's id can
// never be reached, which TestAuth_TenantIsolation covers.
func TestAccount_MerchantIDIsCallersOwn(t *testing.T) {
	seedAccount(t, merchantAID, "Invariant Check")

	resp := apiGet(t, "/v1/account", tokenMerchantA)
	var result map[string]any
	decodeJSON(t, resp, http.StatusOK, &result)

	assert.Equal(t, string(merchantAID), result["merchant_id"],
		"account response must carry the caller's own merchant_id, never another tenant's")
}

// TestAccount_Members verifies GET /account/members returns the standard list
// envelope even when the members collection is empty.
func TestAccount_Members(t *testing.T) {
	resp := apiGet(t, "/v1/account/members", tokenMerchantA)
	data, hasMore, _ := assertListEnvelope(t, resp)
	assert.False(t, hasMore)
	assert.NotNil(t, data) // may be empty slice, not nil
}
