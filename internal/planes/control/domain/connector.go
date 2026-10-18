// Copyright 2026 Sarvex Lab
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

// ConnectorType discriminates what a connector record is *for*. Commerce
// connectors carry the store's backend — they serve stock, shipping, tax
// and order placement to the checkout path. A read-oriented connector type
// carries none of that; it would exist because a surface requires the link.
//
// The discriminator is what lets both live in one collection without the
// checkout path ever having to ask "is this the real backend": it selects
// commerce connectors explicitly (see ConnectorType.IsCommerce).
type ConnectorType string

const (
	// ConnectorTypeCommerce is the store's commerce backend — WooCommerce
	// or Magento. Exactly one per store, and the only kind the
	// serving plane's checkout path will ever call.
	ConnectorTypeCommerce ConnectorType = "commerce"
)

// IsCommerce reports whether this connector type carries the store's
// commerce backend. Any read-oriented connector type is never a
// store's backend, however healthy it is.
func (t ConnectorType) IsCommerce() bool {
	// The zero value is treated as commerce: every connector written
	// before the discriminator existed is a commerce connector, and
	// backfilling is the migration's job, not a read-time surprise.
	return t == "" || t == ConnectorTypeCommerce
}

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
	ConnectorID string     `json:"connector_id"`
	StoreID     StoreID    `json:"store_id"`
	MerchantID  MerchantID `json:"-"` // Denormalized for auth context; never exposed in API
	Platform    Platform   `json:"platform"`
	// ConnectorType discriminates commerce backends from read-oriented
	// links. Empty on records written before the discriminator
	// existed; ConnectorType.IsCommerce reads those as commerce.
	ConnectorType    ConnectorType   `json:"connector_type,omitempty"`
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

// ConnectorPairing extends Connector with the one-time pairing code.
// The code is shown once and never returned again.
type ConnectorPairing struct {
	Connector
	PairingCode string `json:"pairing_code"`
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

// UCPProfileDocument is the UCP business profile Sarvex generates and signs,
// returned by GET /connector/profile for the plugin to serve verbatim at
// /.well-known/ucp on the merchant's origin.
//
// The building blocks inside (services, capabilities, payment_handlers,
// signing_keys) are ucp.dev-defined namespaced objects — Sarvex references them,
// never authors them. The inner shapes are therefore kept as open maps/slices;
// the authoritative schema lives at ucp.dev, not here.
type UCPProfileDocument struct {
	UCP UCPProfileContent `json:"ucp"`
}

// UCPProfileContent holds the UCP version-tagged profile fields.
// Inner objects use map[string]any / []any because their schemas are
// governed by the external UCP specification, not by Sarvex's own types.
type UCPProfileContent struct {
	Version         string         `json:"version"`
	Services        map[string]any `json:"services"`
	Capabilities    []any          `json:"capabilities"`
	PaymentHandlers []any          `json:"payment_handlers"`
	SigningKeys     []any          `json:"signing_keys"`
}
