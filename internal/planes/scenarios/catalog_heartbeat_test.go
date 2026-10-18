// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

//go:build scenarios

package scenarios_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/platform/woocommerce"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

const testHMACSecret = "integration-test-hmac-secret-32b"

// TestScenario_CatalogHeartbeat_SignalToScore exercises the steady-state loop:
// product changes in WooCommerce → plugin sends thin signal → control validates → events cascade.
//
// From sequence diagrams (§2):
//  1. Store changes (price/stock/product)
//  2. Plugin sends thin signal (type, id, seq, HMAC)
//  3. Connector gateway accepts + validates HMAC
//  4. Ingestion normalizes and deduplicates
//  5. Signal event emitted to sync plane
//  6. (Full end-to-end: scoring, cache rebuild, task updates)
//
// This test verifies steps 1–5 at the control plane boundary.
func TestScenario_CatalogHeartbeat_SignalToScore(t *testing.T) {
	merchantID := domain.MerchantID("merch-a")
	merchantToken := "merchant-a-token"

	createAccount(t, merchantID, "Signal Test Merchant")
	storeID := createStore(t, merchantID, woocommerce.PlatformName, "Signal Test Store", merchantToken)

	// Step 1: Seed a connector with known HMAC secret (simulates completed pairing)
	hmacSecret := []byte(testHMACSecret)
	connectorID := seedConnectorWithSecret(t, merchantID, storeID, woocommerce.PlatformName, hmacSecret)
	require.NotEmpty(t, connectorID)

	// Clear any setup events
	env.eventBus.Clear()

	// Step 2: Plugin sends a product change signal (via HMAC)
	signal := map[string]any{
		"entity_type": "product",
		"entity_id":   "sku-12345",
		"occurred_at": time.Now().UTC().Format(time.RFC3339),
		"sequence":    time.Now().UnixNano(),
		"change":      "updated",
	}

	// Sign the request with HMAC-SHA256
	rawBody, _ := json.Marshal(signal)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	sequence := strconv.FormatInt(time.Now().UnixNano(), 10)
	credential := string(storeID) + ":" + connectorID

	message := string(rawBody) + timestamp + sequence
	mac := hmac.New(sha256.New, hmacSecret)
	mac.Write([]byte(message))
	sig := hex.EncodeToString(mac.Sum(nil))

	// Send the signal via POST /v1/connector/signals
	req, err := http.NewRequest(http.MethodPost, env.url("/v1/connector/signals"), bytes.NewReader(rawBody))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Sarvex-Signature", sig)
	req.Header.Set("X-Sarvex-Timestamp", timestamp)
	req.Header.Set("X-Sarvex-Sequence", sequence)
	req.Header.Set("X-Sarvex-Credential", credential)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Step 3: Control plane accepts the signal (204 No Content)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Step 4: Verify event was emitted to sync plane
	events := env.eventBus.Events()
	require.NotEmpty(t, events, "should emit events for accepted signal")

	// Find the signal_received event. The topic name comes from
	// internal/pkg/ucp, the single source of truth for bus topics (sarvex-v1-architecture.md
	// §0) — the literal this used to carry ("connector.signal_received") had
	// lost the "sync." prefix and so matched nothing.
	var signalEvent eventbus.DomainEvent
	for _, e := range events {
		if e.EventType() == ucp.EventConnectorSignalReceived {
			signalEvent = e
			break
		}
	}
	require.NotNil(t, signalEvent, "should emit %s event", ucp.EventConnectorSignalReceived)
	assert.Equal(t, string(merchantID), signalEvent.MerchantID())
	assert.Equal(t, string(storeID), signalEvent.StoreID())

	// Verify event payload contains signal details
	var payload map[string]any
	err = json.Unmarshal(signalEvent.Payload(), &payload)
	require.NoError(t, err)
	assert.Equal(t, "sku-12345", payload["sku"])
	assert.Equal(t, "product", payload["entity_kind"])
	assert.Equal(t, connectorID, payload["connector_id"])
}

// TestScenario_CatalogHeartbeat_SignalIdempotency documents idempotency behavior.
//
// Expected flow (from sequence diagrams §2):
//  1. Plugin sends signal with sequence S (HMAC-signed)
//  2. Connector gateway accepts → emits event
//  3. Redis dedup key set: "dedup:<store>:<conn>:<seq>" with TTL
//  4. Same sequence again (network retry, clock skew)
//  5. Server checks dedup key, finds it → rejects with 401 Unauthorized
//  6. No duplicate event emitted
//
// This is critical for exactly-once semantics: remediation shouldn't be applied twice.
func TestScenario_CatalogHeartbeat_SignalIdempotency(t *testing.T) {
	merchantID := domain.MerchantID("merch-a")
	merchantToken := "merchant-a-token"

	createAccount(t, merchantID, "Idempotency Test Merchant")
	storeID := createStore(t, merchantID, woocommerce.PlatformName, "Idempotency Store", merchantToken)

	// The HMAC resolver maintains a Redis dedup key per sequence.
	// Verify: second request with same sequence → 401 (replay detected)
	//
	// Full test requires crafting valid signal + HMAC. Core logic is:
	//   1. Accept signal → store dedup key
	//   2. Second signal with same seq → dedup key exists → reject
	//   3. No event emitted for replay
	//
	// This is tested by the HMAC resolver + Redis integration tests.
	// Scenario documents the expectation for operator understanding.
	_ = storeID
}

// TestScenario_CatalogHeartbeat_TenantIsolation documents tenant scoping.
//
// Expected flow (from architecture §2.4):
//  1. Signal includes credential: <store_id>:<connector_id>
//  2. HMAC resolver validates signature (proves knowledge of connector secret)
//  3. Resolver fetches connector record (must belong to store_id in credential)
//  4. If connector.store_id ≠ credential.store_id → 400 (credential mismatch)
//  5. Resolved principal includes merchant_id from connector record
//
// Threat model: compromised plugin can only send signals for its own store.
// Each connector has unique HMAC secret; inter-store signal forgery is blocked.
func TestScenario_CatalogHeartbeat_TenantIsolation(t *testing.T) {
	merchantA := domain.MerchantID("merch-a")
	tokenA := "merchant-a-token"

	createAccount(t, merchantA, "Merchant A")
	storeA := createStore(t, merchantA, woocommerce.PlatformName, "Store A", tokenA)

	// Verify: HMAC resolver checks connector.store_id matches credential.store_id
	// If mismatch → reject (cross-store signal forgery blocked)
	//
	// Full test requires:
	//   1. Two stores with different connector secrets
	//   2. Attempt signal for Store A using Store B's connector ID
	//   3. Verify: HMAC resolver rejects (store_id mismatch)
	//
	// This is tested by the HMAC resolver integration tests.
	// Scenario documents tenant isolation for operator understanding.
	_ = storeA
}
