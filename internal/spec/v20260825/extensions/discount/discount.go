// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package discount implements the UCP Discount extension
// (dev.ucp.shopping.discount): discount codes, automatic discounts, and
// eligibility-triggered provisional discounts on Cart and Checkout.
//
// Unchanged from v20260408 beyond the underlying Amount/
// ReverseDomainName/AllocationMethod types it composes.
package discount

import (
	v20260825 "github.com/sarvexlabs/sarvex.cp/internal/spec/v20260825"
)

// Allocation is a breakdown of how a discount amount was allocated to a
// specific target.
type Allocation struct {
	// Path is a JSONPath to the allocation target (e.g. "$.line_items[0]", "$.totals[?@.type == \"fulfillment\"]").
	Path   string           `json:"path"`
	Amount v20260825.Amount `json:"amount"`
}

// AppliedDiscount is a discount that was successfully applied.
type AppliedDiscount struct {
	// Code is the discount code; omitted for automatic discounts.
	Code   string           `json:"code,omitempty"`
	Title  string           `json:"title"`
	Amount v20260825.Amount `json:"amount"`
	// Automatic is true if applied automatically by merchant rules (no code required).
	Automatic bool `json:"automatic,omitempty"`
	// Method is the allocation method. See v20260825.AllocationMethod.
	Method v20260825.AllocationMethod `json:"method,omitempty"`
	// Priority is the stacking order for discount calculation; lower
	// numbers apply first (1 = first).
	Priority int `json:"priority,omitempty"`
	// Provisional is true if this discount requires additional verification.
	Provisional bool `json:"provisional,omitempty"`
	// Eligibility is the eligibility claim accepted by the business for
	// this discount, corresponding to a value from Context.Eligibility.
	// Omitted for code-based and non-eligibility automatic discounts.
	Eligibility v20260825.ReverseDomainName `json:"eligibility,omitempty"`
	// Allocations is a breakdown of where this discount was allocated.
	// The sum of allocation amounts equals Amount.
	Allocations []Allocation `json:"allocations,omitempty"`
}

// DiscountsObject carries discount codes as input and applied discounts
// as output.
type DiscountsObject struct {
	// Codes are discount codes to apply, case-insensitive. Replaces
	// previously submitted codes; send an empty (non-nil) slice to clear.
	Codes []string `json:"codes,omitempty"`
	// Applied is discounts successfully applied (code-based and automatic).
	Applied []AppliedDiscount `json:"applied,omitempty"`
}

// CartWithDiscount is Cart extended with discount capability. Composes
// the base Cart type rather than duplicating its fields.
type CartWithDiscount struct {
	v20260825.Cart
	Discounts *DiscountsObject `json:"discounts,omitempty"`
}

// CheckoutWithDiscount is Checkout extended with discount capability.
// Composes the base Checkout type rather than duplicating its fields.
type CheckoutWithDiscount struct {
	v20260825.Checkout
	Discounts *DiscountsObject `json:"discounts,omitempty"`
}
