// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package base

import (
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/commerce/model"
)

// PickupInfo is what a connector learned about a store's in-store pickup: a
// checkout offers a pickup fulfillment method only when the merchant actually
// has a pickup method turned on. Pickup is a method type inside the
// fulfillment extension, not a capability of its own, so this is checkout-time
// information — nothing about it is advertised in the store's profile.
type PickupInfo struct {
	// Enabled is true when the store has a pickup shipping method enabled.
	Enabled bool `json:"enabled"`
	// Location is where a buyer collects the order. Nil when pickup is not
	// enabled.
	Location *model.RetailLocation `json:"location,omitempty"`
}
