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
	"time"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// --- Test fixtures ---

type mockInstance struct {
	id              string
	version         string
	capabilities    map[string][]wire.Capability
	paymentHandlers map[string][]wire.PaymentHandler
	signingKeys     []wire.JWK
	config          ucp.Configuration
}

func (m *mockInstance) StoreID() string                            { return m.id }
func (m *mockInstance) Version() string                            { return m.version }
func (m *mockInstance) Services() map[string][]wire.Service        { return nil }
func (m *mockInstance) Capabilities() map[string][]wire.Capability { return m.capabilities }
func (m *mockInstance) PaymentHandlers() map[string][]wire.PaymentHandler {
	return m.paymentHandlers
}
func (m *mockInstance) SigningKeys() []wire.JWK          { return m.signingKeys }
func (m *mockInstance) StorefrontURL() string            { return "" }
func (m *mockInstance) Configuration() ucp.Configuration { return m.config }

func defaultInstance() *mockInstance {
	return &mockInstance{
		id:      "inst-001",
		version: "2026-01-23",
		capabilities: map[string][]wire.Capability{
			"dev.ucp.shopping.checkout": {
				{Version: "2026-01-23", Spec: "https://ucp.dev/spec/checkout"},
			},
		},
		paymentHandlers: map[string][]wire.PaymentHandler{
			"com.example.processor": {
				{ID: "processor", Version: "2026-01-23"},
			},
		},
	}
}

func platformProfileJSON(version string, caps map[string][]wire.Capability, handlers map[string][]wire.PaymentHandler) []byte {
	p := wire.Profile{
		UCP: wire.UCP{
			Version:         wire.Version(version),
			Capabilities:    caps,
			PaymentHandlers: handlers,
		},
	}
	b, _ := json.Marshal(p)
	return b
}

// --- ParsePlatformProfileURI ---

func TestParseUCPAgentHeader_Valid(t *testing.T) {
	svc := NewNegotiationService()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("UCP-Agent", `profile="https://agent.example/profiles/agent.json"`)

	uri, err := svc.ParsePlatformProfileURI(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uri != "https://agent.example/profiles/agent.json" {
		t.Fatalf("got %q, want %q", uri, "https://agent.example/profiles/agent.json")
	}
}

func TestParseUCPAgentHeader_Missing(t *testing.T) {
	svc := NewNegotiationService()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	uri, err := svc.ParsePlatformProfileURI(req)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if uri != "" {
		t.Fatalf("expected empty URI, got %q", uri)
	}
}

func TestParseUCPAgentHeader_Malformed(t *testing.T) {
	svc := NewNegotiationService()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("UCP-Agent", `invalid-no-profile-key`)

	_, err := svc.ParsePlatformProfileURI(req)
	if err == nil {
		t.Fatal("expected error for malformed header")
	}
	if _, ok := err.(*ucp.ParseError); !ok {
		t.Fatalf("expected *ParseError, got %T", err)
	}
}

// --- ValidateVersion ---

func TestValidateVersion_Equal(t *testing.T) {
	svc := NewNegotiationService()
	inst := &mockInstance{version: "2026-01-23"}
	profile := &wire.Profile{UCP: wire.UCP{Version: "2026-01-23"}}

	if err := svc.ValidateVersion(inst, profile); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateVersion_PlatformOlder(t *testing.T) {
	svc := NewNegotiationService()
	inst := &mockInstance{version: "2026-01-23"}
	profile := &wire.Profile{UCP: wire.UCP{Version: "2025-06-01"}}

	if err := svc.ValidateVersion(inst, profile); err != nil {
		t.Fatalf("expected nil for older platform version, got: %v", err)
	}
}

func TestValidateVersion_PlatformNewer(t *testing.T) {
	svc := NewNegotiationService()
	inst := &mockInstance{version: "2025-01-01"}
	profile := &wire.Profile{UCP: wire.UCP{Version: "2026-01-23"}}

	err := svc.ValidateVersion(inst, profile)
	if err == nil {
		t.Fatal("expected version_unsupported error")
	}
	var vErr *ucp.VersionUnsupportedError
	if !isVersionUnsupported(err) {
		t.Fatalf("expected *VersionUnsupportedError, got %T: %v", err, err)
	}
	_ = vErr
}

// --- IntersectCapabilities ---

func TestIntersectCapabilities_Basic(t *testing.T) {
	business := map[string][]wire.Capability{
		"dev.ucp.shopping.checkout": {{Version: "2026-01-23"}},
		"dev.ucp.shopping.order":    {{Version: "2026-01-23"}},
	}
	platform := map[string][]wire.Capability{
		"dev.ucp.shopping.checkout": {{Version: "2026-01-23"}},
	}

	result := IntersectCapabilities(business, platform)
	if len(result) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(result))
	}
	if _, ok := result["dev.ucp.shopping.checkout"]; !ok {
		t.Fatal("expected dev.ucp.shopping.checkout in result")
	}
}

func TestIntersectCapabilities_EmptyPlatform(t *testing.T) {
	business := map[string][]wire.Capability{
		"dev.ucp.shopping.checkout": {{Version: "2026-01-23"}},
	}

	result := IntersectCapabilities(business, map[string][]wire.Capability{})
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d", len(result))
	}
}

func TestIntersectCapabilities_NoMutualVersion(t *testing.T) {
	// Business supports v2026-01-23; platform only supports v2025-01-01.
	// No mutual version → capability excluded (Step 2).
	business := map[string][]wire.Capability{
		"dev.ucp.shopping.checkout": {{Version: "2026-01-23"}},
	}
	platform := map[string][]wire.Capability{
		"dev.ucp.shopping.checkout": {{Version: "2025-01-01"}},
	}

	result := IntersectCapabilities(business, platform)
	if len(result) != 0 {
		t.Fatalf("expected empty result when no mutual version, got %d capabilities", len(result))
	}
}

func TestIntersectCapabilities_SelectsHighestMutualVersion(t *testing.T) {
	// Business has two versions; platform supports both; highest should be selected.
	business := map[string][]wire.Capability{
		"dev.ucp.shopping.checkout": {
			{Version: "2025-06-01"},
			{Version: "2026-01-23"},
		},
	}
	platform := map[string][]wire.Capability{
		"dev.ucp.shopping.checkout": {
			{Version: "2025-06-01"},
			{Version: "2026-01-23"},
		},
	}

	result := IntersectCapabilities(business, platform)
	if len(result) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(result))
	}
	caps := result["dev.ucp.shopping.checkout"]
	if len(caps) != 1 || caps[0].Version != "2026-01-23" {
		t.Fatalf("expected highest version 2026-01-23, got %v", caps)
	}
}

func TestIntersectCapabilities_PruneOrphanedExtension(t *testing.T) {
	// "dev.ucp.shopping.checkout.express" extends "dev.ucp.shopping.checkout"
	// but "dev.ucp.shopping.checkout" is NOT in the platform → checkout.express must be pruned.
	business := map[string][]wire.Capability{
		"dev.ucp.shopping.checkout.express": {
			{Version: "2026-01-23", Extends: wire.NewExtends("dev.ucp.shopping.checkout")},
		},
	}
	platform := map[string][]wire.Capability{
		"dev.ucp.shopping.checkout.express": {{Version: "2026-01-23"}},
	}

	result := IntersectCapabilities(business, platform)
	if len(result) != 0 {
		t.Fatalf("expected orphaned extension to be pruned, got %d capabilities", len(result))
	}
}

func TestIntersectCapabilities_TransitiveChain(t *testing.T) {
	// A extends B, B extends C. Platform only supports A and B, not C.
	// B is pruned (C absent) → A also pruned (B absent after pruning).
	business := map[string][]wire.Capability{
		"cap.a": {{Version: "1", Extends: wire.NewExtends("cap.b")}},
		"cap.b": {{Version: "1", Extends: wire.NewExtends("cap.c")}},
		"cap.c": {{Version: "1"}},
	}
	platform := map[string][]wire.Capability{
		"cap.a": {{Version: "1"}},
		"cap.b": {{Version: "1"}},
		// cap.c absent
	}

	result := IntersectCapabilities(business, platform)
	if len(result) != 0 {
		t.Fatalf("expected all transitive extensions pruned, got %v", result)
	}
}

// --- IntersectPaymentHandlers ---

func TestIntersectPaymentHandlers_Basic(t *testing.T) {
	business := map[string][]wire.PaymentHandler{
		"com.example.processor": {{ID: "processor", Version: "2026-01-23"}},
		"com.example.wallet":    {{ID: "wallet", Version: "2026-01-23"}},
	}
	platform := map[string][]wire.PaymentHandler{
		"com.example.processor": {{ID: "processor", Version: "2026-01-23"}},
	}

	result := IntersectPaymentHandlers(business, platform)
	if len(result) != 1 {
		t.Fatalf("expected 1 handler, got %d", len(result))
	}
	if _, ok := result["com.example.processor"]; !ok {
		t.Fatal("expected com.example.processor in result")
	}
}

// --- ValidateNamespaceBinding ---

func TestNamespaceValidation_Valid(t *testing.T) {
	svc := NewNegotiationService()
	profile := &wire.Profile{
		UCP: wire.UCP{
			Capabilities: map[string][]wire.Capability{
				"dev.ucp.shopping.checkout": {
					{Spec: "https://ucp.dev/spec/checkout"},
				},
			},
		},
	}

	if err := svc.ValidateNamespaceBinding(profile); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNamespaceValidation_Mismatch(t *testing.T) {
	svc := NewNegotiationService()
	profile := &wire.Profile{
		UCP: wire.UCP{
			Capabilities: map[string][]wire.Capability{
				"dev.ucp.shopping.checkout": {
					// spec comes from wrong origin
					{Spec: "https://evil.example/spec/checkout"},
				},
			},
		},
	}

	err := svc.ValidateNamespaceBinding(profile)
	if err == nil {
		t.Fatal("expected namespace validation error")
	}
	var nsErr *ucp.NamespaceValidationError
	if !errorAs(err, &nsErr) {
		t.Fatalf("expected *NamespaceValidationError, got %T", err)
	}
	if len(nsErr.Mismatches) == 0 {
		t.Fatal("expected at least one mismatch")
	}
}

// errorAs is a local helper since we can't import errors in test.
func errorAs(err error, target any) bool {
	type asInterface interface{ As(any) bool }
	if a, ok := err.(asInterface); ok {
		return a.As(target)
	}
	// Simple type match fallback.
	switch t := target.(type) {
	case **ucp.NamespaceValidationError:
		if e, ok := err.(*ucp.NamespaceValidationError); ok {
			*t = e
			return true
		}
	case **ucp.VersionUnsupportedError:
		if e, ok := err.(*ucp.VersionUnsupportedError); ok {
			*t = e
			return true
		}
	}
	return false
}

// --- CacheKey ---

func TestCacheKey_Composite(t *testing.T) {
	key := CacheKey("merch-1", "inst-001", "https://platform.example.com/profile")
	key2 := CacheKey("merch-1", "inst-001", "https://platform.example.com/profile")
	if key != key2 {
		t.Fatalf("same inputs should produce same key: %q != %q", key, key2)
	}

	// Different inputs should produce different keys
	key3 := CacheKey("merch-1", "inst-002", "https://platform.example.com/profile")
	if key == key3 {
		t.Fatalf("different instances should produce different keys")
	}

	key4 := CacheKey("merch-1", "inst-001", "https://different.example.com/profile")
	if key == key4 {
		t.Fatalf("different profile URIs should produce different keys")
	}

	key5 := CacheKey("merch-2", "inst-001", "https://platform.example.com/profile")
	if key == key5 {
		t.Fatalf("different merchants should produce different keys")
	}
}

// --- NegotiationCache ---

func TestNegotiationCache_Hit(t *testing.T) {
	cache := NewMemoryCache()
	result := &NegotiationResult{Version: "2026-01-23"}
	cache.SetNegotiation("key1", result, time.Minute)

	got, ok := cache.GetNegotiation("key1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Version != "2026-01-23" {
		t.Fatalf("got version %q", got.Version)
	}
}

func TestNegotiationCache_MissOnDifferentInstance(t *testing.T) {
	cache := NewMemoryCache()
	uri := "https://platform.example.com/profile"

	cache.SetNegotiation(CacheKey("merch-A", "inst-A", uri), &NegotiationResult{Version: "2026-01-23"}, time.Minute)

	_, ok := cache.GetNegotiation(CacheKey("merch-A", "inst-B", uri))
	if ok {
		t.Fatal("expected cache miss for different instance")
	}
}

func TestNegotiationCache_HitSameInstanceAndProfile(t *testing.T) {
	cache := NewMemoryCache()
	merchantID := "merch-001"
	instanceID := "inst-001"
	uri := "https://platform.example.com/profile"

	cache.SetNegotiation(CacheKey(merchantID, instanceID, uri), &NegotiationResult{Version: "2026-01-23"}, time.Minute)

	_, ok := cache.GetNegotiation(CacheKey(merchantID, instanceID, uri))
	if !ok {
		t.Fatal("expected cache hit for same instance + profile URI")
	}
}

func TestNegotiationCache_MissOnDifferentProfileURI(t *testing.T) {
	cache := NewMemoryCache()
	merchantID := "merch-001"
	instanceID := "inst-001"

	cache.SetNegotiation(CacheKey(merchantID, instanceID, "https://agent-v1.example.com/profile"), &NegotiationResult{Version: "2026-01-23"}, time.Minute)

	_, ok := cache.GetNegotiation(CacheKey(merchantID, instanceID, "https://agent-v2.example.com/profile"))
	if ok {
		t.Fatal("expected cache miss for same instance but different platform profile URI")
	}
}

func TestNegotiationCache_TTLExpiry(t *testing.T) {
	cache := NewMemoryCache()
	cache.SetNegotiation("key1", &NegotiationResult{Version: "2026-01-23"}, time.Millisecond)

	time.Sleep(5 * time.Millisecond)

	_, ok := cache.GetNegotiation("key1")
	if ok {
		t.Fatal("expected cache miss after TTL expiry")
	}
}

// --- Negotiate (full flow) ---

func TestNegotiate_FullFlow(t *testing.T) {
	inst := defaultInstance()
	profileBody := platformProfileJSON(
		"2026-01-23",
		map[string][]wire.Capability{
			"dev.ucp.shopping.checkout": {{Version: "2026-01-23", Spec: "https://ucp.dev/spec/checkout"}},
		},
		map[string][]wire.PaymentHandler{
			"com.example.processor": {{ID: "processor", Version: "2026-01-23"}},
		},
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "max-age=300")
		w.Write(profileBody)
	}))
	defer server.Close()

	svc := NewNegotiationService(WithSkipNamespaceValidation(true))
	result, _, _, err := svc.Negotiate(context.Background(), inst, server.URL+"/profile.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Version != "2026-01-23" {
		t.Fatalf("got version %q", result.Version)
	}
	if _, ok := result.Capabilities["dev.ucp.shopping.checkout"]; !ok {
		t.Fatal("expected checkout in result capabilities")
	}
	if _, ok := result.PaymentHandlers["com.example.processor"]; !ok {
		t.Fatal("expected processor in result payment handlers")
	}

	// Second call should use cache (server won't be hit again).
	server.Close()
	cached, _, _, err := svc.Negotiate(context.Background(), inst, server.URL+"/profile.json")
	if err != nil {
		t.Fatalf("expected cached result, got error: %v", err)
	}
	if cached.Version != "2026-01-23" {
		t.Fatalf("cached version mismatch: %q", cached.Version)
	}
}

// --- Middleware ---

func TestMiddleware_NoUCPHeader(t *testing.T) {
	svc := NewNegotiationService()

	e := echo.New()
	called := false
	handler := func(c echo.Context) error {
		called = true
		return c.String(http.StatusOK, "ok")
	}
	mw := WithNegotiation(svc)(handler)

	inst := defaultInstance()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(StoreProfileKey, inst)

	if err := mw(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected handler to be called for non-UCP request")
	}
}

func TestMiddleware_MissingUCPInstance(t *testing.T) {
	svc := NewNegotiationService()

	e := echo.New()
	handler := func(c echo.Context) error { return c.String(http.StatusOK, "ok") }
	mw := WithNegotiation(svc)(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("UCP-Agent", `profile="https://platform.example/profile.json"`)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// Intentionally do NOT set UCPInstanceKey

	if err := mw(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestMiddleware_VersionUnsupported(t *testing.T) {
	// Platform has a newer version than the business → spec requires 422.
	inst := &mockInstance{id: "inst-001", version: "2025-01-01"}
	profileBody := platformProfileJSON("2026-01-23", nil, nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(profileBody)
	}))
	defer server.Close()

	svc := NewNegotiationService(WithSkipNamespaceValidation(true))
	e := echo.New()
	handler := func(c echo.Context) error { return c.String(http.StatusOK, "ok") }
	mw := WithNegotiation(svc)(handler)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("UCP-Agent", `profile="`+server.URL+`/profile.json"`)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(StoreProfileKey, inst)

	if err := mw(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rec.Code)
	}

	var errResp ucp.NegotiationErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decoding error response: %v", err)
	}
	if errResp.Code != "version_unsupported" {
		t.Fatalf("expected version_unsupported code, got %q", errResp.Code)
	}
}

func TestMiddleware_FetchFailure(t *testing.T) {
	inst := defaultInstance()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Non-2xx → profile_unreachable.
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	svc := NewNegotiationService()
	e := echo.New()
	handler := func(c echo.Context) error { return c.String(http.StatusOK, "ok") }
	mw := WithNegotiation(svc)(handler)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("UCP-Agent", `profile="`+server.URL+`/profile.json"`)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(StoreProfileKey, inst)

	if err := mw(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusFailedDependency {
		t.Fatalf("expected 424, got %d", rec.Code)
	}

	var errResp ucp.NegotiationErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decoding error response: %v", err)
	}
	if errResp.Code != "profile_unreachable" {
		t.Fatalf("expected profile_unreachable code, got %q", errResp.Code)
	}
}

func TestMiddleware_CapabilitiesIncompatible(t *testing.T) {
	// Business supports checkout; platform only advertises cart — no intersection.
	inst := &mockInstance{
		id:      "inst-001",
		version: "2026-01-23",
		capabilities: map[string][]wire.Capability{
			"dev.ucp.shopping.checkout": {{Version: "2026-01-23", Spec: "https://ucp.dev/spec/checkout"}},
		},
	}
	profileBody := platformProfileJSON(
		"2026-01-23",
		map[string][]wire.Capability{
			"dev.ucp.shopping.cart": {{Version: "2026-01-23"}},
		},
		nil,
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(profileBody)
	}))
	defer server.Close()

	svc := NewNegotiationService(WithSkipNamespaceValidation(true))
	e := echo.New()
	handler := func(c echo.Context) error { return c.String(http.StatusOK, "ok") }
	mw := WithNegotiation(svc)(handler)

	req := httptest.NewRequest(http.MethodPost, "/checkout-sessions", nil)
	req.Header.Set("UCP-Agent", `profile="`+server.URL+`/profile.json"`)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(StoreProfileKey, inst)

	if err := mw(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp ucp.CapabilitiesIncompatibleResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.UCP.Status != "error" {
		t.Fatalf("expected ucp.status=error, got %q", resp.UCP.Status)
	}
	if resp.UCP.Version != "2026-01-23" {
		t.Fatalf("expected ucp.version=2026-01-23, got %q", resp.UCP.Version)
	}
	if len(resp.Messages) == 0 || resp.Messages[0].Code != "capabilities_incompatible" {
		t.Fatalf("expected capabilities_incompatible message, got %+v", resp.Messages)
	}
}

func TestMiddleware_HandlerErrorWithoutBody(t *testing.T) {
	// Handler returns an error without writing anything.
	// The middleware must NOT write a 200 to the wire; Echo's error handler must run.
	inst := defaultInstance()
	profileBody := platformProfileJSON(
		"2026-01-23",
		map[string][]wire.Capability{
			"dev.ucp.shopping.checkout": {{Version: "2026-01-23", Spec: "https://ucp.dev/spec/checkout"}},
		},
		nil,
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(profileBody)
	}))
	defer server.Close()

	svc := NewNegotiationService(WithSkipNamespaceValidation(true))
	e := echo.New()
	handlerErr := echo.ErrInternalServerError
	handler := func(c echo.Context) error {
		// returns error without calling c.JSON / c.String / etc.
		return handlerErr
	}
	mw := WithNegotiation(svc)(handler)

	req := httptest.NewRequest(http.MethodPost, "/checkout-sessions", nil)
	req.Header.Set("UCP-Agent", `profile="`+server.URL+`/profile.json"`)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(StoreProfileKey, inst)

	err := mw(c)
	if err != handlerErr {
		t.Fatalf("expected handler error to propagate, got %v", err)
	}
	// Response must not have been committed by the middleware — body should be empty
	// so Echo's error handler can write its own response.
	if c.Response().Committed {
		t.Fatal("response must not be committed when handler errors without writing")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected no body written, got %q", rec.Body.String())
	}
}

func TestMiddleware_FullFlow(t *testing.T) {
	inst := defaultInstance()
	profileBody := platformProfileJSON(
		"2026-01-23",
		map[string][]wire.Capability{
			"dev.ucp.shopping.checkout": {{Version: "2026-01-23", Spec: "https://ucp.dev/spec/checkout"}},
		},
		map[string][]wire.PaymentHandler{
			"com.example.processor": {{ID: "processor", Version: "2026-01-23"}},
		},
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(profileBody)
	}))
	defer server.Close()

	svc := NewNegotiationService(WithSkipNamespaceValidation(true))
	e := echo.New()
	handler := func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{
			"id":     "checkout_123",
			"status": "incomplete",
		})
	}
	mw := WithNegotiation(svc)(handler)

	req := httptest.NewRequest(http.MethodPost, "/checkout-sessions", nil)
	req.Header.Set("UCP-Agent", `profile="`+server.URL+`/profile.json"`)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(StoreProfileKey, inst)

	if err := mw(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]json.RawMessage
	if err := json.NewDecoder(strings.NewReader(rec.Body.String())).Decode(&body); err != nil {
		t.Fatalf("decoding response body: %v", err)
	}
	if _, ok := body["ucp"]; !ok {
		t.Fatal("expected 'ucp' field injected into response body")
	}
	if _, ok := body["id"]; !ok {
		t.Fatal("expected 'id' field preserved in response body")
	}
	if _, ok := body["status"]; !ok {
		t.Fatal("expected 'status' field preserved in response body")
	}

	var ucpResult NegotiationResult
	if err := json.Unmarshal(body["ucp"], &ucpResult); err != nil {
		t.Fatalf("decoding ucp field: %v", err)
	}
	if _, ok := ucpResult.Capabilities["dev.ucp.shopping.checkout"]; !ok {
		t.Fatal("expected checkout in ucp.capabilities")
	}
}

// --- injectUCPResponse ---

func TestInjectUCPResponse_Object(t *testing.T) {
	body := []byte(`{"id":"checkout_123","status":"incomplete"}`)
	result := &NegotiationResult{
		Version: "2026-01-23",
		Capabilities: map[string][]CapabilityRef{
			"dev.ucp.shopping.checkout": {{Version: "2026-01-23"}},
		},
	}

	out, err := injectUCPResponse(body, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("decoding output: %v", err)
	}
	if _, ok := m["ucp"]; !ok {
		t.Fatal("expected 'ucp' field in output")
	}
	if _, ok := m["id"]; !ok {
		t.Fatal("expected 'id' field preserved")
	}
}

func TestInjectUCPResponse_NonObject(t *testing.T) {
	body := []byte(`["not","an","object"]`)
	result := &NegotiationResult{Version: "2026-01-23"}

	out, err := injectUCPResponse(body, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != string(body) {
		t.Fatalf("expected body unchanged, got %s", out)
	}
}

// --- CapabilitySets (Agent Behavior Insights emission-gap fix) ---

// TestNegotiate_Succeeded_RecordsRequestedAndDeclaredSets closes the audit
// finding that NegotiationSucceededEvent only ever recorded the
// post-intersection ActiveCapabilities, discarding what the platform asked
// for and what the business declares — exactly the data capability-gap
// analytics needs (agent requested X, merchant doesn't declare X, but
// negotiation still succeeded on the rest of the intersection).
func TestNegotiate_Succeeded_RecordsRequestedAndDeclaredSets(t *testing.T) {
	inst := &mockInstance{
		id:      "inst-001",
		version: "2026-01-23",
		capabilities: map[string][]wire.Capability{
			"dev.ucp.shopping.checkout": {{Version: "2026-01-23", Spec: "https://ucp.dev/spec/checkout"}},
		},
		paymentHandlers: map[string][]wire.PaymentHandler{
			"com.example.card": {{ID: "card", Version: "2026-01-23"}},
		},
	}
	// Platform requests checkout (which the business declares, so it's
	// active) AND cart (which the business does NOT declare — the
	// capability-gap case) and PayPal (a payment handler the business does
	// not declare).
	profileBody := platformProfileJSON(
		"2026-01-23",
		map[string][]wire.Capability{
			"dev.ucp.shopping.checkout": {{Version: "2026-01-23"}},
			"dev.ucp.shopping.cart":     {{Version: "2026-01-23"}},
		},
		map[string][]wire.PaymentHandler{
			"com.example.paypal": {{ID: "paypal", Version: "2026-01-23"}},
		},
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(profileBody)
	}))
	defer server.Close()

	svc := NewNegotiationService(WithSkipNamespaceValidation(true))
	_, _, events, err := svc.Negotiate(context.Background(), inst, server.URL+"/profile.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly one event, got %d", len(events))
	}
	succeeded, ok := events[0].(*NegotiationSucceededEvent)
	if !ok {
		t.Fatalf("expected *NegotiationSucceededEvent, got %T", events[0])
	}

	sets := succeeded.CapabilitySets()
	if !containsString(sets.RequestedCapabilities, "dev.ucp.shopping.cart") {
		t.Errorf("expected requested capabilities to include the gap capability dev.ucp.shopping.cart, got %v", sets.RequestedCapabilities)
	}
	if containsString(sets.DeclaredCapabilities, "dev.ucp.shopping.cart") {
		t.Errorf("business does not declare cart — must not appear in declared set, got %v", sets.DeclaredCapabilities)
	}
	if containsString(sets.ActiveCapabilities, "dev.ucp.shopping.cart") {
		t.Errorf("cart has no mutual version support and must not be active, got %v", sets.ActiveCapabilities)
	}
	if !containsString(sets.ActiveCapabilities, "dev.ucp.shopping.checkout") {
		t.Errorf("expected checkout to be active, got %v", sets.ActiveCapabilities)
	}

	// Payment-handler mismatch is observational data on a *succeeded*
	// negotiation — an empty handler intersection must not fail
	// negotiation (unchanged behavior, verified below).
	if !containsString(sets.RequestedPaymentHandlers, "com.example.paypal") {
		t.Errorf("expected requested payment handlers to include com.example.paypal, got %v", sets.RequestedPaymentHandlers)
	}
	if !containsString(sets.DeclaredPaymentHandlers, "com.example.card") {
		t.Errorf("expected declared payment handlers to include com.example.card, got %v", sets.DeclaredPaymentHandlers)
	}
	if len(sets.ActivePaymentHandlers) != 0 {
		t.Errorf("expected zero active payment handlers (no overlap), got %v", sets.ActivePaymentHandlers)
	}
}

// TestNegotiate_CapabilitiesIncompatible_RecordsRequestedAndDeclaredSets
// verifies the capabilities_incompatible failure path — where the R8
// capability gap is at its most direct (zero-capability intersection) —
// still carries the requested/declared sets, not just the bare reason
// string.
func TestNegotiate_CapabilitiesIncompatible_RecordsRequestedAndDeclaredSets(t *testing.T) {
	inst := &mockInstance{
		id:      "inst-001",
		version: "2026-01-23",
		capabilities: map[string][]wire.Capability{
			"dev.ucp.shopping.checkout": {{Version: "2026-01-23", Spec: "https://ucp.dev/spec/checkout"}},
		},
	}
	profileBody := platformProfileJSON(
		"2026-01-23",
		map[string][]wire.Capability{
			"dev.ucp.shopping.cart": {{Version: "2026-01-23"}},
		},
		nil,
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(profileBody)
	}))
	defer server.Close()

	svc := NewNegotiationService(WithSkipNamespaceValidation(true))
	_, _, events, err := svc.Negotiate(context.Background(), inst, server.URL+"/profile.json")
	if err == nil {
		t.Fatal("expected capabilities_incompatible error")
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly one event, got %d", len(events))
	}
	failed, ok := events[0].(*NegotiationFailedEvent)
	if !ok {
		t.Fatalf("expected *NegotiationFailedEvent, got %T", events[0])
	}
	if failed.Reason() != "capabilities_incompatible" {
		t.Errorf("expected reason capabilities_incompatible, got %q", failed.Reason())
	}

	sets := failed.CapabilitySets()
	if !containsString(sets.RequestedCapabilities, "dev.ucp.shopping.cart") {
		t.Errorf("expected requested capabilities to include dev.ucp.shopping.cart, got %v", sets.RequestedCapabilities)
	}
	if !containsString(sets.DeclaredCapabilities, "dev.ucp.shopping.checkout") {
		t.Errorf("expected declared capabilities to include dev.ucp.shopping.checkout, got %v", sets.DeclaredCapabilities)
	}
	if sets.ActiveCapabilities != nil {
		t.Errorf("expected nil active capabilities on failure, got %v", sets.ActiveCapabilities)
	}
}

// TestNegotiate_VersionUnsupported_RecordsDeclaredAndRequestedSets verifies
// that even the earliest failure path (version mismatch, which fails
// before the pre-fix code ever built a business profile) now carries the
// requested/declared sets — CapabilitySets construction was moved ahead of
// ValidateVersion specifically so this path isn't left with an empty sets
// value.
func TestNegotiate_VersionUnsupported_RecordsDeclaredAndRequestedSets(t *testing.T) {
	inst := &mockInstance{
		id:      "inst-001",
		version: "2025-01-01",
		capabilities: map[string][]wire.Capability{
			"dev.ucp.shopping.checkout": {{Version: "2026-01-23"}},
		},
	}
	profileBody := platformProfileJSON("2026-01-23", map[string][]wire.Capability{
		"dev.ucp.shopping.checkout": {{Version: "2026-01-23"}},
	}, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(profileBody)
	}))
	defer server.Close()

	svc := NewNegotiationService(WithSkipNamespaceValidation(true))
	_, _, events, err := svc.Negotiate(context.Background(), inst, server.URL+"/profile.json")
	if err == nil {
		t.Fatal("expected version_unsupported error")
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly one event, got %d", len(events))
	}
	failed, ok := events[0].(*NegotiationFailedEvent)
	if !ok {
		t.Fatalf("expected *NegotiationFailedEvent, got %T", events[0])
	}
	if failed.Reason() != "version_unsupported" {
		t.Errorf("expected reason version_unsupported, got %q", failed.Reason())
	}
	sets := failed.CapabilitySets()
	if !containsString(sets.DeclaredCapabilities, "dev.ucp.shopping.checkout") {
		t.Errorf("expected declared capabilities populated even on the earliest failure path, got %v", sets.DeclaredCapabilities)
	}
	if !containsString(sets.RequestedCapabilities, "dev.ucp.shopping.checkout") {
		t.Errorf("expected requested capabilities populated even on the earliest failure path, got %v", sets.RequestedCapabilities)
	}
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// --- BusinessProfileFromInstance ---

func TestBusinessProfileFromInstance(t *testing.T) {
	inst := &mockInstance{
		id:      "inst-001",
		version: "2026-01-23",
		capabilities: map[string][]wire.Capability{
			"dev.ucp.shopping.checkout": {{Version: "2026-01-23"}},
		},
		paymentHandlers: map[string][]wire.PaymentHandler{
			"com.example.processor": {{ID: "processor", Version: "2026-01-23"}},
		},
		signingKeys: []wire.JWK{{Kid: "key-1", Kty: "EC"}},
	}

	profile := ucp.BusinessProfileFromStoreProfile(inst)

	if profile.UCP.Version != "2026-01-23" {
		t.Fatalf("version mismatch: %q", profile.UCP.Version)
	}
	if _, ok := profile.UCP.Capabilities["dev.ucp.shopping.checkout"]; !ok {
		t.Fatal("expected capabilities in profile")
	}
	if _, ok := profile.UCP.PaymentHandlers["com.example.processor"]; !ok {
		t.Fatal("expected payment handlers in profile")
	}
	if len(profile.Keys) != 1 || profile.Keys[0].Kid != "key-1" {
		t.Fatal("expected signing keys in profile")
	}
}
