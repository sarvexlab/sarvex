// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/safego"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// drainAndClose reads resp.Body to completion before closing it. Closing an
// HTTP response body without first draining it prevents the underlying
// connection from being returned to the client's idle pool, forcing a fresh
// TCP+TLS handshake on the next probe against the same host — costly given
// probes run repeatedly against the same store every probeInterval.
func drainAndClose(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// StoreURLProvider resolves a store's storefront URL for monitoring.
type StoreURLProvider interface {
	GetStorefrontURL(ctx context.Context, storeID string) (string, error)
}

// Sentinel SKUs the semantic probe battery can raise findings against. Fixed
// and exhaustive — every ConsistencyFinding a probe in this file can ever
// produce uses one of these two. RunProbesForStore always runs the complete
// battery, so this set is what makes recovery reconciliation
// (ReconcileRecoveredProbeFindings) safe to scope by: any currently-open
// monitoring task against one of these SKUs was necessarily re-checked this
// pass, unlike sweep- or discrepancy-subscriber-raised tasks on real product
// SKUs, which a probe pass never touches.
const (
	SKUProfile       = "system:profile"
	SKUPluginGateway = "system:plugin_gateway"
)

// ProbeResult holds the outcome of a semantic probe.
type ProbeResult struct {
	ProbeName string
	Passed    bool
	Latency   time.Duration
	Error     string
	Findings  []*domain.ConsistencyFinding
}

// SemanticProbe runs HTTP requests to the serving plane and validates responses.
type SemanticProbe struct {
	baseURL     string
	client      *http.Client
	logger      *slog.Logger
	urlProvider StoreURLProvider
}

// NewSemanticProbe creates a semantic probe.
func NewSemanticProbe(baseURL string, logger *slog.Logger, urlProvider StoreURLProvider) *SemanticProbe {
	return &SemanticProbe{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		logger:      logger,
		urlProvider: urlProvider,
	}
}

// RunProbes executes all semantic probes for a store concurrently and
// returns their findings. platform selects the route prefix (serving routes
// are mounted at /ucp/:platform/:storeID — see
// internal/planes/serving/api/entry.go). The 5 probes are independent HTTP
// calls against different endpoints, so running them sequentially would pay
// their latency additively (up to 5x the client's 5s timeout) for no
// correctness reason; each writes to its own result slot, so no
// synchronization beyond the WaitGroup is needed.
func (sp *SemanticProbe) RunProbes(ctx context.Context, storeID, platform string) ([]*ProbeResult, error) {
	probes := []func(context.Context) *ProbeResult{
		func(ctx context.Context) *ProbeResult { return sp.probeProfile(ctx, storeID, platform) },
		func(ctx context.Context) *ProbeResult { return sp.probeCatalogSearch(ctx, storeID, platform) },
		func(ctx context.Context) *ProbeResult { return sp.probeCatalogLookup(ctx, storeID, platform) },
		func(ctx context.Context) *ProbeResult { return sp.probeCheckoutCreate(ctx, storeID, platform) },
		func(ctx context.Context) *ProbeResult { return sp.probeProfileEdge(ctx, storeID) },
	}

	results := make([]*ProbeResult, len(probes))
	var wg sync.WaitGroup
	for i, probe := range probes {
		wg.Add(1)
		safego.Go("semantic-probe", func() {
			defer wg.Done()
			results[i] = probe(ctx)
		})
	}
	wg.Wait()

	return results, nil
}

// storeBaseURL builds the serving-plane URL prefix for a store.
func (sp *SemanticProbe) storeBaseURL(storeID, platform string) string {
	return ucp.StoreEndpointURL(sp.baseURL, platform, storeID)
}

// probeProfile validates Sarvex's own copy of the profile (the serving
// plane's mirror the connector's GET /connector/profile call reads) — not
// the plugin's edge copy at the merchant's own domain (see probeProfileEdge).
// No signature required (restrictedMiddlewares — see handler.go).
func (sp *SemanticProbe) probeProfile(ctx context.Context, storeID, platform string) *ProbeResult {
	start := time.Now()
	url := fmt.Sprintf("%s/.well-known/ucp", sp.storeBaseURL(storeID, platform))

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := sp.client.Do(req)
	latency := time.Since(start)

	result := &ProbeResult{
		ProbeName: "profile_endpoint",
		Latency:   latency,
		Findings:  make([]*domain.ConsistencyFinding, 0),
	}

	if err != nil {
		result.Error = fmt.Sprintf("unreachable: %v", err)
		sp.logger.Warn("profile probe failed", "store_id", storeID, "error", err)
		return result
	}
	defer drainAndClose(resp)

	// Check status code
	if resp.StatusCode != http.StatusOK {
		result.Error = fmt.Sprintf("status %d", resp.StatusCode)
		return result
	}

	// Parse and validate structure
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Error = fmt.Sprintf("read body: %v", err)
		return result
	}
	var profile map[string]any
	if err := json.Unmarshal(body, &profile); err != nil {
		result.Error = fmt.Sprintf("invalid JSON: %v", err)
		return result
	}

	// Validate required fields
	if _, ok := profile["ucp"]; !ok {
		result.Findings = append(result.Findings, &domain.ConsistencyFinding{
			SKU:        SKUProfile,
			Field:      "ucp",
			Sources:    map[string]string{"serving": "missing"},
			Severity:   domain.SeverityCritical,
			DetectedAt: start,
		})
	}

	// Generations name the key set differently ("signing_keys", then
	// "keys"); wire.Profile reads either.
	var keyed wire.Profile
	if err := json.Unmarshal(body, &keyed); err != nil || len(keyed.Keys) == 0 {
		result.Findings = append(result.Findings, &domain.ConsistencyFinding{
			SKU:        SKUProfile,
			Field:      "signing_keys",
			Sources:    map[string]string{"serving": "missing"},
			Severity:   domain.SeverityCritical,
			DetectedAt: start,
		})
	}

	result.Passed = len(result.Findings) == 0
	if result.Passed {
		sp.logger.Debug("profile probe passed", "store_id", storeID, "latency_ms", latency.Milliseconds())
	}

	return result
}

// probeCatalogSearch validates catalog search endpoint is reachable and
// enforcing the UCP signature requirement. This probe sends no signature
// (it is a liveness/reachability check, not a full signed-agent conformance
// run — see the validation orchestrator for that), so a healthy signed-only
// endpoint correctly returns 401; only unreachable or 5xx counts as failed.
func (sp *SemanticProbe) probeCatalogSearch(ctx context.Context, storeID, platform string) *ProbeResult {
	start := time.Now()
	url := fmt.Sprintf("%s/catalog/search", sp.storeBaseURL(storeID, platform))

	// search_catalog is POST-only per the reconciled ucp.dev REST transport
	// (source/services/shopping/rest.openapi.json) — a JSON body, not
	// GET+query params.
	body := strings.NewReader(`{"query":"test"}`)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := sp.client.Do(req)
	latency := time.Since(start)

	result := &ProbeResult{
		ProbeName: "catalog_search",
		Latency:   latency,
		Findings:  make([]*domain.ConsistencyFinding, 0),
	}

	if err != nil {
		result.Error = fmt.Sprintf("unreachable: %v", err)
		return result
	}
	defer drainAndClose(resp)

	result.Passed = resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized
	if !result.Passed {
		result.Error = fmt.Sprintf("status %d", resp.StatusCode)
	}

	return result
}

// probeCatalogLookup validates single product lookup, same reachability
// contract as probeCatalogSearch.
func (sp *SemanticProbe) probeCatalogLookup(ctx context.Context, storeID, platform string) *ProbeResult {
	start := time.Now()
	url := fmt.Sprintf("%s/catalog/test-sku", sp.storeBaseURL(storeID, platform))

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := sp.client.Do(req)
	latency := time.Since(start)

	result := &ProbeResult{
		ProbeName: "catalog_lookup",
		Latency:   latency,
		Findings:  make([]*domain.ConsistencyFinding, 0),
	}

	if err != nil {
		result.Error = fmt.Sprintf("unreachable: %v", err)
		return result
	}
	defer drainAndClose(resp)

	// 200 (product exists), 404 (product not found), or 401 (unsigned
	// request correctly rejected) are all healthy-endpoint signals.
	result.Passed = resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized
	if !result.Passed {
		result.Error = fmt.Sprintf("status %d", resp.StatusCode)
	}

	return result
}

// probeCheckoutCreate validates checkout session creation, same
// reachability contract as probeCatalogSearch.
func (sp *SemanticProbe) probeCheckoutCreate(ctx context.Context, storeID, platform string) *ProbeResult {
	start := time.Now()
	url := fmt.Sprintf("%s/checkout-sessions", sp.storeBaseURL(storeID, platform))

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	req.Header.Set("Content-Type", "application/json")
	resp, err := sp.client.Do(req)
	latency := time.Since(start)

	result := &ProbeResult{
		ProbeName: "checkout_create",
		Latency:   latency,
		Findings:  make([]*domain.ConsistencyFinding, 0),
	}

	if err != nil {
		result.Error = fmt.Sprintf("unreachable: %v", err)
		return result
	}
	defer drainAndClose(resp)

	// 201, 400, or 401 (auth required) are all acceptable probe responses.
	result.Passed = resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized
	if !result.Passed {
		result.Error = fmt.Sprintf("unexpected status %d", resp.StatusCode)
	}

	return result
}

// probeProfileEdge validates the plugin's profile fallback behavior.
// This probe checks if the plugin is correctly caching and serving the profile
// at the merchant's origin (typically https://store.example.com/.well-known/ucp).
func (sp *SemanticProbe) probeProfileEdge(ctx context.Context, storeID string) *ProbeResult {
	result := &ProbeResult{
		ProbeName: "profile_edge_cache",
		Findings:  make([]*domain.ConsistencyFinding, 0),
	}

	// Fetch the store's storefront URL from provider (wired from the store
	// profile service).
	storefrontURL, err := sp.urlProvider.GetStorefrontURL(ctx, storeID)
	if err != nil {
		// Storefront URL unavailable (merchant not fully onboarded yet)
		result.Passed = true
		result.Error = fmt.Sprintf("storefront URL unavailable: %v", err)
		return result
	}

	if storefrontURL == "" {
		// Probe deferred pending storefront URL configuration
		result.Passed = true
		result.Error = "storefront URL not configured"
		return result
	}

	return sp.probeProfileEdgeWithURL(ctx, storeID, storefrontURL)
}

// probeProfileEdgeWithURL is a testable version that accepts storefrontURL as a parameter.
func (sp *SemanticProbe) probeProfileEdgeWithURL(ctx context.Context, storeID, storefrontURL string) *ProbeResult {
	result := &ProbeResult{
		ProbeName: "profile_edge_cache",
		Findings:  make([]*domain.ConsistencyFinding, 0),
	}

	// Check if plugin serves the profile at the merchant's origin
	start := time.Now()
	profileURL := fmt.Sprintf("%s/.well-known/ucp", storefrontURL)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, profileURL, nil)
	resp, err := sp.client.Do(req)
	latency := time.Since(start)
	result.Latency = latency

	if err != nil {
		// Plugin endpoint unreachable — this is a critical edge issue
		result.Passed = false
		result.Error = fmt.Sprintf("plugin unreachable: %v", err)
		result.Findings = append(result.Findings, &domain.ConsistencyFinding{
			SKU:        SKUPluginGateway,
			Field:      "endpoint_reachability",
			Severity:   domain.SeverityCritical,
			Sources:    map[string]string{"error": fmt.Sprintf("Not responding at %s: %v", profileURL, err)},
			DetectedAt: time.Now().UTC(),
		})
		if sp.logger != nil {
			sp.logger.Warn("profile edge probe failed", "store_id", storeID, "error", err)
		}
		return result
	}
	defer drainAndClose(resp)

	// Plugin must serve profile, never redirect
	if resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusFound ||
		resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusPermanentRedirect {
		result.Passed = false
		result.Error = fmt.Sprintf("redirect (status %d) — profile must be served as 200", resp.StatusCode)
		result.Findings = append(result.Findings, &domain.ConsistencyFinding{
			SKU:        SKUPluginGateway,
			Field:      "profile_redirect",
			Severity:   domain.SeverityWarning,
			Sources:    map[string]string{"status": fmt.Sprintf("%d", resp.StatusCode)},
			DetectedAt: time.Now().UTC(),
		})
		return result
	}

	// Check for server errors
	if resp.StatusCode >= http.StatusInternalServerError {
		result.Passed = false
		result.Error = fmt.Sprintf("server error (status %d)", resp.StatusCode)
		result.Findings = append(result.Findings, &domain.ConsistencyFinding{
			SKU:        SKUPluginGateway,
			Field:      "server_health",
			Severity:   domain.SeverityCritical,
			Sources:    map[string]string{"status": fmt.Sprintf("%d", resp.StatusCode)},
			DetectedAt: time.Now().UTC(),
		})
		return result
	}

	// Check for not found
	if resp.StatusCode == http.StatusNotFound {
		result.Passed = false
		result.Error = "profile not served by plugin (404)"
		result.Findings = append(result.Findings, &domain.ConsistencyFinding{
			SKU:        SKUPluginGateway,
			Field:      "profile_endpoint",
			Severity:   domain.SeverityCritical,
			Sources:    map[string]string{"status": "404"},
			DetectedAt: time.Now().UTC(),
		})
		return result
	}

	// If we get here, plugin is responding correctly (either 200 or acceptable status)
	if resp.StatusCode != http.StatusOK {
		result.Passed = false
		result.Error = fmt.Sprintf("unexpected status %d", resp.StatusCode)
		return result
	}

	// Verify profile structure
	var profile map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		result.Passed = false
		result.Error = fmt.Sprintf("invalid profile JSON: %v", err)
		result.Findings = append(result.Findings, &domain.ConsistencyFinding{
			SKU:        SKUPluginGateway,
			Field:      "profile_format",
			Severity:   domain.SeverityWarning,
			Sources:    map[string]string{"error": fmt.Sprintf("Invalid JSON: %v", err)},
			DetectedAt: time.Now().UTC(),
		})
		return result
	}

	// Profile must contain UCP section (passed through from Sarvex or cached)
	if _, ok := profile["ucp"]; !ok {
		result.Passed = false
		result.Error = "profile missing UCP section"
		result.Findings = append(result.Findings, &domain.ConsistencyFinding{
			SKU:        SKUPluginGateway,
			Field:      "profile_content",
			Severity:   domain.SeverityWarning,
			Sources:    map[string]string{"missing": "ucp_section"},
			DetectedAt: time.Now().UTC(),
		})
		return result
	}

	// Everything checks out
	result.Passed = true
	return result
}
