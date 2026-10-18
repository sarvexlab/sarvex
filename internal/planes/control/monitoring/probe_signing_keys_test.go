// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package monitoring

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	specall "github.com/sarvexlab/sarvex/internal/spec/all"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// The profile probe raised a critical "signing_keys: serving missing" finding
// for every store served at 2026-08-25, which names the key set "keys".
func TestProbeProfile_FindsSigningKeysInEveryServableGeneration(t *testing.T) {
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
			if probeFlagsSigningKeys(t, doc) {
				t.Errorf("%s profile with a key flagged as missing signing keys: %s", v, doc)
			}
		})
	}
}

func TestProbeProfile_FlagsAProfileWithNoSigningKeys(t *testing.T) {
	for _, doc := range []string{`{"ucp":{}}`, `{"ucp":{},"keys":[]}`, `{"ucp":{},"signing_keys":[]}`} {
		if !probeFlagsSigningKeys(t, []byte(doc)) {
			t.Errorf("profile %s not flagged", doc)
		}
	}
}

func probeFlagsSigningKeys(t *testing.T, profile []byte) bool {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(profile)
	}))
	defer srv.Close()

	probe := NewSemanticProbe(srv.URL, testLogger, &fakeMerchantURLProvider{urls: map[string]string{}})
	res := probe.probeProfile(context.Background(), "s1", "woocommerce")
	if res.Error != "" {
		t.Fatalf("probe error: %s", res.Error)
	}
	for _, f := range res.Findings {
		if f.Field == "signing_keys" {
			return true
		}
	}
	return false
}
