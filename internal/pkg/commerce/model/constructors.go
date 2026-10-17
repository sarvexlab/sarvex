// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package model

// NewFulfillmentOption is a fulfillment option with a single total, the shape
// every platform's shipping or pickup rate takes once its own amount format is
// converted to minor units.
func NewFulfillmentOption(id, title string, amountMinor int64) FulfillmentOption {
	return FulfillmentOption{
		ID:     id,
		Title:  title,
		Totals: Totals{{Type: TotalTypeTotal, Amount: SignedAmount(amountMinor)}},
	}
}
