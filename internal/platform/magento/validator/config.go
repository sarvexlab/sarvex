// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"encoding/json"
	"fmt"

	"github.com/sarvexlab/sarvex/internal/platform/magento"
)

// UnmarshalConfig unmarshals the generic config into a Magento magento.MerchantConfig.
// Handles both old (store_url, access_token) and new (base_url, admin_user, admin_password) formats.
func UnmarshalConfig(genericCfg any) (*magento.MerchantConfig, error) {
	if cfg, ok := genericCfg.(magento.MerchantConfig); ok {
		return &cfg, nil
	}
	if cfgPtr, ok := genericCfg.(*magento.MerchantConfig); ok {
		return cfgPtr, nil
	}

	data, err := json.Marshal(genericCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	// Unmarshal into the canonical config
	var cfg magento.MerchantConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal magento config: %w", err)
	}

	return &cfg, nil
}
