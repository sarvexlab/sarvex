// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package base

import (
	"encoding/json"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/commerce/model"
)

// ─── Status ───────────────────────────────────────────────────────────────────

// CheckoutStatus is an alias for the canonical checkout lifecycle state.
type CheckoutStatus = model.CheckoutStatus

const (
	CheckoutStatusIncomplete         = model.CheckoutStatusIncomplete
	CheckoutStatusRequiresEscalation = model.CheckoutStatusRequiresEscalation
	CheckoutStatusReadyForComplete   = model.CheckoutStatusReadyForComplete
	CheckoutStatusCompleteInProgress = model.CheckoutStatusCompleteInProgress
	CheckoutStatusCompleted          = model.CheckoutStatusCompleted
	CheckoutStatusCanceled           = model.CheckoutStatusCanceled
)

// ─── Checkout ─────────────────────────────────────────────────────────────────

// CheckoutResponse is the connector-facing checkout response. A flat composite
// using canonical v20260408 types throughout — no parallel type system needed.
type CheckoutResponse struct {
	ID     string         `json:"id,omitempty"`
	Status CheckoutStatus `json:"status,omitempty"`
	// Currency is an ISO 4217 currency code. Required by the canonical
	// v20260408/v20260825 Checkout schema (see Checkout.Validate's
	// required-field list) — every connector must set this from its
	// store's configured currency.
	Currency    string                  `json:"currency,omitempty"`
	LineItems   []model.LineItem        `json:"line_items,omitempty"`
	Totals      model.Totals            `json:"totals,omitempty"`
	Buyer       *model.BuyerWithConsent `json:"buyer,omitempty"`
	Fulfillment *model.Fulfillment      `json:"fulfillment,omitempty"`
	Discounts   *model.DiscountsObject  `json:"discounts,omitempty"`
	// Payment reflects the buyer-submitted payment state (instruments only —
	// never a credential, per architecture invariant 8). Present so an agent
	// can read back the payment on the checkout it is building; the serving
	// plane fills it from the submitted payment when the connector leaves it
	// unset (checkout_service.go's echoablePayment).
	Payment *ResponsePayment         `json:"payment,omitempty"`
	Order   *model.OrderConfirmation `json:"order,omitempty"`
	// Links are mandatory for legal compliance per the canonical Checkout
	// schema (Checkout.Validate's required-field list includes "links");
	// no omitempty here to match. WooCommerce/Magento have no reliable
	// source for real policy-page URLs, so this is usually empty rather
	// than absent — see MarshalJSON, which defaults a nil slice to `[]`
	// so the key is always present on the wire.
	Links       []model.Link    `json:"links"`
	Messages    []model.Message `json:"messages,omitempty"`
	ContinueURL string          `json:"continue_url,omitempty"`
}

// ResponsePayment is the payment object on a checkout response. Instruments
// carries no omitempty and is always rendered (as [] when empty) because an
// agent iterates it — an absent or null value would break that. Credentials
// are never included (architecture invariant 8); this mirrors only the
// selected instruments' non-credential fields.
type ResponsePayment struct {
	Instruments []model.SelectedPaymentInstrument `json:"instruments"`
}

// MarshalJSON renders a nil Instruments as [] rather than null, so the field
// is always an iterable array on the wire.
func (p ResponsePayment) MarshalJSON() ([]byte, error) {
	if p.Instruments == nil {
		p.Instruments = []model.SelectedPaymentInstrument{}
	}
	type alias ResponsePayment
	return json.Marshal(alias(p))
}

// MarshalJSON defaults a nil Links to an explicit empty array. Links has no
// omitempty (see field comment), but a nil slice would otherwise still
// serialize as `null`, which fails the canonical schema's required, non-
// nullable "links": []Link just as surely as an absent key does.
func (r CheckoutResponse) MarshalJSON() ([]byte, error) {
	if r.Links == nil {
		r.Links = []model.Link{}
	}
	type alias CheckoutResponse
	return json.Marshal(alias(r))
}

// ─── Cart ─────────────────────────────────────────────────────────────────────

// CartResponse is the connector-facing cart response.
type CartResponse struct {
	ID        string           `json:"id,omitempty"`
	LineItems []model.LineItem `json:"line_items,omitempty"`
	// Currency is required on a cart response (cart.json: ucp, id, line_items,
	// currency, totals); the merchant determines it.
	Currency    string                 `json:"currency,omitempty"`
	Totals      model.Totals           `json:"totals,omitempty"`
	Buyer       *model.Buyer           `json:"buyer,omitempty"`
	Discounts   *model.DiscountsObject `json:"discounts,omitempty"`
	Messages    []model.Message        `json:"messages,omitempty"`
	ContinueURL string                 `json:"continue_url,omitempty"`
}

// ─── Catalog ──────────────────────────────────────────────────────────────────

// CatalogSearchResponse is the connector-facing catalog search response.
type CatalogSearchResponse struct {
	Products []model.Product `json:"products,omitempty"`
	Cursor   *string         `json:"cursor,omitempty"`
	HasMore  bool            `json:"has_more"`
}

// CatalogLookupResponse is the connector-facing catalog lookup response.
type CatalogLookupResponse struct {
	Product *model.Product `json:"product,omitempty"`
}

// ─── Order events ─────────────────────────────────────────────────────────────

// OrderEvent is the internal event emitted by connectors on order state changes.
// It has no canonical v20260408 equivalent — the spec's Order shape is response-only.
type OrderEvent struct {
	OrderID  string        `json:"order_id"`
	Status   string        `json:"status"`
	Items    []OrderItem   `json:"items,omitempty"`
	Tracking *TrackingInfo `json:"tracking,omitempty"`
}

// OrderItem is an individual order line in an OrderEvent.
type OrderItem struct {
	LineItemID string `json:"line_item_id"`
	Quantity   int    `json:"quantity"`
	Status     string `json:"status"`
}

// TrackingInfo holds shipment tracking data in an OrderEvent.
type TrackingInfo struct {
	Carrier        string `json:"carrier,omitempty"`
	TrackingNumber string `json:"tracking_number,omitempty"`
	TrackingURL    string `json:"tracking_url,omitempty"`
}
