// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package directagent registers the direct_agent surface: the fact that any
// agent discovering a store's profile can transact with it, with no
// distribution partner involved.
//
// It has no prerequisites, no external steps, no artifacts and no
// verification signal. It auto-enrolls the moment a store reaches active.
//
// It exists so that "active with zero distribution partners" is an explicit,
// visible, supported condition rather than an implied one (R3, R6). Without
// it, a merchant looking at a Surfaces page with no cards on it would
// reasonably conclude their store wasn't reaching anybody — which would be
// wrong, and would push them toward enrolling in something they may not want
// or qualify for.
package directagent

import (
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/surface"
)

// SurfaceID is the registry id.
const SurfaceID = "direct_agent"

// Descriptor is the direct_agent surface. Every collection is empty by
// design — this surface's whole content is that it requires nothing.
func Descriptor() domain.SurfaceDescriptor {
	return domain.SurfaceDescriptor{
		SurfaceID:      SurfaceID,
		DisplayNameKey: "surfaces.direct_agent.name",
		DescriptionKey: "surfaces.direct_agent.description",
		Protocol:       domain.SurfaceProtocolUCP,
		AutoEnroll:     true,
	}
}

// Registration returns the surface with its (empty) adapter set.
func Registration() surface.Registration {
	return surface.Registration{Descriptor: Descriptor()}
}
