// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"encoding/json"
	"fmt"

	"github.com/sarvexlab/sarvex/internal/platform/frozen/shopify"
)

// UnmarshalConfig unmarshals the generic config into a shopify.MerchantConfig.
// Tries direct type assertion first, then JSON unmarshal as fallback.
func UnmarshalConfig(genericCfg any) (*shopify.MerchantConfig, error) {
	if cfg, ok := genericCfg.(shopify.MerchantConfig); ok {
		return &cfg, nil
	}
	if cfgPtr, ok := genericCfg.(*shopify.MerchantConfig); ok {
		return cfgPtr, nil
	}

	data, err := json.Marshal(genericCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	var cfg shopify.MerchantConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal shopify config: %w", err)
	}
	return &cfg, nil
}
