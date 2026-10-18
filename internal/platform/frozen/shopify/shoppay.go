// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package shopify

import "github.com/sarvexlab/sarvex/internal/pkg/domain"

// Shop Pay is Shopify's own payment handler: the token it yields can only be
// redeemed by a Shopify checkout. It therefore belongs to this module rather
// than to the shared handler registry (internal/pkg/ucp/paymenthandler), and —
// Shopify being frozen and unserved — no supported platform offers it.

// ShopPayHandler is the handler's reverse-domain name.
const ShopPayHandler domain.PaymentHandlerType = "dev.shopify.shop_pay"

// Shop Pay's published handler definition.
const (
	ShopPayVersion      = "2026-01-23"
	ShopPaySchema       = "https://shopify.dev/ucp/shop-pay-handler/2026-01-23/schema.json"
	ShopPaySpec         = "https://shopify.dev/ucp/shop-pay-handler/2026-01-23/spec.md"
	ShopPayConfigSchema = "https://shopify.dev/ucp/shop-pay-handler/2026-01-23/business_config.json"
)

// ShopPayConfig is the handler's public business config.
type ShopPayConfig struct {
	ShopID string `json:"shop_id" validate:"required"`
}

// ShopPayAgentConfig is the handler's agent-side config.
type ShopPayAgentConfig struct {
	ClientID string `json:"client_id" validate:"required"`
}

// DummyShopPayConfig is a placeholder business config.
func DummyShopPayConfig() ShopPayConfig {
	return ShopPayConfig{
		ShopID: "12345678",
	}
}
