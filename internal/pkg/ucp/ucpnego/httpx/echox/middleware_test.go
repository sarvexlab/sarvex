// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package echox_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/ucpnego/httpx"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/ucpnego/httpx/echox"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wire"
)

// The own profile is served in the form of the generation it declares: a
// 2026-04-08 profile publishes its keys as "signing_keys", a 2026-08-25 one
// as "keys".
func TestProfileHandler_ServesTheDeclaredGenerationsForm(t *testing.T) {
	for version, member := range map[wire.Version]string{"2026-04-08": "signing_keys", "2026-08-25": "keys"} {
		own := &wire.Profile{UCP: wire.UCP{Version: version}, Keys: []wire.JWK{{Kid: "k1", Kty: "EC"}}}
		neg, err := httpx.NewBusinessNegotiator(own, httpx.NewFetcher())
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/.well-known/ucp", nil), rec)
		if err := echox.ProfileHandler(neg)(c); err != nil {
			t.Fatal(err)
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		if _, ok := doc[member]; !ok || len(doc) != 2 {
			t.Errorf("%s profile = %s, want ucp + %q only", version, rec.Body.Bytes(), member)
		}
	}
}
