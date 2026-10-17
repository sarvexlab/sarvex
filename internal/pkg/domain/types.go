// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package domain

// CommerceSystemType names a commerce system. The supported backend platforms
// own their names (woocommerce.PlatformName, magento.PlatformName) and are
// listed in internal/platform; the constants below are the systems that are not
// supported backends (frozen connectors, or not a backend at all).
type CommerceSystemType string

const (
	Custom        CommerceSystemType = "custom"
	Commercetools CommerceSystemType = "commercetools"
	Bigcommerce   CommerceSystemType = "bigcommerce"
)

type ProtocolType string

const (
	ProtocolTypeREST ProtocolType = "rest"
	ProtocolTypeMCP  ProtocolType = "mcp"
	ProtocolTypeA2A  ProtocolType = "a2a"
	ProtocolTypeAP2  ProtocolType = "ap2"
	ProtocolTypeEP   ProtocolType = "ep" // Embedded Protocol
)

type ServiceType string

const (
	UCPServiceTypeShopping ServiceType = "dev.ucp.shopping"
)

type CapabilityType string

const (
	CapabilityTypeCheckout        CapabilityType = "dev.ucp.shopping.checkout"
	CapabilityTypeOrder           CapabilityType = "dev.ucp.shopping.order"
	CapabilityTypeCatalog         CapabilityType = "dev.ucp.shopping.catalog"
	CapabilityTypeCart            CapabilityType = "dev.ucp.shopping.cart"
	CapabilityTypeIdentityLinking CapabilityType = "dev.ucp.common.identity_linking"
)

type ExtensionType string

const (
	ExtensionTypeFulfillment ExtensionType = "dev.ucp.shopping.fulfillment"
	ExtensionTypeDiscount    ExtensionType = "dev.ucp.shopping.discount"
	// ExtensionTypeAP2Mandates is the 2026-04-08 name of the AP2 mandate
	// extension. It is renamed "dev.ucp.common.payment.ap2_mandate" in
	// 2026-08-25 (see spec/v20260825/extensions/ap2.CapabilityName), so the
	// name is per spec generation, not one constant.
	ExtensionTypeAP2Mandates  ExtensionType = "dev.ucp.shopping.ap2_mandate"
	ExtensionTypeBuyerConsent ExtensionType = "dev.ucp.shopping.buyer_consent"
)

// Pickup is not a capability of its own in the UCP spec: it is a fulfillment
// method type (and a business-location destination) inside the fulfillment
// extension, so there is deliberately no "pickup" ExtensionType here.

type PaymentHandlerType string

const (
	GooglePay PaymentHandlerType = "com.google.pay"
	Stripe    PaymentHandlerType = "com.stripe"
	PayPal    PaymentHandlerType = "com.paypal"
	Adyen     PaymentHandlerType = "com.adyen"
	Braintree PaymentHandlerType = "com.braintree"
)

// MerchantID is the opaque per-tenant identifier resolved from the auth principal.
// It is never sourced from a URL parameter. Partitions billing, identity, team, notification prefs.
type MerchantID string

// StoreID is the per-store identifier for a catalog/serving partition.
// Each store owns one connector. Partitions catalog, serving cache, sessions, profile, pairing, validation.
type StoreID string

// Principal is the resolved identity attached to every authenticated request.
type Principal struct {
	MerchantID  MerchantID `json:"merchant_id"`
	StoreID     StoreID    `json:"store_id,omitempty"`     // Set by HMAC auth for connector requests
	ConnectorID string     `json:"connector_id,omitempty"` // Set by HMAC auth for connector requests
}

const (
	// Event types for Negotiation
	EventTypeUCPNegotiationSucceeded = "ucp.negotiation.succeeded"
	EventTypeUCPNegotiationFailed    = "ucp.negotiation.failed"
)
