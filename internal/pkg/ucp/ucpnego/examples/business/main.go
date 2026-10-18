// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Business-side example.
//
// Run with: go run ./examples/business
//
// This wires up the BusinessNegotiator as middleware in front of a
// checkout handler. It does three useful things:
//
//  1. Serves its own profile at /.well-known/ucp (required by the spec
//     for businesses).
//  2. Negotiates capabilities on every UCP-bearing request before
//     running the handler.
//  3. Inside the handler, reads the negotiation Result from context and
//     builds a response containing the filtered capability set.
package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/types"
	specall "github.com/sarvexlab/sarvex/internal/spec/all"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/ucpnego/httpx"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/ucpnego/negotiate"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

func main() {
	own := buildBusinessProfile()

	fetcher := httpx.NewFetcher(
		httpx.WithCacheSize(512),
		httpx.WithFetchTimeout(3*time.Second),
	)

	neg, err := httpx.NewBusinessNegotiator(own, fetcher,
		httpx.WithContinueURL(func(r *http.Request) string {
			return "https://merchant.example.com" + r.URL.Path
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()

	// /.well-known/ucp — required hosting per the spec
	mux.HandleFunc("/.well-known/ucp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Cache-Control with public + max-age >= 60s, no private/no-store
		w.Header().Set("Cache-Control", "public, max-age=300")
		body, err := profileDocument(own)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(body)
	})

	// Application endpoint, behind the negotiation middleware.
	mux.Handle("/checkout-sessions",
		neg.Middleware(http.HandlerFunc(handleCreateCheckout)))

	addr := ":8080"
	log.Printf("business listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func handleCreateCheckout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// The middleware guarantees this is present.
	result := httpx.MustFromContext(r.Context())

	// Real handlers do work here: build cart, calculate totals, etc.
	// We skip ahead to the response.
	envelope := httpx.BuildSuccessEnvelope(result, negotiate.OpCheckout)

	payload := map[string]interface{}{
		"checkout": map[string]interface{}{
			"id":     "chk_demo_123",
			"status": "ready_for_complete",
			"totals": map[string]interface{}{
				"subtotal": 12500,
				"tax":      1250,
				"total":    13750,
			},
		},
	}

	if err := httpx.WriteSuccess(w, envelope, payload); err != nil {
		log.Printf("write response: %v", err)
	}
}

// buildBusinessProfile constructs the business's own UCP profile.
// In production this would be loaded from config / generated at deploy.
func buildBusinessProfile() *wire.Profile {
	return &wire.Profile{
		UCP: wire.UCP{
			Version: version,
			Services: map[string][]wire.Service{
				"dev.ucp.shopping": {{
					Version:   version,
					Spec:      ucpDev("/specification/overview"),
					Transport: wire.TransportREST,
					Endpoint:  "https://merchant.example.com/ucp/v1",
					Schema:    ucpDev("/services/shopping/rest.openapi.json"),
				}},
			},
			Capabilities: map[string][]wire.Capability{
				"dev.ucp.shopping.checkout": {{
					Version: version,
					Spec:    ucpDev("/specification/checkout"),
					Schema:  ucpDev("/schemas/shopping/checkout.json"),
				}},
				"dev.ucp.shopping.fulfillment": {{
					Version: version,
					Spec:    ucpDev("/specification/fulfillment"),
					Schema:  ucpDev("/schemas/shopping/fulfillment.json"),
					Extends: wire.NewExtends("dev.ucp.shopping.checkout"),
				}},
				"dev.ucp.shopping.discount": {{
					Version: version,
					Spec:    ucpDev("/specification/discount"),
					Schema:  ucpDev("/schemas/shopping/discount.json"),
					Extends: wire.NewExtends("dev.ucp.shopping.checkout", "dev.ucp.shopping.cart"),
				}},
			},
			PaymentHandlers: map[string][]wire.PaymentHandler{
				"com.example.processor_tokenizer": {{
					ID:      "processor_tokenizer",
					Version: version,
					Spec:    "https://example.com/specs/payments/processor_tokenizer",
					Schema:  "https://example.com/specs/payments/merchant_tokenizer.json",
					AvailableInstruments: []wire.PaymentInstrument{
						{Type: "card", Constraints: &wire.InstrumentConstraints{Brands: []string{"visa", "mastercard", "amex"}}},
					},
				}},
			},
		},
		Keys: []wire.JWK{{
			Kid: "business_2026", Kty: "EC", Crv: "P-256",
			X:   "WbbXwVYGdJoP4Xm3qCkGvBRcRvKtEfXDbWvPzpPS8LA",
			Y:   "sP4jHHxYqC89HBo8TjrtVOAGHfJDflYxw7MFMxuFMPY",
			Use: "sig", Alg: "ES256",
		}},
	}
}

// unused import guard for build determinism (fmt is sometimes elided)
var _ = fmt.Sprintf

// version is the spec generation this example speaks: the oldest one Sarvex
// knows.
var version = wire.Version(specall.Default().Baseline())

// ucpDev is the ucp.dev URL of path in version.
func ucpDev(path string) string { return "https://ucp.dev/" + string(version) + path }

// profileDocument writes p in the form of the generation it declares.
func profileDocument(p *wire.Profile) ([]byte, error) {
	gen, ok := specall.Default().Generations().Get(types.Version(p.UCP.Version))
	if !ok {
		return nil, fmt.Errorf("no generation registered for %s", p.UCP.Version)
	}
	return gen.EncodeProfile(p)
}
