// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	specall "github.com/sarvexlab/sarvex/internal/spec/all"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// The harness checked only for "signing_keys", 2026-04-08's name for the key
// set; 2026-08-25 publishes "keys", so every store served at the latest
// generation failed profile_has_signing_keys with its key right there.
func TestProfileEndpoint_FindsSigningKeysInEveryServableGeneration(t *testing.T) {
	reg := specall.Default()
	for _, v := range reg.Servable() {
		t.Run(string(v), func(t *testing.T) {
			gen, ok := reg.Generations().Get(v)
			if !ok {
				t.Fatalf("no generation for %s", v)
			}
			doc, err := gen.EncodeProfile(&wire.Profile{
				UCP:  wire.UCP{Version: wire.Version(v)},
				Keys: []wire.JWK{{Kid: "k1", Kty: "EC", Crv: "P-256", X: "x", Y: "y"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if failed := profileRequirementFailed(t, doc, "profile_has_signing_keys"); failed {
				t.Errorf("%s profile with a key reported as missing signing keys: %s", v, doc)
			}
		})
	}
}

func TestProfileEndpoint_FailsWhenNoSigningKeysArePublished(t *testing.T) {
	for _, doc := range []string{`{"ucp":{}}`, `{"ucp":{},"keys":[]}`, `{"ucp":{},"signing_keys":[]}`} {
		if !profileRequirementFailed(t, []byte(doc), "profile_has_signing_keys") {
			t.Errorf("profile %s passed profile_has_signing_keys", doc)
		}
	}
}

func profileRequirementFailed(t *testing.T, profile []byte, requirement string) bool {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(profile)
	}))
	defer srv.Close()

	h := NewHTTPTestHarness(srv.URL, testLogger())
	results := &TestResults{}
	if err := h.testProfileEndpoint(context.Background(), domain.StoreID("s1"), domain.Platform("woocommerce"), results); err != nil {
		t.Fatal(err)
	}
	for _, f := range results.Failed {
		if f.Requirement == requirement {
			return true
		}
	}
	return false
}
