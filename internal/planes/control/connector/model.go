// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package connector

import (
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// heartbeatStaleAfter bounds how long a connector's last-reported health may
// be trusted with no further word from the plugin. The dev-mode simulator
// heartbeats every 30s (internal/planes/sync/sim/plugin_simulator.go); this
// gives several missed beats of slack before declaring the connector down,
// so one slow request doesn't flap the console.
const heartbeatStaleAfter = 2 * time.Minute

// ProxyHealthModel is the embedded sub-document for plugin reverse proxy health.
type ProxyHealthModel struct {
	LastProfileServedAt    *time.Time `bson:"last_profile_served_at"`
	AdvertisedEndpointBase *string    `bson:"advertised_endpoint_base"`
	ProfileRouteOK         bool       `bson:"profile_route_ok"`
	ServingFromCache       bool       `bson:"serving_from_cache"`
}

// ConnectorModel is the MongoDB document stored in control_connectors collection.
// _id is the connector_id — one document per store connection (each store has one connector).
type ConnectorModel struct {
	Proxy            ProxyHealthModel                  `bson:"proxy"`
	UpdatedAt        time.Time                         `bson:"updated_at"`
	CreatedAt        time.Time                         `bson:"created_at"`
	LastFullSyncAt   *time.Time                        `bson:"last_full_sync_at"`
	PairingExpiresAt *time.Time                        `bson:"pairing_expires_at"`
	LastHeartbeatAt  *time.Time                        `bson:"last_heartbeat_at"`
	PluginVersion    *string                           `bson:"plugin_version"`
	LastSignalAt     *time.Time                        `bson:"last_signal_at"`
	StoreCredentials *secretmanagement.EncryptedConfig `bson:"store_credentials,omitempty" json:"-"`
	Status           domain.ConnectorStatus            `bson:"status"`
	PairingCodeHash  string                            `bson:"pairing_code_hash"`
	MerchantID       string                            `bson:"merchant_id"` // denormalized for query, resolvable from store mapping
	ID               string                            `bson:"_id"`
	StoreID          string                            `bson:"store_id"`
	SiteURL          string                            `bson:"site_url"`
	Platform         domain.Platform                   `bson:"platform"`
	SyncModel        domain.SyncModel                  `bson:"sync_model"`
	HMACSecret       []byte                            `bson:"hmac_secret"`
	FreshnessWarning bool                              `bson:"freshness_warning"`
}

// ToDomain converts the model to a domain.Connector.
//
// Status/FreshnessWarning are corrected for staleness here rather than
// trusted verbatim from storage: m.Status only ever changes inside
// RecordHeartbeat (internal/planes/sync/connector/connector_service.go), so
// once a connector's heartbeats stop arriving — HMAC verification failing,
// the plugin going dark, whatever — nothing ever writes a new status and it
// freezes at whatever it last successfully reported. Read time is the only
// place that can know the clock has moved on without heartbeats to match.
//
// A connector still in pairing, or one that has never sent a heartbeat yet
// (LastHeartbeatAt nil — CompletePairing sets Status=Healthy immediately, on
// the reasonable assumption the plugin's first heartbeat is seconds away),
// is left alone: there's no prior liveness evidence to judge staleness
// against, and flagging it down would just flap on every fresh pairing.
func (m *ConnectorModel) ToDomain() domain.Connector {
	status := m.Status
	freshnessWarning := m.FreshnessWarning
	if status != domain.ConnectorStatusPairing && m.LastHeartbeatAt != nil &&
		time.Since(*m.LastHeartbeatAt) > heartbeatStaleAfter {
		status = domain.ConnectorStatusDown
		freshnessWarning = true
	}

	return domain.Connector{
		ConnectorID:      m.ID,
		StoreID:          domain.StoreID(m.StoreID),
		MerchantID:       domain.MerchantID(m.MerchantID),
		Platform:         m.Platform,
		Status:           status,
		SyncModel:        m.SyncModel,
		LastSignalAt:     m.LastSignalAt,
		LastFullSyncAt:   m.LastFullSyncAt,
		LastHeartbeatAt:  m.LastHeartbeatAt,
		FreshnessWarning: freshnessWarning,
		PluginVersion:    m.PluginVersion,
		Proxy: domain.ProxyHealth{
			ProfileRouteOK:         m.Proxy.ProfileRouteOK,
			LastProfileServedAt:    m.Proxy.LastProfileServedAt,
			ServingFromCache:       m.Proxy.ServingFromCache,
			AdvertisedEndpointBase: m.Proxy.AdvertisedEndpointBase,
		},
		HMACSecret: m.HMACSecret,
	}
}
