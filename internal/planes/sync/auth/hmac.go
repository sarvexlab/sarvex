// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"

	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
	"github.com/sarvexlab/sarvex/internal/planes/sync/connector"
	"github.com/sarvexlab/sarvex/internal/planes/sync/domain"
)

// NewHMACResolver returns an HMACResolver that verifies HMAC-signed connector requests.
// Validates X-Sarvex-Signature header per sarvex-v1-control-plane.yaml securitySchemes.hmacSignature.
// Credential format: <store_id>:<connector_id>
// Signature is HMAC-SHA256(body + timestamp + sequence, signing_secret)
// Prevents replays by checking sequence numbers against Redis dedup store.
func NewHMACResolver(log *slog.Logger,
	connectorSvc connector.Service,
	redisClient *redis.Client,
	secretMgmt secretmanagement.SecretManagementService,
	dedupTTL time.Duration) HMACResolver {
	return func(c echo.Context, signature string) (domain.Principal, error) {
		ctx := c.Request().Context()

		if signature == "" {
			return domain.Principal{}, errors.New("empty hmac signature")
		}

		// Extract required headers
		timestamp := c.Request().Header.Get("X-Sarvex-Timestamp")
		sequence := c.Request().Header.Get("X-Sarvex-Sequence")
		credential := c.Request().Header.Get("X-Sarvex-Credential")

		if timestamp == "" {
			return domain.Principal{}, errors.New("missing X-Sarvex-Timestamp header")
		}
		if sequence == "" {
			return domain.Principal{}, errors.New("missing X-Sarvex-Sequence header")
		}
		if credential == "" {
			return domain.Principal{}, errors.New("missing X-Sarvex-Credential header")
		}

		// Parse credential format: <store_id>:<connector_id>
		parts := strings.SplitN(credential, ":", 2)
		if len(parts) != 2 {
			return domain.Principal{}, errors.New("invalid credential format: expected <store_id>:<connector_id>")
		}
		storeID := domain.StoreID(parts[0])
		connectorID := parts[1]

		if storeID == "" || connectorID == "" {
			return domain.Principal{}, errors.New("credential contains empty store_id or connector_id")
		}

		// Check sequence number for replays (before expensive connector lookup)
		dedupKey := fmt.Sprintf("dedup:%s:%s:%s", storeID, connectorID, sequence)
		exists, err := redisClient.Exists(ctx, dedupKey).Result()
		if err != nil {
			log.Error("dedup check failed", "key", dedupKey, "error", err)
			return domain.Principal{}, errors.New("replay detection unavailable")
		}
		if exists > 0 {
			log.Warn("duplicate sequence detected (replay)",
				"store_id", storeID,
				"connector_id", connectorID,
				"sequence", sequence)
			return domain.Principal{}, errors.New("duplicate sequence: replay detected")
		}

		// Buffer request body
		body, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return domain.Principal{}, fmt.Errorf("failed to read body: %w", err)
		}
		// Restore body so downstream handlers can read it
		c.Request().Body = io.NopCloser(strings.NewReader(string(body)))

		// Look up connector by connector_id to get HMAC secret
		conn, err := connectorSvc.GetByConnector(ctx, connectorID)
		if err != nil {
			return domain.Principal{}, fmt.Errorf("connector lookup failed: %w", err)
		}

		// Verify the connector belongs to the requested store
		if domain.StoreID(conn.StoreID) != storeID {
			return domain.Principal{}, errors.New("connector does not belong to requested store")
		}

		// Decrypt the stored HMAC secret (encrypted EncryptedConfig JSON).
		var encryptedConfig secretmanagement.EncryptedConfig
		if err := json.Unmarshal(conn.HMACSecret, &encryptedConfig); err != nil {
			log.Error("failed to unmarshal encrypted hmac secret",
				"store_id", storeID,
				"connector_id", connectorID,
				"error", err)
			return domain.Principal{}, errors.New("invalid encrypted hmac secret format")
		}

		decrypted, err := secretMgmt.Decrypt(ctx, &encryptedConfig)
		if err != nil {
			log.Error("failed to decrypt hmac secret",
				"store_id", storeID,
				"connector_id", connectorID,
				"error", err)
			return domain.Principal{}, errors.New("hmac secret decryption failed")
		}

		// Extract the plaintext pairing code from decrypted config.
		pairingCodeAny, ok := decrypted["hmac_secret"]
		if !ok {
			log.Error("hmac_secret key missing in decrypted config",
				"store_id", storeID,
				"connector_id", connectorID)
			return domain.Principal{}, errors.New("hmac_secret not found in decrypted config")
		}

		pairingCode, ok := pairingCodeAny.(string)
		if !ok {
			log.Error("hmac_secret is not a string",
				"store_id", storeID,
				"connector_id", connectorID)
			return domain.Principal{}, errors.New("invalid hmac_secret type")
		}

		// Reconstruct message: body + timestamp + sequence
		message := string(body) + timestamp + sequence

		// Compute expected HMAC-SHA256 using decrypted pairing code.
		h := hmac.New(sha256.New, []byte(pairingCode))
		h.Write([]byte(message))
		expectedHMAC := hex.EncodeToString(h.Sum(nil))

		// Provided signature is in hex format
		providedHMAC := strings.TrimSpace(signature)

		// Constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(expectedHMAC), []byte(providedHMAC)) != 1 {
			log.Warn("hmac signature mismatch",
				"store_id", storeID,
				"connector_id", connectorID,
				"sequence", sequence)
			return domain.Principal{}, errors.New("hmac signature verification failed")
		}

		// Mark sequence as seen (with TTL expiration for replay prevention)
		if err := redisClient.Set(ctx, dedupKey, "1", dedupTTL).Err(); err != nil {
			log.Error("failed to mark sequence as seen", "key", dedupKey, "error", err)
			// Log but don't fail - this is a safety-net issue, not an auth failure
		}

		// Return Principal with store_id, connector_id, and merchant_id (merchant_id comes from the connector record)
		return domain.Principal{
			StoreID:     storeID,
			ConnectorID: connectorID,
			MerchantID:  conn.MerchantID,
		}, nil
	}
}
