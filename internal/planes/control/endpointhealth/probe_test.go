// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/ucpsig"
)

func testIdentity(t *testing.T) *Identity {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	jwk, err := ucpsig.JWKFromPrivateKey("test-probe-key", priv)
	require.NoError(t, err)
	signer, err := ucpsig.NewSigner(priv, jwk)
	require.NoError(t, err)
	return &Identity{
		Signer:     signer,
		ProfileURL: "https://sarvex.test/_probe/.well-known/ucp",
		Profile:    referenceProfile(jwk),
	}
}

// negotiatingMuxOpts controls a test fixture's behavior. Passed by pointer
// so a test can start the httptest.Server (to learn its URL) and then set
// fields like declaredEndpoint that depend on that URL, before issuing any
// request.
type negotiatingMuxOpts struct {
	profileInvalidJSON   bool
	declaredEndpoint     string
	negotiationFails     bool
	discoveredSKU        string
	catalogMissingFields bool
	catalogSlow          time.Duration
}

// negotiatingMux is a fixture standing in for the serving plane's real
// pipeline: it requires UCP-Agent + Signature headers on
// /catalog/search and /catalog/* (mirroring signature verification
// running before negotiation), and lets each test independently control
// profile/negotiation/catalog behavior via opts. Routes are registered
// under prefix (ucp.StoreEndpointURL's "/ucp/{platform}/{storeID}") since
// that's what every probe actually requests.
func negotiatingMux(identity *Identity, opts *negotiatingMuxOpts, prefix string) *http.ServeMux {
	mux := http.NewServeMux()

	writeUCPEnvelope := func(w http.ResponseWriter, products any) {
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{
			"ucp": map[string]any{
				"status":       "ok",
				"capabilities": identity.Profile.UCP.Capabilities,
			},
		}
		if products != nil {
			body["products"] = products
		}
		_ = json.NewEncoder(w).Encode(body)
	}

	mux.HandleFunc(prefix+"/.well-known/ucp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if opts.profileInvalidJSON {
			_, _ = w.Write([]byte("not json"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ucp": map[string]any{
				"version":  "2026-04-08",
				"services": map[string]any{"dev.ucp.shopping.checkout": []map[string]any{{"endpoint": opts.declaredEndpoint}}},
			},
			"signing_keys": []any{},
		})
	})

	mux.HandleFunc(prefix+"/catalog/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Signature") == "" || r.Header.Get("UCP-Agent") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"code": "signature_missing"}})
			return
		}
		if opts.negotiationFails {
			w.WriteHeader(http.StatusFailedDependency)
			return
		}
		writeUCPEnvelope(w, []map[string]any{{"id": opts.discoveredSKU}})
	})

	mux.HandleFunc(prefix+"/catalog/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Signature") == "" || r.Header.Get("UCP-Agent") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if opts.catalogSlow > 0 {
			time.Sleep(opts.catalogSlow)
		}
		// Unlike /catalog/search (a list envelope with a "data" array),
		// GET /catalog/:itemId returns the product object directly at the
		// top level, with "ucp" injected alongside it by negotiation
		// middleware (negotiation.injectUCPResponse) — see
		// internal/planes/serving/api/handler.go's CatalogLookup.
		w.Header().Set("Content-Type", "application/json")
		// The spec's Product shape (v20260408.Product) — price on
		// price_range, availability on the variants. See
		// internal/spec/vYYYYMMDD/codec/catalog.go.
		product := map[string]any{"id": "canary", "ucp": map[string]any{"status": "ok"}}
		if !opts.catalogMissingFields {
			product["price_range"] = map[string]any{
				"min": map[string]any{"amount": 999, "currency": "USD"},
				"max": map[string]any{"amount": 999, "currency": "USD"},
			}
			product["variants"] = []map[string]any{{
				"id":           "canary",
				"sku":          "canary",
				"availability": map[string]any{"available": true, "status": "in_stock"},
			}}
		}
		_ = json.NewEncoder(w).Encode(product)
	})

	return mux
}

// newTestServer starts a negotiatingMux-backed httptest.Server and fills
// in opts.declaredEndpoint to match what probeProfileReachable computes
// for (storeID, platform) against this exact server — the only field that
// legitimately depends on knowing the server's URL first.
func newTestServer(t *testing.T, identity *Identity, opts *negotiatingMuxOpts, storeID, platform string) *httptest.Server {
	t.Helper()
	prefix := "/ucp/" + platform + "/" + storeID
	if opts.discoveredSKU == "" {
		opts.discoveredSKU = "canary"
	}
	srv := httptest.NewServer(negotiatingMux(identity, opts, prefix))
	t.Cleanup(srv.Close)
	if opts.declaredEndpoint == "" {
		opts.declaredEndpoint = srv.URL + prefix
	}
	return srv
}

func TestProbeRunner_AllPass(t *testing.T) {
	identity := testIdentity(t)
	opts := &negotiatingMuxOpts{}
	srv := newTestServer(t, identity, opts, "store-1", "woocommerce")

	runner := NewProbeRunner(srv.URL, identity, DefaultConfig())
	results := runner.Run(context.Background(), "m-1", "store-1", "woocommerce")
	require.Len(t, results, 4)
	for _, r := range results {
		assert.Equalf(t, OutcomePass, r.Outcome, "probe %s: %s", r.ProbeType, r.Detail)
	}
}

func TestProbeRunner_ProfileReachable_EndpointMismatch(t *testing.T) {
	identity := testIdentity(t)
	opts := &negotiatingMuxOpts{declaredEndpoint: "https://someone-else.example/ucp/x/y"}
	srv := newTestServer(t, identity, opts, "store-1", "woocommerce")

	runner := NewProbeRunner(srv.URL, identity, DefaultConfig())
	results := runner.Run(context.Background(), "m-1", "store-1", "woocommerce")
	profile := findResult(results, ProbeProfileReachable)
	require.NotNil(t, profile)
	assert.Equal(t, OutcomeFail, profile.Outcome)
	assert.Equal(t, FailureEndpointMismatch, profile.FailureCode)
}

func TestProbeRunner_ProfileReachable_InvalidJSON(t *testing.T) {
	identity := testIdentity(t)
	opts := &negotiatingMuxOpts{profileInvalidJSON: true}
	srv := newTestServer(t, identity, opts, "store-1", "woocommerce")

	runner := NewProbeRunner(srv.URL, identity, DefaultConfig())
	results := runner.Run(context.Background(), "m-1", "store-1", "woocommerce")
	profile := findResult(results, ProbeProfileReachable)
	require.NotNil(t, profile)
	assert.Equal(t, OutcomeFail, profile.Outcome)
	assert.Equal(t, FailureInvalidJSON, profile.FailureCode)
}

func TestProbeRunner_Unreachable(t *testing.T) {
	identity := testIdentity(t)
	// A server that's already closed guarantees connection-refused for
	// every probe's request, exercising the "unreachable" transport-error
	// classification uniformly.
	srv := httptest.NewServer(http.NewServeMux())
	srv.Close()

	runner := NewProbeRunner(srv.URL, identity, DefaultConfig())
	results := runner.Run(context.Background(), "m-1", "store-1", "woocommerce")

	// profile_reachable and signature_roundtrip each issue their own HTTP
	// call and classify a connection failure directly as unreachable.
	assert.Equal(t, FailureUnreachable, findResult(results, ProbeProfileReachable).FailureCode)
	assert.Equal(t, FailureUnreachable, findResult(results, ProbeSignatureRoundtrip).FailureCode)

	// negotiation_handshake shares signature_roundtrip's single HTTP call
	// (see probeSignatureAndNegotiation's doc comment) — a transport error
	// on that shared call degrades negotiation ("not reached") rather than
	// separately reporting unreachable.
	nego := findResult(results, ProbeNegotiationHandshake)
	assert.Equal(t, OutcomeDegraded, nego.Outcome)

	// catalog_lookup never discovers a canary SKU when the shared
	// search call fails, so it degrades (no SKU to look up) instead of
	// making its own doomed HTTP call.
	catalog := findResult(results, ProbeCatalogLookup)
	assert.Equal(t, OutcomeDegraded, catalog.Outcome)
}

func TestProbeRunner_NegotiationHandshake_Fails(t *testing.T) {
	identity := testIdentity(t)
	opts := &negotiatingMuxOpts{negotiationFails: true}
	srv := newTestServer(t, identity, opts, "store-1", "woocommerce")

	runner := NewProbeRunner(srv.URL, identity, DefaultConfig())
	results := runner.Run(context.Background(), "m-1", "store-1", "woocommerce")

	sig := findResult(results, ProbeSignatureRoundtrip)
	nego := findResult(results, ProbeNegotiationHandshake)
	require.NotNil(t, sig)
	require.NotNil(t, nego)
	assert.Equal(t, OutcomePass, sig.Outcome, "signature verification is independent of negotiation outcome")
	assert.Equal(t, OutcomeFail, nego.Outcome)
	assert.Equal(t, FailureNoCommonCapability, nego.FailureCode)
}

func TestProbeRunner_CatalogLookup_MissingPriceOrAvailability(t *testing.T) {
	identity := testIdentity(t)
	opts := &negotiatingMuxOpts{catalogMissingFields: true}
	srv := newTestServer(t, identity, opts, "store-1", "woocommerce")

	runner := NewProbeRunner(srv.URL, identity, DefaultConfig())
	results := runner.Run(context.Background(), "m-1", "store-1", "woocommerce")
	catalog := findResult(results, ProbeCatalogLookup)
	require.NotNil(t, catalog)
	assert.Equal(t, OutcomeFail, catalog.Outcome)
	assert.Equal(t, FailureMissingPriceOrAvailability, catalog.FailureCode)
}

func TestProbeRunner_CatalogLookup_Timeout(t *testing.T) {
	identity := testIdentity(t)
	opts := &negotiatingMuxOpts{catalogSlow: 50 * time.Millisecond}
	srv := newTestServer(t, identity, opts, "store-1", "woocommerce")

	cfg := DefaultConfig()
	cfg.CatalogLookupBudget = 5 * time.Millisecond
	runner := NewProbeRunner(srv.URL, identity, cfg)
	results := runner.Run(context.Background(), "m-1", "store-1", "woocommerce")
	catalog := findResult(results, ProbeCatalogLookup)
	require.NotNil(t, catalog)
	assert.Equal(t, OutcomeTimeout, catalog.Outcome)
}

// TestProbeRunner_NeverMutates asserts no probe ever creates a checkout
// session, places an order, or otherwise writes to a merchant's commerce
// backend (R2). GET is the read-only default for every probed route;
// POST is allowed only to /catalog/search, the one read-only route the
// reconciled ucp.dev REST transport requires POST for (search_catalog
// takes a JSON body, not GET+query params — see catalog_contract.go) —
// any other POST (or PUT/PATCH/DELETE) would indicate a probe reaching a
// mutating route (checkout-sessions, carts, orders).
func TestProbeRunner_NeverMutates(t *testing.T) {
	identity := testIdentity(t)
	type call struct{ method, path string }
	var calls []call
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, call{method: r.Method, path: r.URL.Path})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ucp":          map[string]any{"version": "2026-04-08"},
			"signing_keys": []any{},
			"products":     []map[string]any{{"id": "canary"}},
			"price_range":  map[string]any{"min": map[string]any{"amount": 100, "currency": "USD"}},
			"variants": []map[string]any{{
				"id":           "canary",
				"availability": map[string]any{"available": true, "status": "in_stock"},
			}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	runner := NewProbeRunner(srv.URL, identity, DefaultConfig())
	runner.Run(context.Background(), "m-1", "store-1", "woocommerce")

	require.NotEmpty(t, calls)
	for _, c := range calls {
		if strings.HasSuffix(c.path, "/catalog/search") {
			assert.Equal(t, http.MethodPost, c.method, "catalog search is POST-only per the reconciled ucp.dev transport")
			continue
		}
		assert.Equal(t, http.MethodGet, c.method, "expected GET for read-only route %s, got %s (probes must never mutate)", c.path, c.method)
	}
}

func findResult(results []*ProbeResult, probeType ProbeType) *ProbeResult {
	for _, r := range results {
		if r.ProbeType == probeType {
			return r
		}
	}
	return nil
}
