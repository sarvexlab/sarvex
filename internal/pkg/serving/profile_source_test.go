// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wireintersect"
)

// fakeCache is a ServingCache that returns a preset profile/error for GetProfile.
type fakeCache struct {
	profile *CacheProfile
	err     error
}

func (f *fakeCache) GetProduct(context.Context, string, string) (*CacheProduct, error) {
	return nil, ErrNotFound
}
func (f *fakeCache) Search(context.Context, string, string, int, string) (*CacheSearchResult, error) {
	return nil, ErrNotFound
}
func (f *fakeCache) GetProfile(context.Context, string) (*CacheProfile, error) {
	return f.profile, f.err
}
func (f *fakeCache) Watermark(context.Context, string) (*CacheWatermark, error) {
	return nil, ErrNotFound
}

func TestCacheProfileSource_Hit(t *testing.T) {
	prof := &CacheProfile{
		StoreID:    "store_1",
		MerchantID: "merch_1",
		UCP:        UCPBlockCache{Version: "2026-04-08"},
	}
	src := NewCacheProfileSource(&fakeCache{profile: prof})

	body, etag, err := src.GetProfile(context.Background(), "store_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if etag == "" {
		t.Fatal("expected non-empty etag")
	}

	// Bytes must match the spec-shaped CacheProfile JSON (StoreID/MerchantID omitted).
	want, _ := json.Marshal(prof)
	if string(body) != string(want) {
		t.Fatalf("body mismatch\n got: %s\nwant: %s", body, want)
	}
	if got := string(body); contains(got, "store_1") || contains(got, "merch_1") {
		t.Fatalf("internal identifiers leaked into profile body: %s", got)
	}
}

func TestCacheProfileSource_StableEtag(t *testing.T) {
	prof := &CacheProfile{UCP: UCPBlockCache{Version: "2026-04-08"}}
	src := NewCacheProfileSource(&fakeCache{profile: prof})

	_, etag1, err := src.GetProfile(context.Background(), "store_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, etag2, err := src.GetProfile(context.Background(), "store_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if etag1 != etag2 {
		t.Fatalf("etag not stable for identical content: %q vs %q", etag1, etag2)
	}
}

func TestCacheProfileSource_NotFoundDegrades(t *testing.T) {
	src := NewCacheProfileSource(&fakeCache{err: ErrNotFound})

	body, etag, err := src.GetProfile(context.Background(), "missing")
	if err != nil {
		t.Fatalf("expected no error on missing snapshot, got: %v", err)
	}
	if body != nil || etag != "" {
		t.Fatalf("expected empty result on missing snapshot, got body=%v etag=%q", body, etag)
	}
}

func TestCacheProfileSource_GetStructuredProfile_Hit(t *testing.T) {
	prof := &CacheProfile{
		StoreID:    "store_1",
		MerchantID: "merch_1",
		UCP:        UCPBlockCache{Version: "2026-04-08"},
	}
	src := NewCacheProfileSource(&fakeCache{profile: prof})

	provider, err := src.GetStructuredProfile(context.Background(), "store_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected a non-nil StoreProvider")
	}
	if provider.StoreID() != "store_1" {
		t.Fatalf("got StoreID %q, want %q", provider.StoreID(), "store_1")
	}
}

// A missing snapshot must be a distinguishable error (ucp.ErrStoreProfileNotFound),
// not a (nil, nil) "success" — negotiation.WithStoreConfigProvider relies on
// this to answer 404 rather than mistaking a missing profile for a loaded
// one and surfacing a misleading 500 further down the middleware chain.
func TestCacheProfileSource_GetStructuredProfile_NotFoundIsAnError(t *testing.T) {
	src := NewCacheProfileSource(&fakeCache{err: ErrNotFound})

	provider, err := src.GetStructuredProfile(context.Background(), "missing")
	if !errors.Is(err, ucp.ErrStoreProfileNotFound) {
		t.Fatalf("expected ucp.ErrStoreProfileNotFound, got: %v", err)
	}
	if provider != nil {
		t.Fatalf("expected nil provider on not-found, got: %v", provider)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// The store's profile, as negotiation sees it, keeps each extension's
// extends and each service's transport. (They used to be dropped, so an
// agent could negotiate an extension — fulfillment, say — without its parent
// checkout: intersection prunes orphaned extensions by the business
// capability's extends, and there was none.)
func TestStructuredProfile_KeepsExtendsAndTransport(t *testing.T) {
	prof := &CacheProfile{StoreID: "store_1", UCP: UCPBlockCache{
		Version: "2026-08-25",
		Services: map[string][]wire.Service{"dev.ucp.shopping": {{
			ID: "dev.ucp.shopping", Version: "2026-08-25", Spec: "https://ucp.dev/spec", Transport: wire.TransportREST, Endpoint: "https://x/ucp",
		}}},
		Capabilities: map[string][]wire.Capability{
			"dev.ucp.shopping.checkout": {{ID: "dev.ucp.shopping.checkout", Version: "2026-08-25", Spec: "s", Schema: "c"}},
			"dev.ucp.shopping.fulfillment": {{ID: "dev.ucp.shopping.fulfillment", Version: "2026-08-25", Spec: "s", Schema: "f",
				Extends: wire.NewExtends("dev.ucp.shopping.checkout")}},
		},
	}}
	sp, err := NewCacheProfileSource(&fakeCache{profile: prof}).GetStructuredProfile(context.Background(), "store_1")
	if err != nil {
		t.Fatal(err)
	}
	if got := sp.Capabilities()["dev.ucp.shopping.fulfillment"][0].ExtendsList(); len(got) != 1 || got[0] != "dev.ucp.shopping.checkout" {
		t.Errorf("fulfillment extends = %v", got)
	}
	if got := sp.Services()["dev.ucp.shopping"][0].Transport; got != wire.TransportREST {
		t.Errorf("service transport = %q", got)
	}

	// And so intersection prunes the extension when the platform lacks checkout.
	business := ucp.BusinessProfileFromStoreProfile(sp)
	platform := map[string][]wire.Capability{
		"dev.ucp.shopping.fulfillment": {{ID: "dev.ucp.shopping.fulfillment", Version: "2026-08-25"}},
	}
	if got := wireintersect.Capabilities(business.UCP.Capabilities, platform); len(got) != 0 {
		t.Errorf("negotiated %v without the parent checkout", got)
	}
}
