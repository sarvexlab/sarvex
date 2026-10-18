// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package activation

import (
	"fmt"

	"github.com/sarvexlab/sarvex/internal/pkg/domain"
)

// KnownCapabilities is the closed set of UCP capability identifiers a store
// may declare, taken from internal/pkg/domain — the single place these
// reverse-domain names are defined. Nothing in this repo may introduce a
// capability identifier as a free-form string, here or in a surface
// descriptor's required_capabilities.
//
// (The brief names internal/pkg/ucp/types as the canonical source; that
// package holds the wire payload structs for spec version v20260408 and
// defines no identifiers. internal/pkg/domain.CapabilityType is where they
// actually live.)
func KnownCapabilities() []string {
	return []string{
		string(domain.CapabilityTypeCheckout),
		string(domain.CapabilityTypeOrder),
		string(domain.CapabilityTypeCatalog),
		string(domain.CapabilityTypeCart),
		string(domain.CapabilityTypeIdentityLinking),
	}
}

// IsKnownCapability reports whether id is one of the canonical identifiers.
func IsKnownCapability(id string) bool {
	for _, known := range KnownCapabilities() {
		if known == id {
			return true
		}
	}
	return false
}

// ValidateCapabilities rejects any identifier outside the canonical set.
// Used both when a store declares its capabilities and when a surface
// descriptor is registered, so a typo in either place fails loudly rather
// than producing a requirement no store can ever satisfy.
func ValidateCapabilities(ids []string) error {
	for _, id := range ids {
		if !IsKnownCapability(id) {
			return fmt.Errorf("unknown capability identifier %q (not in internal/pkg/domain.CapabilityType)", id)
		}
	}
	return nil
}
