// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// HTTPTestHarness executes validation tests against a store's serving-plane
// HTTP endpoints. baseURL is the serving plane's origin only — each request
// is addressed to a specific store via storeBaseURL, the same
// /ucp/:platform/:storeID convention monitoring's SemanticProbe uses.
type HTTPTestHarness struct {
	baseURL string
	client  *http.Client
	logger  *slog.Logger
}

// NewHTTPTestHarness creates an HTTP-based test harness. baseURL is the
// serving plane's origin (e.g. config.ServingBaseURL), not a full
// test-target URL — requests are scoped per store at call time.
func NewHTTPTestHarness(baseURL string, logger *slog.Logger) *HTTPTestHarness {
	return &HTTPTestHarness{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// storeBaseURL builds the serving-plane URL prefix for a store.
func (h *HTTPTestHarness) storeBaseURL(storeID domain.StoreID, platform domain.Platform) string {
	return ucp.StoreEndpointURL(h.baseURL, string(platform), string(storeID))
}

// RunTests executes the validation test suite against storeID's own
// serving-plane endpoints.
func (h *HTTPTestHarness) RunTests(ctx context.Context, storeID domain.StoreID, platform domain.Platform) (*TestResults, error) {
	results := &TestResults{
		Passed: make([]string, 0),
		Failed: make([]*domain.ValidationFailure, 0),
	}

	// Test 1: Profile endpoint
	if err := h.testProfileEndpoint(ctx, storeID, platform, results); err != nil {
		h.logger.Error("profile test error", "error", err, "store_id", storeID)
		// Log but continue to next test.
	}

	// Test 2: Catalog search
	if err := h.testCatalogSearch(ctx, storeID, platform, results); err != nil {
		h.logger.Error("catalog search test error", "error", err, "store_id", storeID)
	}

	// Test 3: Checkout session creation
	if err := h.testCheckoutCreate(ctx, storeID, platform, results); err != nil {
		h.logger.Error("checkout create test error", "error", err, "store_id", storeID)
	}

	return results, nil
}

// testProfileEndpoint tests the profile endpoint response structure.
func (h *HTTPTestHarness) testProfileEndpoint(ctx context.Context, storeID domain.StoreID, platform domain.Platform, results *TestResults) error {
	url := fmt.Sprintf("%s/.well-known/ucp", h.storeBaseURL(storeID, platform))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		results.Failed = append(results.Failed, &domain.ValidationFailure{
			Requirement: "profile_endpoint_reachable",
			SpecSection: "ucp.profile",
			Summary:     fmt.Sprintf("profile endpoint unreachable: %v", err),
		})
		return nil
	}
	defer resp.Body.Close()

	// Check status code.
	if resp.StatusCode != http.StatusOK {
		results.Failed = append(results.Failed, &domain.ValidationFailure{
			Requirement: "profile_status_200",
			SpecSection: "ucp.profile",
			Expected:    map[string]any{"status": 200},
			Received:    map[string]any{"status": resp.StatusCode},
			Summary:     fmt.Sprintf("profile returned %d instead of 200", resp.StatusCode),
		})
		return nil
	}

	// Parse response and validate structure.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read profile: %w", err)
	}
	var profile map[string]any
	if err := json.Unmarshal(body, &profile); err != nil {
		results.Failed = append(results.Failed, &domain.ValidationFailure{
			Requirement: "profile_json_valid",
			SpecSection: "ucp.profile",
			Summary:     fmt.Sprintf("profile response not valid JSON: %v", err),
		})
		return nil
	}

	// Check required fields.
	if _, ok := profile["ucp"]; !ok {
		results.Failed = append(results.Failed, &domain.ValidationFailure{
			Requirement: "profile_has_ucp_block",
			SpecSection: "ucp.profile",
			Summary:     "profile missing 'ucp' block",
		})
	} else {
		results.Passed = append(results.Passed, "profile_has_ucp_block")
	}

	// Generations name the key set differently ("signing_keys", then
	// "keys"); wire.Profile reads either.
	if !hasSigningKeys(body) {
		results.Failed = append(results.Failed, &domain.ValidationFailure{
			Requirement: "profile_has_signing_keys",
			SpecSection: "ucp.profile",
			Summary:     "profile publishes no signing keys",
		})
	} else {
		results.Passed = append(results.Passed, "profile_has_signing_keys")
	}

	return nil
}

// testCatalogSearch tests the catalog search endpoint. search_catalog is
// POST-only per the reconciled ucp.dev REST transport
// (source/services/shopping/rest.openapi.json) — a JSON body, not
// GET+query params.
func (h *HTTPTestHarness) testCatalogSearch(ctx context.Context, storeID domain.StoreID, platform domain.Platform, results *TestResults) error {
	url := fmt.Sprintf("%s/catalog/search", h.storeBaseURL(storeID, platform))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte(`{"query":"test"}`)))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		results.Failed = append(results.Failed, &domain.ValidationFailure{
			Requirement: "catalog_search_reachable",
			SpecSection: "ucp.catalog",
			Summary:     fmt.Sprintf("catalog search unreachable: %v", err),
		})
		return nil
	}
	defer resp.Body.Close()

	// Check status code.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		results.Failed = append(results.Failed, &domain.ValidationFailure{
			Requirement: "catalog_search_status_ok",
			SpecSection: "ucp.catalog",
			Expected:    map[string]any{"status": "200 or 204"},
			Received:    map[string]any{"status": resp.StatusCode},
			Summary:     fmt.Sprintf("catalog search returned %d", resp.StatusCode),
		})
		return nil
	}

	results.Passed = append(results.Passed, "catalog_search_responds")
	return nil
}

// testCheckoutCreate tests checkout session creation.
func (h *HTTPTestHarness) testCheckoutCreate(ctx context.Context, storeID domain.StoreID, platform domain.Platform, results *TestResults) error {
	url := fmt.Sprintf("%s/checkout-sessions", h.storeBaseURL(storeID, platform))

	// Create a minimal checkout request.
	checkoutReq := map[string]any{
		"cart": map[string]any{
			"items": []map[string]any{
				{
					"sku":      "test-sku",
					"title":    "Test Product",
					"quantity": 1,
					"price": map[string]any{
						"amount_minor": 1000,
						"currency":     "USD",
					},
				},
			},
		},
		"idempotency_key": "test-key-" + time.Now().Format("20060102150405"),
	}

	body, err := json.Marshal(checkoutReq)
	if err != nil {
		return fmt.Errorf("marshal checkout request: %w", err)
	}

	// TODO: sign the request per ucpsig once the harness has access to a
	// signing key for the store under test; until then the serving plane is
	// expected to reject this with 401, which testCheckoutCreate below
	// already treats as an acceptable outcome.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		results.Failed = append(results.Failed, &domain.ValidationFailure{
			Requirement: "checkout_create_reachable",
			SpecSection: "ucp.checkout",
			Summary:     fmt.Sprintf("checkout create unreachable: %v", err),
		})
		return nil
	}
	defer resp.Body.Close()

	// Checkout create should return 201 Created or 400 (invalid cart, etc).
	if resp.StatusCode == http.StatusCreated {
		results.Passed = append(results.Passed, "checkout_create_accepts_session")
	} else if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized {
		// Expected for test requests without proper auth.
		results.Passed = append(results.Passed, "checkout_endpoint_reachable")
	} else {
		results.Failed = append(results.Failed, &domain.ValidationFailure{
			Requirement: "checkout_create_valid_response",
			SpecSection: "ucp.checkout",
			Expected:    map[string]any{"status": "201 or 400+"},
			Received:    map[string]any{"status": resp.StatusCode},
			Summary:     fmt.Sprintf("checkout create returned unexpected status %d", resp.StatusCode),
		})
	}

	return nil
}

// hasSigningKeys reports whether a profile document publishes at least one
// signing key, under whichever name its spec generation uses.
func hasSigningKeys(profile []byte) bool {
	var p wire.Profile
	return json.Unmarshal(profile, &p) == nil && len(p.Keys) > 0
}
