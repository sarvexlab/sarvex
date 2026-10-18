// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

//go:build scenarios

package scenarios_test

import (
	"testing"

	"github.com/sarvexlab/sarvex/internal/platform/woocommerce"

	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// TestScenario_PerRequestNegotiation exercises agent-merchant capability negotiation:
// agent submits UCP request with its profile → serving plane intersects → returns active capabilities.
//
// From sequence diagrams (§4):
//  1. Agent->>Edge: UCP API call + UCP-Agent profile="https://agent/.well-known/ucp"
//  2. Edge verifies signature, resolves agent profile
//  3. Negotiation: intersect merchant ⊕ agent capabilities
//  4. Prune extensions to fixpoint (mutual versions)
//  5. Return active capability set (empty if no intersection)
//
// Design rules:
//   - **Server selects**: Sarvex chooses what's active, not the agent
//   - **Fixpoint extension pruning**: resolve circular dependencies (e.g., A requires B, B requires A)
//   - **Graceful fallback**: empty intersection → 200 with error status + continue_url
//   - **Caching**: agent profile fetched once per URL, cached ≥60s
//
// This test verifies the negotiation logic at the serving plane boundary.
func TestScenario_PerRequestNegotiation_IntersectionSuccess(t *testing.T) {
	// Note: Full per-request negotiation testing requires:
	//   - Serving plane running (not just control plane)
	//   - Mock agent profile URL
	//   - Real capability intersection logic
	//
	// This scenario documents the expected flow:
	//
	// 1. Merchant profile advertises:
	//    - catalog v1.0, v1.1
	//    - checkout v2.0
	//    - order v1.5
	//
	// 2. Agent submits request with profile:
	//    - catalog v1.1 (highest mutual version)
	//    - checkout v2.0
	//    - (order not supported)
	//
	// 3. Negotiation engine:
	//    - Intersect: catalog {v1.0, v1.1} ∩ {v1.1} = {v1.1}
	//    - Intersect: checkout {v2.0} ∩ {v2.0} = {v2.0}
	//    - Intersect: order {v1.5} ∩ {} = {} (agent doesn't support)
	//    - Active: [catalog v1.1, checkout v2.0]
	//
	// 4. Response: 200 ucp.capabilities = [active set]
	//
	// TODO: implement full test when serving plane is available

	merchantID := domain.MerchantID("merch-a")
	merchantToken := "merchant-a-token"

	createAccount(t, merchantID, "Negotiation Merchant")
	storeID := createStore(t, merchantID, woocommerce.PlatformName, "Negotiation Store", merchantToken)
	require.NotEmpty(t, storeID)

	// In a real test:
	// 1. Create store with specific profile (mock)
	// 2. Mock agent profile at a URL
	// 3. Agent calls serving edge with UCP-Agent header
	// 4. Verify response contains active intersection
}

// TestScenario_PerRequestNegotiation_EmptyIntersection verifies graceful handling
// when agent and merchant have no common capabilities.
func TestScenario_PerRequestNegotiation_EmptyIntersection(t *testing.T) {
	// Expected behavior:
	//   - Agent supports: [order v3.0]
	//   - Merchant supports: [catalog v1.0, checkout v2.0]
	//   - Intersection: {}
	//   - Response: 200 ucp.status=error + messages + continue_url
	//
	// TODO: implement when serving plane is available
}

// TestScenario_PerRequestNegotiation_ExtensionPruning verifies fixpoint logic
// for complex extension dependencies.
func TestScenario_PerRequestNegotiation_ExtensionPruning(t *testing.T) {
	// Scenario:
	//   - Checkout base + fulfillment extension
	//   - Fulfillment requires buyer_consent extension
	//   - Agent supports: [checkout, fulfillment, buyer_consent]
	//   - Merchant supports: [checkout, fulfillment] (no buyer_consent)
	//   - Pruning step: remove fulfillment (unmet dependency on buyer_consent)
	//   - Active: [checkout only]
	//
	// TODO: implement when serving plane is available
}

// TestScenario_PerRequestNegotiation_SignatureVerification verifies agent profile
// signatures are validated before trusting the advertised capabilities.
func TestScenario_PerRequestNegotiation_SignatureVerification(t *testing.T) {
	// Design rule: agent UCP responses must be signed
	//   - Agent publishes signing_keys[] in its profile
	//   - Agent signs each UCP response with a kid (key id)
	//   - Serving edge verifies: kid ∈ signing_keys, sig valid
	//   - If signature fails: 424 profile_unreachable (malicious/broken agent)
	//
	// TODO: implement when serving plane is available
}

// TestScenario_PerRequestNegotiation_ProfileCaching verifies agent profiles
// are cached with ≥60s TTL to avoid repeated fetches.
func TestScenario_PerRequestNegotiation_ProfileCaching(t *testing.T) {
	// Expected behavior:
	//   1. First request: fetch agent profile from URL
	//   2. Second request (within 60s): serve from cache
	//   3. Third request (after 60s): refetch
	//
	// TODO: verify cache headers + timing when serving plane is available
}
