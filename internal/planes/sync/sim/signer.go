// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sim

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
	"time"

	"github.com/redis/go-redis/v9"
)

// Signer sends HMAC-signed requests to the sync plane's connector-ingest
// endpoints exactly as the real WooCommerce plugin would (see
// plugin/woocommerce/src/HMAC.php and internal/planes/sync/auth/hmac.go),
// and allocates the signing sequence number from the Redis counter shared
// with every other simulator process signing for the same connector
// (sim:seq:{connector_id}) — so heartbeats, signals, and CLI mutations for
// one connector never collide on the receiving dedup key regardless of
// which process sent them.
type Signer struct {
	baseURL    string
	httpClient *http.Client
	redis      *redis.Client
}

// NewSigner creates a Signer that sends to baseURL (the sync plane's HTTP
// root, e.g. http://localhost:8082/v1).
func NewSigner(baseURL string, redisClient *redis.Client) *Signer {
	return &Signer{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		redis:      redisClient,
	}
}

// NextSequence allocates the next HMAC signing sequence number for connectorID.
func (s *Signer) NextSequence(ctx context.Context, connectorID string) (int64, error) {
	return s.redis.Incr(ctx, "sim:seq:"+connectorID).Result()
}

// Send sends an HMAC-signed request to a connector endpoint (path is
// relative to baseURL, e.g. "/connector/signals"), checks for a successful
// status, and discards the response body — the fire-and-forget case
// (heartbeats, signals) where the caller only needs to know it landed.
func (s *Signer) Send(ctx context.Context, method, path, connectorID, storeID, pairingCode string, sequence int64, payload any) error {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	resp, err := s.DoRaw(ctx, method, path, connectorID, storeID, pairingCode, sequence, bodyBytes)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// DoRaw sends an HMAC-signed request with a raw body (nil/empty for a
// bodyless request, e.g. the profile doorway's GET) and returns the live
// HTTP response for the caller to inspect — Send's discard-the-body
// semantics aren't enough where the response itself is the payload.
func (s *Signer) DoRaw(ctx context.Context, method, path, connectorID, storeID, pairingCode string, sequence int64, body []byte) (*http.Response, error) {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	sequenceStr := strconv.FormatInt(sequence, 10)

	// Matches the server's verification exactly (auth.NewHMACResolver):
	// message = body + timestamp + sequence, where body is whatever bytes
	// were actually sent — empty string for a bodyless request.
	message := string(body) + timestamp + sequenceStr
	h := hmac.New(sha256.New, []byte(pairingCode))
	h.Write([]byte(message))
	signature := hex.EncodeToString(h.Sum(nil))

	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Sarvex-Signature", signature)
	req.Header.Set("X-Sarvex-Timestamp", timestamp)
	req.Header.Set("X-Sarvex-Sequence", sequenceStr)
	req.Header.Set("X-Sarvex-Credential", storeID+":"+connectorID)

	return s.httpClient.Do(req)
}
