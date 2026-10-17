// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package negotiation

import (
	"github.com/sarvexlab/sarvex/internal/pkg/domain"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/types"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
	specall "github.com/sarvexlab/sarvex/internal/spec/all"
)

// SelectVersion returns the highest version in servable that does not
// exceed ceiling (the platform's declared UCP protocol version) —
// "the highest version we support that the platform also supports,"
// applied to the top-level negotiation gate the same way
// IntersectCapabilities/wireintersect.Capabilities already apply
// highest-mutual-version selection per capability.
//
// The order of servable does not matter. Returns ok=false when every
// entry in servable is newer than ceiling — no mutual version exists —
// which callers translate to VersionUnsupportedError.
func SelectVersion(servable []wire.Version, ceiling wire.Version) (chosen wire.Version, ok bool) {
	for _, v := range servable {
		if v > ceiling {
			continue
		}
		if !ok || v > chosen {
			chosen, ok = v, true
		}
	}
	return chosen, ok
}

// ServableVersions returns the generations that serve the catalog — the
// non-checkout operations — converted to the wire.Version type this package
// negotiates in. It is the production value passed to WithServableVersions;
// a generation joins it by serving the catalog (spec.Version.Serves).
func ServableVersions() []wire.Version {
	return toWireVersions(specall.Default().Serving(string(domain.CapabilityTypeCatalog)))
}

// CheckoutServableVersions is ServableVersions' checkout counterpart: the
// generations that serve checkout (and with it cart and order) — the
// production value passed to WithCheckoutServableVersions.
func CheckoutServableVersions() []wire.Version {
	return toWireVersions(specall.Default().Serving(string(domain.CapabilityTypeCheckout)))
}

func toWireVersions(versions []types.Version) []wire.Version {
	out := make([]wire.Version, len(versions))
	for i, v := range versions {
		out[i] = wire.Version(v.String())
	}
	return out
}
