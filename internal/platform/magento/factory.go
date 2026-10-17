// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package magento

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/resilience"
)

// PlatformName is Magento's registry name.
const PlatformName domain.CommerceSystemType = "magento"

// Factory returns a base.ConnectorFactory for Magento.
func Factory() base.ConnectorFactory {
	return func(cfg base.MerchantConfig, logger *slog.Logger) (base.Connector, error) {
		if cfg.Credentials == nil {
			return nil, fmt.Errorf("magento: no credentials provider configured for store-API pull calls")
		}

		mgCfg, ok := cfg.Config.(MerchantConfig)
		if !ok {
			ptr, okPtr := cfg.Config.(*MerchantConfig)
			if !okPtr {
				temp, err := json.Marshal(cfg.Config)
				if err != nil {
					return nil, errMissingField("config: expected magento.MerchantConfig")
				}
				if err = json.Unmarshal(temp, &mgCfg); err != nil {
					return nil, errMissingField("config: expected magento.MerchantConfig")
				}
			} else {
				mgCfg = *ptr
			}
		}
		mgCfg.MerchantID = cfg.MerchantID
		mgCfg.StoreID = cfg.StoreID
		if err := mgCfg.Validate(); err != nil {
			return nil, err
		}
		return NewConnector(mgCfg, cfg.Credentials, resilience.Config{}, logger)
	}
}
