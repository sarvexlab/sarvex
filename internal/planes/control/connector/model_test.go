// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package connector

import (
	"testing"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/platform/woocommerce"

	"github.com/stretchr/testify/assert"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

func TestConnectorModel_ToDomain_CarriesAllTimestamps(t *testing.T) {
	signal := time.Date(2026, 7, 16, 6, 20, 0, 0, time.UTC)
	fullSync := time.Date(2026, 7, 16, 6, 25, 0, 0, time.UTC)
	heartbeat := time.Now().Add(-10 * time.Second)

	m := &ConnectorModel{
		ID:              "conn-1",
		StoreID:         "store-1",
		MerchantID:      "merch-1",
		Platform:        woocommerce.PlatformName,
		Status:          domain.ConnectorStatusHealthy,
		SyncModel:       domain.SyncModelRealtime,
		LastSignalAt:    &signal,
		LastFullSyncAt:  &fullSync,
		LastHeartbeatAt: &heartbeat,
	}

	got := m.ToDomain()
	assert.Equal(t, &signal, got.LastSignalAt)
	assert.Equal(t, &fullSync, got.LastFullSyncAt)
	assert.Equal(t, &heartbeat, got.LastHeartbeatAt)
}

// Regression: Status only ever changes inside RecordHeartbeat
// (internal/planes/sync/connector/connector_service.go) — once heartbeats
// stop arriving (HMAC verification failing, the plugin going dark), nothing
// ever writes a new status, so the stored value freezes at whatever it last
// reported. The console showed a connector as healthy indefinitely after its
// heartbeats had actually stopped. ToDomain now corrects for this at read
// time instead of trusting storage verbatim.
func TestConnectorModel_ToDomain_StaleHeartbeatReadsAsDown(t *testing.T) {
	stale := time.Now().Add(-10 * time.Minute)
	m := &ConnectorModel{
		ID:              "conn-1",
		StoreID:         "store-1",
		Status:          domain.ConnectorStatusHealthy,
		LastHeartbeatAt: &stale,
	}

	got := m.ToDomain()
	assert.Equal(t, domain.ConnectorStatusDown, got.Status)
	assert.True(t, got.FreshnessWarning)
	// The raw timestamp is still reported as-is — only the derived status changes.
	assert.Equal(t, &stale, got.LastHeartbeatAt)
}

func TestConnectorModel_ToDomain_RecentHeartbeatKeepsReportedStatus(t *testing.T) {
	recent := time.Now().Add(-10 * time.Second)
	m := &ConnectorModel{
		ID:              "conn-1",
		StoreID:         "store-1",
		Status:          domain.ConnectorStatusDegraded,
		LastHeartbeatAt: &recent,
	}

	got := m.ToDomain()
	assert.Equal(t, domain.ConnectorStatusDegraded, got.Status)
	assert.False(t, got.FreshnessWarning)
}

// A store mid-pairing has never sent a heartbeat by definition — staleness
// doesn't apply until there's a first heartbeat to go stale from.
func TestConnectorModel_ToDomain_PairingStatusNeverGoesStale(t *testing.T) {
	m := &ConnectorModel{
		ID:              "conn-1",
		StoreID:         "store-1",
		Status:          domain.ConnectorStatusPairing,
		LastHeartbeatAt: nil,
	}

	got := m.ToDomain()
	assert.Equal(t, domain.ConnectorStatusPairing, got.Status)
	assert.False(t, got.FreshnessWarning)
}

// CompletePairing (internal/planes/control/connector/connector_service.go)
// sets Status=Healthy immediately, before any heartbeat has arrived — the
// plugin's first heartbeat is expected within seconds, not staleness.
func TestConnectorModel_ToDomain_NeverHeartbeatedButNotPairing_LeftAlone(t *testing.T) {
	m := &ConnectorModel{
		ID:              "conn-1",
		StoreID:         "store-1",
		Status:          domain.ConnectorStatusHealthy,
		LastHeartbeatAt: nil,
	}

	got := m.ToDomain()
	assert.Equal(t, domain.ConnectorStatusHealthy, got.Status)
	assert.False(t, got.FreshnessWarning)
}
