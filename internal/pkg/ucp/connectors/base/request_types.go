// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package base

import (
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/commerce/model"
)

// ─── Line item references ──────────────────────────────────────────────────────

// LineItemCreateRequest specifies a catalog item and quantity for checkout or cart creation.
type LineItemCreateRequest struct {
	Item     ItemRef `json:"item"`
	Quantity int     `json:"quantity"`
}

// LineItemUpdateRequest specifies quantity changes on an existing line item.
// Quantity 0 signals removal.
type LineItemUpdateRequest struct {
	ID       string  `json:"id,omitempty"`
	Item     ItemRef `json:"item"`
	Quantity int     `json:"quantity"`
}

// ItemRef identifies a catalog item by its platform-native ID (SKU or variant ID).
type ItemRef struct {
	ID string `json:"id"`
}

// ─── Fulfillment ──────────────────────────────────────────────────────────────

// FulfillmentRequest carries the buyer's fulfillment selections (platform → business).
type FulfillmentRequest struct {
	// MethodType is the type of the fulfillment method the buyer chose
	// ("shipping" or "pickup"); empty means shipping, the historical default.
	MethodType       string `json:"method_type,omitempty"`
	SelectedMethodID string `json:"selected_method_id,omitempty"`
	// SelectedDestinationID is the id the agent gave the chosen destination, so
	// the business can echo it back on the destination it resolves.
	SelectedDestinationID string                    `json:"selected_destination_id,omitempty"`
	SelectedDestination   *model.PostalAddress      `json:"selected_destination,omitempty"`
	Groups                []FulfillmentGroupRequest `json:"groups,omitempty"`
}

// FulfillmentMethodRequest carries per-method selection information.
type FulfillmentMethodRequest struct {
	ID                    string                    `json:"id"`
	SelectedDestinationID string                    `json:"selected_destination_id,omitempty"`
	Groups                []FulfillmentGroupRequest `json:"groups,omitempty"`
}

// FulfillmentGroupRequest carries the buyer's selected option for one fulfillment group.
type FulfillmentGroupRequest struct {
	ID               string `json:"id"`
	SelectedOptionID string `json:"selected_option_id,omitempty"`
}

// ─── Checkout ─────────────────────────────────────────────────────────────────

// CreateCheckoutRequest is sent to connectors when the platform initiates a checkout.
type CreateCheckoutRequest struct {
	LineItems   []LineItemCreateRequest `json:"line_items"`
	Buyer       *model.BuyerWithConsent `json:"buyer,omitempty"`
	Context     *model.Context          `json:"context,omitempty"`
	Payment     *model.Payment          `json:"payment,omitempty"`
	Fulfillment *FulfillmentRequest     `json:"fulfillment,omitempty"`
	Discounts   *model.DiscountsObject  `json:"discounts,omitempty"`
}

// UpdateCheckoutRequest replaces the full checkout session state (PUT semantics).
type UpdateCheckoutRequest struct {
	LineItems   []LineItemUpdateRequest `json:"line_items,omitempty"`
	Buyer       *model.BuyerWithConsent `json:"buyer,omitempty"`
	Context     *model.Context          `json:"context,omitempty"`
	Payment     *model.Payment          `json:"payment,omitempty"`
	Fulfillment *FulfillmentRequest     `json:"fulfillment,omitempty"`
	Discounts   *model.DiscountsObject  `json:"discounts,omitempty"`
}

// CompleteCheckoutRequest is the final submission to place an order.
// Payment credential is opaque — never stored (architecture invariant 7).
// IdempotencyKey is scoped to the merchant and passed to the payment handler
// (e.g., Stripe's Idempotency-Key header) for exactly-once payment guarantee.
type CompleteCheckoutRequest struct {
	Payment *model.Payment `json:"payment"`
	// Buyer is the buyer the session holds, for backends that can only record
	// the buyer at the moment the order is placed (Magento guest checkout
	// needs the email and billing contact then).
	Buyer          *model.BuyerWithConsent `json:"buyer,omitempty"`
	Context        *model.Context          `json:"context,omitempty"`
	IdempotencyKey string                  `json:"idempotency_key,omitempty"`
}

// ─── Cart ─────────────────────────────────────────────────────────────────────

// CreateCartRequest is sent to connectors when the platform creates a cart.
type CreateCartRequest struct {
	LineItems []LineItemCreateRequest `json:"line_items"`
	Buyer     *model.Buyer            `json:"buyer,omitempty"`
	Context   *model.Context          `json:"context,omitempty"`
	Discounts *model.DiscountsObject  `json:"discounts,omitempty"`
}

// UpdateCartRequest replaces the full cart state (PUT semantics).
type UpdateCartRequest struct {
	LineItems []LineItemUpdateRequest `json:"line_items,omitempty"`
	Buyer     *model.Buyer            `json:"buyer,omitempty"`
	Discounts *model.DiscountsObject  `json:"discounts,omitempty"`
}

// ─── Catalog ──────────────────────────────────────────────────────────────────

// CatalogSearchRequest holds parameters for a connector catalog search.
type CatalogSearchRequest struct {
	Query  string `json:"q,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}
