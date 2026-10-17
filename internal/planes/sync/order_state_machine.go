// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

// TransitionPreconditions returns preconditions that must hold before
// a specific transition can be safely executed.
//
// Example: before refunding an order, check that:
// 1. Order is in "confirmed" or "shipped" state (not already cancelled/refunded)
// 2. Refund amount <= original order total
// 3. No concurrent refund in flight
type TransitionPrecondition struct {
	Name        string
	Description string
	Check       func(order *Order) (bool, string)
}
