// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package connector

import (
	"context"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// ConnectorRepository is the persistence interface for connector records.
// The single writer is ConnectorService; other readers consume events.
type ConnectorRepository interface {
	// Console operations (merchant-scoped for bearer auth)
	Get(ctx context.Context, merchantID domain.MerchantID, connectorID string) (*ConnectorModel, error)
	List(ctx context.Context, merchantID domain.MerchantID, afterID string, limit int) ([]*ConnectorModel, bool, error)
	Delete(ctx context.Context, merchantID domain.MerchantID, connectorID string) error

	// Plugin operations (connector-scoped for HMAC auth)
	GetByConnector(ctx context.Context, connectorID string) (*ConnectorModel, error)

	// Store operations (store-scoped for credential retrieval)
	GetByStoreID(ctx context.Context, storeID string) (*ConnectorModel, error)

	// Pairing operations
	GetByPairingCode(ctx context.Context, pairingCode string) (*ConnectorModel, error)

	// Generic
	Upsert(ctx context.Context, m *ConnectorModel) error
}

// ProfileSource is an alias to ucp.ProfileSource for backward compatibility.
type ProfileSource = ucp.ProfileSource
