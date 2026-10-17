// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package httpx_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/ucpnego/httpx"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/ucpnego/negotiate"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wire"
)

// TestEndToEnd_Success runs the full handshake: platform serves its
// profile, business fetches it, intersects, returns a response, platform
// parses it.
func TestEndToEnd_Success(t *testing.T) {
	platformProfile := buildPlatformProfile()
	businessProfile := buildBusinessProfile()

	// 1. Platform serves its own profile.
	platformSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_ = json.NewEncoder(w).Encode(platformProfile)
	}))
	defer platformSrv.Close()

	// 2. Business runs the negotiation middleware in front of the handler.
	// We bend the fetcher's HTTP client onto the test server's cert pool.
	fetcher := httpx.NewFetcher(
		httpx.WithCacheSize(8),
		httpx.WithFetchTimeout(2*time.Second),
		httpx.WithHTTPClient(&http.Client{
			Transport: platformSrv.Client().Transport,
			Timeout:   2 * time.Second,
		}),
	)
	neg, err := httpx.NewBusinessNegotiator(businessProfile, fetcher,
		httpx.WithContinueURL(func(r *http.Request) string {
			return "https://merchant.example.com/cart"
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	handler := neg.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := httpx.MustFromContext(r.Context())
		env := httpx.BuildSuccessEnvelope(result, negotiate.OpCheckout)
		_ = httpx.WriteSuccess(w, env, map[string]interface{}{
			"checkout": map[string]interface{}{"id": "chk_123"},
		})
	}))

	businessSrv := httptest.NewTLSServer(handler)
	defer businessSrv.Close()

	// 3. Platform makes a signed request.
	platformURL := platformSrv.URL + "/.well-known/ucp"
	client, err := httpx.NewPlatformClient(platformURL,
		httpx.WithPlatformHTTPClient(businessSrv.Client()),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	body := bytes.NewBufferString(`{"checkout":{"line_items":[]}}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, businessSrv.URL+"/checkout-sessions", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var env httpx.ResponseEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if env.UCP.Status == "error" {
		t.Fatalf("unexpected negotiation failure: %+v", env.Messages)
	}

	// Verify the intersection: business advertises checkout+fulfillment+discount,
	// platform advertises checkout+fulfillment. Discount is pruned (orphan, since
	// platform lacks discount entirely). Result for OpCheckout response filtering
	// should be {checkout, fulfillment}.
	caps := env.UCP.Capabilities
	if _, ok := caps["dev.ucp.shopping.checkout"]; !ok {
		t.Errorf("expected checkout in response, got %v", caps)
	}
	if _, ok := caps["dev.ucp.shopping.fulfillment"]; !ok {
		t.Errorf("expected fulfillment in response, got %v", caps)
	}
	if _, ok := caps["dev.ucp.shopping.discount"]; ok {
		t.Errorf("discount should have been pruned, got %v", caps)
	}
}

// TestEndToEnd_NegotiationFailure verifies the 200-OK-with-error response
// shape when no capabilities intersect.
func TestEndToEnd_NegotiationFailure(t *testing.T) {
	// Platform advertises NOTHING the business supports.
	platformProfile := &wire.Profile{
		UCP: wire.UCP{
			Version: "2026-04-08",
			Capabilities: map[string][]wire.Capability{
				"com.acme.unique_thing": {{
					Version: "2026-04-08",
					Spec:    "https://acme.com/spec",
					Schema:  "https://acme.com/schema.json",
				}},
			},
		},
	}
	businessProfile := buildBusinessProfile()

	platformSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(platformProfile)
	}))
	defer platformSrv.Close()

	fetcher := httpx.NewFetcher(httpx.WithHTTPClient(&http.Client{
		Transport: platformSrv.Client().Transport,
	}))
	neg, _ := httpx.NewBusinessNegotiator(businessProfile, fetcher,
		httpx.WithContinueURL(func(r *http.Request) string { return "https://merchant/cart" }),
	)

	businessSrv := httptest.NewTLSServer(neg.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be reached on negotiation failure")
	})))
	defer businessSrv.Close()

	client, _ := httpx.NewPlatformClient(platformSrv.URL+"/.well-known/ucp",
		httpx.WithPlatformHTTPClient(businessSrv.Client()),
	)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		businessSrv.URL+"/checkout-sessions", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Spec: capabilities_incompatible is 200 OK with ucp.status = "error".
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var env httpx.ResponseEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if env.UCP.Status != "error" {
		t.Errorf("ucp.status = %q, want %q", env.UCP.Status, "error")
	}
	if env.ContinueURL == "" {
		t.Error("expected continue_url on negotiation failure")
	}
	if len(env.Messages) == 0 || env.Messages[0].Code != "capabilities_incompatible" {
		t.Errorf("expected capabilities_incompatible message, got %+v", env.Messages)
	}
}

// TestEndToEnd_InvalidUCPAgent verifies the 4xx response shape when the
// platform sends a malformed UCP-Agent header.
func TestEndToEnd_InvalidUCPAgent(t *testing.T) {
	businessProfile := buildBusinessProfile()
	fetcher := httpx.NewFetcher()
	neg, _ := httpx.NewBusinessNegotiator(businessProfile, fetcher)
	businessSrv := httptest.NewServer(neg.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be reached")
	})))
	defer businessSrv.Close()

	req, _ := http.NewRequest(http.MethodPost, businessSrv.URL+"/checkout-sessions",
		bytes.NewBufferString(`{}`))
	req.Header.Set("UCP-Agent", "garbage")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}

	var body httpx.DiscoveryError
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Code != httpx.CodeInvalidProfileURL {
		t.Errorf("code = %q, want %q", body.Code, httpx.CodeInvalidProfileURL)
	}
}

// silence the unused-import warning if testServer is reorganized later
var _ = tls.VersionTLS12

// --- helpers -------------------------------------------------------------

func buildBusinessProfile() *wire.Profile {
	return &wire.Profile{
		UCP: wire.UCP{
			Version: "2026-04-08",
			Capabilities: map[string][]wire.Capability{
				"dev.ucp.shopping.checkout": {{
					Version: "2026-04-08",
					Spec:    "https://ucp.dev/spec/checkout",
					Schema:  "https://ucp.dev/schemas/checkout.json",
				}},
				"dev.ucp.shopping.fulfillment": {{
					Version: "2026-04-08",
					Spec:    "https://ucp.dev/spec/fulfillment",
					Schema:  "https://ucp.dev/schemas/fulfillment.json",
					Extends: wire.NewExtends("dev.ucp.shopping.checkout"),
				}},
				"dev.ucp.shopping.discount": {{
					Version: "2026-04-08",
					Spec:    "https://ucp.dev/spec/discount",
					Schema:  "https://ucp.dev/schemas/discount.json",
					Extends: wire.NewExtends("dev.ucp.shopping.checkout", "dev.ucp.shopping.cart"),
				}},
			},
		},
	}
}

func buildPlatformProfile() *wire.Profile {
	return &wire.Profile{
		UCP: wire.UCP{
			Version: "2026-04-08",
			Capabilities: map[string][]wire.Capability{
				"dev.ucp.shopping.checkout": {{
					Version: "2026-04-08",
					Spec:    "https://ucp.dev/spec/checkout",
					Schema:  "https://ucp.dev/schemas/checkout.json",
				}},
				"dev.ucp.shopping.fulfillment": {{
					Version: "2026-04-08",
					Spec:    "https://ucp.dev/spec/fulfillment",
					Schema:  "https://ucp.dev/schemas/fulfillment.json",
					Extends: wire.NewExtends("dev.ucp.shopping.checkout"),
				}},
				// note: NO discount — should be pruned by intersection
			},
		},
	}
}
