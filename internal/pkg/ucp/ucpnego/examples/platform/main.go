// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Platform-side example.
//
// Run with: go run ./examples/platform
//
// Demonstrates the platform side:
//
//  1. Serves its own profile (so the business can fetch it during
//     negotiation). In production this would be served from your CDN.
//  2. Uses PlatformClient to attach UCP-Agent to every outbound request.
//  3. Parses the negotiated capability set out of the response.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/types"
	specall "github.com/sarvexlab/sarvex/internal/spec/all"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/ucpnego/httpx"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

func main() {
	var (
		serve      = flag.Bool("serve", false, "serve the platform profile (for the business to fetch)")
		profileURL = flag.String("profile-url", "https://platform.example/.well-known/ucp", "URL where this platform's profile is reachable")
		target     = flag.String("target", "http://localhost:8080/checkout-sessions", "business endpoint to call")
	)
	flag.Parse()

	own := buildPlatformProfile()

	if *serve {
		go servePlatformProfile(own)
	}

	client, err := httpx.NewPlatformClient(*profileURL,
		httpx.WithOwnProfile(own),
		httpx.WithPlatformHTTPClient(&http.Client{Timeout: 5 * time.Second}),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	body := bytes.NewBufferString(`{"checkout":{"line_items":[{"id":"prod_1","quantity":2}]}}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, *target, body)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("dispatch: %v", err)
	}
	defer resp.Body.Close()

	fmt.Printf("HTTP %d\n", resp.StatusCode)

	var env httpx.ResponseEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		log.Fatalf("decode response: %v", err)
	}

	if env.UCP.Status == "error" {
		fmt.Println("negotiation failed:")
		for _, m := range env.Messages {
			fmt.Printf("  - [%s] %s: %s\n", m.Type, m.Code, m.Content)
		}
		if env.ContinueURL != "" {
			fmt.Printf("  fallback: %s\n", env.ContinueURL)
		}
		os.Exit(1)
	}

	fmt.Printf("negotiated protocol version: %s\n", env.UCP.Version)
	fmt.Println("active capabilities:")
	for name, entries := range env.UCP.Capabilities {
		for _, e := range entries {
			fmt.Printf("  - %s @ %s\n", name, e.Version)
		}
	}
	if len(env.UCP.PaymentHandlers) > 0 {
		fmt.Println("active payment handlers:")
		for name := range env.UCP.PaymentHandlers {
			fmt.Printf("  - %s\n", name)
		}
	}
}

func servePlatformProfile(own *wire.Profile) {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/ucp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		body, err := profileDocument(own)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(body)
	})
	log.Printf("platform profile served on :9090")
	log.Fatal(http.ListenAndServe(":9090", mux))
}

func buildPlatformProfile() *wire.Profile {
	return &wire.Profile{
		UCP: wire.UCP{
			Version: version,
			Services: map[string][]wire.Service{
				"dev.ucp.shopping": {{
					Version:   version,
					Spec:      ucpDev("/specification/overview"),
					Transport: wire.TransportREST,
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
				// note: platform does NOT advertise discount — it will
				// be pruned from the intersection.
			},
			PaymentHandlers: map[string][]wire.PaymentHandler{
				"com.example.processor_tokenizer": {{
					ID:      "processor_tokenizer_v1",
					Version: version,
					Spec:    "https://example.com/specs/payments/processor_tokenizer",
					Schema:  "https://example.com/schemas/payments/delegate-payment.json",
				}},
			},
		},
		Keys: []wire.JWK{{
			Kid: "platform_2026", Kty: "EC", Crv: "P-256",
			X:   "MKBCTNIcKUSDii11ySs3526iDZ8AiTo7Tu6KPAqv7D4",
			Y:   "4Etl6SRW2YiLUrN5vfvVHuhp7x8PxltmWWlbbM4IFyM",
			Use: "sig", Alg: "ES256",
		}},
	}
}

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
