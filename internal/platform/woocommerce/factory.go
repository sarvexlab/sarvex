// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package woocommerce

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/sarvexlab/sarvex/internal/pkg/domain"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/resilience"
)

// PlatformName is WooCommerce's registry name.
const PlatformName domain.CommerceSystemType = "woocommerce"

// Factory returns a base.ConnectorFactory for WooCommerce.
func Factory() base.ConnectorFactory {
	return func(cfg base.MerchantConfig, logger *slog.Logger) (base.Connector, error) {
		if cfg.Credentials == nil {
			return nil, fmt.Errorf("woocommerce: no credentials provider configured for store-API pull calls")
		}

		raw, err := cfg.Credentials.GetStoreCredentials(context.Background(), cfg.StoreID)
		if err != nil {
			return nil, fmt.Errorf("resolve credentials for store %s: %w", cfg.StoreID, err)
		}

		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("encode credentials for store %s: %w", cfg.StoreID, err)
		}
		var wcCfg MerchantConfig
		if err := json.Unmarshal(encoded, &wcCfg); err != nil {
			return nil, fmt.Errorf("decode credentials for store %s: %w", cfg.StoreID, err)
		}

		wcCfg = wcCfg.WithDefaults()

		wcCfg.MerchantID = cfg.MerchantID
		wcCfg.StoreID = cfg.StoreID
		wcCfg.Platform = cfg.Platform
		if err := wcCfg.Validate(); err != nil {
			return nil, err
		}
		return NewConnector(wcCfg, cfg.Credentials, resilience.Config{}, logger)
	}
}
