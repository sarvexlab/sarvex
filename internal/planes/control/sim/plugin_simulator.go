// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package sim provides an in-process plugin simulator for local development.
// It eliminates the need for a real store installation by driving the
// pairing-completion leg of the plugin<->Sarvex protocol a platform's plugin
// would (HTTP, HMAC-signed):
//
//   - Completes the pairing handshake after BeginPairing is called.
//   - Provisions the pairing with the paired store's simulated credentials for
//     the store's own platform (the platform registry's simulated surface), so
//     the real store-API client (internal/planes/sync/sim runs the persistent
//     simulated store those credentials point at) can genuinely pull from it —
//     see internal/pkg/simstore for the shared, deterministic store_id ->
//     credentials conventions both simulators use.
//
// Heartbeats and connector signals are simulated by the sync plane's own
// plugin simulator (internal/planes/sync/sim) — those endpoints moved off
// the control plane's HTTP surface.
//
// Enabled only when KINDE_ISSUER_URL is unset (dev mode). Never compiled
// out — just never wired in production.
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
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/platform"
)

// pairingEvent carries what BeginPairing produces.
type pairingEvent struct {
	connectorID string
	pairingCode string
	storeID     string
	merchantID  string
	platform    domain.Platform
}

// connectorState tracks active connector info for sending signed requests.
type connectorState struct {
	pairingCode string
	storeID     string
}

// PluginSimulator is an in-process stand-in for a platform plugin's
// pairing-completion leg. It calls the real HTTP endpoint on the control
// plane instead of using the service layer.
type PluginSimulator struct {
	baseURL         string
	simStoreBaseURL string
	httpClient      *http.Client
	log             *slog.Logger
	pairings        chan pairingEvent
	redisClient     *redis.Client
	mu              sync.Mutex
	connectors      map[string]*connectorState // keyed by connector_id
}

// New creates a PluginSimulator configured to call real HTTP endpoints at
// baseURL. Call Run in a goroutine to start it. redisClient backs the HMAC
// signing sequence counter, shared with the sync plane's plugin simulator so
// both processes signing requests for the same connector never collide on
// the replay-dedup key. simStoreBaseURL is the sync plane's persistent fake
// WooCommerce store server (internal/planes/sync/sim); it is embedded into
// the store_credentials this simulator sends at pairing completion so the
// real connector can genuinely pull from that fake store afterward.
func New(baseURL, simStoreBaseURL string, redisClient *redis.Client, log *slog.Logger) *PluginSimulator {
	return &PluginSimulator{
		baseURL:         baseURL,
		simStoreBaseURL: simStoreBaseURL,
		httpClient:      &http.Client{Timeout: 10 * time.Second},
		log:             log,
		pairings:        make(chan pairingEvent, 32),
		redisClient:     redisClient,
		connectors:      make(map[string]*connectorState),
	}
}

// PairingHook returns a function suitable for ConnectorService.SetDevPairingHook.
// Each call notifies the simulator of a new pairing to complete.
func (s *PluginSimulator) PairingHook() func(connectorID, pairingCode, storeID, merchantID string, platform domain.Platform) {
	return func(connectorID, pairingCode, storeID, merchantID string, platform domain.Platform) {
		select {
		case s.pairings <- pairingEvent{connectorID, pairingCode, storeID, merchantID, platform}:
		default:
			s.log.Warn("plugin sim: pairing queue full, dropping", "connector_id", connectorID)
		}
	}
}

// Run drives the simulation loop. Blocks until ctx is cancelled.
func (s *PluginSimulator) Run(ctx context.Context) {
	s.log.Info("plugin simulator started (dev mode)", "base_url", s.baseURL)

	for {
		select {
		case <-ctx.Done():
			s.log.Info("plugin simulator stopped")
			return
		case e := <-s.pairings:
			go s.completePairing(ctx, e)
		}
	}
}

func (s *PluginSimulator) completePairing(ctx context.Context, e pairingEvent) {
	// Brief delay — simulates the merchant entering the code in WooCommerce admin.
	select {
	case <-ctx.Done():
		return
	case <-time.After(1000 * time.Millisecond):
	}

	// Store connector state for future signed requests.
	s.mu.Lock()
	s.connectors[e.connectorID] = &connectorState{
		pairingCode: e.pairingCode,
		storeID:     e.storeID,
	}
	s.mu.Unlock()

	credentials, err := pairingCredentials(s.simStoreBaseURL, e.storeID, e.platform)
	if err != nil {
		s.log.Error("plugin sim: cannot pair", "error", err, "connector_id", e.connectorID)
		return
	}

	// Retry pairing completion with exponential backoff until successful.
	const maxRetries = 1000
	var backoff time.Duration = 500 * time.Millisecond

	for attempt := 1; attempt <= maxRetries; attempt++ {
		sequence, err := s.nextSequence(ctx, e.connectorID)
		if err != nil {
			s.log.Error("plugin sim: failed to allocate sequence", "error", err, "connector_id", e.connectorID)
			return
		}

		// store_credentials points the pairing at the sync plane's persistent
		// simulated store for this store_id (internal/planes/sync/sim), in the
		// shape of the store's own platform (its simulated surface, from the
		// platform registry), deterministically derived so neither simulator
		// has to coordinate out of band — see internal/pkg/simstore. Sending
		// real credentials here (rather than null) is what makes the real
		// store-API client's FetchCatalog/FetchProduct/etc. calls actually
		// succeed against the simulated store once seeded.
		payload := map[string]any{
			"connector_id":      e.connectorID,
			"pairing_code":      e.pairingCode,
			"store_credentials": credentials,
		}

		err = s.sendSignedRequest(ctx, "POST", "/connectors/pairing/complete",
			e.connectorID, e.storeID, e.pairingCode, sequence, payload)
		if err != nil {
			s.log.Warn("plugin sim: pairing completion HTTP failed (will retry)",
				"error", err,
				"connector_id", e.connectorID,
				"attempt", attempt,
				"max_retries", maxRetries)
			if !s.waitWithContext(ctx, backoff) {
				return // context cancelled
			}
			backoff *= 2 // exponential backoff
			continue
		}

		// Success!
		s.log.Info("plugin sim: pairing completed via HTTP (HMAC-signed)",
			"connector_id", e.connectorID,
			"store_id", e.storeID,
			"attempts", attempt)
		return
	}

	// All retries exhausted
	s.log.Error("plugin sim: pairing completion failed after all retries",
		"connector_id", e.connectorID,
		"max_retries", maxRetries)
}

// nextSequence allocates the next HMAC signing sequence number for connectorID
// from a counter shared (via Redis) with the sync plane's plugin simulator —
// both processes sign requests for the same connector, and the dedup key on
// the receiving end (dedup:{store}:{connector}:{sequence}) is endpoint-agnostic.
func (s *PluginSimulator) nextSequence(ctx context.Context, connectorID string) (int64, error) {
	return s.redisClient.Incr(ctx, "sim:seq:"+connectorID).Result()
}

// waitWithContext waits for the given duration or until ctx is cancelled.
func (s *PluginSimulator) waitWithContext(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// sendSignedRequest sends an HMAC-signed POST request to a connector endpoint.
func (s *PluginSimulator) sendSignedRequest(ctx context.Context, method, path, connectorID, storeID, pairingCode string, sequence int64, payload any) error {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	sequenceStr := strconv.FormatInt(sequence, 10)

	// Compute HMAC-SHA256(body + timestamp + sequence)
	message := string(bodyBytes) + timestamp + sequenceStr
	h := hmac.New(sha256.New, []byte(pairingCode))
	h.Write([]byte(message))
	signature := hex.EncodeToString(h.Sum(nil))

	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Sarvex-Signature", signature)
	req.Header.Set("X-Sarvex-Timestamp", timestamp)
	req.Header.Set("X-Sarvex-Sequence", sequenceStr)
	req.Header.Set("X-Sarvex-Credential", storeID+":"+connectorID)

	resp, err := s.httpClient.Do(req)
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

// pairingCredentials is the store_credentials a simulated store on the given
// platform is paired with: that platform's own simulated credentials (its
// simulated surface, from the platform registry). A platform with no simulated
// store cannot be paired by the simulator.
func pairingCredentials(simStoreBaseURL, storeID string, name domain.Platform) (map[string]any, error) {
	p, ok := platform.Get(name)
	if !ok || p.Sim == nil {
		return nil, fmt.Errorf("no simulated store for platform %q", name)
	}
	return p.Sim.Credentials(simStoreBaseURL, storeID), nil
}
