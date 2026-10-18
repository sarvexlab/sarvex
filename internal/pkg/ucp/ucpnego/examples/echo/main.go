// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Echo example: business-side UCP server using echo/v4.
//
// Run with:
//
//	cd examples/echo
//	go run .
//
// Demonstrates:
//   - Serving /.well-known/ucp outside the negotiation middleware (it
//     must not be behind negotiation itself).
//   - Applying echox.Middleware to a route group.
//   - Reading the negotiation Result inside a handler via echox.MustResult.
//   - Producing a UCP-conformant success response.
package main

import (
	"log"
	"net/http"
	"time"

	specall "github.com/sarvexlab/sarvex/internal/spec/all"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/ucpnego/httpx"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/ucpnego/httpx/echox"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/ucpnego/negotiate"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

func main() {
	own := buildOwnProfile()

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

	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Recover())
	e.Use(middleware.Logger())

	// /.well-known/ucp is NOT behind the negotiation middleware — the
	// platform needs to fetch it before it has anything to negotiate.
	e.GET("/.well-known/ucp", echox.ProfileHandler(neg))

	// UCP-bearing endpoints are gated by negotiation.
	ucp := e.Group("/ucp/v1", echox.Middleware(neg))
	ucp.POST("/checkout-sessions", createCheckout)
	ucp.GET("/checkout-sessions/:id", getCheckout)

	log.Fatal(e.Start(":8080"))
}

func createCheckout(c echo.Context) error {
	result := echox.MustResult(c)

	env := httpx.BuildSuccessEnvelope(result, negotiate.OpCheckout)
	body := map[string]interface{}{
		"ucp": env,
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
	return c.JSON(http.StatusOK, body)
}

func getCheckout(c echo.Context) error {
	result := echox.MustResult(c)
	env := httpx.BuildSuccessEnvelope(result, negotiate.OpCheckout)
	return c.JSON(http.StatusOK, map[string]interface{}{
		"ucp": env,
		"checkout": map[string]interface{}{
			"id":     c.Param("id"),
			"status": "ready_for_complete",
		},
	})
}

func buildOwnProfile() *wire.Profile {
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
			},
			PaymentHandlers: map[string][]wire.PaymentHandler{
				"com.example.processor_tokenizer": {{
					ID:      "processor_tokenizer",
					Version: version,
					Spec:    "https://example.com/specs/payments/processor_tokenizer",
					Schema:  "https://example.com/specs/payments/merchant_tokenizer.json",
					AvailableInstruments: []wire.PaymentInstrument{
						{Type: "card", Constraints: &wire.InstrumentConstraints{Brands: []string{"visa", "mastercard"}}},
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

// version is the spec generation this example speaks: the oldest one Sarvex
// knows.
var version = wire.Version(specall.Default().Baseline())

// ucpDev is the ucp.dev URL of path in version.
func ucpDev(path string) string { return "https://ucp.dev/" + string(version) + path }
