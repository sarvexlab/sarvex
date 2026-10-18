// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// TestHTTPTestHarness_ScopesRequestsToStore is the regression test for the
// bug where every check hit a single flat baseURL regardless of which store
// was being validated. Two different (storeID, platform) pairs against the
// same server must hit two different, correctly-scoped paths.
func TestHTTPTestHarness_ScopesRequestsToStore(t *testing.T) {
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		switch {
		case r.URL.Path == "/ucp/woocommerce/store-a/.well-known/ucp":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ucp":{},"signing_keys":[{"kid":"k1","kty":"EC","crv":"P-256","x":"x","y":"y"}]}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	harness := NewHTTPTestHarness(srv.URL, testLogger())
	_, err := harness.RunTests(context.Background(), domain.StoreID("store-a"), domain.Platform("woocommerce"))
	if err != nil {
		t.Fatalf("RunTests failed: %v", err)
	}

	wantPrefix := "/ucp/woocommerce/store-a/"
	for _, p := range gotPaths {
		if len(p) < len(wantPrefix) || p[:len(wantPrefix)] != wantPrefix {
			t.Errorf("request path %q not scoped under store prefix %q — harness is not addressing the requested store", p, wantPrefix)
		}
	}
	if len(gotPaths) == 0 {
		t.Fatal("expected the harness to make requests")
	}
}

// TestHTTPTestHarness_DifferentStoresHitDifferentPaths guards against the
// harness silently ignoring storeID/platform and always hitting the same
// fixed origin no matter which store is being validated.
func TestHTTPTestHarness_DifferentStoresHitDifferentPaths(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path // last request wins — checkout-sessions is always called last
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	harness := NewHTTPTestHarness(srv.URL, testLogger())

	if _, err := harness.RunTests(context.Background(), domain.StoreID("store-a"), domain.Platform("woocommerce")); err != nil {
		t.Fatalf("RunTests(store-a) failed: %v", err)
	}
	pathA := gotPath

	if _, err := harness.RunTests(context.Background(), domain.StoreID("store-b"), domain.Platform("magento")); err != nil {
		t.Fatalf("RunTests(store-b) failed: %v", err)
	}
	pathB := gotPath

	if pathA == "" || pathB == "" {
		t.Fatalf("expected non-empty request paths, got pathA=%q pathB=%q", pathA, pathB)
	}
	if pathA == pathB {
		t.Errorf("expected store-a and store-b runs to hit different paths, both hit %q", pathA)
	}
}
