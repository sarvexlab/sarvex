// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/google/uuid"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/account"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/connector"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/store"
)

// seedStore writes a store directly to MongoDB and returns its ID.
// The test is registered for cleanup of anything that cares about the store.
func seedStore(t *testing.T, merchantID domain.MerchantID, displayName string, platform domain.Platform) domain.StoreID {
	t.Helper()
	storeID := domain.StoreID(uuid.New().String())
	now := time.Now()
	err := env.storeRepo.Upsert(context.Background(), &store.StoreModel{
		ID:          string(storeID),
		MerchantID:  string(merchantID),
		Platform:    platform,
		DisplayName: displayName,
		Status:      domain.StoreStatusOnboarding,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	require.NoError(t, err)
	return storeID
}

// seedConnector writes a connector directly with a known HMAC secret.
// storeID must already exist. Returns the connectorID and the raw secret bytes.
func seedConnector(t *testing.T, merchantID domain.MerchantID, storeID domain.StoreID, platform domain.Platform, secret []byte) string {
	t.Helper()
	connectorID := uuid.New().String()
	now := time.Now()
	err := env.connectorRepo.Upsert(context.Background(), &connector.ConnectorModel{
		ID:         connectorID,
		StoreID:    string(storeID),
		MerchantID: string(merchantID),
		Platform:   platform,
		Status:     domain.ConnectorStatusHealthy,
		SyncModel:  domain.SyncModelRealtime,
		HMACSecret: secret,
		CreatedAt:  now,
		UpdatedAt:  now,
	})
	require.NoError(t, err)
	return connectorID
}

// seedAccount writes an account record directly.
func seedAccount(t *testing.T, merchantID domain.MerchantID, name string) {
	t.Helper()
	now := time.Now()
	err := env.accountRepo.Upsert(context.Background(), &account.AccountModel{
		ID:        string(merchantID),
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)
}

// testHMACSecret is a fixed secret used for HMAC-signed requests in tests.
var testHMACSecret = []byte("integration-test-hmac-secret-32b")
