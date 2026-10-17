// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"log/slog"

	ucpvalidator "github.com/sarvexlab/sarvex/internal/pkg/ucp/validator"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/validator/registry"
)

// Factory returns a registry.ValidatorFactory for WooCommerce.
// Usage: reg.RegisterFactory(woocommerce.PlatformName, validator.Factory())
func Factory() registry.ValidatorFactory {
	return func(cfg ucpvalidator.MerchantConfig, logger *slog.Logger) (ucpvalidator.Validator, error) {
		wcCfg, err := UnmarshalConfig(cfg.Config)
		if err != nil {
			return nil, err
		}

		// Ensure registry MerchantID is authoritative
		wcCfg.MerchantID = cfg.MerchantID
		wcCfg.StoreID = cfg.StoreID
		return NewValidator(*wcCfg, logger)
	}
}
