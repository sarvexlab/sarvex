// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package wire

import (
	"encoding/json"
	"testing"

	ucpwire "github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wire"
)

// A 2026-04-08 profile publishes its keys as "signing_keys", and only there.
func TestProfileKeysFieldName(t *testing.T) {
	in := FromCanonical(&ucpwire.Profile{UCP: ucpwire.UCP{Version: "2026-04-08"}, Keys: []ucpwire.JWK{{Kid: "k1", Kty: "EC"}}})
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["signing_keys"]; !ok {
		t.Errorf("marshaled profile missing top-level %q field: %s", "signing_keys", b)
	}
	if _, ok := raw["keys"]; ok {
		t.Errorf("marshaled profile must not carry the later generation's %q field: %s", "keys", b)
	}
	var out Profile
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Canonical().FindKey("k1") == nil {
		t.Errorf("keys did not round-trip: %s", b)
	}
}
