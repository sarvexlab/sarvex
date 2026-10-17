// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package release is the UCP 2026-08-25 generation as the spec registry sees
// it: its version, what Sarvex serves in it, its codec and its profile
// document. internal/spec/all registers it.
package release

import (
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/generation"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/types"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wireadapt"
	"github.com/sarvexlabs/sarvex.cp/internal/spec"
	v20260825 "github.com/sarvexlabs/sarvex.cp/internal/spec/v20260825"
	"github.com/sarvexlabs/sarvex.cp/internal/spec/v20260825/codec"
	"github.com/sarvexlabs/sarvex.cp/internal/spec/v20260825/profile"
)

// New returns the 2026-08-25 generation.
func New() spec.Version { return release{} }

type release struct{}

func (release) ID() types.Version { return v20260825.Release }

// Serves is every capability and extension Sarvex renders in 2026-08-25:
// catalog (codec/catalog.go), and checkout, cart and order with the buyer
// consent, discount and fulfillment extensions (codec).
func (release) Serves() []string {
	return []string{
		string(domain.ExtensionTypeBuyerConsent),
		string(domain.CapabilityTypeCart),
		string(domain.CapabilityTypeCatalog),
		string(domain.CapabilityTypeCheckout),
		string(domain.ExtensionTypeDiscount),
		string(domain.ExtensionTypeFulfillment),
		string(domain.CapabilityTypeOrder),
	}
}

func (release) Codec() wireadapt.Codec { return codec.New() }

func (release) Generation() generation.Generation { return profile.New() }
