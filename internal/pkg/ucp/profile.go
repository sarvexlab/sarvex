// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package ucp

import "github.com/sarvexlabs/sarvex.cp/internal/pkg/domain"

const GooglePay_20260123 = "2026-01-23"

// Configuration is the per-instance merchant configuration the gateway resolves
// before dispatching to a connector. It is not a UCP wire construct — the wire
// profile types live in internal/spec/v20260408/wire.
type Configuration struct {
	MerchantID string                    `json:"merchant_id"`
	StoreID    string                    `json:"store_id"`
	Platform   domain.CommerceSystemType `json:"platform"` // "shopify" | "bigcommerce" | "magento" | "commercetools" | "woocommerce"
	Config     any                       `json:"config"`   // platform-specific config struct (e.g. shopify.MerchantConfig)
}
