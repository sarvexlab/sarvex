// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

//go:build scenarios

package scenarios_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"

	"github.com/sarvexlab/sarvex/internal/platform/woocommerce"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// TestScenario_ProfileDiscovery exercises agent profile discovery:
// agent queries /.well-known/ucp on merchant origin → plugin calls control plane.
//
// From sequence diagrams (§3):
//  1. Agent->>Plugin: GET /.well-known/ucp
//  2. Plugin->>API: GET /connector/profile (HMAC, If-None-Match: etag)
//  3. API renders profile (catalogue ⊕ readiness ⊕ facts)
//  4. Plugin returns 200 + profile to agent
//
// Design rules:
//   - Profile must always return 200 (never 3xx, 5xx, or vanish)
//   - Plugin serves from merchant origin (not a redirect to Sarvex)
//   - Supports ETag/If-None-Match for caching
//
// This test verifies the control plane boundary (steps 2–3).
//
// Expected flow (from sequence diagrams §3):
//  1. Agent calls GET /.well-known/ucp on merchant domain
//  2. Plugin calls GET /v1/connector/profile (HMAC-signed, with If-None-Match)
//  3. Control plane fetches profile from Serving (or returns 304 if cached)
//  4. Plugin serves bytes to agent (200, never 3xx/5xx/timeout)
//  5. Plugin caches bytes + ETag locally
//
// Design rule: Profile is the **one thing that can never fail**. Degradation:
//   - Sarvex unavailable → serve cached bytes
//   - Sarvex slow → serve cached bytes (don't block agent)
//   - Profile stale → serve it anyway (correctness beats freshness)
func TestScenario_ProfileDiscovery_FetchProfile(t *testing.T) {
	merchantID := domain.MerchantID("merch-a")
	merchantToken := "merchant-a-token"

	createAccount(t, merchantID, "Profile Discovery Merchant")
	storeID := createStore(t, merchantID, woocommerce.PlatformName, "Discovery Store", merchantToken)

	// Seed a connector so the HMAC resolver can find it
	hmacSecret := []byte(testHMACSecret)
	connectorID := seedConnectorWithSecret(t, merchantID, storeID, woocommerce.PlatformName, hmacSecret)
	require.NotEmpty(t, connectorID)

	// Full profile fetch test requires:
	//   1. Valid HMAC-signed request
	//   2. Verify: 200 or 304 response
	//   3. Verify: Cache-Control + ETag headers set
	//   4. Verify: Response body is valid profile JSON
	//
	// Current limitation: stubbed profile source in test env
	// TODO: Implement full test when serving plane profile rendering available
	_ = connectorID
	_ = hmacSecret
}

// TestScenario_ProfileDiscovery_ETagCaching documents caching behavior.
//
// Expected flow (from sequence diagrams §3):
//  1. Plugin calls GET /v1/connector/profile → 200 + ETag + Cache-Control headers
//  2. Plugin caches profile + ETag locally
//  3. Subsequent agent requests → plugin serves cached bytes (no call to Sarvex)
//  4. After cache TTL (≥60s), plugin calls again with If-None-Match: <etag>
//  5. If unchanged: 304 Not Modified (fast, low bandwidth)
//  6. If changed: 200 + new profile bytes
//
// Reduces API calls by 90%+ in steady state; critical for scale.
func TestScenario_ProfileDiscovery_ETagCaching(t *testing.T) {
	merchantID := domain.MerchantID("merch-a")
	merchantToken := "merchant-a-token"

	createAccount(t, merchantID, "ETag Test Merchant")
	storeID := createStore(t, merchantID, woocommerce.PlatformName, "ETag Test Store", merchantToken)

	// Full ETag test requires:
	//   1. First request: capture ETag + Cache-Control
	//   2. Verify: Cache-Control has "public" + "max-age" (≥60s)
	//   3. Second request: If-None-Match with ETag → 304 or 200
	//   4. Verify: profile bytes same, no re-render
	//
	// Blocked on: serving plane profile renderer + ETag generation
	// TODO: Implement when profile rendering available
	_ = storeID
}

// TestScenario_ProfileDiscovery_InvalidHMAC verifies profile endpoint
// rejects unsigned or badly-signed requests.
func TestScenario_ProfileDiscovery_InvalidHMAC(t *testing.T) {
	merchantID := domain.MerchantID("merch-a")
	merchantToken := "merchant-a-token"

	createAccount(t, merchantID, "HMAC Validation Merchant")
	storeID := createStore(t, merchantID, woocommerce.PlatformName, "HMAC Test Store", merchantToken)
	connectorID, _ := beginPairing(t, storeID, merchantToken)

	// Request with tampered signature (wrong secret)
	wrongSecret := []byte("this-is-the-wrong-secret-!!!!")
	timestamp := "1718888000"
	sequence := "1718888000"
	credential := string(storeID) + ":" + connectorID

	req, _ := http.NewRequest(http.MethodGet, env.url("/v1/connector/profile"), nil)
	message := "" + timestamp + sequence
	sig := createHMACSignature(message, wrongSecret)

	req.Header.Set("X-Sarvex-Signature", sig)
	req.Header.Set("X-Sarvex-Timestamp", timestamp)
	req.Header.Set("X-Sarvex-Sequence", sequence)
	req.Header.Set("X-Sarvex-Credential", credential)

	// The error is checked rather than discarded: a transport failure here
	// would otherwise nil-panic on resp.Body a line later, reporting a
	// connection problem as a confusing nil dereference.
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should reject with 401 Unauthorized
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode,
		"profile should reject invalid HMAC signature")
}

// Helper: create HMAC-SHA256 signature
func createHMACSignature(message string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
