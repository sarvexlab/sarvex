// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

//go:build integration

// Profile-doorway tests require a real Redis (Signer.NextSequence's
// sequence counter) so they're gated behind the integration tag and
// REDIS_ADDRESS, matching the Mongo-backed catalog tests' convention.
package sim

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/sarvexlab/sarvex/internal/pkg/devinfra"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
	"github.com/sarvexlab/sarvex/internal/pkg/simstore"
	"github.com/sarvexlab/sarvex/internal/planes/sync/connector"
)

const testPairingCode = "test-pairing-code-abc123"

// fakeConnectorRepo is a read-only in-memory connector.ConnectorRepository
// keyed by store id.
type fakeConnectorRepo struct {
	byStore map[string]*connector.ConnectorModel
}

func (f *fakeConnectorRepo) GetByConnector(_ context.Context, connectorID string) (*connector.ConnectorModel, error) {
	for _, m := range f.byStore {
		if m.ID == connectorID {
			return m, nil
		}
	}
	return nil, connector.ErrNotFound
}

func (f *fakeConnectorRepo) Upsert(context.Context, *connector.ConnectorModel) error {
	return errors.New("fakeConnectorRepo is read-only")
}

func (f *fakeConnectorRepo) GetByStoreID(_ context.Context, storeID string) (*connector.ConnectorModel, error) {
	if m, ok := f.byStore[storeID]; ok {
		return m, nil
	}
	return nil, connector.ErrNotFound
}

func (f *fakeConnectorRepo) ListPaired(context.Context) ([]*connector.ConnectorModel, error) {
	out := make([]*connector.ConnectorModel, 0, len(f.byStore))
	for _, m := range f.byStore {
		out = append(out, m)
	}
	return out, nil
}

var _ connector.ConnectorRepository = (*fakeConnectorRepo)(nil)

func testSecretMgmt(t *testing.T) secretmanagement.SecretManagementService {
	t.Helper()
	svc, err := secretmanagement.New(context.Background(), devinfra.SecretConfig())
	require.NoError(t, err)
	return svc
}

func pairedConnector(t *testing.T, secretMgmt secretmanagement.SecretManagementService, storeID, connectorID string) *connector.ConnectorModel {
	t.Helper()
	encrypted, err := secretMgmt.Encrypt(context.Background(), map[string]any{"hmac_secret": testPairingCode})
	require.NoError(t, err)
	hmacSecret, err := json.Marshal(encrypted)
	require.NoError(t, err)
	return &connector.ConnectorModel{ID: connectorID, StoreID: storeID, HMACSecret: hmacSecret}
}

func connectTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_ADDRESS")
	if addr == "" {
		t.Skip("REDIS_ADDRESS not set; skipping profile-doorway integration tests")
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	require.NoError(t, client.Ping(context.Background()).Err())
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestHTTP_ProfileDoorway_FetchesAndServesRealSignedProfile(t *testing.T) {
	const storeID, connectorID = "store-profile-1", "conn-profile-1"
	secretMgmt := testSecretMgmt(t)

	// Stands in for the sync plane's real GET /connector/profile — verifies
	// the request the way auth.NewHMACResolver actually does, so a passing
	// test proves Signer.DoRaw signs a bodyless GET correctly, not just that
	// something was sent.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/connector/profile", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		assert.Empty(t, body, "profile fetch is a GET; body must be empty")

		message := string(body) + r.Header.Get("X-Sarvex-Timestamp") + r.Header.Get("X-Sarvex-Sequence")
		h := hmac.New(sha256.New, []byte(testPairingCode))
		h.Write([]byte(message))
		want := hex.EncodeToString(h.Sum(nil))
		assert.Equal(t, want, r.Header.Get("X-Sarvex-Signature"))
		assert.Equal(t, storeID+":"+connectorID, r.Header.Get("X-Sarvex-Credential"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ucp":{"catalog":{}}}`))
	}))
	defer upstream.Close()

	repo := &fakeConnectorRepo{byStore: map[string]*connector.ConnectorModel{
		storeID: pairedConnector(t, secretMgmt, storeID, connectorID),
	}}
	signer := NewSigner(upstream.URL, connectTestRedis(t))
	srv := NewServer(simstore.NewMemoryRepository(), "http://localhost:4106", repo, secretMgmt, signer, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/stores/" + storeID + "/.well-known/ucp")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	body, _ := io.ReadAll(resp.Body)
	assert.JSONEq(t, `{"ucp":{"catalog":{}}}`, string(body))
}

func TestHTTP_ProfileDoorway_UnpairedStoreReturns404(t *testing.T) {
	secretMgmt := testSecretMgmt(t)
	repo := &fakeConnectorRepo{byStore: map[string]*connector.ConnectorModel{}}
	signer := NewSigner("http://unused.invalid", connectTestRedis(t))
	srv := NewServer(simstore.NewMemoryRepository(), "http://localhost:4106", repo, secretMgmt, signer, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/stores/never-paired/.well-known/ucp")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHTTP_ProfileDoorway_FallsBackToLastKnownGoodOnUpstreamFailure(t *testing.T) {
	const storeID, connectorID = "store-profile-2", "conn-profile-2"
	secretMgmt := testSecretMgmt(t)

	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ucp":{"catalog":{}}}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	repo := &fakeConnectorRepo{byStore: map[string]*connector.ConnectorModel{
		storeID: pairedConnector(t, secretMgmt, storeID, connectorID),
	}}
	signer := NewSigner(upstream.URL, connectTestRedis(t))
	srv := NewServer(simstore.NewMemoryRepository(), "http://localhost:4106", repo, secretMgmt, signer, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// First call: upstream healthy, populates the cache.
	resp1, err := http.Get(ts.URL + "/stores/" + storeID + "/.well-known/ucp")
	require.NoError(t, err)
	resp1.Body.Close()
	require.Equal(t, http.StatusOK, resp1.StatusCode)

	// Second call: upstream now 500s — must serve the cached body, not 5xx.
	resp2, err := http.Get(ts.URL + "/stores/" + storeID + "/.well-known/ucp")
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	body, _ := io.ReadAll(resp2.Body)
	assert.JSONEq(t, `{"ucp":{"catalog":{}}}`, string(body))
	assert.Equal(t, 2, calls)
}
