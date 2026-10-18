// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package wire

import (
	"encoding/json"
	"testing"

	ucpwire "github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// TestProfileKeysFieldName pins the one deliberate field rename this
// generation makes to the profile document: "signing_keys" (2026-04-08)
// becomes the canonical "keys" (RFC 7517 JWK Set).
func TestProfileKeysFieldName(t *testing.T) {
	in := FromCanonical(&ucpwire.Profile{UCP: ucpwire.UCP{Version: "2026-08-25"}, Keys: []ucpwire.JWK{{Kid: "k1", Kty: "OKP", Crv: "Ed25519"}}})
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["keys"]; !ok {
		t.Errorf("marshaled profile missing top-level %q field: %s", "keys", b)
	}
	if _, ok := raw["signing_keys"]; ok {
		t.Errorf("marshaled profile must not carry the deprecated %q field: %s", "signing_keys", b)
	}
	var out Profile
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Canonical().FindKey("k1") == nil {
		t.Errorf("keys did not round-trip: %s", b)
	}
}
