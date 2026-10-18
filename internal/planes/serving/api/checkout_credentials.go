// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// checkoutOnlyCredentials resolves a store's backend API credentials for the
// serving plane's checkout path — the one live merchant call the architecture
// permits (sarvex-v1-architecture.md A10.2: "Checkout is the one spec-mandated exception: it
// calls the merchant backend live").
//
// The serving plane previously had no way to resolve credentials at all: its
// registry was built with a provider that always errored, guarding the rule
// that serving must never make store-API *pull* calls. But base.Registry
// hands one provider to the whole connector, so the guard also blocked
// CheckoutCreate/Update/Complete, and every agent checkout failed with a 503
// long before reaching the merchant. The guard was right about pulls and
// wrong about checkout; blockPullConnector below restores it at the
// granularity the connector interface actually documents — the "Store-API
// pull (sync-plane only)" section of base.Connector.
//
// Credentials are read the same way the sync plane reads them
// (connector.ConnectorService.GetStoreCredentials): the connector record the
// control plane owns, decrypted through secretmanagement. It is a read-only
// read of another plane's collection by name, which is the sanctioned
// cross-plane pattern (sarvex-v1-architecture.md A10.1) — importing sync's connector package
// would not be.
//
// Resolved fresh per call and never cached, so a credential rotation at
// pairing takes effect on the very next checkout.
func checkoutOnlyCredentials(db *mongo.Database, secrets secretmanagement.SecretManagementService) base.CredentialsFunc {
	return func(ctx context.Context, storeID string) (map[string]any, error) {
		if secrets == nil {
			return nil, fmt.Errorf("serving: secret management not configured, cannot resolve credentials for store %s", storeID)
		}

		var record struct {
			StoreCredentials *secretmanagement.EncryptedConfig `bson:"store_credentials"`
		}
		err := db.Collection("control_connectors").
			FindOne(ctx, bson.M{"store_id": storeID}).
			Decode(&record)
		if err != nil {
			return nil, fmt.Errorf("serving: read connector for store %s: %w", storeID, err)
		}
		if record.StoreCredentials == nil {
			return nil, fmt.Errorf("serving: no store credentials recorded for store %s", storeID)
		}

		creds, err := secrets.Decrypt(ctx, record.StoreCredentials)
		if err != nil {
			return nil, fmt.Errorf("serving: decrypt store credentials for store %s: %w", storeID, err)
		}
		return creds, nil
	}
}
