// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package monitoring

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// testLogger is a minimal logger for testing.
var testLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeMerchantURLProvider is a test mock that returns configured URLs.
type fakeMerchantURLProvider struct {
	urls map[string]string // merchantID -> storefrontURL
	err  error             // error to return (if set, overrides all)
}

func (f *fakeMerchantURLProvider) GetStorefrontURL(ctx context.Context, merchantID string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if url, ok := f.urls[merchantID]; ok {
		return url, nil
	}
	return "", nil
}

func TestProbeProfileEdge_Unreachable(t *testing.T) {
	// Create a probe and test with an invalid URL
	probe := NewSemanticProbe("http://localhost", testLogger, &fakeMerchantURLProvider{urls: map[string]string{}})
	result := probe.probeProfileEdgeWithURL(context.Background(), "merchant123", "http://invalid.local.invalid")

	if result.Passed {
		t.Error("expected probe to fail for unreachable endpoint")
	}
	if len(result.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(result.Findings))
	}
	if result.Findings[0].Severity != domain.SeverityCritical {
		t.Errorf("expected severity %s, got %s", domain.SeverityCritical, result.Findings[0].Severity)
	}
	if result.Findings[0].Field != "endpoint_reachability" {
		t.Errorf("expected field endpoint_reachability, got %s", result.Findings[0].Field)
	}
}

func TestProbeProfileEdge_Redirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/ucp" {
			http.Redirect(w, r, "/new-location", http.StatusMovedPermanently)
		}
	}))
	defer server.Close()

	probe := NewSemanticProbe("http://localhost", testLogger, &fakeMerchantURLProvider{urls: map[string]string{}})
	result := probe.probeProfileEdgeWithURL(context.Background(), "merchant123", server.URL)

	if result.Passed {
		t.Error("expected probe to fail for redirect")
	}
	if len(result.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(result.Findings))
	}
	if result.Findings[0].Severity != domain.SeverityWarning {
		t.Errorf("expected severity %s, got %s", domain.SeverityWarning, result.Findings[0].Severity)
	}
}

func TestProbeProfileEdge_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/ucp" {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	probe := NewSemanticProbe("http://localhost", testLogger, &fakeMerchantURLProvider{urls: map[string]string{}})
	result := probe.probeProfileEdgeWithURL(context.Background(), "merchant123", server.URL)

	if result.Passed {
		t.Error("expected probe to fail for server error")
	}
	if len(result.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(result.Findings))
	}
	if result.Findings[0].Severity != domain.SeverityCritical {
		t.Errorf("expected severity %s, got %s", domain.SeverityCritical, result.Findings[0].Severity)
	}
	if result.Findings[0].Field != "server_health" {
		t.Errorf("expected field server_health, got %s", result.Findings[0].Field)
	}
}

func TestProbeProfileEdge_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/ucp" {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	probe := NewSemanticProbe("http://localhost", testLogger, &fakeMerchantURLProvider{urls: map[string]string{}})
	result := probe.probeProfileEdgeWithURL(context.Background(), "merchant123", server.URL)

	if result.Passed {
		t.Error("expected probe to fail for 404")
	}
	if len(result.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(result.Findings))
	}
	if result.Findings[0].Severity != domain.SeverityCritical {
		t.Errorf("expected severity %s, got %s", domain.SeverityCritical, result.Findings[0].Severity)
	}
	if result.Findings[0].Field != "profile_endpoint" {
		t.Errorf("expected field profile_endpoint, got %s", result.Findings[0].Field)
	}
}

func TestProbeProfileEdge_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/ucp" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("{ invalid json"))
		}
	}))
	defer server.Close()

	probe := NewSemanticProbe("http://localhost", testLogger, &fakeMerchantURLProvider{urls: map[string]string{}})
	result := probe.probeProfileEdgeWithURL(context.Background(), "merchant123", server.URL)

	if result.Passed {
		t.Error("expected probe to fail for invalid JSON")
	}
	if len(result.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(result.Findings))
	}
	if result.Findings[0].Severity != domain.SeverityWarning {
		t.Errorf("expected severity %s, got %s", domain.SeverityWarning, result.Findings[0].Severity)
	}
	if result.Findings[0].Field != "profile_format" {
		t.Errorf("expected field profile_format, got %s", result.Findings[0].Field)
	}
}

func TestProbeProfileEdge_MissingUCPSection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/ucp" {
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			io.Copy(w, bytes.NewReader([]byte(`{"other": "data"}`)))
		}
	}))
	defer server.Close()

	probe := NewSemanticProbe("http://localhost", testLogger, &fakeMerchantURLProvider{urls: map[string]string{}})
	result := probe.probeProfileEdgeWithURL(context.Background(), "merchant123", server.URL)

	if result.Passed {
		t.Error("expected probe to fail for missing UCP section")
	}
	if len(result.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(result.Findings))
	}
	if result.Findings[0].Severity != domain.SeverityWarning {
		t.Errorf("expected severity %s, got %s", domain.SeverityWarning, result.Findings[0].Severity)
	}
	if result.Findings[0].Field != "profile_content" {
		t.Errorf("expected field profile_content, got %s", result.Findings[0].Field)
	}
}

func TestProbeProfileEdge_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/ucp" {
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			io.Copy(w, bytes.NewReader([]byte(`{"ucp": {"version": "1.0"}}`)))
		}
	}))
	defer server.Close()

	probe := NewSemanticProbe("http://localhost", testLogger, &fakeMerchantURLProvider{urls: map[string]string{}})
	result := probe.probeProfileEdgeWithURL(context.Background(), "merchant123", server.URL)

	if !result.Passed {
		t.Error("expected probe to pass for valid profile")
	}
	if len(result.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(result.Findings))
	}
}

func TestProbeProfileEdge_StorefrontURLNotConfigured(t *testing.T) {
	// When provider returns empty string, probe should defer gracefully
	provider := &fakeMerchantURLProvider{urls: map[string]string{}}
	probe := NewSemanticProbe("http://localhost", testLogger, provider)
	result := probe.probeProfileEdge(context.Background(), "merchant123")

	// Should pass but be deferred
	if !result.Passed {
		t.Error("expected probe to pass when storefront URL not configured")
	}
	if result.Error != "storefront URL not configured" {
		t.Errorf("expected error 'storefront URL not configured', got %s", result.Error)
	}
}

func TestProbeProfileEdge_StorefrontURLProviderError(t *testing.T) {
	// When provider returns error, probe should defer gracefully
	provider := &fakeMerchantURLProvider{err: fmt.Errorf("merchant not found")}
	probe := NewSemanticProbe("http://localhost", testLogger, provider)
	result := probe.probeProfileEdge(context.Background(), "merchant123")

	// Should pass but be deferred
	if !result.Passed {
		t.Error("expected probe to pass when provider returns error")
	}
	if result.Error == "" {
		t.Error("expected error message when provider fails")
	}
}

func TestProbeProfileEdge_WithValidStorefrontURL(t *testing.T) {
	// When storefront URL is provided, probe should check the actual endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/ucp" {
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			io.Copy(w, bytes.NewReader([]byte(`{"ucp": {"version": "1.0"}}`)))
		}
	}))
	defer server.Close()

	provider := &fakeMerchantURLProvider{urls: map[string]string{
		"merchant123": server.URL,
	}}
	probe := NewSemanticProbe("http://localhost", testLogger, provider)
	result := probe.probeProfileEdge(context.Background(), "merchant123")

	if !result.Passed {
		t.Error("expected probe to pass for valid storefront URL with correct profile")
	}
	if len(result.Findings) != 0 {
		t.Errorf("expected no findings for valid profile, got %d", len(result.Findings))
	}
}

// TestSemanticProbe_RunProbes_RunsConcurrently guards against RunProbes
// regressing to sequential execution: each of its 5 handlers sleeps for
// perRequestDelay, so a sequential run would take at least 5x that; a
// concurrent run should take roughly 1x plus scheduling slack.
func TestSemanticProbe_RunProbes_RunsConcurrently(t *testing.T) {
	const perRequestDelay = 50 * time.Millisecond

	mux := http.NewServeMux()
	delayThenRespond := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(perRequestDelay)
			if body != "" {
				w.Header().Set("Content-Type", "application/json")
			}
			w.WriteHeader(status)
			if body != "" {
				io.Copy(w, bytes.NewReader([]byte(body)))
			}
		}
	}
	mux.HandleFunc("/ucp/woocommerce/store1/.well-known/ucp", delayThenRespond(http.StatusOK, `{"ucp":{},"signing_keys":[{"kid":"k1","kty":"EC","crv":"P-256","x":"x","y":"y"}]}`))
	mux.HandleFunc("/ucp/woocommerce/store1/catalog/search", delayThenRespond(http.StatusOK, ""))
	mux.HandleFunc("/ucp/woocommerce/store1/catalog/test-sku", delayThenRespond(http.StatusOK, ""))
	mux.HandleFunc("/ucp/woocommerce/store1/checkout-sessions", delayThenRespond(http.StatusCreated, ""))
	mux.HandleFunc("/.well-known/ucp", delayThenRespond(http.StatusOK, `{"ucp":{}}`))
	server := httptest.NewServer(mux)
	defer server.Close()

	provider := &fakeMerchantURLProvider{urls: map[string]string{"store1": server.URL}}
	probe := NewSemanticProbe(server.URL, testLogger, provider)

	start := time.Now()
	results, err := probe.RunProbes(context.Background(), "store1", "woocommerce")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("RunProbes returned error: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}
	if elapsed >= 3*perRequestDelay {
		t.Errorf("RunProbes took %v for 5 probes each delayed %v — expected well under %v if they ran concurrently", elapsed, perRequestDelay, 3*perRequestDelay)
	}

	wantNames := map[string]bool{
		"profile_endpoint":   false,
		"catalog_search":     false,
		"catalog_lookup":     false,
		"checkout_create":    false,
		"profile_edge_cache": false,
	}
	for _, r := range results {
		if _, ok := wantNames[r.ProbeName]; !ok {
			t.Errorf("unexpected probe name %q", r.ProbeName)
			continue
		}
		wantNames[r.ProbeName] = true
		if !r.Passed {
			t.Errorf("probe %q expected to pass, got error %q", r.ProbeName, r.Error)
		}
	}
	for name, seen := range wantNames {
		if !seen {
			t.Errorf("expected probe %q to run, but it didn't", name)
		}
	}
}
