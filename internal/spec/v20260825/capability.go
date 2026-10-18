// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package v20260825

import "fmt"

// The Payment capability schema (instruments only) lives in payment.go
// alongside the other payment-related types — see the Payment type there.

// Cart is a shopping cart with estimated pricing before checkout:
// lightweight pre-purchase exploration with no payment info or complex
// status states.
//
// New in this spec version: Actions (outstanding extension-defined
// actions) and Policies (a return/refund/warranty terms snapshot).
type Cart struct {
	UCP         CartResponseMetadata `json:"ucp,omitempty"`
	ID          string               `json:"id,omitempty"`
	LineItems   []LineItem           `json:"line_items"`
	Context     *Context             `json:"context,omitempty"`
	Signals     *Signals             `json:"signals,omitempty"`
	Attribution Attribution          `json:"attribution,omitempty"`
	// Buyer carries optional buyer information for personalized estimates.
	Buyer *Buyer `json:"buyer,omitempty"`
	// Currency is an ISO 4217 currency code, determined by the merchant
	// from context or geo-IP.
	Currency string `json:"currency,omitempty"`
	// Totals is the estimated cost breakdown; may be partial if
	// shipping/tax are not yet calculable.
	Totals Totals `json:"totals,omitempty"`
	// Actions are outstanding extension-defined Actions for this cart.
	Actions  Actions   `json:"actions,omitempty"`
	Messages []Message `json:"messages,omitempty"`
	Links    []Link    `json:"links,omitempty"`
	// Policies are return/refund/warranty terms applying to the items in
	// this cart.
	Policies []Policy `json:"policies,omitempty"`
	// ContinueURL enables cart handoff, sharing, and session recovery.
	ContinueURL string `json:"continue_url,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// Validate checks the required fields for a full Cart response
// (["ucp", "id", "line_items", "currency", "totals"]) and the Totals
// invariant. Request payloads (create/update) carry a different required
// set per the spec's ucp_request annotations and are not covered here.
func (c Cart) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("ucp: cart: id required")
	}
	if c.Currency == "" {
		return fmt.Errorf("ucp: cart: currency required")
	}
	if len(c.Totals) > 0 {
		if err := c.Totals.Validate(); err != nil {
			return fmt.Errorf("ucp: cart: %w", err)
		}
	}
	return nil
}

// Checkout is the base checkout schema. Extensions (ap2, buyerconsent,
// discount, fulfillment) compose additional fields onto this via Go
// struct embedding — see the extensions subpackages.
//
// New in this spec version: Actions and Policies (see Cart).
type Checkout struct {
	UCP         CheckoutResponseMetadata `json:"ucp,omitempty"`
	ID          string                   `json:"id,omitempty"`
	LineItems   []LineItem               `json:"line_items"`
	Buyer       *Buyer                   `json:"buyer,omitempty"`
	Context     *Context                 `json:"context,omitempty"`
	Signals     *Signals                 `json:"signals,omitempty"`
	Attribution Attribution              `json:"attribution,omitempty"`
	// Status indicates the current phase and required action. See
	// CheckoutStatus for the lifecycle values.
	Status   CheckoutStatus `json:"status,omitempty"`
	Currency string         `json:"currency,omitempty"`
	Totals   Totals         `json:"totals,omitempty"`
	// Actions are outstanding extension-defined Actions for this checkout.
	Actions  Actions   `json:"actions,omitempty"`
	Messages []Message `json:"messages,omitempty"`
	// Links are mandatory for legal compliance (Privacy Policy, TOS).
	Links []Link `json:"links"`
	// Policies are return/refund/warranty terms applying to the items in
	// this checkout.
	Policies  []Policy `json:"policies,omitempty"`
	ExpiresAt string   `json:"expires_at,omitempty"`
	// ContinueURL is for checkout handoff and session recovery. MUST be
	// present when Status is CheckoutStatusRequiresEscalation.
	ContinueURL string             `json:"continue_url,omitempty"`
	Payment     *Payment           `json:"payment,omitempty"`
	Order       *OrderConfirmation `json:"order,omitempty"`
}

// Validate checks the required fields for a full Checkout response
// (["ucp", "id", "line_items", "status", "currency", "totals", "links"]),
// the Totals invariant, and the continue_url-on-requires_escalation rule.
func (c Checkout) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("ucp: checkout: id required")
	}
	if c.Currency == "" {
		return fmt.Errorf("ucp: checkout: currency required")
	}
	if len(c.Totals) > 0 {
		if err := c.Totals.Validate(); err != nil {
			return fmt.Errorf("ucp: checkout: %w", err)
		}
	}
	if c.Status == CheckoutStatusRequiresEscalation && c.ContinueURL == "" {
		return fmt.Errorf("ucp: checkout: continue_url required when status is requires_escalation")
	}
	return nil
}

// OrderPlatformConfig is the platform's order capability configuration:
// where the merchant should send order lifecycle webhooks.
type OrderPlatformConfig struct {
	WebhookURL string `json:"webhook_url"`
}

// Order is an order with line items, buyer-facing fulfillment
// expectations, and event logs.
//
// New in this spec version: Policies, a durable snapshot of the policies
// that applied to the items at checkout (unlike Cart/Checkout, Order does
// not carry Actions).
type Order struct {
	UCP OrderResponseMetadata `json:"ucp,omitempty"`
	ID  string                `json:"id,omitempty"`
	// Label is a human-readable order label. MUST only be provided by the business.
	Label        string            `json:"label,omitempty"`
	CheckoutID   string            `json:"checkout_id,omitempty"`
	PermalinkURL string            `json:"permalink_url,omitempty"`
	LineItems    []OrderLineItem   `json:"line_items,omitempty"`
	Fulfillment  *OrderFulfillment `json:"fulfillment,omitempty"`
	Adjustments  []Adjustment      `json:"adjustments,omitempty"`
	Currency     string            `json:"currency,omitempty"`
	Totals       Totals            `json:"totals,omitempty"`
	// Policies is a snapshot of the policies that applied to the items at
	// checkout, captured on the order as a durable record.
	Policies []Policy  `json:"policies,omitempty"`
	Messages []Message `json:"messages,omitempty"`
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

// CatalogSearchRequest is the request body for the catalog search operation.
type CatalogSearchRequest struct {
	// Query is a free-text search query.
	Query       string             `json:"query,omitempty"`
	Context     *Context           `json:"context,omitempty"`
	Signals     *Signals           `json:"signals,omitempty"`
	Attribution Attribution        `json:"attribution,omitempty"`
	Filters     *SearchFilters     `json:"filters,omitempty"`
	Pagination  *PaginationRequest `json:"pagination,omitempty"`
}

// CatalogSearchResponse is the response body for the catalog search
// operation. New in this spec version: Actions and Policies.
type CatalogSearchResponse struct {
	UCP        CatalogResponseMetadata `json:"ucp"`
	Products   []Product               `json:"products"`
	Pagination *PaginationResponse     `json:"pagination,omitempty"`
	Actions    Actions                 `json:"actions,omitempty"`
	Messages   []Message               `json:"messages,omitempty"`
	Policies   []Policy                `json:"policies,omitempty"`
}

// CatalogLookupRequest is the request body for batch product/variant
// lookup by identifier.
type CatalogLookupRequest struct {
	// IDs are identifiers to look up. Implementations MUST support
	// product ID and variant ID; MAY support secondary identifiers.
	IDs         []string       `json:"ids"`
	Filters     *SearchFilters `json:"filters,omitempty"`
	Context     *Context       `json:"context,omitempty"`
	Signals     *Signals       `json:"signals,omitempty"`
	Attribution Attribution    `json:"attribution,omitempty"`
}

// CatalogLookupProduct is a Product whose variants carry the correlation
// metadata (LookupVariant) required in lookup responses. Its Variants
// field shadows Product.Variants at JSON marshal/unmarshal time.
type CatalogLookupProduct struct {
	Product
	Variants []LookupVariant `json:"variants"`
}

// CatalogLookupResponse is the response body for batch product/variant
// lookup. New in this spec version: Actions and Policies.
type CatalogLookupResponse struct {
	UCP      CatalogResponseMetadata `json:"ucp"`
	Products []CatalogLookupProduct  `json:"products"`
	Actions  Actions                 `json:"actions,omitempty"`
	Messages []Message               `json:"messages,omitempty"`
	Policies []Policy                `json:"policies,omitempty"`
}

// GetProductRequest is the request body for single-product retrieval,
// supporting interactive variant narrowing via Selected and Preferences.
type GetProductRequest struct {
	// ID is a product or variant identifier.
	ID       string           `json:"id"`
	Selected []SelectedOption `json:"selected,omitempty"`
	// Preferences lists option names in relaxation priority order.
	Preferences []string       `json:"preferences,omitempty"`
	Filters     *SearchFilters `json:"filters,omitempty"`
	Context     *Context       `json:"context,omitempty"`
	Signals     *Signals       `json:"signals,omitempty"`
	Attribution Attribution    `json:"attribution,omitempty"`
}

// DetailProductOption is a ProductOption with availability signals
// relative to the effective selections, used in get_product responses.
type DetailProductOption struct {
	Name   string              `json:"name"`
	Values []DetailOptionValue `json:"values"`
}

// DetailProduct is a Product extended with effective option selections
// and availability signals, returned by get_product. Its Options field
// shadows Product.Options at JSON marshal/unmarshal time.
type DetailProduct struct {
	Product
	// Selected anchors the featured variant and availability signals.
	Selected []SelectedOption      `json:"selected,omitempty"`
	Options  []DetailProductOption `json:"options,omitempty"`
}

// GetProductResponse is the response body for single-product retrieval.
// New in this spec version: Actions and Policies.
type GetProductResponse struct {
	UCP CatalogResponseMetadata `json:"ucp"`
	// Product is the requested product with full detail — singular, since
	// this is a single-resource operation.
	Product  DetailProduct `json:"product"`
	Actions  Actions       `json:"actions,omitempty"`
	Messages []Message     `json:"messages,omitempty"`
	Policies []Policy      `json:"policies,omitempty"`
}
