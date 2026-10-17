// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api_test

// The sync plane's HTTP surface is the connector-ingest edge and nothing
// else: three HMAC-signed routes the WooCommerce plugin calls
// (sarvex-v1-architecture.md). This file tests those.
//
// It replaces a verbatim copy of the *control* plane's route tests that had
// been left in this directory: same test names, same helpers, importing
// control/api and asserting a route table that predates the plane split.
// It failed because the control plane no longer serves /connector/* — those
// routes live here — and it would never have caught a regression in the
// sync handler, since it never constructed one.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sarvexlab/sarvex/internal/planes/sync/connector"

	"github.com/sarvexlab/sarvex/internal/platform/woocommerce"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncapi "github.com/sarvexlab/sarvex/internal/planes/sync/api"
	"github.com/sarvexlab/sarvex/internal/planes/sync/domain"
)

const validSignature = "valid-signature"

// stubIngestService is a controllable sync connector.Service.
type stubIngestService struct {
	signalErr    error
	heartbeatErr error
	profileBody  []byte
	profileEtag  string
	notModified  bool
	profileErr   error
}

func (s *stubIngestService) AcceptSignal(_ context.Context, _ string, _ domain.ConnectorSignal) error {
	return s.signalErr
}

func (s *stubIngestService) RecordHeartbeat(_ context.Context, _ string, _ domain.Heartbeat) error {
	return s.heartbeatErr
}

func (s *stubIngestService) FetchProfile(_ context.Context, _ string, _ string) ([]byte, string, bool, error) {
	return s.profileBody, s.profileEtag, s.notModified, s.profileErr
}

func (s *stubIngestService) GetByConnector(_ context.Context, id string) (domain.Connector, error) {
	return domain.Connector{ConnectorID: id}, nil
}

func (s *stubIngestService) GetStoreCredentials(_ context.Context, _ string) (map[string]any, domain.Platform, error) {
	return map[string]any{}, woocommerce.PlatformName, nil
}

func newSyncTestEcho(svc *stubIngestService) *echo.Echo {
	deps := syncapi.Dependencies{
		Connector: svc,
		ResolveHMAC: func(_ echo.Context, sig string) (domain.Principal, error) {
			if sig != validSignature {
				return domain.Principal{}, errors.New("bad signature")
			}
			return domain.Principal{MerchantID: "merch-1", StoreID: "store-1", ConnectorID: "conn-1"}, nil
		},
	}
	e := echo.New()
	if err := syncapi.New(deps).SetupRoutes("/v1", e.Group("/v1")); err != nil {
		panic(err)
	}
	return e
}

func do(e *echo.Echo, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func signed() map[string]string {
	return map[string]string{"X-Sarvex-Signature": validSignature}
}

// syncRoutes is the sync plane's entire HTTP surface.
var syncRoutes = []struct {
	method string
	path   string
	body   string
}{
	{http.MethodPost, "/v1/connector/signals", `{"entity_type":"product","entity_id":"p1","occurred_at":"2026-01-01T00:00:00Z","sequence":1,"change":"updated"}`},
	{http.MethodPost, "/v1/connector/heartbeat", `{"plugin_version":"1.0.0","platform_version":"8.0","site_url":"https://shop.example","healthy":true}`},
	{http.MethodGet, "/v1/connector/profile", ""},
}

// TestSyncRoutes_AreRegistered asserts every connector-ingest route exists
// — a 404 here means a route was dropped or renamed.
func TestSyncRoutes_AreRegistered(t *testing.T) {
	e := newSyncTestEcho(&stubIngestService{profileBody: []byte(`{"ucp":{}}`), profileEtag: `"v1"`})

	for _, r := range syncRoutes {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			rec := do(e, r.method, r.path, r.body, signed())
			assert.NotEqual(t, http.StatusNotFound, rec.Code, "route not registered")
		})
	}
}

// TestSyncRoutes_RequireHMAC asserts every ingest route rejects an
// unsigned request. These are the plugin's only way in, and they are
// HMAC-authenticated rather than bearer-authenticated (sarvex-v1-architecture.md A10.5) —
// an unsigned call must never reach a handler.
func TestSyncRoutes_RequireHMAC(t *testing.T) {
	e := newSyncTestEcho(&stubIngestService{})

	for _, r := range syncRoutes {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			rec := do(e, r.method, r.path, r.body, nil)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assertErrorEnvelope(t, rec)
		})
	}
}

// TestSyncRoutes_RejectInvalidSignature covers the other half: a signature
// that is present but does not verify.
func TestSyncRoutes_RejectInvalidSignature(t *testing.T) {
	e := newSyncTestEcho(&stubIngestService{})

	for _, r := range syncRoutes {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			rec := do(e, r.method, r.path, r.body, map[string]string{"X-Sarvex-Signature": "forged"})
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}
}

func TestFetchProfile_ReturnsBodyAndEtag(t *testing.T) {
	body := []byte(`{"ucp":{"version":"2026-04-08"}}`)
	e := newSyncTestEcho(&stubIngestService{profileBody: body, profileEtag: `"abc123"`})

	rec := do(e, http.MethodGet, "/v1/connector/profile", "", signed())
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, string(body), rec.Body.String())
	assert.Equal(t, `"abc123"`, rec.Header().Get("ETag"))
}

// No servable profile yet answers 409 (sarvex-v1-control-plane.yaml), never a
// 200: the plugin serves a 200's body verbatim at /.well-known/ucp, so an empty
// one would replace its last-known-good copy with nothing. (It used to be an
// empty 200.)
func TestFetchProfile_NotReadyIs409(t *testing.T) {
	e := newSyncTestEcho(&stubIngestService{profileErr: connector.ErrProfileNotReady})

	rec := do(e, http.MethodGet, "/v1/connector/profile", "", signed())
	assert.Equal(t, http.StatusConflict, rec.Code)
	assertErrorEnvelope(t, rec)
}

// TestFetchProfile_NotModified covers the plugin's conditional fetch: the
// doorway caches last-known-good and revalidates by ETag.
func TestFetchProfile_NotModified(t *testing.T) {
	e := newSyncTestEcho(&stubIngestService{notModified: true, profileEtag: `"abc123"`})

	headers := signed()
	headers["If-None-Match"] = `"abc123"`
	rec := do(e, http.MethodGet, "/v1/connector/profile", "", headers)
	assert.Equal(t, http.StatusNotModified, rec.Code)
}

// TestAcceptSignal_Accepted and the heartbeat equivalent cover the plugin's
// two write paths on the happy path: a well-formed signed request is accepted
// with 204 and no body. Thin events, fat pull — the response carries nothing
// because Sarvex pulls the entity itself (sarvex-v1-architecture.md).
func TestAcceptSignal_Accepted(t *testing.T) {
	e := newSyncTestEcho(&stubIngestService{})

	rec := do(e, http.MethodPost, "/v1/connector/signals",
		`{"entity_type":"product","entity_id":"sku-123","occurred_at":"2026-01-01T00:00:00Z","sequence":42,"change":"updated"}`,
		signed())
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestRecordHeartbeat_Accepted(t *testing.T) {
	e := newSyncTestEcho(&stubIngestService{})

	rec := do(e, http.MethodPost, "/v1/connector/heartbeat",
		`{"plugin_version":"1.2.3","platform_version":"6.5","site_url":"https://shop.example.com","healthy":true}`,
		signed())
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
}

// TestIngestRoutes_RejectIncompletePayload covers the validation half: a
// correctly signed request whose body is missing a required field is a 422,
// not a 500 and not a silent accept. A signal without an entity_id would
// otherwise queue a pull for nothing.
func TestIngestRoutes_RejectIncompletePayload(t *testing.T) {
	cases := []struct {
		name string
		path string
		body string
	}{
		{"signal without entity_id", "/v1/connector/signals", `{"entity_type":"product"}`},
		{"heartbeat without site_url", "/v1/connector/heartbeat", `{"plugin_version":"1.0"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newSyncTestEcho(&stubIngestService{})

			rec := do(e, http.MethodPost, tc.path, tc.body, signed())
			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			assertErrorEnvelope(t, rec)
		})
	}
}

func assertErrorEnvelope(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&env))
	assert.NotEmpty(t, env.Error.Code, "error.code must be present")
	assert.NotEmpty(t, env.Error.Message, "error.message must be present")
}
