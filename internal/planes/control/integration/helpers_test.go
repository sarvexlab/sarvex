// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// ── HTTP request helpers ───────────────────────────────────────────────────

func apiGet(t *testing.T, path, token string) *http.Response {
	t.Helper()
	return doRequest(t, http.MethodGet, path, token, nil)
}

func apiPost(t *testing.T, path, token string, body any) *http.Response {
	t.Helper()
	return doRequest(t, http.MethodPost, path, token, body)
}

func apiPatch(t *testing.T, path, token string, body any) *http.Response {
	t.Helper()
	return doRequest(t, http.MethodPatch, path, token, body)
}

func apiDelete(t *testing.T, path, token string) *http.Response {
	t.Helper()
	return doRequest(t, http.MethodDelete, path, token, nil)
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

// apiPostOperateAs sends a bearer-authenticated POST with an X-Operate-As header.
func apiPostOperateAs(t *testing.T, path, token, operateAs string, body any) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(http.MethodPost, env.url(path), bodyReader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Operate-As", operateAs)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// ── HMAC signed requests ───────────────────────────────────────────────────

// ── Response assertion helpers ─────────────────────────────────────────────

// decodeJSON asserts the response has the expected status and decodes the body
// into v (if non-nil).
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

// assertStatus asserts only the HTTP status code.
func assertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	require.Equal(t, want, resp.StatusCode,
		"unexpected status %d (body: %s)", resp.StatusCode, string(b))
}

// assertErrorEnvelope asserts the response is a non-2xx with a well-formed
// error envelope and returns the decoded envelope.
func assertErrorEnvelope(t *testing.T, resp *http.Response, wantStatus int) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, wantStatus, resp.StatusCode,
		"unexpected status %d (body: %s)", resp.StatusCode, string(b))

	var env map[string]any
	require.NoError(t, json.Unmarshal(b, &env), "body: %s", string(b))

	errObj, ok := env["error"].(map[string]any)
	require.True(t, ok, "response missing 'error' object; body: %s", string(b))
	require.NotEmpty(t, errObj["code"], "error.code must not be empty")
	require.NotEmpty(t, errObj["message"], "error.message must not be empty")
	return env
}

// assertListEnvelope decodes a list response and verifies the standard envelope
// shape: { "data": [...], "page": { "cursor": null|string, "has_more": bool } }.
func assertListEnvelope(t *testing.T, resp *http.Response) (data []any, hasMore bool, cursor any) {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode,
		"unexpected status %d (body: %s)", resp.StatusCode, string(b))

	var env map[string]any
	require.NoError(t, json.Unmarshal(b, &env), "body: %s", string(b))

	rawData, ok := env["data"]
	require.True(t, ok, "response missing 'data' key; body: %s", string(b))
	data, ok = rawData.([]any)
	require.True(t, ok, "'data' is not an array; body: %s", string(b))

	page, ok := env["page"].(map[string]any)
	require.True(t, ok, "response missing 'page' object; body: %s", string(b))
	hasMore, _ = page["has_more"].(bool)
	cursor = page["cursor"]
	return data, hasMore, cursor
}
