// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"time"

	pkgdomain "github.com/sarvexlab/sarvex/internal/pkg/domain"
)

// Platform identifies a connectable system. For commerce backends it is the
// platform registry's name (internal/platform), so no plane keeps its own list
// of platforms.
type Platform = pkgdomain.CommerceSystemType

// ConnectorStatus is the health state of a paired store connection.
type ConnectorStatus string

const (
	ConnectorStatusPairing  ConnectorStatus = "pairing"
	ConnectorStatusHealthy  ConnectorStatus = "healthy"
	ConnectorStatusDegraded ConnectorStatus = "degraded"
	ConnectorStatusDown     ConnectorStatus = "down"
)

// SyncModel describes how the connector receives updates.
type SyncModel string

const (
	SyncModelRealtime SyncModel = "realtime"
	SyncModelPoll     SyncModel = "poll"
)

// ProxyHealth describes the health of the plugin's Role 2 reverse proxy.
type ProxyHealth struct {
	ProfileRouteOK         bool       `json:"profile_route_ok"`
	LastProfileServedAt    *time.Time `json:"last_profile_served_at"`
	ServingFromCache       bool       `json:"serving_from_cache"`
	AdvertisedEndpointBase *string    `json:"advertised_endpoint_base"`
}

// Connector is a paired store connection, owned by exactly one store.
type Connector struct {
	ConnectorID      string          `json:"connector_id"`
	StoreID          StoreID         `json:"store_id"`
	MerchantID       MerchantID      `json:"-"` // Denormalized for auth context; never exposed in API
	Platform         Platform        `json:"platform"`
	Status           ConnectorStatus `json:"status"`
	SyncModel        SyncModel       `json:"sync_model"`
	LastSignalAt     *time.Time      `json:"last_signal_at"`
	LastFullSyncAt   *time.Time      `json:"last_full_sync_at"`
	LastHeartbeatAt  *time.Time      `json:"last_heartbeat_at"`
	FreshnessWarning bool            `json:"freshness_warning"`
	PluginVersion    *string         `json:"plugin_version"`
	Proxy            ProxyHealth     `json:"proxy"`
	HMACSecret       []byte          `json:"-"` // Signing secret for HMAC verification (not exposed in API)
}

// ConnectorSignal is the thin event the plugin sends when something changes.
// It carries only identity — Sarvex pulls the full entity via the connector.
type ConnectorSignal struct {
	EntityType EntityType `json:"entity_type"`
	EntityID   string     `json:"entity_id"`
	OccurredAt string     `json:"occurred_at"`
	Sequence   int64      `json:"sequence"`
	Change     ChangeKind `json:"change"`
}

// EntityType identifies what changed in the merchant store.
type EntityType string

const (
	EntityProduct     EntityType = "product"
	EntityPrice       EntityType = "price"
	EntityInventory   EntityType = "inventory"
	EntityOrderStatus EntityType = "order_status"
)

// ChangeKind describes the nature of the change.
type ChangeKind string

const (
	ChangeCreated ChangeKind = "created"
	ChangeUpdated ChangeKind = "updated"
	ChangeDeleted ChangeKind = "deleted"
)

// Heartbeat is the periodic liveness signal from the plugin.
type Heartbeat struct {
	PluginVersion   string `json:"plugin_version"`
	PlatformVersion string `json:"platform_version"`
	SiteURL         string `json:"site_url"`
	Healthy         bool   `json:"healthy"`
}
