// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package shopify exposes a factory function compatible with base.ConnectorFactory.
// Import this package in your server bootstrap and call RegisterFactory once.
package shopify

import (
	"encoding/json"
	"log/slog"

	"github.com/sarvexlab/sarvex/internal/pkg/domain"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// PlatformName is Shopify's registry name.
const PlatformName domain.CommerceSystemType = "shopify"

// Factory returns a base.ConnectorFactory for Shopify.
// Usage:
//
//	reg.RegisterFactory(shopify.PlatformName, shopify.Factory())
func Factory() base.ConnectorFactory {
	return func(cfg base.MerchantConfig, logger *slog.Logger) (base.Connector, error) {
		shopifyCfg, ok := cfg.Config.(MerchantConfig)
		if !ok {
			// Also accept a pointer.
			ptr, okPtr := cfg.Config.(*MerchantConfig)
			if !okPtr {
				temp, err := json.Marshal(cfg.Config)
				if err != nil {
					return nil, errMissingField("config: expected shopify.MerchantConfig")
				}
				if err = json.Unmarshal(temp, &shopifyCfg); err != nil {
					return nil, errMissingField("config: expected shopify.MerchantConfig")
				}
			} else {
				shopifyCfg = *ptr
			}
		}
		// Ensure the MerchantID from the registry config is authoritative.
		shopifyCfg.MerchantID = cfg.MerchantID
		return NewConnector(shopifyCfg, logger)
	}
}
