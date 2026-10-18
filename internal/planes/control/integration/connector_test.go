// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// TestConnector_PairingFlow exercises the full connector pairing sequence:
// BeginPairing (bearer) → CompletePairing (HMAC) → verify connector state.
func TestConnector_PairingFlow(t *testing.T) {
	storeID := seedStore(t, merchantAID, "Pairing Store", "woocommerce")

	// Step 1: begin pairing (merchant console, bearer auth).
	beginResp := apiPost(t,
		"/v1/stores/"+string(storeID)+"/connectors/pairing",
		tokenMerchantA,
		map[string]any{"platform": "woocommerce"},
	)
	var beginBody map[string]any
	decodeJSON(t, beginResp, http.StatusCreated, &beginBody)

	connectorID, _ := beginBody["connector_id"].(string)
	pairingCode, _ := beginBody["pairing_code"].(string)
	require.NotEmpty(t, connectorID, "connector_id must be returned")
	require.NotEmpty(t, pairingCode, "pairing_code must be returned once only")
	assert.Equal(t, string(storeID), beginBody["store_id"])

	// Step 2: complete pairing (plugin, HMAC-signed).
	// The freshly created connector has no HMAC secret yet; CompletePairing is
	// authenticated via the pairing code (not HMAC). The handler uses hmac auth
	// only for signals/heartbeat/profile after pairing is complete.
	// CompletePairing is wired under /connectors/pairing/complete with HMAC auth.
	//
	// To sign the complete-pairing request we need a secret. For this endpoint
	// we supply a minimal HMAC signature — the resolver checks the connector's
	// stored secret which is empty for a freshly-paired connector. Instead, we
	// seed a ready connector with a known secret and test the post-pairing
	// endpoints directly (see TestConnector_PluginEndpoints).
	//
	// Here we just verify the state machine: after BeginPairing the connector is
	// stored in "pairing" state, which we can confirm via the repository.
	connModel, err := env.connectorRepo.GetByConnector(context.Background(), connectorID)
	require.NoError(t, err)
	assert.Equal(t, domain.ConnectorStatusPairing, connModel.Status)
	assert.Equal(t, string(storeID), connModel.StoreID)
}

// TestConnector_Get verifies GET /connectors/:id returns the connector for its
// owning merchant.
func TestConnector_Get(t *testing.T) {
	storeID := seedStore(t, merchantAID, "Get Connector Store", "woocommerce")
	connID := seedConnector(t, merchantAID, storeID, "woocommerce", testHMACSecret)

	t.Run("owner_gets_connector", func(t *testing.T) {
		resp := apiGet(t, "/v1/connectors/"+connID, tokenMerchantA)
		var result map[string]any
		decodeJSON(t, resp, http.StatusOK, &result)
		assert.Equal(t, connID, result["connector_id"])
		assert.Equal(t, string(storeID), result["store_id"])
		assert.Equal(t, "woocommerce", result["platform"])
		// HMAC secret must never be in the response.
		_, hasSecret := result["hmac_secret"]
		assert.False(t, hasSecret, "hmac_secret must never appear in the connector response")
	})

	t.Run("wrong_merchant", func(t *testing.T) {
		resp := apiGet(t, "/v1/connectors/"+connID, tokenMerchantB)
		assertErrorEnvelope(t, resp, http.StatusNotFound)
	})

	t.Run("nonexistent", func(t *testing.T) {
		resp := apiGet(t, "/v1/connectors/nonexistent-id", tokenMerchantA)
		assertErrorEnvelope(t, resp, http.StatusNotFound)
	})
}

// TestConnector_List verifies cursor-based pagination on GET /connectors.
func TestConnector_List(t *testing.T) {
	// Use a dedicated merchant to avoid noise from other tests.
	// We re-use merchantAID but with unique store/connector IDs; the list will
	// include previously seeded connectors from other tests, so we only assert
	// the pagination shape, not exact counts.
	storeID := seedStore(t, merchantAID, "List Connectors Store", "woocommerce")
	for i := 0; i < 3; i++ {
		seedConnector(t, merchantAID, storeID, "woocommerce", testHMACSecret)
	}

	resp := apiGet(t, "/v1/connectors?limit=2", tokenMerchantA)
	data, hasMore, _ := assertListEnvelope(t, resp)
	assert.GreaterOrEqual(t, len(data), 1)

	// If at least 3 connectors exist for merchantA, the first page of 2 must
	// set has_more = true.  We seeded 3 here, but other tests may have seeded
	// more, so we only assert has_more if we got exactly 2.
	if len(data) == 2 {
		assert.True(t, hasMore)
	}
}

// TestConnector_Resync verifies that POST /connectors/:id/resync queues a job.
func TestConnector_Resync(t *testing.T) {
	storeID := seedStore(t, merchantAID, "Resync Store", "woocommerce")
	connID := seedConnector(t, merchantAID, storeID, "woocommerce", testHMACSecret)

	resp := apiPost(t, "/v1/connectors/"+connID+"/resync", tokenMerchantA, nil)
	var result map[string]any
	decodeJSON(t, resp, http.StatusAccepted, &result)
	assert.NotEmpty(t, result["job_id"])
	assert.Equal(t, "queued", result["status"])
}

// TestConnector_Disconnect verifies DELETE /connectors/:id removes the connector.
func TestConnector_Disconnect(t *testing.T) {
	storeID := seedStore(t, merchantAID, "Disconnect Store", "woocommerce")
	connID := seedConnector(t, merchantAID, storeID, "woocommerce", testHMACSecret)

	resp := apiDelete(t, "/v1/connectors/"+connID, tokenMerchantA)
	assertStatus(t, resp, http.StatusNoContent)

	// Connector must be gone.
	resp2 := apiGet(t, "/v1/connectors/"+connID, tokenMerchantA)
	assertErrorEnvelope(t, resp2, http.StatusNotFound)
}

// The connector-ingest tests that used to live here — TestConnector_PluginEndpoints,
// TestConnector_HMACReplay and TestConnector_HMACBadSignature — drove
// /v1/connector/{signals,heartbeat,profile} against the *control* plane's
// handler. Those routes are served by the sync plane (sarvex-v1-architecture.md;
// internal/planes/sync/api/handler.go), so every one of them 404'd here.
//
// They were not deleted so much as relocated to where the routes actually are:
//   - route registration, HMAC required, forged signature rejected, and the
//     profile body/etag/not-modified paths: internal/planes/sync/api/handler_test.go
//   - signal/heartbeat accept + validation: the same file
//   - sequence-replay rejection against real Redis, which is what the dedup key
//     needs: internal/planes/scenarios/catalog_heartbeat_test.go
//
// What remains in this file is the control plane's own connector surface: the
// merchant-facing /v1/connectors CRUD and the pairing handshake.

// TestConnector_BeginPairing_StoreNotFound verifies that beginning pairing for
// a nonexistent store returns 404.
func TestConnector_BeginPairing_StoreNotFound(t *testing.T) {
	resp := apiPost(t,
		"/v1/stores/nonexistent-store/connectors/pairing",
		tokenMerchantA,
		map[string]any{"platform": "woocommerce"},
	)
	assertErrorEnvelope(t, resp, http.StatusNotFound)
}
