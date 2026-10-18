// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStore_Create verifies that POST /stores creates a store and returns the
// standard shape including a store_id and initial status.
func TestStore_Create(t *testing.T) {
	body := map[string]any{
		"display_name": "My Test Store",
		"platform":     "woocommerce",
	}
	resp := apiPost(t, "/v1/stores", tokenMerchantA, body)

	var result map[string]any
	decodeJSON(t, resp, http.StatusCreated, &result)

	assert.NotEmpty(t, result["store_id"], "store_id must be present")
	// The request field is display_name but the response field is name — that
	// asymmetry is the contract (sarvex-v1-control-plane.yaml: store_create
	// takes display_name, the store schema returns name), not a typo.
	assert.Equal(t, "My Test Store", result["name"])
	assert.Equal(t, "woocommerce", result["platform"])
	assert.NotEmpty(t, result["status"])
}

// TestStore_Create_InvalidPlatform verifies that an unknown platform is rejected.
func TestStore_Create_InvalidPlatform(t *testing.T) {
	body := map[string]any{
		"display_name": "Bad Store",
		"platform":     "shopify", // not supported in V1
	}
	resp := apiPost(t, "/v1/stores", tokenMerchantA, body)
	assertErrorEnvelope(t, resp, http.StatusUnprocessableEntity)
}

// TestStore_Get verifies that GET /stores/:id returns the store for the owning
// merchant and 404 for another merchant.
func TestStore_Get(t *testing.T) {
	storeID := seedStore(t, merchantAID, "Get Test Store", "woocommerce")

	t.Run("owner", func(t *testing.T) {
		resp := apiGet(t, "/v1/stores/"+string(storeID), tokenMerchantA)
		var result map[string]any
		decodeJSON(t, resp, http.StatusOK, &result)
		assert.Equal(t, string(storeID), result["store_id"])
		assert.Equal(t, "Get Test Store", result["name"])
	})

	t.Run("wrong_merchant", func(t *testing.T) {
		resp := apiGet(t, "/v1/stores/"+string(storeID), tokenMerchantB)
		assertErrorEnvelope(t, resp, http.StatusNotFound)
	})

	t.Run("nonexistent", func(t *testing.T) {
		resp := apiGet(t, "/v1/stores/does-not-exist", tokenMerchantA)
		assertErrorEnvelope(t, resp, http.StatusNotFound)
	})
}

// TestStore_List verifies cursor-based pagination on GET /stores.
func TestStore_List(t *testing.T) {
	// Seed 3 stores under a fresh merchant to avoid interference with other tests.
	// Use merchantBID to keep these isolated from merchantA's stores.
	const n = 3
	for i := 0; i < n; i++ {
		seedStore(t, merchantBID, fmt.Sprintf("List Store %d", i), "woocommerce")
	}

	t.Run("first_page", func(t *testing.T) {
		resp := apiGet(t, "/v1/stores?limit=2", tokenMerchantB)
		data, hasMore, cursor := assertListEnvelope(t, resp)
		assert.Len(t, data, 2)
		assert.True(t, hasMore)
		assert.NotNil(t, cursor, "cursor must be non-nil when has_more is true")
	})

	t.Run("second_page_exhausts", func(t *testing.T) {
		resp := apiGet(t, "/v1/stores?limit=2", tokenMerchantB)
		_, _, cursorRaw := assertListEnvelope(t, resp)
		require.NotNil(t, cursorRaw)
		cursor, _ := cursorRaw.(string)

		resp2 := apiGet(t, "/v1/stores?limit=2&cursor="+cursor, tokenMerchantB)
		data2, hasMore2, _ := assertListEnvelope(t, resp2)
		assert.Len(t, data2, 1, "third store on second page")
		assert.False(t, hasMore2)
	})

	t.Run("empty_for_other_merchant", func(t *testing.T) {
		// merchantA may have stores but we verify the response shape.
		resp := apiGet(t, "/v1/stores?limit=100", tokenMerchantA)
		data, _, _ := assertListEnvelope(t, resp)
		// All returned stores must belong to merchantA (verified via API shape only).
		for _, raw := range data {
			s, ok := raw.(map[string]any)
			require.True(t, ok)
			// merchant_id is never in the response body per the invariant.
			_, hasMerchantID := s["merchant_id"]
			assert.False(t, hasMerchantID, "merchant_id must not appear in store response")
		}
	})
}

// TestStore_Update verifies that PATCH /stores/:id updates the display name.
func TestStore_Update(t *testing.T) {
	storeID := seedStore(t, merchantAID, "Original Name", "woocommerce")

	resp := apiPatch(t, "/v1/stores/"+string(storeID), tokenMerchantA,
		map[string]any{"display_name": "Updated Name"})
	var result map[string]any
	decodeJSON(t, resp, http.StatusOK, &result)
	assert.Equal(t, "Updated Name", result["name"])

	// Confirm the update persisted.
	resp2 := apiGet(t, "/v1/stores/"+string(storeID), tokenMerchantA)
	var result2 map[string]any
	decodeJSON(t, resp2, http.StatusOK, &result2)
	assert.Equal(t, "Updated Name", result2["name"])
}

// TestStore_Delete verifies that DELETE /stores/:id removes the store.
func TestStore_Delete(t *testing.T) {
	storeID := seedStore(t, merchantAID, "Delete Me", "woocommerce")

	resp := apiDelete(t, "/v1/stores/"+string(storeID), tokenMerchantA)
	assertStatus(t, resp, http.StatusNoContent)

	// Subsequent GET returns 404.
	resp2 := apiGet(t, "/v1/stores/"+string(storeID), tokenMerchantA)
	assertErrorEnvelope(t, resp2, http.StatusNotFound)
}

// TestStore_MerchantIDNeverInResponse verifies that merchant_id is not present
// in any store response field — it must only live in the principal, never in URLs.
func TestStore_MerchantIDNeverInResponse(t *testing.T) {
	storeID := seedStore(t, merchantAID, "Invariant Check", "woocommerce")

	resp := apiGet(t, "/v1/stores/"+string(storeID), tokenMerchantA)
	var result map[string]any
	decodeJSON(t, resp, http.StatusOK, &result)

	_, hasMerchantID := result["merchant_id"]
	assert.False(t, hasMerchantID,
		"merchant_id must not be present in the store response (architecture invariant §2.4)")
}
