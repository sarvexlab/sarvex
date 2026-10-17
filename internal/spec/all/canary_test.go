// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package all_test

import (
	"bytes"
	"slices"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wireadapt"
	"github.com/sarvexlab/sarvex/internal/spec/all"
	"github.com/sarvexlab/sarvex/internal/spec/spectest"
)

// Registering one more generation is new code plus one registration line, and
// changes nothing an existing generation produces: every existing generation
// renders every resource, catalog read and profile document byte-for-byte as
// it does without the canary, is served for the same capabilities, and the
// baseline stays put. Only Latest moves — by design (types: the published
// profile's envelope follows the newest generation).
func TestCanaryGenerationChangesNoExistingOutput(t *testing.T) {
	plain, withCanary := all.New(), spectest.WithCanary()

	if withCanary.Latest() != spectest.CanaryVersion {
		t.Errorf("Latest = %s, want the canary", withCanary.Latest())
	}
	if withCanary.Baseline() != plain.Baseline() {
		t.Errorf("Baseline moved: %s -> %s", plain.Baseline(), withCanary.Baseline())
	}

	resources := map[wireadapt.Resource]any{
		wireadapt.Checkout: checkoutWithPickupAndShipping(),
		wireadapt.Cart:     &base.CartResponse{ID: "cart_1", Currency: "USD"},
		wireadapt.Order:    map[string]any{"id": "ord_1", "checkout_id": "co_1", "permalink_url": "https://shop.example/o/1", "line_items": []any{}, "fulfillment": map[string]any{}, "totals": []any{}, "currency": "USD"},
	}
	page := model.CatalogPage{Products: []model.CatalogProduct{{SKU: "SKU-1", Title: "Mug", PriceMinor: 1500, Currency: "USD", InStock: true}}, Cursor: "c", HasMore: true}
	profile := &wire.Profile{UCP: wire.UCP{Version: "2026-08-25"}, Keys: []wire.JWK{{Kid: "k1", Kty: "EC"}}}

	same := func(what string, a, b []byte, errA, errB error) {
		t.Helper()
		if errA != nil || errB != nil {
			t.Fatalf("%s: %v / %v", what, errA, errB)
		}
		if !bytes.Equal(a, b) {
			t.Errorf("%s changed when the canary was registered:\n%s\n%s", what, a, b)
		}
	}
	for _, v := range plain.All() {
		for r, c := range resources {
			a, errA := plain.Codecs().EncodeResponse(v, r, c, nil)
			b, errB := withCanary.Codecs().EncodeResponse(v, r, c, nil)
			same(string(v)+" "+string(r), a, b, errA, errB)
		}
		a, errA := plain.Codecs().EncodeCatalogSearch(v, page)
		b, errB := withCanary.Codecs().EncodeCatalogSearch(v, page)
		same(string(v)+" catalog search", a, b, errA, errB)

		pg, _ := plain.Generations().Get(v)
		cg, _ := withCanary.Generations().Get(v)
		a, errA = pg.EncodeProfile(profile)
		b, errB = cg.EncodeProfile(profile)
		same(string(v)+" profile", a, b, errA, errB)

		gv, _ := plain.Get(v)
		for _, c := range gv.Serves() {
			if !slices.Contains(withCanary.Serving(c), v) {
				t.Errorf("%s no longer serves %s with the canary registered", v, c)
			}
		}
	}
}
