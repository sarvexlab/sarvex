// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wire"
)

// ProbeResult is the outcome of one synthetic check against one store.
type ProbeResult struct {
	ProbeType   ProbeType
	Outcome     Outcome
	LatencyMs   int64
	FailureCode FailureCode
	Detail      string
}

// ProbeRunner executes the synthetic checks against one store's public
// serving-plane endpoints. All requests are non-mutating GETs; no probe may
// create a checkout session, place an order, or write to a merchant's
// commerce backend.
type ProbeRunner struct {
	baseURL  string
	client   *http.Client
	identity *Identity
	cfg      Config
}

// NewProbeRunner constructs a ProbeRunner. baseURL is the serving plane's
// public base URL (same value monitoring.SemanticProbe is constructed
// with).
func NewProbeRunner(baseURL string, identity *Identity, cfg Config) *ProbeRunner {
	return &ProbeRunner{
		baseURL:  baseURL,
		client:   &http.Client{Timeout: 5 * time.Second},
		identity: identity,
		cfg:      cfg,
	}
}

// Run executes the probe battery for one store.
//
// The four store-level probes — profile, signature, negotiation, catalog —
// always run: they test the store's own endpoints and mean the same thing
// for every store. They are also exactly the checks that make a store
// `verified` (see activation.Verifier).
func (p *ProbeRunner) Run(ctx context.Context, merchantID, storeID, platform string) []*ProbeResult {
	base := ucp.StoreEndpointURL(p.baseURL, platform, storeID)

	profileResult, declaredSKU := p.probeProfileReachable(ctx, base)
	sigResult, negoResult, canarySKU := p.probeSignatureAndNegotiation(ctx, base, declaredSKU)
	catalogResult := p.probeCatalogLookup(ctx, base, canarySKU)

	return []*ProbeResult{profileResult, sigResult, negoResult, catalogResult}
}

// probeProfileReachable fetches /.well-known/ucp unsigned (restricted
// middlewares — no signature required, see api/handler.go), asserts valid
// JSON, and asserts at least one declared service endpoint matches what
// Sarvex actually serves for this store. Returns a SKU discovered from the
// profile if one happens to be embedded (none are — kept nil) so the
// signature is uniform with the other probe*() methods.
func (p *ProbeRunner) probeProfileReachable(ctx context.Context, base string) (*ProbeResult, string) {
	start := time.Now()
	url := base + "/.well-known/ucp"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set(ucp.ProbeHeader, "health")

	resp, err := p.client.Do(req)
	latency := time.Since(start)
	result := &ProbeResult{ProbeType: ProbeProfileReachable, LatencyMs: latency.Milliseconds()}

	if err != nil {
		return classifyTransportError(result, err), ""
	}
	defer drainAndClose(resp)

	if resp.StatusCode != http.StatusOK {
		result.Outcome = OutcomeFail
		result.FailureCode = FailureUnexpectedStatus
		result.Detail = fmt.Sprintf("status %d", resp.StatusCode)
		return result, ""
	}

	var profile wire.Profile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		result.Outcome = OutcomeFail
		result.FailureCode = FailureInvalidJSON
		result.Detail = err.Error()
		return result, ""
	}

	if !declaredEndpointMatches(profile, base) {
		result.Outcome = OutcomeFail
		result.FailureCode = FailureEndpointMismatch
		result.Detail = "no declared service endpoint matches the URL Sarvex serves for this store"
		return result, ""
	}

	result.Outcome = OutcomePass
	return result, ""
}

// declaredEndpointMatches reports whether any service entry across any
// capability vertical declares an endpoint matching base — the profile is
// free to declare per-vertical endpoints, so a match on any one is
// sufficient evidence the profile wasn't served stale/misconfigured.
func declaredEndpointMatches(profile wire.Profile, base string) bool {
	for _, services := range profile.UCP.Services {
		for _, svc := range services {
			if svc.Endpoint != "" && strings.HasPrefix(svc.Endpoint, base) {
				return true
			}
		}
	}
	return false
}

// probeSignatureAndNegotiation issues one signed POST carrying UCP-Agent
// (required for signature verification to resolve a key at all — see
// ucpsig.Verifier.VerifyRequest — and its presence unconditionally triggers
// full negotiation in the same pipeline pass, per
// internal/planes/serving/api/handler.go's appliedMiddlewares order:
// identity -> signature -> negotiation). Because the pipeline runs
// signature verification strictly before negotiation, the response
// unambiguously attributes failure to one stage or the other. Returns the
// discovered canary SKU (first search result) for reuse by
// probeCatalogLookup when no CanarySKU override is configured.
//
// search_catalog is POST-only per the reconciled ucp.dev REST transport
// (source/services/shopping/rest.openapi.json) — a JSON body, not
// GET+query params. Idempotency-Key is set purely because
// ucpsig.Signer.SignRequest requires one on any POST/PUT/PATCH/DELETE
// (method-based, not semantics-based); the server itself has no
// idempotency requirement on this read-only route.
func (p *ProbeRunner) probeSignatureAndNegotiation(ctx context.Context, base, overrideSKU string) (*ProbeResult, *ProbeResult, string) {
	start := time.Now()
	url := base + "/catalog/search"
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(`{"query":"","pagination":{"limit":1}}`))
	req.Header.Set(ucp.ProbeHeader, "health")
	req.Header.Set("UCP-Agent", `profile="`+p.identity.ProfileURL+`"`)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprintf("probe-%d", time.Now().UnixNano()))

	if err := p.identity.Signer.SignRequest(req); err != nil {
		fail := &ProbeResult{ProbeType: ProbeSignatureRoundtrip, Outcome: OutcomeFail, FailureCode: FailureSignatureInvalid, Detail: "local signing failed: " + err.Error()}
		skip := &ProbeResult{ProbeType: ProbeNegotiationHandshake, Outcome: OutcomeDegraded, Detail: "not reached: local signing failed"}
		return fail, skip, overrideSKU
	}

	resp, err := p.client.Do(req)
	latency := time.Since(start)

	sig := &ProbeResult{ProbeType: ProbeSignatureRoundtrip, LatencyMs: latency.Milliseconds()}
	nego := &ProbeResult{ProbeType: ProbeNegotiationHandshake, LatencyMs: latency.Milliseconds()}

	if err != nil {
		classifyTransportError(sig, err)
		nego.Outcome = OutcomeDegraded
		nego.Detail = "not reached: transport error on shared request"
		return sig, nego, overrideSKU
	}
	defer func() { drainAndClose(resp) }()

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		sig.Outcome = OutcomeFail
		sig.FailureCode = signatureFailureCode(body)
		sig.Detail = string(body)
		nego.Outcome = OutcomeDegraded
		nego.Detail = "not reached: signature verification failed first"
		return sig, nego, overrideSKU
	}

	// Signature verification passed (any status other than 401 from this
	// pipeline means the request cleared WithSignatureVerification).
	sig.Outcome = OutcomePass

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode == http.StatusFailedDependency || resp.StatusCode == http.StatusUnprocessableEntity || resp.StatusCode == http.StatusBadGateway {
		nego.Outcome = OutcomeFail
		nego.FailureCode = FailureNoCommonCapability
		nego.Detail = fmt.Sprintf("negotiation error, status %d: %s", resp.StatusCode, truncate(string(body), 300))
		return sig, nego, overrideSKU
	}

	// Products (not "data") — the reconciled ucp.dev search_response shape
	// (v20260408.CatalogSearchResponse), where Product.ID is the only
	// identifier CacheProduct has (Sarvex's serving cache has no separate
	// product/variant id — see internal/spec/vYYYYMMDD/codec/catalog.go).
	var envelope struct {
		UCP struct {
			Status       string                     `json:"status"`
			Capabilities map[string]json.RawMessage `json:"capabilities"`
		} `json:"ucp"`
		Products []struct {
			ID string `json:"id"`
		} `json:"products"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		nego.Outcome = OutcomeDegraded
		nego.Detail = "response body did not carry a decodable ucp negotiation block: " + err.Error()
		return sig, nego, overrideSKU
	}

	if envelope.UCP.Status == "error" || len(envelope.UCP.Capabilities) == 0 {
		nego.Outcome = OutcomeFail
		nego.FailureCode = FailureNoCommonCapability
		nego.Detail = "empty capability intersection"
		return sig, nego, overrideSKU
	}

	nego.Outcome = OutcomePass

	sku := overrideSKU
	if sku == "" && len(envelope.Products) > 0 {
		sku = envelope.Products[0].ID
	}
	return sig, nego, sku
}

func signatureFailureCode(body []byte) FailureCode {
	var decoded struct {
		Message struct {
			Code string `json:"code"`
		} `json:"message"`
	}
	_ = json.Unmarshal(body, &decoded)
	if decoded.Message.Code == "signature_missing" {
		return FailureSignatureMissing
	}
	return FailureSignatureInvalid
}

// probeCatalogLookup issues a signed, negotiated GET for a specific SKU and
// asserts it resolves within budget with price and availability present.
// If no SKU is known (no CanarySKU override and the store's catalog is
// currently empty), the check degrades rather than failing — an empty
// catalog isn't an endpoint-health problem.
func (p *ProbeRunner) probeCatalogLookup(ctx context.Context, base, sku string) *ProbeResult {
	result := &ProbeResult{ProbeType: ProbeCatalogLookup}
	if p.cfg.CanarySKU != "" {
		sku = p.cfg.CanarySKU
	}
	if sku == "" {
		result.Outcome = OutcomeDegraded
		result.Detail = "no canary SKU configured and catalog search returned no items"
		return result
	}

	budget := p.cfg.CatalogLookupBudget
	if budget <= 0 {
		budget = DefaultConfig().CatalogLookupBudget
	}
	reqCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	start := time.Now()
	url := fmt.Sprintf("%s/catalog/%s", base, sku)
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	req.Header.Set(ucp.ProbeHeader, "health")
	req.Header.Set("UCP-Agent", `profile="`+p.identity.ProfileURL+`"`)
	if err := p.identity.Signer.SignRequest(req); err != nil {
		result.Outcome = OutcomeFail
		result.FailureCode = FailureSignatureInvalid
		result.Detail = "local signing failed: " + err.Error()
		return result
	}

	resp, err := p.client.Do(req)
	latency := time.Since(start)
	result.LatencyMs = latency.Milliseconds()

	if err != nil {
		return classifyTransportError(result, err)
	}
	defer drainAndClose(resp)

	if latency > budget {
		result.Outcome = OutcomeTimeout
		result.FailureCode = FailureTimeout
		result.Detail = fmt.Sprintf("latency %s exceeded budget %s", latency, budget)
		return result
	}

	if resp.StatusCode != http.StatusOK {
		result.Outcome = OutcomeFail
		result.FailureCode = FailureUnexpectedStatus
		result.Detail = fmt.Sprintf("status %d", resp.StatusCode)
		return result
	}

	// Asserted against the spec's Product shape (v20260408.Product), which is
	// what this endpoint serves: price lives on price_range, and availability
	// on the variants. It used to look for flat "price"/"availability" keys —
	// Sarvex's internal CacheProduct projection — so the check could only ever
	// fail once the endpoint returned the shape it is contracted to return.
	var product struct {
		PriceRange *struct {
			Min *struct {
				Currency string `json:"currency"`
			} `json:"min"`
		} `json:"price_range"`
		Variants []struct {
			Availability *struct {
				Status string `json:"status"`
			} `json:"availability"`
		} `json:"variants"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&product); err != nil {
		result.Outcome = OutcomeFail
		result.FailureCode = FailureInvalidJSON
		result.Detail = err.Error()
		return result
	}

	hasPrice := product.PriceRange != nil && product.PriceRange.Min != nil
	hasAvailability := len(product.Variants) > 0 && product.Variants[0].Availability != nil
	if !hasPrice || !hasAvailability {
		result.Outcome = OutcomeFail
		result.FailureCode = FailureMissingPriceOrAvailability
		result.Detail = fmt.Sprintf("has_price=%v has_availability=%v", hasPrice, hasAvailability)
		return result
	}

	result.Outcome = OutcomePass
	return result
}

func classifyTransportError(result *ProbeResult, err error) *ProbeResult {
	if isTimeoutError(err) {
		result.Outcome = OutcomeTimeout
		result.FailureCode = FailureTimeout
	} else {
		result.Outcome = OutcomeFail
		result.FailureCode = FailureUnreachable
	}
	result.Detail = err.Error()
	return result
}

// isTimeoutError reports whether err (or anything it wraps) is a network
// timeout — covers both http.Client's context-deadline wrapping and the
// net.Error.Timeout() convention used by lower-level dial/read errors.
func isTimeoutError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

func drainAndClose(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
