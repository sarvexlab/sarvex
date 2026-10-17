// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// DefaultCatalogue returns the default UCP definitions catalogue.
// This is the authoritative source of truth for all catalogue data.
// Authored at the oldest known spec generation (Catalogue.SpecVersion below is
// set from backend.Version, i.e. the spec registry's Baseline — not
// "2026-06-15", the separately-tracked Version/SpecURL of
// DefaultScoringRubric; see that function's doc comment).
// No runtime file I/O: all data is hardcoded.
func DefaultCatalogue() *Catalogue {
	backend := NewCommerceBackendServiceRegistry()
	services := make([]wire.Service, 0, len(backend.Services))
	capabilities := make([]wire.Capability, 0) // Capabilities are now nested within services, so this is left empty
	paymentHandlers := make([]wire.PaymentHandler, 0, len(backend.PaymentHandlers))
	for handlerType, handlerConfig := range backend.PaymentHandlers {
		if handlerConfig.Enabled {
			paymentHandlers = append(paymentHandlers, wire.PaymentHandler{
				ID:      string(handlerType),
				Version: wire.Version(handlerConfig.Version),
				Spec:    handlerConfig.Specification,
				Schema:  handlerConfig.Schema,
			})
		}
	}
	for serviceType, serviceConfig := range backend.Services {
		if serviceConfig.Enabled {
			for protocolType, protocolConfig := range serviceConfig.ProtocolBindings {
				if protocolConfig.Enabled {
					service := wire.Service{
						Version:   wire.Version(protocolConfig.Version),
						Spec:      protocolConfig.Specification,
						Schema:    protocolConfig.Schema,
						ID:        string(serviceType),
						Transport: wire.Transport(protocolType),
						Endpoint:  string(serviceType), // TODO: This is a placeholder; real endpoint may differ based on protocol and service configuration - would be per platform type and store in format of "{base sarvex platform service}/ucp/:platform/:storeId"
					}
					services = append(services, service)
				}
			}
			for capabilityType, capabilityConfig := range serviceConfig.Capabilities {
				for extensionType, extensionConfig := range capabilityConfig.Extenstions {
					if !extensionConfig.Enabled {
						continue
					}
					// Each extension is its own capability entry, identified by
					// its own reverse-domain name (not the parent's) and
					// naming its parent(s) via Extends — the parent capability
					// itself leaves Extends empty (see Capability's doc
					// comment). Using the parent's ID here previously produced
					// a duplicate-ID entry per enabled extension, which failed
					// validateCatalogue's uniqueness check.
					extension := wire.Capability{
						Version: wire.Version(extensionConfig.Version),
						Spec:    extensionConfig.Specification,
						Schema:  extensionConfig.Schema,
						ID:      string(extensionType),
						Extends: wire.NewExtends(extensionConfig.Extends...),
					}
					capabilities = append(capabilities, extension)
				}
				capability := wire.Capability{
					Version: wire.Version(capabilityConfig.Version),
					Spec:    capabilityConfig.Specification,
					Schema:  capabilityConfig.Schema,
					ID:      string(capabilityType),
				}
				capabilities = append(capabilities, capability)
			}
		}

	}

	return &Catalogue{
		Version:         backend.Version,
		SpecVersion:     backend.Version,
		BuiltAt:         time.Now().UTC(),
		Services:        services,
		Capabilities:    capabilities,
		PaymentHandlers: paymentHandlers,
		ScoringRubric:   DefaultScoringRubric(),

		RequiredBusinessFacts: DefaultRequiredBusinessFacts(),
		AttributeFields:       DefaultAttributeFields(),
	}
}
