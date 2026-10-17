// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package sim provides an in-process plugin simulator for local development,
// plus (server.go, cli.go) a persistent, controllable simulated store the real
// connectors can genuinely pull from — served through every registered
// platform's REST surface (internal/platform/<name>/sim) over the shared
// internal/pkg/simstore model. It eliminates the need for a real store
// installation by driving the heartbeat leg of the plugin<->Sarvex protocol
// the plugin would (HTTP, HMAC-signed):
//
//   - Sends periodic heartbeats so the connector stays healthy.
//
// Catalog-change signals are no longer sent on a timer with fabricated
// entity ids — a real merchant's plugin never invents changes on its own,
// something in the store changes first. Signals are instead sent by the
// `sim seed/mutate/fix` CLI (cli.go) against this package's persistent
// Store, referencing real SKUs a real FetchProduct/FetchPricing/
// FetchInventory pull can resolve.
//
// Pairing completion is simulated separately by the control plane's own
// plugin simulator (internal/planes/control/sim) — that endpoint stayed on
// the control plane's HTTP surface. Since the two simulators run in separate
// processes, this one discovers newly-paired connectors by polling the
// shared connector store (via ConnectorService.DevListConnectors) rather
// than an in-process callback.
//
// Enabled only when KINDE_ISSUER_URL is unset (dev mode). Never compiled
// out — just never wired in production.
package sim

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/sarvexlab/sarvex/internal/planes/sync/connector"
	"github.com/sarvexlab/sarvex/internal/planes/sync/domain"
)

// connectorState tracks active connector info for sending signed requests.
type connectorState struct {
	pairingCode string
	storeID     string
}

// PluginSimulator is an in-process stand-in for the WooCommerce plugin's
// heartbeat leg. It calls real HTTP endpoints on the sync plane instead of
// using the service layer.
type PluginSimulator struct {
	baseURL      string
	signer       *Signer
	log          *slog.Logger
	connectorSvc *connector.ConnectorService
	mu           sync.Mutex
	connectors   map[string]*connectorState // keyed by connector_id
}

// New creates a PluginSimulator configured to call real HTTP endpoints at
// baseURL. Call Run in a goroutine to start it. connectorSvc backs discovery
// of newly-paired connectors (DevListConnectors). redisClient backs the HMAC
// signing sequence counter, shared with every other simulator process (the
// control plane's plugin simulator, and the `sim` CLI) signing requests for
// the same connector, so none of them collide on the replay-dedup key.
func New(baseURL string, connectorSvc *connector.ConnectorService, redisClient *redis.Client, log *slog.Logger) *PluginSimulator {
	return &PluginSimulator{
		baseURL:      baseURL,
		signer:       NewSigner(baseURL, redisClient),
		log:          log,
		connectorSvc: connectorSvc,
		connectors:   make(map[string]*connectorState),
	}
}

// Run drives the simulation loop. Blocks until ctx is cancelled.
func (s *PluginSimulator) Run(ctx context.Context) {
	s.log.Info("plugin simulator started (dev mode)", "base_url", s.baseURL)

	discovery := time.NewTicker(10 * time.Second)
	heartbeat := time.NewTicker(30 * time.Second)
	defer discovery.Stop()
	defer heartbeat.Stop()

	s.discover(ctx)

	for {
		select {
		case <-ctx.Done():
			s.log.Info("plugin simulator stopped")
			return
		case <-discovery.C:
			s.discover(ctx)
		case <-heartbeat.C:
			go s.sendAllHeartbeats(ctx)
		}
	}
}

// discover polls for newly-paired connectors and starts tracking them.
// Already-known connectors are left untouched.
func (s *PluginSimulator) discover(ctx context.Context) {
	devConnectors, err := s.connectorSvc.DevListConnectors(ctx)
	if err != nil {
		s.log.Warn("plugin sim: discovery failed", "error", err)
		return
	}

	for _, dc := range devConnectors {
		s.mu.Lock()
		_, known := s.connectors[dc.ConnectorID]
		if !known {
			s.connectors[dc.ConnectorID] = &connectorState{
				pairingCode: dc.PairingCode,
				storeID:     dc.StoreID,
			}
		}
		s.mu.Unlock()

		if !known {
			s.log.Info("plugin sim: discovered paired connector", "connector_id", dc.ConnectorID, "store_id", dc.StoreID)
			// Immediate heartbeat so the connector is seen as live right away.
			s.sendHeartbeat(ctx, dc.ConnectorID)
		}
	}
}

func (s *PluginSimulator) sendAllHeartbeats(ctx context.Context) {
	s.mu.Lock()
	ids := make([]string, 0, len(s.connectors))
	for id := range s.connectors {
		ids = append(ids, id)
	}
	s.mu.Unlock()

	for _, id := range ids {
		s.sendHeartbeat(ctx, id)
	}
}

func (s *PluginSimulator) sendHeartbeat(ctx context.Context, connectorID string) {
	s.mu.Lock()
	state, ok := s.connectors[connectorID]
	s.mu.Unlock()
	if !ok {
		return
	}

	sequence, err := s.signer.NextSequence(ctx, connectorID)
	if err != nil {
		s.log.Warn("plugin sim: failed to allocate sequence", "error", err, "connector_id", connectorID)
		return
	}

	hb := domain.Heartbeat{
		PluginVersion:   "1.0.0-sim",
		PlatformVersion: "6.5.0",
		SiteURL:         "https://sim-store.example",
		Healthy:         true,
	}

	if err := s.signer.Send(ctx, "POST", "/connector/heartbeat", connectorID, state.storeID, state.pairingCode, sequence, hb); err != nil {
		s.log.Warn("plugin sim: heartbeat failed", "error", err, "connector_id", connectorID)
	} else {
		s.log.Debug("plugin sim: heartbeat sent via HTTP", "connector_id", connectorID)
	}
}
