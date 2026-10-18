// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"fmt"
)

// Order is an order with line items, buyer-facing fulfillment
// expectations, and event logs.
type Order struct {
	ID string `json:"id,omitempty"`
	// Label is a human-readable order label. MUST only be provided by the business.
	Label        string            `json:"label,omitempty"`
	CheckoutID   string            `json:"checkout_id,omitempty"`
	PermalinkURL string            `json:"permalink_url,omitempty"`
	LineItems    []OrderLineItem   `json:"line_items,omitempty"`
	Fulfillment  *OrderFulfillment `json:"fulfillment,omitempty"`
	Adjustments  []Adjustment      `json:"adjustments,omitempty"`
	Currency     string            `json:"currency,omitempty"`
	Totals       Totals            `json:"totals,omitempty"`
	Messages     []Message         `json:"messages,omitempty"`
	// Attribution is a read-only snapshot of the originating checkout's attribution.
	Attribution *Attribution `json:"attribution,omitempty"`
}

// Validate checks the required fields for an Order
// (["ucp", "id", "checkout_id", "permalink_url", "line_items",
// "fulfillment", "currency", "totals"]) and the Totals invariant.
func (o Order) Validate() error {
	if o.ID == "" {
		return fmt.Errorf("ucp: order: id required")
	}
	if o.CheckoutID == "" {
		return fmt.Errorf("ucp: order: checkout_id required")
	}
	if o.PermalinkURL == "" {
		return fmt.Errorf("ucp: order: permalink_url required")
	}
	if o.Fulfillment == nil {
		return fmt.Errorf("ucp: order: fulfillment required")
	}
	if o.Currency == "" {
		return fmt.Errorf("ucp: order: currency required")
	}
	if len(o.Totals) > 0 {
		if err := o.Totals.Validate(); err != nil {
			return fmt.Errorf("ucp: order: %w", err)
		}
	}
	return nil
}
