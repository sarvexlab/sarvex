// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package generation_test

// This file proves the core claim of the UCP multi-version plan: adding a
// second generation to the registry requires no change to the registry,
// SniffVersion, or any existing generation's code — only registering a
// new Generation implementation. Both v20260408 and v20260825 coexist
// here; negotiation code that resolves through the registry (rather than
// hardcoding one generation) automatically dispatches to the right one
// per profile.

import (
	"encoding/json"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/generation"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
	v20260408 "github.com/sarvexlab/sarvex/internal/spec/v20260408/profile"
	v20260825 "github.com/sarvexlab/sarvex/internal/spec/v20260825/profile"
)

func dualRegistry() *generation.Registry {
	reg := generation.NewRegistry()
	reg.Register(v20260408.New())
	reg.Register(v20260825.New())
	return reg
}

func TestRegistry_BothGenerationsCoexist(t *testing.T) {
	reg := dualRegistry()

	if got := len(reg.All()); got != 2 {
		t.Fatalf("All() has %d generations, want 2", got)
	}
	if _, ok := reg.Get("2026-04-08"); !ok {
		t.Error("v2026-04-08 not registered")
	}
	if _, ok := reg.Get("2026-08-25"); !ok {
		t.Error("v2026-08-25 not registered")
	}
}

func TestRegistry_DecodeProfile_SelectsRightGenerationPerProfile(t *testing.T) {
	reg := dualRegistry()

	oldProfile := json.RawMessage(`{"ucp":{"version":"2026-04-08"},"signing_keys":[{"kid":"k1","kty":"EC"}]}`)
	newProfile := json.RawMessage(`{"ucp":{"version":"2026-08-25"},"keys":[{"kid":"k1","kty":"OKP","crv":"Ed25519"}]}`)

	for _, tc := range []struct {
		name string
		raw  json.RawMessage
		want string
	}{{"old", oldProfile, "2026-04-08"}, {"new", newProfile, "2026-08-25"}} {
		p, err := reg.DecodeProfile(tc.raw)
		if err != nil {
			t.Fatalf("DecodeProfile(%s) error = %v", tc.name, err)
		}
		if string(p.UCP.Version) != tc.want {
			t.Errorf("%s profile resolved to %v, want %v", tc.name, p.UCP.Version, tc.want)
		}
		if p.FindKey("k1") == nil {
			t.Errorf("%s profile's keys did not decode: %+v", tc.name, p.Keys)
		}
	}
}

// Each generation writes a profile in its own document form: the same keys
// under that generation's own member name, and only that one.
func TestGeneration_EncodeProfile_UsesItsOwnKeyName(t *testing.T) {
	p := &wire.Profile{UCP: wire.UCP{Version: "2026-08-25"}, Keys: []wire.JWK{{Kid: "k1", Kty: "EC"}}}
	for _, tc := range []struct {
		gen         generation.Generation
		key, absent string
	}{{v20260408.New(), "signing_keys", "keys"}, {v20260825.New(), "keys", "signing_keys"}} {
		b, err := tc.gen.EncodeProfile(p)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		if _, ok := doc[tc.key]; !ok {
			t.Errorf("%s: no %q in %s", tc.gen.Version(), tc.key, b)
		}
		if _, ok := doc[tc.absent]; ok {
			t.Errorf("%s: unexpected %q in %s", tc.gen.Version(), tc.absent, b)
		}
		back, err := tc.gen.DecodeProfile(b)
		if err != nil || back.FindKey("k1") == nil {
			t.Errorf("%s: profile did not round-trip: %v %+v", tc.gen.Version(), err, back)
		}
	}
}

// TestRegistry_AddingAGenerationDoesNotAffectExisting is the literal
// claim from the version registry's own doc comment ("adding a new spec
// version means adding a constant, an entry, and a new package --
// existing entries and packages never change"): a registry with only
// v2026-04-08 registered behaves identically whether or not v2026-08-25
// exists elsewhere in the binary.
func TestRegistry_AddingAGenerationDoesNotAffectExisting(t *testing.T) {
	soloReg := generation.NewRegistry()
	soloReg.Register(v20260408.New())

	oldProfile := json.RawMessage(`{"ucp":{"version":"2026-04-08"}}`)
	soloResult, err := soloReg.DecodeProfile(oldProfile)
	if err != nil {
		t.Fatalf("solo registry DecodeProfile error = %v", err)
	}

	dualResult, err := dualRegistry().DecodeProfile(oldProfile)
	if err != nil {
		t.Fatalf("dual registry DecodeProfile error = %v", err)
	}

	if soloResult.UCP.Version != dualResult.UCP.Version {
		t.Errorf("solo=%v dual=%v: registering v2026-08-25 changed v2026-04-08 decode behavior",
			soloResult.UCP.Version, dualResult.UCP.Version)
	}
}
