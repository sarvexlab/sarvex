// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"encoding/json"
	"fmt"

	"github.com/sarvexlab/sarvex/internal/platform/woocommerce"
)

// UnmarshalConfig unmarshals the generic config into a WooCommerce woocommerce.MerchantConfig.
// Tries direct type assertion first, then JSON unmarshal as fallback.
func UnmarshalConfig(genericCfg any) (*woocommerce.MerchantConfig, error) {
	if cfg, ok := genericCfg.(woocommerce.MerchantConfig); ok {
		return &cfg, nil
	}
	if cfgPtr, ok := genericCfg.(*woocommerce.MerchantConfig); ok {
		return cfgPtr, nil
	}

	data, err := json.Marshal(genericCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	var cfg woocommerce.MerchantConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal woocommerce config: %w", err)
	}
	cfg = cfg.WithDefaults()
	return &cfg, nil
}
