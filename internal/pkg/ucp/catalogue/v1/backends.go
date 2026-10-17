// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"encoding/json"

	specall "github.com/sarvexlabs/sarvex.cp/internal/spec/all"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/paymenthandler"
	"github.com/sarvexlabs/sarvex.cp/internal/platform"
)

// ConformanceMockHandlerEnvVar, when set to a non-empty value, makes the
// checkout-complete path accept the official UCP conformance suite's mock
// payment handler id (MockPaymentHandlerID) even though it is not advertised
// in the store's profile. A handful of suite tests hardcode that id when
// completing (ap2 mandate, token binding, card credential); the handler
// cannot be advertised because its id is not a valid reverse-domain profile
// key (it would fail the suite's own discovery schema), so it is allowed at
// completion only, under this env gate. `sarvex conform serve` sets the var;
// nothing else does, so production never accepts an unadvertised handler.
const ConformanceMockHandlerEnvVar = "UCP_CONFORMANCE_MOCK_PAYMENT_HANDLER"

// MockPaymentHandlerID is the handler id the official conformance suite's
// static fixtures use for the checkout-completion payment instrument.
const MockPaymentHandlerID = "mock_payment_handler"

type CommerceBackendServiceRegistry struct {
	Version         string                                                                   `json:"version"`
	PaymentHandlers map[domain.PaymentHandlerType]CommerceBackendServicePaymentHandlerConfig `json:"payment_handlers"`
	Services        map[domain.ServiceType]CommerceBackendServiceEntryConfig                 `json:"services"`
	Compositions    map[domain.CommerceSystemType]CommerceBackendServicesConfig              `json:"compositions"`
}

type CommerceBackendServicesConfig struct {
	BackendType  domain.CommerceSystemType `json:"backend_type"`
	ConfigSchema json.RawMessage           `json:"config_schema,omitempty"`
}

type CommerceBackendServiceEntryConfig struct {
	Capabilities     map[domain.CapabilityType]CommerceBackendServiceCapabilitiesConfig  `json:"capabilities"`
	ProtocolBindings map[domain.ProtocolType]CommerceBackendServiceProtocolBindingConfig `json:"protocol_bindings"`
	Enabled          bool                                                                `json:"enabled"`
	ServiceType      domain.ServiceType                                                  `json:"service_type"`
}

type CommerceBackendServiceProtocolBindingConfig struct {
	Enabled       bool                `json:"enabled"`
	Specification string              `json:"specification"`
	Transport     domain.ProtocolType `json:"transport"`
	Schema        string              `json:"schema"`
	Version       string              `json:"version"`
}

type CommerceBackendServiceCapabilitiesConfig struct {
	Extenstions   map[domain.ExtensionType]CommerceBackendServiceExtensionsConfig `json:"extensions"`
	Enabled       bool                                                            `json:"enabled"`
	Schema        string                                                          `json:"schema"`
	Specification string                                                          `json:"specification"`
	Version       string                                                          `json:"version"`
}

type CommerceBackendServiceExtensionsConfig struct {
	Enabled       bool     `json:"enabled"`
	Schema        string   `json:"schema"`
	Specification string   `json:"specification"`
	Version       string   `json:"version"`
	Extends       []string `json:"extends"`
}

type CommerceBackendServicePaymentHandlerConfig struct {
	Enabled              bool                                                   `json:"enabled"`
	Id                   string                                                 `json:"id"`
	Version              string                                                 `json:"version"`
	Schema               string                                                 `json:"schema"`
	InstrumentSchema     string                                                 `json:"instrument_schema"`
	Specification        string                                                 `json:"spec"`
	ConfigSchema         json.RawMessage                                        `json:"config_schema,omitempty"`
	AvailableInstruments []CommerceBackendServicePaymentHandlerInstrumentConfig `json:"available_instruments"`
	Config               any                                                    `json:"config"`
}

type CommerceBackendServicePaymentHandlerInstrumentConfig struct {
	Type        string                                                          `json:"type"`
	Constraints CommerceBackendServicePaymentHandlerInstrumentConstraintsConfig `json:"constraints"`
}

type CommerceBackendServicePaymentHandlerInstrumentConstraintsConfig struct {
	Brands []string `json:"brands"`
}

// defaultRESTProtocol returns the standard REST protocol binding used by all backends.
func defaultRESTProtocol() CommerceBackendServiceProtocolBindingConfig {
	return CommerceBackendServiceProtocolBindingConfig{
		Enabled:       true,
		Specification: ucpDev("/specification/overview"),
		Transport:     domain.ProtocolTypeREST,
		Schema:        ucpDev("/services/shopping/rest.openapi.json"),
		Version:       authoredAt(),
	}
}

// baseExtensions returns the standard extensions shared across most backends.
func baseExtensions() map[domain.ExtensionType]CommerceBackendServiceExtensionsConfig {
	return map[domain.ExtensionType]CommerceBackendServiceExtensionsConfig{
		domain.ExtensionTypeDiscount: {
			Enabled:       true,
			Schema:        ucpDev("/schemas/shopping/discount.json"),
			Specification: ucpDev("/specification/discount"),
			Version:       authoredAt(),
			Extends:       []string{"dev.ucp.shopping.checkout", "dev.ucp.shopping.cart"},
		},
		domain.ExtensionTypeFulfillment: {
			Enabled:       true,
			Schema:        ucpDev("/schemas/shopping/fulfillment.json"),
			Specification: ucpDev("/specification/fulfillment"),
			Version:       authoredAt(),
			Extends:       []string{"dev.ucp.shopping.checkout"},
		},
		domain.ExtensionTypeAP2Mandates: {
			Enabled:       true,
			Schema:        ucpDev("/schemas/shopping/ap2_mandates.json"),
			Specification: ucpDev("/specification/ap2-mandates"),
			Version:       authoredAt(),
			Extends:       []string{"dev.ucp.shopping.checkout"},
		},
	}
}

// cartCapability returns the standard Cart capability.
func cartCapability() CommerceBackendServiceCapabilitiesConfig {
	return CommerceBackendServiceCapabilitiesConfig{
		Enabled:       true,
		Schema:        ucpDev("/schemas/shopping/cart.json"),
		Specification: ucpDev("/specification/cart"),
		Version:       authoredAt(),
	}
}

// catalogCapability returns the standard Catalog capability.
func catalogCapability() CommerceBackendServiceCapabilitiesConfig {
	return CommerceBackendServiceCapabilitiesConfig{
		Enabled:       true,
		Schema:        ucpDev("/schemas/shopping/catalog.json"),
		Specification: ucpDev("/specification/catalog"),
		Version:       authoredAt(),
	}
}

// orderCapability returns the standard Order capability.
func orderCapability() CommerceBackendServiceCapabilitiesConfig {
	return CommerceBackendServiceCapabilitiesConfig{
		Enabled:       true,
		Schema:        ucpDev("/schemas/shopping/order.json"),
		Specification: ucpDev("/specification/order"),
		Version:       authoredAt(),
	}
}

// identityLinkingCapability returns the IdentityLinking capability with specified enablement.
func identityLinkingCapability(enabled bool) CommerceBackendServiceCapabilitiesConfig {
	return CommerceBackendServiceCapabilitiesConfig{
		Enabled:       enabled,
		Schema:        ucpDev("/schemas/common/identity_linking.json"),
		Specification: ucpDev("/specification/identity-linking"),
	}
}

// checkoutCapability returns a Checkout capability with the given extensions.
func checkoutCapability(exts map[domain.ExtensionType]CommerceBackendServiceExtensionsConfig) CommerceBackendServiceCapabilitiesConfig {
	return CommerceBackendServiceCapabilitiesConfig{
		Enabled:       true,
		Schema:        ucpDev("/schemas/shopping/checkout.json"),
		Specification: ucpDev("/specification/checkout"),
		Version:       authoredAt(),
		Extenstions:   exts,
	}
}

// buyerConsentExt adds BuyerConsent extension to a extensions map.
func buyerConsentExt(exts map[domain.ExtensionType]CommerceBackendServiceExtensionsConfig) map[domain.ExtensionType]CommerceBackendServiceExtensionsConfig {
	exts[domain.ExtensionTypeBuyerConsent] = CommerceBackendServiceExtensionsConfig{
		Enabled:       true,
		Schema:        ucpDev("/schemas/shopping/buyer_consent.json"),
		Specification: ucpDev("/specification/buyer-consent/"),
		Version:       authoredAt(),
		Extends:       []string{"dev.ucp.shopping.checkout"},
	}
	return exts
}

// shoppingService builds a shopping service with the given checkout extensions and identity linking enablement.
func shoppingService(checkoutExts map[domain.ExtensionType]CommerceBackendServiceExtensionsConfig, enableIdentityLinking bool) CommerceBackendServiceEntryConfig {
	caps := map[domain.CapabilityType]CommerceBackendServiceCapabilitiesConfig{
		domain.CapabilityTypeCheckout: checkoutCapability(checkoutExts),
		domain.CapabilityTypeCart:     cartCapability(),
		domain.CapabilityTypeCatalog:  catalogCapability(),
		domain.CapabilityTypeOrder:    orderCapability(),
	}
	caps[domain.CapabilityTypeIdentityLinking] = identityLinkingCapability(enableIdentityLinking)
	return CommerceBackendServiceEntryConfig{
		ServiceType: domain.UCPServiceTypeShopping,
		Enabled:     true,
		ProtocolBindings: map[domain.ProtocolType]CommerceBackendServiceProtocolBindingConfig{
			domain.ProtocolTypeREST: defaultRESTProtocol(),
		},
		Capabilities: caps,
	}
}

func googlePayHandler(configSchema func(domain.PaymentHandlerType) json.RawMessage) CommerceBackendServicePaymentHandlerConfig {
	return CommerceBackendServicePaymentHandlerConfig{
		Enabled:       true,
		Id:            "google_pay",
		Version:       ucp.GooglePay_20260123,
		Schema:        "https://pay.google.com/gp/p/ucp/2026-01-23/schemas/config.json",
		Specification: "https://pay.google.com/gp/p/ucp/2026-01-23/",
		ConfigSchema:  configSchema(domain.GooglePay),
		Config:        paymenthandler.DummyGooglePayConfig(),
	}
}

func NewCommerceBackendServiceRegistry() CommerceBackendServiceRegistry {
	phSchemaRegistry := paymenthandler.NewPaymentHandlerSchemaRegistry()
	paymentHandlerSchema := func(t domain.PaymentHandlerType) json.RawMessage {
		s, _ := phSchemaRegistry.Get(string(t))
		return s
	}
	return CommerceBackendServiceRegistry{
		Version: authoredAt(),
		PaymentHandlers: map[domain.PaymentHandlerType]CommerceBackendServicePaymentHandlerConfig{
			domain.GooglePay: googlePayHandler(paymentHandlerSchema),
		},
		Services: map[domain.ServiceType]CommerceBackendServiceEntryConfig{
			domain.UCPServiceTypeShopping: shoppingService(buyerConsentExt(baseExtensions()), false),
		},
		Compositions: compositions(),
	}
}

// compositions lists one backend composition per supported platform, with the
// schema of the merchant config its connector reads — straight from the
// platform registry, so a newly registered platform is published here without
// this file changing.
func compositions() map[domain.CommerceSystemType]CommerceBackendServicesConfig {
	out := map[domain.CommerceSystemType]CommerceBackendServicesConfig{}
	for _, p := range platform.All() {
		out[p.Name] = CommerceBackendServicesConfig{BackendType: p.Name, ConfigSchema: p.ConfigSchema}
	}
	return out
}

// authoredAt is the spec generation the catalogue's content is written at:
// the oldest one Sarvex knows. The profile renderer re-points every ucp.dev
// URL at each generation it declares (Catalogue.SpecVersion), so the
// catalogue itself names no generation.
func authoredAt() string { return specall.Default().Baseline().String() }

// ucpDev is the ucp.dev URL of path in the catalogue's authored generation.
func ucpDev(path string) string { return "https://ucp.dev/" + authoredAt() + path }
