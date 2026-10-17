// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package connector

import (
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
	"github.com/sarvexlab/sarvex/internal/planes/sync/domain"
)

// ProxyHealthModel is the embedded sub-document for plugin reverse proxy health.
type ProxyHealthModel struct {
	ProfileRouteOK         bool       `bson:"profile_route_ok"`
	LastProfileServedAt    *time.Time `bson:"last_profile_served_at"`
	ServingFromCache       bool       `bson:"serving_from_cache"`
	AdvertisedEndpointBase *string    `bson:"advertised_endpoint_base"`
}

// ConnectorModel is the MongoDB document stored in control_connectors collection.
// _id is the connector_id — one document per store connection (each store has one connector).
type ConnectorModel struct {
	ID               string                            `bson:"_id"`
	StoreID          string                            `bson:"store_id"`
	MerchantID       string                            `bson:"merchant_id"` // denormalized for query, resolvable from store mapping
	Platform         domain.Platform                   `bson:"platform"`
	Status           domain.ConnectorStatus            `bson:"status"`
	SyncModel        domain.SyncModel                  `bson:"sync_model"`
	SiteURL          string                            `bson:"site_url"`
	HMACSecret       []byte                            `bson:"hmac_secret"`
	StoreCredentials *secretmanagement.EncryptedConfig `bson:"store_credentials,omitempty" json:"-"`
	PairingCodeHash  string                            `bson:"pairing_code_hash"`
	PairingExpiresAt *time.Time                        `bson:"pairing_expires_at"`
	LastSignalAt     *time.Time                        `bson:"last_signal_at"`
	LastFullSyncAt   *time.Time                        `bson:"last_full_sync_at"`
	FreshnessWarning bool                              `bson:"freshness_warning"`
	PluginVersion    *string                           `bson:"plugin_version"`
	Proxy            ProxyHealthModel                  `bson:"proxy"`
	LastHeartbeatAt  *time.Time                        `bson:"last_heartbeat_at"`
	CreatedAt        time.Time                         `bson:"created_at"`
	UpdatedAt        time.Time                         `bson:"updated_at"`
}

// ToDomain converts the model to a domain.Connector.
func (m *ConnectorModel) ToDomain() domain.Connector {
	return domain.Connector{
		ConnectorID:      m.ID,
		StoreID:          domain.StoreID(m.StoreID),
		MerchantID:       domain.MerchantID(m.MerchantID),
		Platform:         m.Platform,
		Status:           m.Status,
		SyncModel:        m.SyncModel,
		LastSignalAt:     m.LastSignalAt,
		LastFullSyncAt:   m.LastFullSyncAt,
		LastHeartbeatAt:  m.LastHeartbeatAt,
		FreshnessWarning: m.FreshnessWarning,
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
