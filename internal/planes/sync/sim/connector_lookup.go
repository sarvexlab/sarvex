// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sim

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
	"github.com/sarvexlab/sarvex/internal/planes/sync/connector"
)

// resolveConnector recovers storeID's connector_id and plaintext pairing
// code (the HMAC signing secret) from the connector repository, decrypting
// the same way ConnectorService.DevListConnectors does. Shared by CLI
// (signing mutation signals) and Server (signing the profile-doorway
// fetch) — both need to sign a request as the paired connector without the
// full ConnectorService (event bus, profile source) a one-shot/standalone
// process has no use for.
func resolveConnector(ctx context.Context, repo connector.ConnectorRepository, secretMgmt secretmanagement.SecretManagementService, storeID string) (connectorID, pairingCode string, err error) {
	model, err := repo.GetByStoreID(ctx, storeID)
	if err != nil {
		return "", "", fmt.Errorf("no connector paired for store %s yet — pair it from the console first: %w", storeID, err)
	}

	var encryptedConfig secretmanagement.EncryptedConfig
	if err := json.Unmarshal(model.HMACSecret, &encryptedConfig); err != nil {
		return "", "", fmt.Errorf("store %s: invalid encrypted hmac secret: %w", storeID, err)
	}
	decrypted, err := secretMgmt.Decrypt(ctx, &encryptedConfig)
	if err != nil {
		return "", "", fmt.Errorf("store %s: decrypt hmac secret: %w", storeID, err)
	}
	code, ok := decrypted["hmac_secret"].(string)
	if !ok {
		return "", "", fmt.Errorf("store %s: hmac_secret missing from decrypted pairing credential", storeID)
	}
	return model.ID, code, nil
}
