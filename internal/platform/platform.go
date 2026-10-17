// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package platform is the single registry of the backend e-commerce platforms
// Sarvex supports (docs/plans/version-platform-modularity.md).
//
// Each platform is one self-contained module under internal/platform/<name>:
// its connector and merchant config (the <name> package), its credential
// validator (<name>/validator), its simulated store (<name>/sim), and a
// pointer to its merchant-side plugin (plugin/<name>). It is registered here,
// once, and every other part of the system asks this registry instead of
// naming platforms itself — the planes' composition roots register connectors
// and validators from All, the control plane accepts exactly the names
// Supported reports, the definitions catalogue publishes each platform's
// config schema from ConfigSchema, and the simulator serves every platform's
// Sim. Adding a platform therefore means adding its module and one line in
// All; no established platform's code and no plane's wiring changes.
//
// The frozen connectors (internal/platform/frozen/*) are deliberately absent:
// they exist to keep CI honest, not to be served (CLAUDE.md §6).
package platform

import (
	"encoding/json"
	"slices"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/simstore"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/connectors/base"
	validatorregistry "github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/validator/registry"
	"github.com/sarvexlabs/sarvex.cp/internal/platform/magento"
	magentosim "github.com/sarvexlabs/sarvex.cp/internal/platform/magento/sim"
	magentovalidator "github.com/sarvexlabs/sarvex.cp/internal/platform/magento/validator"
	"github.com/sarvexlabs/sarvex.cp/internal/platform/woocommerce"
	woosim "github.com/sarvexlabs/sarvex.cp/internal/platform/woocommerce/sim"
	woovalidator "github.com/sarvexlabs/sarvex.cp/internal/platform/woocommerce/validator"
)

// Name identifies a backend platform on the wire and in storage
// ("woocommerce", "magento", ...).
type Name = domain.CommerceSystemType

// Platform is one supported backend platform.
type Platform struct {
	Name      Name
	Connector base.ConnectorFactory
	Validator validatorregistry.ValidatorFactory
	// ConfigSchema is the JSON Schema of the merchant config the platform's
	// connector and validator read.
	ConfigSchema json.RawMessage
	// Plugin is the repository path of the merchant-side plugin that signals
	// changes and serves the profile doorway for this platform.
	Plugin string
	// Sim is the platform's simulated store, served by the dev/test simulator
	// (internal/planes/sync/sim) and paired with by the control plane's
	// dev-mode plugin simulator.
	Sim simstore.Surface
}

// All returns every supported platform.
func All() []Platform {
	return []Platform{
		{
			Name: woocommerce.PlatformName, Connector: woocommerce.Factory(), Validator: woovalidator.Factory(),
			ConfigSchema: (&woocommerce.MerchantConfig{}).Schema(), Plugin: "plugin/woocommerce", Sim: woosim.Surface{},
		},
		{
			Name: magento.PlatformName, Connector: magento.Factory(), Validator: magentovalidator.Factory(),
			ConfigSchema: (&magento.MerchantConfig{}).Schema(), Plugin: "plugin/magento", Sim: magentosim.Surface{},
		},
	}
}

// Default is the platform a tool uses when none is named: the first one
// registered (WooCommerce, V1's primary target).
func Default() Name { return All()[0].Name }

// Names returns the name of every supported platform, in registration order.
func Names() []Name {
	all := All()
	out := make([]Name, len(all))
	for i, p := range all {
		out[i] = p.Name
	}
	return out
}

// Get returns the supported platform called name.
func Get(name Name) (Platform, bool) {
	for _, p := range All() {
		if p.Name == name {
			return p, true
		}
	}
	return Platform{}, false
}

// Supported reports whether name is a supported platform.
func Supported(name Name) bool {
	return slices.Contains(Names(), name)
}

// RegisterConnectors registers every supported platform's connector factory.
func RegisterConnectors(reg *base.Registry) {
	for _, p := range All() {
		reg.RegisterFactory(p.Name, p.Connector)
	}
}

// RegisterValidators registers every supported platform's credential validator.
func RegisterValidators(reg *validatorregistry.Registry) {
	for _, p := range All() {
		reg.RegisterFactory(p.Name, p.Validator)
	}
}
