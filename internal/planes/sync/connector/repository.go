// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package connector

import (
	"context"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
)

// ConnectorRepository is the persistence interface for connector records.
// The single writer is ConnectorService; other readers consume events.
type ConnectorRepository interface {
	// Plugin operations (connector-scoped for HMAC auth)
	GetByConnector(ctx context.Context, connectorID string) (*ConnectorModel, error)
	Upsert(ctx context.Context, m *ConnectorModel) error

	// GetByStoreID is read-only: it backs GetStoreCredentials, which resolves
	// a store's decrypted credentials for the store-API pull methods. It must
	// never be paired with a write — the control plane is the sole writer of
	// connector/credential records (pairing, disconnect); the sync plane only
	// reads them here.
	GetByStoreID(ctx context.Context, storeID string) (*ConnectorModel, error)

	// ListPaired is dev-tooling-only: it backs the in-process plugin
	// simulator's discovery of newly-paired connectors to send heartbeats
	// and signals for. Deliberately unscoped (no merchant_id) — never called
	// from an HTTP route, and only ever wired when KINDE_ISSUER_URL is unset.
	ListPaired(ctx context.Context) ([]*ConnectorModel, error)
}

// ProfileSource is an alias to ucp.ProfileSource for backward compatibility.
type ProfileSource = ucp.ProfileSource
