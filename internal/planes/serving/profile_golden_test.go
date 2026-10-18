// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/golden"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
	"github.com/sarvexlab/sarvex/internal/platform"
)

// TestProfileGoldens freezes the /.well-known/ucp document rendered for a store
// on every supported platform. The profile is one document spanning every
// servable generation (each capability lists the versions it is served at), so
// adding a generation is expected to change it — and the change must be
// reviewed and labelled rather than slip through.
func TestProfileGoldens(t *testing.T) {
	renderer := NewProfileRenderer(testCatalogue(t), "https://serving.test", testLogger())
	for _, p := range platform.All() {
		t.Run(string(p.Name), func(t *testing.T) {
			profile := &StoreProfileData{
				StoreID:       "store-golden",
				MerchantID:    "merchant-golden",
				Platform:      string(p.Name),
				StorefrontURL: "https://shop.example.com",
				SigningKeys:   []wire.JWK{{Kid: "key-1", Kty: "EC", Crv: "P-256", X: "x", Y: "y"}},
			}
			golden.Assert(t, "profile/"+string(p.Name), renderer.RenderProfile("store-golden", "merchant-golden", profile), nil)
		})
	}
}
