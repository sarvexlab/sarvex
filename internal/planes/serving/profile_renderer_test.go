// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	specall "github.com/sarvexlabs/sarvex.cp/internal/spec/all"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/catalogue"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wire"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestProfileRenderer_RenderProfile_WithNilCatalogue(t *testing.T) {
	// Should return empty services/capabilities/handlers if catalogue is not available.
	renderer := NewProfileRenderer(nil, "https://serving.test", testLogger())

	profile := &StoreProfileData{
		StoreID:       "store1",
		MerchantID:    "merchant1",
		StorefrontURL: "https://store.example.com",
		SigningKeys:   []wire.JWK{},
	}

	cacheProfile := renderer.RenderProfile("store1", "merchant1", profile)

	if cacheProfile == nil {
		t.Fatalf("expected cacheProfile to be non-nil")
	}

	if cacheProfile.StoreID != "store1" {
		t.Errorf("expected StoreID='store1', got %s", cacheProfile.StoreID)
	}

	if cacheProfile.MerchantID != "merchant1" {
		t.Errorf("expected MerchantID='merchant1', got %s", cacheProfile.MerchantID)
	}

	// Check spec-compliant structure
	if cacheProfile.UCP.Version == "" {
		t.Errorf("expected UCP version to be set")
	}

	// With no store overrides and no catalogue, should have empty maps.
	if cacheProfile.UCP.Services == nil || len(cacheProfile.UCP.Services) > 0 {
		t.Errorf("expected empty services when catalogue is nil")
	}

	if cacheProfile.UCP.Capabilities == nil || len(cacheProfile.UCP.Capabilities) > 0 {
		t.Errorf("expected empty capabilities when catalogue is nil and no store overrides")
	}

	if cacheProfile.UCP.PaymentHandlers == nil || len(cacheProfile.UCP.PaymentHandlers) > 0 {
		t.Errorf("expected empty payment handlers when catalogue is nil")
	}
}

func TestProfileRenderer_RenderProfile_WithCatalogueAndV1Caps(t *testing.T) {
	// When catalogue has V1 capabilities, they should be included in the profile.
	cat, err := catalogue.LoadCatalogue(testContext(), testLogger())
	if err != nil || cat == nil {
		t.Fatalf("failed to load test catalogue: %v", err)
	}

	renderer := NewProfileRenderer(cat, "https://serving.test", testLogger())

	profile := &StoreProfileData{
		StoreID:       "store1",
		MerchantID:    "merchant1",
		StorefrontURL: "https://store.example.com",
		SigningKeys:   []wire.JWK{},
	}

	cacheProfile := renderer.RenderProfile("store1", "merchant1", profile)

	if cacheProfile == nil {
		t.Fatalf("expected cacheProfile to be non-nil")
	}

	// Should have UCP version from catalogue
	if cacheProfile.UCP.Version == "" {
		t.Errorf("expected UCP version to be set from catalogue")
	}

	// Should have services from catalogue
	if len(cacheProfile.UCP.Services) == 0 {
		t.Errorf("expected services to be populated from catalogue")
	}

	// Should have capabilities (at least catalog and checkout from V1).
	if cacheProfile.UCP.Capabilities == nil {
		t.Errorf("expected capabilities to be non-nil")
	}

	// Check that profile contains expected capabilities.
	if _, hasCatalog := cacheProfile.UCP.Capabilities["dev.ucp.shopping.catalog"]; !hasCatalog {
		t.Errorf("expected catalog capability in profile")
	}

	if _, hasCheckout := cacheProfile.UCP.Capabilities["dev.ucp.shopping.checkout"]; !hasCheckout {
		t.Errorf("expected checkout capability in profile")
	}

	// Cart is V1-implemented (POST/GET/PUT /carts, cart_service.go).
	if _, hasCart := cacheProfile.UCP.Capabilities["dev.ucp.shopping.cart"]; !hasCart {
		t.Errorf("expected cart capability in profile")
	}

	// identity_linking has no routes, so it must not be advertised.
	if _, has := cacheProfile.UCP.Capabilities["dev.ucp.common.identity_linking"]; has {
		t.Errorf("should not include identity_linking (not V1 implemented)")
	}

	// Order IS V1-implemented (GET/PUT /orders/{id}, order_service.go) —
	// declaring it is required for the official UCP conformance suite's
	// reference shopping agent to negotiate at all (see loader.go's
	// v1ImplementedCapabilities doc comment).
	if _, hasOrder := cacheProfile.UCP.Capabilities["dev.ucp.shopping.order"]; !hasOrder {
		t.Errorf("expected order capability in profile")
	}

	// Should have payment handlers from catalogue
	if len(cacheProfile.UCP.PaymentHandlers) == 0 {
		t.Errorf("expected payment handlers to be populated from catalogue")
	}
}

func TestProfileRenderer_RenderProfile_StoreOverrides(t *testing.T) {
	// If store has explicit capability overrides, they should be used instead of catalogue defaults.
	cat, err := catalogue.LoadCatalogue(testContext(), testLogger())
	if err != nil || cat == nil {
		t.Fatalf("failed to load test catalogue: %v", err)
	}

	renderer := NewProfileRenderer(cat, "https://serving.test", testLogger())

	// Store with explicit capability overrides.
	profile := &StoreProfileData{
		StoreID:       "store1",
		MerchantID:    "merchant1",
		StorefrontURL: "https://store.example.com",
		SigningKeys:   []wire.JWK{},
		Capabilities: map[string][]wire.Capability{
			"dev.ucp.shopping.catalog": {
				{
					ID:      "dev.ucp.shopping.catalog",
					Version: "1.0.0",
				},
			},
			// Only catalog, no checkout
		},
	}

	cacheProfile := renderer.RenderProfile("store1", "merchant1", profile)

	if cacheProfile == nil {
		t.Fatalf("expected cacheProfile to be non-nil")
	}

	// Should have overridden capabilities (just catalog).
	if cacheProfile.UCP.Capabilities == nil {
		t.Errorf("expected capabilities to be non-nil")
	}

	if _, hasCatalog := cacheProfile.UCP.Capabilities["dev.ucp.shopping.catalog"]; !hasCatalog {
		t.Errorf("expected catalog capability in profile")
	}

	if _, hasCheckout := cacheProfile.UCP.Capabilities["dev.ucp.shopping.checkout"]; hasCheckout {
		t.Errorf("should not include checkout (store override)")
	}
}

// A store's capability set narrows the V1 surface; it must not replace the
// rendered per-version entries, must not add capabilities the V1 gate
// excludes, and must not leave an extension published without its parent.
func TestProfileRenderer_RenderProfile_StoreSetFiltersNotReplaces(t *testing.T) {
	cat, err := catalogue.LoadCatalogue(testContext(), testLogger())
	if err != nil {
		t.Fatalf("load catalogue: %v", err)
	}
	renderer := NewProfileRenderer(cat, "https://serving.test", testLogger())

	one := func(id string) []wire.Capability {
		return []wire.Capability{{ID: id, Version: "1.0.0"}}
	}
	profile := &StoreProfileData{
		StoreID: "store1", MerchantID: "merchant1",
		Capabilities: map[string][]wire.Capability{
			"dev.ucp.shopping.catalog":        one("dev.ucp.shopping.catalog"),
			"dev.ucp.common.identity_linking": one("dev.ucp.common.identity_linking"), // not V1
			"dev.ucp.shopping.fulfillment":    one("dev.ucp.shopping.fulfillment"),
			"dev.ucp.shopping.buyer_consent":  one("dev.ucp.shopping.buyer_consent"),
		},
	}

	caps := renderer.RenderProfile("store1", "merchant1", profile).UCP.Capabilities

	if got, want := len(caps["dev.ucp.shopping.catalog"]), len(specall.Default().Servable()); got != want {
		t.Errorf("catalog entries = %d, want one per servable version (%d)", got, want)
	}
	for _, e := range caps["dev.ucp.shopping.catalog"] {
		if e.Version == "1.0.0" {
			t.Errorf("stored single-version entry leaked into the profile: %+v", e)
		}
	}
	if _, ok := caps["dev.ucp.common.identity_linking"]; ok {
		t.Error("store set must not add a capability outside the V1 gate")
	}
	// checkout is not in the store set, so its extensions have no parent.
	for _, ext := range []string{"dev.ucp.shopping.fulfillment", "dev.ucp.shopping.buyer_consent"} {
		if _, ok := caps[ext]; ok {
			t.Errorf("%s published without its parent checkout", ext)
		}
	}
	if len(caps) != 1 {
		t.Errorf("want only catalog, got %v", caps)
	}
}

func renderHandlers(t *testing.T, p *StoreProfileData) map[string][]wire.PaymentHandler {
	t.Helper()
	cat, err := catalogue.LoadCatalogue(testContext(), testLogger())
	if err != nil {
		t.Fatalf("load catalogue: %v", err)
	}
	p.StoreID, p.MerchantID = "store1", "merchant1"
	return NewProfileRenderer(cat, "https://serving.test", testLogger()).
		RenderProfile("store1", "merchant1", p).UCP.PaymentHandlers
}

func TestProfileRenderer_PaymentHandlers_PlatformApplicability(t *testing.T) {
	woo := renderHandlers(t, &StoreProfileData{Platform: "woocommerce"})
	if _, ok := woo["dev.shopify.shop_pay"]; ok {
		t.Error("Shop Pay is Shopify-only and must not be advertised for a WooCommerce store")
	}
	if _, ok := woo["com.google.pay"]; !ok {
		t.Error("Google Pay applies to every platform")
	}
	magento := renderHandlers(t, &StoreProfileData{Platform: "magento"})
	if _, ok := magento["dev.shopify.shop_pay"]; ok {
		t.Error("Shop Pay is Shopify-only and must not be advertised for a Magento store")
	}
}

func TestProfileRenderer_PaymentHandlers_FollowStoreConfiguration(t *testing.T) {
	full := map[string]string{
		"merchant_name": "M", "merchant_id": "1", "merchant_origin": "https://m.example",
		"gateway": "stripe", "gateway_merchant_id": "acct",
	}

	configured := renderHandlers(t, &StoreProfileData{
		Platform: "woocommerce",
		PaymentHandlers: map[string]PaymentHandlerSetting{
			"com.google.pay": {Enabled: true, Params: full},
		},
	})
	gp := configured["com.google.pay"]
	if len(gp) != 1 || gp[0].Config == nil {
		t.Fatalf("configured Google Pay must be published with its config: %+v", gp)
	}
	var cfg struct {
		MerchantInfo struct {
			MerchantID string `json:"merchant_id"`
		} `json:"merchant_info"`
	}
	if err := json.Unmarshal(gp[0].Config, &cfg); err != nil || cfg.MerchantInfo.MerchantID != "1" {
		t.Errorf("config not built from the merchant's parameters: %s (%v)", gp[0].Config, err)
	}

	if got := renderHandlers(t, &StoreProfileData{
		Platform: "woocommerce",
		PaymentHandlers: map[string]PaymentHandlerSetting{
			"com.google.pay": {Enabled: false, Params: full},
		},
	}); len(got) != 0 {
		t.Errorf("a disabled handler must not be advertised: %v", got)
	}

	if got := renderHandlers(t, &StoreProfileData{
		Platform: "woocommerce",
		PaymentHandlers: map[string]PaymentHandlerSetting{
			"com.google.pay": {Enabled: true, Params: map[string]string{"merchant_id": "1"}},
		},
	}); len(got) != 0 {
		t.Errorf("an enabled but incomplete handler must be withheld: %v", got)
	}

	if got := renderHandlers(t, &StoreProfileData{
		Platform:        "woocommerce",
		PaymentHandlers: map[string]PaymentHandlerSetting{},
	}); len(got) != 0 {
		t.Errorf("a deliberately empty configuration advertises nothing: %v", got)
	}
}

func TestProfileRenderer_RenderProfile_SigningKeys(t *testing.T) {
	// Signing keys should be converted and included in the profile, under
	// "keys" (not the legacy "signing_keys") since the rendered document's
	// ucp.version is specall.Default().Latest() = 2026-08-25 today — see CacheProfile's doc
	// comment on the two fields being mutually exclusive per generation.
	renderer := NewProfileRenderer(nil, "https://serving.test", testLogger())

	signingKeys := []wire.JWK{
		{Kid: "key1", Kty: "EC"},
		{Kid: "key2", Kty: "EC"},
	}

	profile := &StoreProfileData{
		StoreID:       "store1",
		MerchantID:    "merchant1",
		StorefrontURL: "https://store.example.com",
		SigningKeys:   signingKeys,
	}

	cacheProfile := renderer.RenderProfile("store1", "merchant1", profile)

	if cacheProfile == nil {
		t.Fatalf("expected cacheProfile to be non-nil")
	}

	if len(cacheProfile.SigningKeys) != 0 {
		t.Errorf("expected the legacy signing_keys field to stay empty when rendering at 2026-08-25, got %d entries", len(cacheProfile.SigningKeys))
	}

	if len(cacheProfile.Keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(cacheProfile.Keys))
	}

	if cacheProfile.Keys[0].Kid != "key1" {
		t.Errorf("expected first key kid='key1', got %s", cacheProfile.Keys[0].Kid)
	}
}

// TestProfileRenderer_RenderProfile_DeclaresLatestAsTopLevelVersion proves
// the root document's ucp.version is specall.Default().Latest() (the chronologically
// newest registered generation, computed automatically — not a hardcoded
// constant), not the older 2026-04-08 that used to be hardcoded —
// sarvex-v1-architecture.md §D R3 / the "AI shopping agents prefer the
// newest compatible version in the main manifest" requirement.
func TestProfileRenderer_RenderProfile_DeclaresLatestAsTopLevelVersion(t *testing.T) {
	cat := testCatalogue(t)
	renderer := NewProfileRenderer(cat, "https://serving.test", testLogger())

	cacheProfile := renderer.RenderProfile("store1", "merchant1", &StoreProfileData{Platform: "woocommerce"})

	if cacheProfile.UCP.Version != specall.Default().Latest().String() {
		t.Errorf("UCP.Version = %q, want specall.Default().Latest() = %q", cacheProfile.UCP.Version, specall.Default().Latest().String())
	}
}

// Every capability and extension Sarvex serves is declared at every servable
// generation, and each entry points at ITS generation's spec and schema — an
// agent that negotiated 2026-08-25 must be sent to 2026-08-25's contract, not
// the catalogue's 2026-04-08 one.
func TestProfileRenderer_EveryServedCapabilityIsDeclaredAtEveryServableVersion(t *testing.T) {
	cat := testCatalogue(t)
	renderer := NewProfileRenderer(cat, "https://serving.test", testLogger())

	caps := renderer.RenderProfile("store1", "merchant1", &StoreProfileData{Platform: "woocommerce"}).UCP.Capabilities

	want := []string{
		"dev.ucp.shopping.catalog", "dev.ucp.shopping.checkout", "dev.ucp.shopping.cart", "dev.ucp.shopping.order",
		"dev.ucp.shopping.fulfillment", "dev.ucp.shopping.discount", "dev.ucp.shopping.buyer_consent",
	}
	for _, id := range want {
		entries := caps[id]
		got := map[wire.Version]wire.Capability{}
		for _, e := range entries {
			got[e.Version] = e
		}
		for _, v := range specall.Default().Servable() {
			e, ok := got[wire.Version(v)]
			if !ok {
				t.Errorf("%s is not declared at %s (declared: %d entries)", id, v, len(entries))
				continue
			}
			for _, url := range []string{e.Spec, e.Schema} {
				if !strings.Contains(url, "/"+v.String()+"/") {
					t.Errorf("%s@%s points at %q, want a %s URL", id, v, url, v)
				}
			}
		}
	}
}

func testContext() context.Context {
	return context.Background()
}

// The published profile must tell an agent where this store's services
// actually answer. The catalogue can only supply a placeholder — it is
// store-agnostic — so the renderer is the only place the real URL can be
// stamped in. Getting this wrong publishes a service id where a URL belongs,
// which fails the profile_reachable probe with an endpoint mismatch and pins
// the store at profile_published.
func TestRenderProfile_DeclaresTheStoresOwnServingEndpoint(t *testing.T) {
	cat := testCatalogue(t)
	renderer := NewProfileRenderer(cat, "https://serving.test", testLogger())

	profile := renderer.RenderProfile("store-1", "merchant-1", &StoreProfileData{
		Platform: "woocommerce",
	})

	if len(profile.UCP.Services) == 0 {
		t.Fatal("expected at least one service in the rendered profile")
	}
	const want = "https://serving.test/ucp/woocommerce/store-1"
	for id, svcs := range profile.UCP.Services {
		for _, svc := range svcs {
			if svc.Endpoint != want {
				t.Errorf("service %q endpoint = %q, want %q", id, svc.Endpoint, want)
			}
		}
	}
}

// With no origin configured the catalogue's value is left alone rather than a
// broken relative URL being published.
func TestRenderProfile_NoServingOriginLeavesCatalogueValue(t *testing.T) {
	cat := testCatalogue(t)
	renderer := NewProfileRenderer(cat, "", testLogger())

	profile := renderer.RenderProfile("store-1", "merchant-1", &StoreProfileData{
		Platform: "woocommerce",
	})

	for _, svcs := range profile.UCP.Services {
		for _, svc := range svcs {
			if strings.HasPrefix(svc.Endpoint, "http") {
				t.Errorf("expected the catalogue placeholder, got a URL: %q", svc.Endpoint)
			}
		}
	}
}
