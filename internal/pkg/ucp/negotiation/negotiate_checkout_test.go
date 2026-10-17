// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package negotiation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// These tests prove the thing the multi-version negotiation gate exists
// for: two operation domains negotiating the same store against the same
// platform can land on genuinely different protocol versions when they're
// configured with different servable sets — catalog (ServableVersions) vs
// checkout (CheckoutServableVersions), per specall.Default().Servable()/
// specall.Default().Serving("dev.ucp.shopping.checkout")'s doc comments and sarvex-v1-architecture.md §D
// R3/R1. Without this, negotiation would always resolve to whatever the
// single hardcoded version was, regardless of how many generations Sarvex
// actually knows about.

func newProfileServer(t *testing.T, version string) *httptest.Server {
	t.Helper()
	body := platformProfileJSON(version, map[string][]wire.Capability{
		"dev.ucp.shopping.checkout": {{Version: wire.Version(version), Spec: "https://ucp.dev/spec/checkout"}},
		"dev.ucp.shopping.catalog":  {{Version: wire.Version(version), Spec: "https://ucp.dev/spec/catalog"}},
	}, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(body); err != nil {
			t.Errorf("write profile response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func instanceDeclaring(t *testing.T, version string) *mockInstance {
	t.Helper()
	return &mockInstance{
		id:      "store-multiversion",
		version: version,
		capabilities: map[string][]wire.Capability{
			"dev.ucp.shopping.checkout": {{Version: wire.Version(version), Spec: "https://ucp.dev/spec/checkout"}},
			"dev.ucp.shopping.catalog":  {{Version: wire.Version(version), Spec: "https://ucp.dev/spec/catalog"}},
		},
	}
}

func TestNegotiate_And_NegotiateCheckout_SelectFromDistinctServableSets(t *testing.T) {
	server := newProfileServer(t, "2026-08-25")
	inst := instanceDeclaring(t, "2026-08-25")

	svc := NewNegotiationService(
		WithSkipNamespaceValidation(true),
		WithServableVersions([]wire.Version{"2026-04-08", "2026-08-25"}),
		WithCheckoutServableVersions([]wire.Version{"2026-04-08"}),
	)

	profileURI := server.URL + "/profile.json"

	catalogResult, _, _, err := svc.Negotiate(context.Background(), inst, profileURI)
	if err != nil {
		t.Fatalf("Negotiate: unexpected error: %v", err)
	}
	if catalogResult.Version != "2026-08-25" {
		t.Fatalf("Negotiate selected %q, want the highest mutual version 2026-08-25", catalogResult.Version)
	}

	checkoutResult, _, _, err := svc.NegotiateCheckout(context.Background(), inst, profileURI)
	if err != nil {
		t.Fatalf("NegotiateCheckout: unexpected error: %v", err)
	}
	if checkoutResult.Version != "2026-04-08" {
		t.Fatalf("NegotiateCheckout selected %q, want it capped at 2026-04-08 regardless of what Negotiate picked", checkoutResult.Version)
	}

	// Re-running Negotiate must still return 2026-08-25 — proving the two
	// operations' cache entries didn't collide and clobber each other.
	catalogAgain, _, _, err := svc.Negotiate(context.Background(), inst, profileURI)
	if err != nil {
		t.Fatalf("Negotiate (second call): unexpected error: %v", err)
	}
	if catalogAgain.Version != "2026-08-25" {
		t.Fatalf("Negotiate's cached result changed to %q after NegotiateCheckout ran — cache key collision", catalogAgain.Version)
	}
}

func TestNegotiateCheckout_FallsBackToServableVersionsWhenUnset(t *testing.T) {
	server := newProfileServer(t, "2026-08-25")
	inst := instanceDeclaring(t, "2026-08-25")

	svc := NewNegotiationService(
		WithSkipNamespaceValidation(true),
		WithServableVersions([]wire.Version{"2026-04-08", "2026-08-25"}),
		// CheckoutServableVersions deliberately left unset.
	)

	result, _, _, err := svc.NegotiateCheckout(context.Background(), inst, server.URL+"/profile.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Version != "2026-08-25" {
		t.Fatalf("NegotiateCheckout without CheckoutServableVersions = %q, want it to follow ServableVersions (2026-08-25)", result.Version)
	}
}

func TestIsCheckoutRequest(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/ucp/woocommerce/conn-1/checkout-sessions", true},
		{"/ucp/woocommerce/conn-1/checkout-sessions/sess-1", true},
		{"/ucp/woocommerce/conn-1/checkout-sessions/sess-1/complete", true},
		{"/ucp/woocommerce/conn-1/catalog/search", false},
		{"/ucp/woocommerce/conn-1/catalog/sku-1", false},
		{"/.well-known/ucp", false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if got := isCheckoutRequest(req); got != tc.want {
			t.Errorf("isCheckoutRequest(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestMiddleware_CheckoutAndCatalogNegotiateIndependently drives
// WithNegotiation end-to-end (the same shape as TestMiddleware_FullFlow)
// for two requests against the same store and platform profile, one to a
// checkout path and one to a catalog path, and asserts the injected "ucp"
// field's version differs exactly as configured.
func TestMiddleware_CheckoutAndCatalogNegotiateIndependently(t *testing.T) {
	server := newProfileServer(t, "2026-08-25")
	inst := instanceDeclaring(t, "2026-08-25")

	svc := NewNegotiationService(
		WithSkipNamespaceValidation(true),
		WithServableVersions([]wire.Version{"2026-04-08", "2026-08-25"}),
		WithCheckoutServableVersions([]wire.Version{"2026-04-08"}),
	)

	e := echo.New()
	handler := func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{"ok": true})
	}
	mw := WithNegotiation(svc)(handler)

	run := func(path string) string {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("UCP-Agent", `profile="`+server.URL+`/profile.json"`)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set(StoreProfileKey, inst)

		if err := mw(c); err != nil {
			t.Fatalf("middleware error for %s: %v", path, err)
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(strings.NewReader(rec.Body.String())).Decode(&body); err != nil {
			t.Fatalf("decoding response body for %s: %v", path, err)
		}
		var result NegotiationResult
		if err := json.Unmarshal(body["ucp"], &result); err != nil {
			t.Fatalf("decoding ucp field for %s: %v", path, err)
		}
		return result.Version
	}

	if got := run("/checkout-sessions"); got != "2026-04-08" {
		t.Errorf("checkout path negotiated %q, want 2026-04-08", got)
	}
	if got := run("/catalog/search"); got != "2026-08-25" {
		t.Errorf("catalog path negotiated %q, want 2026-08-25", got)
	}
}
