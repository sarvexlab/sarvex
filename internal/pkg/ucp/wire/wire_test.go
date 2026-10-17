// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package wire

import (
	"encoding/json"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wireintersect"
)

// Capability must satisfy wireintersect.Capability so the generic
// intersection algorithm negotiation.IntersectCapabilities now delegates
// to works against this generation without any per-generation copy.
var _ wireintersect.Capability = Capability{}

func TestParseVersion(t *testing.T) {
	if _, err := ParseVersion(""); err == nil {
		t.Fatal("empty version should error")
	}
	if _, err := ParseVersion("not-a-date"); err == nil {
		t.Fatal("invalid version should error")
	}
	v, err := ParseVersion("2026-04-08")
	if err != nil {
		t.Fatalf("valid version errored: %v", err)
	}
	if v.String() != "2026-04-08" {
		t.Fatalf("String() = %q", v.String())
	}
}

func TestVersionCompare(t *testing.T) {
	older, newer := Version("2026-01-01"), Version("2026-04-08")
	if older.Compare(newer) != -1 {
		t.Error("older.Compare(newer) should be -1")
	}
	if newer.Compare(older) != 1 {
		t.Error("newer.Compare(older) should be 1")
	}
	if older.Compare(older) != 0 {
		t.Error("equal Compare should be 0")
	}
}

func TestExtendsFieldUnmarshalString(t *testing.T) {
	var e ExtendsField
	if err := json.Unmarshal([]byte(`"dev.ucp.shopping.checkout"`), &e); err != nil {
		t.Fatal(err)
	}
	if got := e.List(); len(got) != 1 || got[0] != "dev.ucp.shopping.checkout" {
		t.Fatalf("List() = %v", got)
	}
}

func TestExtendsFieldUnmarshalArray(t *testing.T) {
	var e ExtendsField
	if err := json.Unmarshal([]byte(`["a.b.c","d.e.f"]`), &e); err != nil {
		t.Fatal(err)
	}
	if got := e.List(); len(got) != 2 {
		t.Fatalf("List() = %v", got)
	}
}

func TestExtendsFieldUnmarshalNull(t *testing.T) {
	var e ExtendsField
	if err := json.Unmarshal([]byte(`null`), &e); err != nil {
		t.Fatal(err)
	}
	if e.List() != nil {
		t.Fatalf("List() should be nil, got %v", e.List())
	}
}

func TestExtendsFieldMarshal(t *testing.T) {
	cases := map[string]struct {
		in   ExtendsField
		want string
	}{
		"empty":    {NewExtends(), `null`},
		"single":   {NewExtends("a.b.c"), `"a.b.c"`},
		"multiple": {NewExtends("a.b.c", "d.e.f"), `["a.b.c","d.e.f"]`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			b, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tc.want {
				t.Fatalf("Marshal = %s, want %s", b, tc.want)
			}
		})
	}
}

func TestCapabilityExtensionHelpers(t *testing.T) {
	root := Capability{Extends: NewExtends()}
	if root.IsExtension() {
		t.Error("root capability should not be an extension")
	}
	ext := Capability{Extends: NewExtends("dev.ucp.shopping.checkout")}
	if !ext.IsExtension() {
		t.Error("capability with parent should be an extension")
	}
	if got := ext.ExtendsList(); len(got) != 1 {
		t.Fatalf("ExtendsList = %v", got)
	}
}

func validProfile() *Profile {
	return &Profile{
		UCP: UCP{
			Version: "2026-04-08",
			Capabilities: map[string][]Capability{
				"dev.ucp.shopping.checkout": {{
					Version: "2026-04-08",
					Spec:    "https://ucp.dev/spec",
					Schema:  "https://ucp.dev/schema",
				}},
			},
		},
		Keys: []JWK{{Kid: "k1", Kty: "EC"}},
	}
}

func TestProfileValidate(t *testing.T) {
	if err := validProfile().Validate(); err != nil {
		t.Fatalf("valid profile errored: %v", err)
	}

	var nilP *Profile
	if err := nilP.Validate(); err == nil {
		t.Error("nil profile should error")
	}

	badVersion := validProfile()
	badVersion.UCP.Version = "nope"
	if err := badVersion.Validate(); err == nil {
		t.Error("bad version should error")
	}

	badName := validProfile()
	badName.UCP.Capabilities["nodots"] = badName.UCP.Capabilities["dev.ucp.shopping.checkout"]
	delete(badName.UCP.Capabilities, "dev.ucp.shopping.checkout")
	if err := badName.Validate(); err == nil {
		t.Error("non-reverse-domain capability name should error")
	}

	missingSpec := validProfile()
	caps := missingSpec.UCP.Capabilities["dev.ucp.shopping.checkout"]
	caps[0].Spec = ""
	if err := missingSpec.Validate(); err == nil {
		t.Error("missing spec should error")
	}
}

func TestProfileFindKey(t *testing.T) {
	p := validProfile()
	if got := p.FindKey("k1"); got == nil || got.Kid != "k1" {
		t.Fatalf("FindKey(k1) = %v", got)
	}
	if got := p.FindKey("missing"); got != nil {
		t.Fatalf("FindKey(missing) should be nil, got %v", got)
	}
	var nilP *Profile
	if nilP.FindKey("k1") != nil {
		t.Error("FindKey on nil profile should be nil")
	}
}

func TestProfileJSONRoundTrip(t *testing.T) {
	in := validProfile()
	in.UCP.Capabilities["dev.ucp.shopping.fulfillment"] = []Capability{{
		Version: "2026-04-08",
		Spec:    "https://ucp.dev/spec",
		Schema:  "https://ucp.dev/schema",
		Extends: NewExtends("dev.ucp.shopping.checkout"),
	}}
	in.UCP.PaymentHandlers = map[string][]PaymentHandler{
		"google_pay": {{
			ID:                   "google_pay",
			Version:              "2026-04-08",
			AvailableInstruments: []PaymentInstrument{{Type: "card", Constraints: &InstrumentConstraints{Brands: []string{"visa"}}}},
		}},
	}

	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Profile
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if err := out.Validate(); err != nil {
		t.Fatalf("round-tripped profile failed validation: %v", err)
	}
	ext := out.UCP.Capabilities["dev.ucp.shopping.fulfillment"][0]
	if !ext.IsExtension() || ext.ExtendsList()[0] != "dev.ucp.shopping.checkout" {
		t.Errorf("extends did not round-trip: %v", ext.ExtendsList())
	}
	h := out.UCP.PaymentHandlers["google_pay"][0]
	if len(h.AvailableInstruments) != 1 || h.AvailableInstruments[0].Constraints.Brands[0] != "visa" {
		t.Errorf("payment instrument did not round-trip: %+v", h)
	}
}

// A counterparty's profile may be in any generation, so its keys are found
// under either name. (Before this, a 2026-08-25 profile's "keys" were dropped
// on decode, and every signature from that profile failed to verify.)
func TestProfileUnmarshalReadsEitherKeyName(t *testing.T) {
	for _, doc := range []string{
		`{"ucp":{"version":"2026-08-25"},"keys":[{"kid":"k1","kty":"EC"}]}`,
		`{"ucp":{"version":"2026-04-08"},"signing_keys":[{"kid":"k1","kty":"EC"}]}`,
	} {
		var p Profile
		if err := json.Unmarshal([]byte(doc), &p); err != nil {
			t.Fatal(err)
		}
		if p.FindKey("k1") == nil {
			t.Errorf("key not found in %s", doc)
		}
	}
}

// A root capability has no extends member at all; "extends": null is not a
// valid value (the schema allows a string or an array).
func TestCapabilityRootOmitsExtends(t *testing.T) {
	b, err := json.Marshal(Capability{Version: "2026-08-25", Spec: "s", Schema: "c"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["extends"]; ok {
		t.Errorf("root capability carries extends: %s", b)
	}
}
