// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package journey_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ── HTTP helpers ──────────────────────────────────────────────────────────────

func apiGet(t *testing.T, path, token string) *http.Response {
	t.Helper()
	return doRequest(t, http.MethodGet, path, token, nil, nil)
}

func apiPost(t *testing.T, path, token string, body any) *http.Response {
	t.Helper()
	return doRequest(t, http.MethodPost, path, token, body, nil)
}

func doRequest(t *testing.T, method, path, token string, body any, extraHeaders map[string]string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, env.controlURL(path), bodyReader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
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

// ── HMAC signing ──────────────────────────────────────────────────────────────

// hmacSignedRequest builds and executes an HMAC-signed request: against the
// sync plane for the plugin's /connector/* routes, the control plane otherwise.
// credential is "<store_id>:<connector_id>", secret is the connector's HMAC key.
func hmacSignedRequest(t *testing.T, method, path, credential string, secret []byte, body any, extraHeaders map[string]string) *http.Response {
	t.Helper()

	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		require.NoError(t, err)
	}

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	sequence := strconv.FormatInt(time.Now().UnixNano(), 10)

	// HMAC-SHA256(body + timestamp + sequence, secret)
	message := string(bodyBytes) + timestamp + sequence
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(message))
	sig := hex.EncodeToString(mac.Sum(nil))

	var bodyReader io.Reader
	if len(bodyBytes) > 0 {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// The plugin's /connector/* routes are the sync plane's; pairing
	// completion is the control plane's.
	url := env.controlURL(path)
	if strings.HasPrefix(path, "/v1/connector/") {
		url = env.syncURL(path)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	require.NoError(t, err)
	if len(bodyBytes) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Sarvex-Signature", sig)
	req.Header.Set("X-Sarvex-Timestamp", timestamp)
	req.Header.Set("X-Sarvex-Sequence", sequence)
	req.Header.Set("X-Sarvex-Credential", credential)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// ── Journey step helpers ──────────────────────────────────────────────────────

// operatorCreateMerchant calls POST /v1/operator/merchants and returns the merchant_id.
func operatorCreateMerchant(t *testing.T, name string) string {
	t.Helper()
	resp := apiPost(t, "/v1/operator/merchants", "operator-token", map[string]any{
		"name":        name,
		"owner_email": fmt.Sprintf("founder+%d@e2e.example.com", time.Now().UnixNano()),
	})
	var result map[string]any
	decodeJSON(t, resp, http.StatusCreated, &result)
	id, ok := result["merchant_id"].(string)
	require.True(t, ok && id != "", "merchant_id missing from response: %v", result)
	return id
}

// operatorInviteMember calls POST /v1/operator/merchants/:id/members.
func operatorInviteMember(t *testing.T, merchantID, email string) {
	t.Helper()
	resp := apiPost(t, "/v1/operator/merchants/"+merchantID+"/members", "operator-token", map[string]any{
		"email": email,
		"role":  "owner",
	})
	var result map[string]any
	decodeJSON(t, resp, http.StatusCreated, &result)
}

// operatorCreateStore calls POST /v1/operator/merchants/:id/stores and returns the store_id.
func operatorCreateStore(t *testing.T, merchantID, name, platform string) string {
	t.Helper()
	resp := apiPost(t, "/v1/operator/merchants/"+merchantID+"/stores", "operator-token", map[string]any{
		"name":     name,
		"platform": platform,
	})
	var result map[string]any
	decodeJSON(t, resp, http.StatusCreated, &result)
	id, ok := result["store_id"].(string)
	require.True(t, ok && id != "", "store_id missing from response: %v", result)
	return id
}

// beginPairing calls POST /v1/stores/:store_id/connectors/pairing and returns connector_id + pairing_code.
func beginPairing(t *testing.T, merchantID, storeID string) (connectorID, pairingCode string) {
	t.Helper()
	token := fmt.Sprintf("merchant:%s", merchantID)
	resp := apiPost(t, "/v1/stores/"+storeID+"/connectors/pairing", token, map[string]any{
		"platform": "woocommerce",
	})
	var result map[string]any
	decodeJSON(t, resp, http.StatusCreated, &result)
	connectorID, _ = result["connector_id"].(string)
	pairingCode, _ = result["pairing_code"].(string)
	require.NotEmpty(t, connectorID, "connector_id missing")
	require.NotEmpty(t, pairingCode, "pairing_code missing")
	return
}

// injectHMACSecret replaces the connector's HMAC secret with secret, stored
// the way pairing stores it (an encrypted {"hmac_secret": ...} config). This
// simulates delivering a known secret to the plugin out of band (in
// production it arrives via QR code or config file on plugin install).
func injectHMACSecret(t *testing.T, connectorID string, secret []byte) {
	t.Helper()
	ctx := context.Background()
	conn, err := env.connectorRepo.GetByConnector(ctx, connectorID)
	require.NoError(t, err)
	enc, err := env.secretMgmt.Encrypt(ctx, map[string]any{"hmac_secret": string(secret)})
	require.NoError(t, err)
	conn.HMACSecret, err = json.Marshal(enc)
	require.NoError(t, err)
	require.NoError(t, env.connectorRepo.Upsert(ctx, conn))
}

// completePairing simulates the plugin calling POST /v1/connectors/pairing/complete with HMAC auth.
// storeCreds is the WooCommerce credential map (store_base_url, consumer_key, consumer_secret, webhook_secret).
func completePairing(t *testing.T, connectorID, storeID, pairingCode string, secret []byte, storeCreds map[string]any) {
	t.Helper()
	credential := storeID + ":" + connectorID
	body := map[string]any{
		"connector_id":      connectorID,
		"pairing_code":      pairingCode,
		"store_credentials": storeCreds,
	}
	resp := hmacSignedRequest(t, http.MethodPost, "/v1/connectors/pairing/complete", credential, secret, body, nil)
	var result map[string]any
	decodeJSON(t, resp, http.StatusOK, &result)
	require.Equal(t, "healthy", result["status"], "connector should be healthy after pairing: %v", result)
}

// sendSignal simulates the plugin sending a product change signal.
func sendSignal(t *testing.T, connectorID, storeID string, secret []byte, entityID, entityType string) {
	t.Helper()
	credential := storeID + ":" + connectorID
	body := map[string]any{
		"entity_type": entityType,
		"entity_id":   entityID,
		"occurred_at": time.Now().UTC().Format(time.RFC3339),
		"sequence":    time.Now().UnixNano(),
		"change":      "updated",
	}
	resp := hmacSignedRequest(t, http.MethodPost, "/v1/connector/signals", credential, secret, body, nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode, "signal should be accepted")
	resp.Body.Close()
}

// waitUntil polls fn until it returns true or timeout expires.
func waitUntil(t *testing.T, timeout time.Duration, desc string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", desc)
}

// connectorIsHealthy checks the connector status via the API.
func connectorIsHealthy(t *testing.T, merchantID, connectorID string) bool {
	t.Helper()
	token := fmt.Sprintf("merchant:%s", merchantID)
	resp := apiGet(t, "/v1/connectors/"+connectorID, token)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return false
	}
	var result map[string]any
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false
	}
	return result["status"] == "healthy"
}

// getReadiness returns the readiness overall score for a store.
func getReadiness(t *testing.T, merchantID, storeID string) (int, bool) {
	t.Helper()
	token := fmt.Sprintf("merchant:%s", merchantID)
	resp := apiGet(t, "/v1/stores/"+storeID+"/catalog/readiness", token)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return 0, false
	}
	var result map[string]any
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, false
	}
	if overall, ok := result["overall"].(float64); ok {
		return int(overall), true
	}
	return 0, true
}

// listRemediations returns the list of available remediations for a store.
func listRemediations(t *testing.T, merchantID, storeID string) []map[string]any {
	t.Helper()
	token := fmt.Sprintf("merchant:%s", merchantID)
	resp := apiGet(t, "/v1/stores/"+storeID+"/catalog/remediations", token)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil
	}
	var result struct {
		Data []map[string]any `json:"data"`
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}
	return result.Data
}

// applyRemediations calls POST /v1/stores/:store_id/catalog/remediations/apply.
func applyRemediations(t *testing.T, merchantID, storeID string, remediationIDs []string) map[string]any {
	t.Helper()
	token := fmt.Sprintf("merchant:%s", merchantID)
	resp := apiPost(t, "/v1/stores/"+storeID+"/catalog/remediations/apply", token, map[string]any{
		"remediation_ids": remediationIDs,
	})
	var result map[string]any
	decodeJSON(t, resp, http.StatusOK, &result)
	return result
}

// fetchProfile calls GET /v1/connector/profile with HMAC auth.
func fetchProfile(t *testing.T, connectorID, storeID string, secret []byte) ([]byte, int) {
	t.Helper()
	credential := storeID + ":" + connectorID
	resp := hmacSignedRequest(t, http.MethodGet, "/v1/connector/profile", credential, secret, nil, nil)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode
}

// merchantToken returns a dynamic bearer token for any merchant.
func merchantToken(merchantID string) string {
	return fmt.Sprintf("merchant:%s", merchantID)
}

// operatorResync calls POST /v1/connectors/:id/resync to trigger a full catalog import.
func operatorResync(t *testing.T, merchantID, connectorID string) {
	t.Helper()
	resp := apiPost(t, "/v1/connectors/"+connectorID+"/resync", merchantToken(merchantID), nil)
	require.True(t, resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusOK,
		"resync expected 202/200 got %d", resp.StatusCode)
	resp.Body.Close()
}

// storeCredentials builds the WooCommerce credential map pointing to the mock.
func storeCredentials(wcURL, merchantID string) map[string]any {
	return map[string]any{
		"store_base_url":  wcURL,
		"consumer_key":    "ck_e2e_test_" + merchantID,
		"consumer_secret": "cs_e2e_test_" + merchantID,
		"webhook_secret":  "wh_e2e_test_" + merchantID,
		"store_currency":  "USD",
		"api_version":     "v3",
		"merchant_id":     merchantID,
	}
}
