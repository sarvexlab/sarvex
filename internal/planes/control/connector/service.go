// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package connector manages store connections, pairing, and the plugin ingest edge.
package connector

import (
	"context"

	pkgdomain "github.com/sarvexlabs/sarvex.cp/internal/pkg/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// CredentialsValidator is the narrow slice of the validator registry that
// CompletePairing needs: validate a raw credential map for a platform before
// encrypting and persisting it.
type CredentialsValidator interface {
	ValidateCredentials(ctx context.Context, platform pkgdomain.CommerceSystemType, storeID string, creds map[string]any) error
}

// Service manages connector lifecycle and the plugin ingest edge.
type Service interface {
	// Console endpoints (bearer auth, scoped by merchantID)
	List(ctx context.Context, merchantID domain.MerchantID, cursor string, limit int) (domain.ListOf[domain.Connector], error)
	Get(ctx context.Context, merchantID domain.MerchantID, connectorID string) (domain.Connector, error)
	TriggerResync(ctx context.Context, merchantID domain.MerchantID, connectorID string) (domain.JobAccepted, error)

	// Pairing flow (bearer auth for BeginPairing, HMAC for CompletePairing)
	BeginPairing(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, platform domain.Platform) (domain.ConnectorPairing, error)
	// CompletePairing verifies the pairing code, validates and persists storeCreds
	// (nil = no credentials this call), provisions signing keys, and marks the
	// connector healthy.
	CompletePairing(ctx context.Context, connectorID string, pairingCode string, storeCreds map[string]any) (domain.Connector, error)

	// GetStoreCredentials returns the decrypted store API credentials for a store.
	// Used by serving and sync planes to populate Configuration().Config so the
	// connector factories can make live calls to the merchant's store.
	GetStoreCredentials(ctx context.Context, storeID string) (map[string]any, domain.Platform, error)

	// Plugin endpoints (HMAC auth, scoped by connectorID)
	GetByConnector(ctx context.Context, connectorID string) (domain.Connector, error)
	Disconnect(ctx context.Context, connectorID string) error
}
