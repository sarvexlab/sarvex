// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build scenarios

package scenarios_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/account"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/connector"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// HTTP request helpers

func apiGet(t *testing.T, path, token string) *http.Response {
	t.Helper()
	return doRequest(t, http.MethodGet, path, token, nil)
}

func apiPost(t *testing.T, path, token string, body any) *http.Response {
	t.Helper()
	return doRequest(t, http.MethodPost, path, token, body)
}

func doRequest(t *testing.T, method, path, token string, body any) *http.Response {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, env.url(path), bodyReader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, wantStatus int, v any) {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, wantStatus, resp.StatusCode,
		"unexpected status %d (body: %s)", resp.StatusCode, string(b))
	if v != nil {
		require.NoError(t, json.Unmarshal(b, v),
			"failed to decode response body: %s", string(b))
	}
}

// Scenario helpers

func createAccount(t *testing.T, merchantID domain.MerchantID, name string) {
	t.Helper()
	now := time.Now()
	err := env.accountRepo.Upsert(context.Background(), &account.AccountModel{
		ID:        string(merchantID),
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)
}

func createStore(t *testing.T, merchantID domain.MerchantID, platform domain.Platform, displayName string, token string) domain.StoreID {
	t.Helper()

	resp := apiPost(t, "/v1/stores", token, map[string]any{
		"display_name": displayName,
		"platform":     platform,
	})
	var result map[string]any
	decodeJSON(t, resp, http.StatusCreated, &result)

	storeID := result["store_id"].(string)
	return domain.StoreID(storeID)
}

func beginPairing(t *testing.T, storeID domain.StoreID, token string) (connectorID, pairingCode string) {
	t.Helper()

	resp := apiPost(t, "/v1/stores/"+string(storeID)+"/connectors/pairing", token, map[string]any{
		"platform": "woocommerce",
	})
	var result map[string]any
	decodeJSON(t, resp, http.StatusCreated, &result)

	connectorID, _ = result["connector_id"].(string)
	pairingCode, _ = result["pairing_code"].(string)
	return
}

// seedConnectorWithSecret stands in for a completed pairing: it writes a
// healthy connector whose HMAC secret the resolver can actually verify against.
//
// The secret is stored the way BeginPairing stores it — encrypted through
// Vault, with the marshalled EncryptedConfig reference in the record —
// not as raw bytes. The HMAC resolver unmarshals that reference and decrypts
// it, so a raw secret fails to parse and every signed scenario request 401s.
func seedConnectorWithSecret(t *testing.T, merchantID domain.MerchantID, storeID domain.StoreID, platform domain.Platform, secret []byte) string {
	t.Helper()
	connectorID := "test-conn-" + uuid.New().String()

	encrypted, err := env.secretMgmt.Encrypt(context.Background(), map[string]any{
		"hmac_secret": string(secret),
	})
	require.NoError(t, err, "encrypt hmac secret")
	secretRef, err := json.Marshal(encrypted)
	require.NoError(t, err, "marshal encrypted hmac secret")

	now := time.Now()
	err = env.connectorRepo.Upsert(context.Background(), &connector.ConnectorModel{
		ID:         connectorID,
		StoreID:    string(storeID),
		MerchantID: string(merchantID),
		Platform:   platform,
		Status:     domain.ConnectorStatusHealthy,
		SyncModel:  domain.SyncModelRealtime,
		HMACSecret: secretRef,
		CreatedAt:  now,
		UpdatedAt:  now,
	})
	require.NoError(t, err)
	return connectorID
}
