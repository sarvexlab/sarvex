// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sarvexlab/sarvex/internal/platform"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// MerchantResolver answers the one question the agent cannot answer from
// the protocol alone: which merchant owns a given store. Order tracking
// reads Sarvex's own record through the control plane, and that call needs
// a merchant-scoped bearer token.
//
// Deliberately narrow, and deliberately an interface: the only production
// implementation is the sync plane's connector repository, but this package
// is shared protocol machinery (sarvex-v1-architecture.md) and must not import a plane.
// Callers adapt whatever they have at their own composition boundary — the
// same pattern internal/planes/control/api/activation_adapters.go uses.
type MerchantResolver interface {
	MerchantForStore(ctx context.Context, storeID string) (string, error)
}

// MerchantResolverFunc adapts a plain function to MerchantResolver, so a
// caller can supply a closure over whatever repository it already holds
// without declaring a type for it.
type MerchantResolverFunc func(ctx context.Context, storeID string) (string, error)

func (f MerchantResolverFunc) MerchantForStore(ctx context.Context, storeID string) (string, error) {
	return f(ctx, storeID)
}

// Agent plays the *agent* role Google's checkout surface plays in
// production. It drives discovery, catalog search, and the checkout
// create/update/complete lifecycle against a live store's serving plane
// exactly as a real UCP agent would, then reads back what Sarvex recorded
// via the control plane's order-tracking endpoint.
//
// It talks to the serving and control planes over HTTP only, never via a Go
// import, matching the plane-isolation invariant (sarvex-v1-architecture.md A10.1) — which is
// also why the wire shapes it decodes are declared here rather than imported
// from the serving plane.
type Agent struct {
	servingBaseURL string // e.g. http://localhost:8083/ucp (no trailing slash)
	controlBaseURL string // e.g. http://localhost:8081/v1 (no trailing slash)
	merchants      MerchantResolver
	httpClient     *http.Client
}

// NewAgent constructs an Agent. servingBaseURL and controlBaseURL match the
// SERVING_BASE_URL/CONTROL_PLANE_BASE_URL env vars scripts/dev/up.sh
// exports. merchants may be nil when the caller never calls TrackOrder —
// every other method is pure protocol and needs no merchant lookup.
func NewAgent(servingBaseURL, controlBaseURL string, merchants MerchantResolver) *Agent {
	return &Agent{
		servingBaseURL: strings.TrimRight(servingBaseURL, "/"),
		controlBaseURL: strings.TrimRight(controlBaseURL, "/"),
		merchants:      merchants,
		httpClient:     &http.Client{Timeout: 20 * time.Second},
	}
}

// LineItemInput is one line an agent wants to buy, identified by SKU (the
// catalog item id the serving plane's catalog search/lookup returns).
type LineItemInput struct {
	SKU      string
	Quantity int
}

// BuyerInput is the buyer information an agent supplies on checkout update.
type BuyerInput struct {
	Email     string
	FirstName string
	LastName  string
	Phone     string
}

// AddressInput is a fulfillment destination an agent supplies on checkout
// update.
type AddressInput struct {
	FirstName   string
	LastName    string
	Street      string
	City        string
	Region      string
	PostalCode  string
	CountryCode string
}

// Profile is the subset of a store's `.well-known/ucp` response the agent
// needs: the negotiated payment handler ids it can pass to CompleteCheckout.
type Profile struct {
	Raw             map[string]any
	PaymentHandlers []string // handler ids, in profile order
}

func (a *Agent) checkoutBaseURL(storeID string) string {
	// The path's {platform} segment is decorative — the serving plane
	// resolves entirely on {storeID} (negotiation.WithStoreConfigProvider
	// reads only c.Param("storeID")) — so any platform string reaches the
	// same store; the registry's default stands in for it.
	return fmt.Sprintf("%s/%s/%s", a.servingBaseURL, platform.Default(), storeID)
}

// FetchProfile fetches and parses a store's UCP business profile.
func (a *Agent) FetchProfile(ctx context.Context, storeID string) (*Profile, error) {
	var raw map[string]any
	if err := a.doJSON(ctx, http.MethodGet, a.checkoutBaseURL(storeID)+"/.well-known/ucp", nil, nil, &raw); err != nil {
		return nil, fmt.Errorf("fetch profile: %w", err)
	}

	profile := &Profile{Raw: raw}
	ucpBlock, _ := raw["ucp"].(map[string]any)
	handlers, _ := ucpBlock["payment_handlers"].(map[string]any)
	for _, entries := range handlers {
		list, ok := entries.([]any)
		if !ok || len(list) == 0 {
			continue
		}
		entry, ok := list[0].(map[string]any)
		if !ok {
			continue
		}
		if id, ok := entry["id"].(string); ok && id != "" {
			profile.PaymentHandlers = append(profile.PaymentHandlers, id)
		}
	}
	return profile, nil
}

// CatalogItem mirrors the wire shape GET /catalog/{itemId} returns: the
// reconciled ucp.dev Product (model.Product), built by
// each version codec's catalog rendering (internal/spec/vYYYYMMDD/codec/
// catalog.go). Declared here rather than
// imported from the serving plane, for the reason given on Agent: this is
// an HTTP client, and importing a plane's Go types would couple it to a
// plane's internals.
//
// This deliberately mirrors the *wire* shape, not serving.CacheProduct. An
// earlier version of this struct copied the cache projection — flat
// description/price/stock — which decodes nothing: the route renders a
// Product, where description is an object, price lives in price_range and
// in each variant, and stock is a variant's availability. The conformance
// suite reported that mismatch as a product defect ("cannot unmarshal
// object into ... Description of type string") until the shape was
// corrected here.
type CatalogItem struct {
	// ID is the product's Global ID. The catalog codecs set it to
	// the SKU, which is what SKU() returns and what a lookup is keyed by.
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Description Description `json:"description"`
	PriceRange  PriceRange  `json:"price_range"`
	Variants    []Variant   `json:"variants"`
}

// Description is ucp.dev's structured product description.
type Description struct {
	Plain string `json:"plain,omitempty"`
	HTML  string `json:"html,omitempty"`
}

// Money is an amount in minor units with its currency, as ucp.dev renders
// prices on the wire (`amount`, not Sarvex's internal `amount_minor`).
type Money struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

// PriceRange is a product's price span; a single-price product reports the
// same value as both bounds.
type PriceRange struct {
	Min Money `json:"min"`
	Max Money `json:"max"`
}

// VariantAvailability is a variant's stock state.
type VariantAvailability struct {
	Available bool   `json:"available"`
	Status    string `json:"status"`
}

// Variant is one purchasable variant of a product. Sarvex renders exactly
// one per cached product today, but the field is a list on the wire and is
// read as one.
type Variant struct {
	ID           string               `json:"id"`
	SKU          string               `json:"sku"`
	Title        string               `json:"title"`
	Description  Description          `json:"description"`
	Price        Money                `json:"price"`
	Availability *VariantAvailability `json:"availability,omitempty"`
}

// SKU returns the item's stock-keeping unit, preferring the featured
// variant's and falling back to the product id (which
// catalog codecs set to the SKU).
func (c *CatalogItem) SKU() string {
	if len(c.Variants) > 0 && c.Variants[0].SKU != "" {
		return c.Variants[0].SKU
	}
	return c.ID
}

// Price returns the item's price, preferring the featured variant's over
// the product-level range so a multi-variant product reports what would
// actually be charged.
func (c *CatalogItem) Price() Money {
	if len(c.Variants) > 0 && c.Variants[0].Price.Amount > 0 {
		return c.Variants[0].Price
	}
	return c.PriceRange.Min
}

// InStock reports whether the featured variant is purchasable. A product
// with no variant, or a variant with no availability block, is reported as
// out of stock: an agent must not treat an absent answer as a yes.
func (c *CatalogItem) InStock() bool {
	if len(c.Variants) == 0 || c.Variants[0].Availability == nil {
		return false
	}
	return c.Variants[0].Availability.Available
}

// CatalogSearchProduct mirrors the reconciled ucp.dev Product shape
// (dev.ucp.shopping.catalog.search's search_response.products) — only the
// fields this simulator actually surfaces (see printJSON callers in
// cmd/sim/catalog.go for the full wire body).
type CatalogSearchProduct struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description struct {
		Plain string `json:"plain,omitempty"`
	} `json:"description"`
	PriceRange struct {
		Min struct {
			Amount   int64  `json:"amount"`
			Currency string `json:"currency"`
		} `json:"min"`
	} `json:"price_range"`
}

// CatalogSearchResult is POST /catalog/search's response envelope — the
// reconciled ucp.dev search_response shape (products/pagination), not
// Sarvex's own {"data":[...],"page":{...}} list envelope: serving-plane
// UCP endpoints conform to ucp.dev, not Sarvex's own OpenAPI conventions
// (sarvex-v1-architecture.md).
type CatalogSearchResult struct {
	Products   []CatalogSearchProduct `json:"products"`
	Pagination struct {
		Cursor      string `json:"cursor"`
		HasNextPage bool   `json:"has_next_page"`
	} `json:"pagination"`
}

// SearchCatalog searches a store's live catalog (POST /catalog/search) —
// the "browse the catalog to build a cart" step an agent runs before
// creating a checkout. search_catalog is POST-only per the reconciled
// ucp.dev REST transport (source/services/shopping/rest.openapi.json) — a
// JSON body, not GET+query params. limit <= 0 and an empty cursor omit
// Pagination from the request entirely, letting the serving plane apply
// its own defaults.
func (a *Agent) SearchCatalog(ctx context.Context, storeID, query string, limit int, cursor string) (*CatalogSearchResult, error) {
	body := map[string]any{}
	if query != "" {
		body["query"] = query
	}
	if limit > 0 || cursor != "" {
		pagination := map[string]any{}
		if limit > 0 {
			pagination["limit"] = limit
		}
		if cursor != "" {
			pagination["cursor"] = cursor
		}
		body["pagination"] = pagination
	}
	endpoint := a.checkoutBaseURL(storeID) + "/catalog/search"

	var resp CatalogSearchResult
	if err := a.doJSON(ctx, http.MethodPost, endpoint, body, nil, &resp); err != nil {
		return nil, fmt.Errorf("search catalog: %w", err)
	}
	return &resp, nil
}

// LookupCatalogItem retrieves a single catalog item by id
// (GET /catalog/{itemId}) — the same lookup an agent runs to confirm a
// specific item's current price/stock before adding it to a checkout.
func (a *Agent) LookupCatalogItem(ctx context.Context, storeID, itemID string) (*CatalogItem, error) {
	endpoint := fmt.Sprintf("%s/catalog/%s", a.checkoutBaseURL(storeID), url.PathEscape(itemID))
	var item CatalogItem
	if err := a.doJSON(ctx, http.MethodGet, endpoint, nil, nil, &item); err != nil {
		return nil, fmt.Errorf("lookup catalog item %s: %w", itemID, err)
	}
	return &item, nil
}

// CreateCheckout creates a new checkout session (POST /checkout-sessions),
// the first step of "run a checkout to completion."
func (a *Agent) CreateCheckout(ctx context.Context, storeID string, items []LineItemInput) (*base.CheckoutResponse, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("create checkout: at least one line item is required")
	}
	lineItems := make([]base.LineItemCreateRequest, len(items))
	for i, it := range items {
		lineItems[i] = base.LineItemCreateRequest{Item: base.ItemRef{ID: it.SKU}, Quantity: it.Quantity}
	}
	body := map[string]any{"line_items": lineItems}

	headers := map[string]string{"Idempotency-Key": fmt.Sprintf("sim-checkout-create-%d", time.Now().UnixNano())}
	var resp base.CheckoutResponse
	url := a.checkoutBaseURL(storeID) + "/checkout-sessions"
	if err := a.doJSON(ctx, http.MethodPost, url, body, headers, &resp); err != nil {
		return nil, fmt.Errorf("create checkout: %w", err)
	}
	return &resp, nil
}

// UpdateCheckout replaces the session's buyer/fulfillment (PUT semantics
// per spec) — the "checkout update" step, e.g. supplying the buyer and
// shipping address the connector needs to compute live shipping/tax.
//
// The destination goes out in the ucp.dev fulfillment wire shape (methods →
// destinations + selected_destination_id), which is the only shape the
// serving plane reads. A business answers a destination with the shipping
// options it offers; an agent then chooses one, as a buyer would, with a
// second full-replacement PUT. UpdateCheckout does both, so a checkout it
// updates is ready to complete, and returns the final response.
func (a *Agent) UpdateCheckout(ctx context.Context, storeID, checkoutID string, buyer *BuyerInput, address *AddressInput) (*base.CheckoutResponse, error) {
	resp, err := a.putCheckout(ctx, storeID, checkoutID, buyer, address, nil)
	if err != nil {
		return nil, err
	}
	if address == nil {
		return resp, nil
	}
	selected := firstUnselectedOptions(resp.Fulfillment)
	if len(selected) == 0 {
		return resp, nil
	}
	return a.putCheckout(ctx, storeID, checkoutID, buyer, address, selected)
}

// putCheckout sends one full-replacement PUT. selections maps a fulfillment
// group id to the option id the agent chooses for it.
func (a *Agent) putCheckout(ctx context.Context, storeID, checkoutID string, buyer *BuyerInput, address *AddressInput, selections map[string]string) (*base.CheckoutResponse, error) {
	body := map[string]any{}
	if buyer != nil {
		body["buyer"] = model.BuyerWithConsent{Buyer: model.Buyer{
			Email:       buyer.Email,
			FirstName:   buyer.FirstName,
			LastName:    buyer.LastName,
			PhoneNumber: buyer.Phone,
		}}
	}
	if address != nil {
		const destinationID = "dest-1"
		method := fulfillmentMethodRequest{
			Type: "shipping",
			Destinations: []fulfillmentDestinationRequest{{
				ID: destinationID,
				PostalAddress: model.PostalAddress{
					FirstName:       address.FirstName,
					LastName:        address.LastName,
					StreetAddress:   address.Street,
					AddressLocality: address.City,
					AddressRegion:   address.Region,
					PostalCode:      address.PostalCode,
					AddressCountry:  address.CountryCode,
				},
			}},
			SelectedDestinationID: destinationID,
		}
		for groupID, optionID := range selections {
			method.Groups = append(method.Groups, fulfillmentGroupRequest{ID: groupID, SelectedOptionID: optionID})
		}
		body["fulfillment"] = fulfillmentRequest{Methods: []fulfillmentMethodRequest{method}}
	}

	var resp base.CheckoutResponse
	url := fmt.Sprintf("%s/checkout-sessions/%s", a.checkoutBaseURL(storeID), checkoutID)
	if err := a.doJSON(ctx, http.MethodPut, url, body, nil, &resp); err != nil {
		return nil, fmt.Errorf("update checkout: %w", err)
	}
	return &resp, nil
}

// fulfillmentRequest and friends are the checkout `fulfillment` request
// object on the wire (ucp.dev shopping fulfillment).
type fulfillmentRequest struct {
	Methods []fulfillmentMethodRequest `json:"methods"`
}

type fulfillmentMethodRequest struct {
	Type                  string                          `json:"type"`
	Destinations          []fulfillmentDestinationRequest `json:"destinations,omitempty"`
	SelectedDestinationID string                          `json:"selected_destination_id,omitempty"`
	Groups                []fulfillmentGroupRequest       `json:"groups,omitempty"`
}

type fulfillmentDestinationRequest struct {
	ID string `json:"id"`
	model.PostalAddress
}

type fulfillmentGroupRequest struct {
	ID               string `json:"id"`
	SelectedOptionID string `json:"selected_option_id"`
}

// firstUnselectedOptions picks the first offered option of every
// fulfillment group that has options but no selection yet, keyed by group id.
func firstUnselectedOptions(f *model.Fulfillment) map[string]string {
	if f == nil {
		return nil
	}
	out := map[string]string{}
	for _, m := range f.Methods {
		for _, g := range m.Groups {
			if g.ID == "" || len(g.Options) == 0 {
				continue
			}
			if g.SelectedOptionID != nil && *g.SelectedOptionID != "" {
				continue
			}
			out[g.ID] = g.Options[0].ID
		}
	}
	return out
}

// CompleteCheckoutResult is CompleteCheckout's parsed response — the
// serving plane's complete endpoint returns a bespoke envelope (handler.go
// CheckoutComplete), not a bare base.CheckoutResponse.
type CompleteCheckoutResult struct {
	SessionID string `json:"session_id"`
	State     string `json:"state"`
	Status    string `json:"status,omitempty"`
	Order     *struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		Total     int64  `json:"total"`
		Currency  string `json:"currency"`
		Permalink string `json:"permalink"`
	} `json:"order,omitempty"`
	ContinueURL string `json:"continue_url,omitempty"`
}

// CompleteCheckout submits an opaque payment credential + handler_id to
// finalize the checkout — the "complete the checkout" step. The credential
// is a fabricated sim token; no real payment ever moves, matching how the
// serving plane's schema has no field able to hold a raw credential
// (architecture invariant 8) — Sarvex only ever sees this opaque shape.
func (a *Agent) CompleteCheckout(ctx context.Context, storeID, checkoutID, handlerID string) (*CompleteCheckoutResult, error) {
	if handlerID == "" {
		return nil, fmt.Errorf("complete checkout: handler_id is required")
	}
	payment := model.Payment{
		Instruments: []model.SelectedPaymentInstrument{{
			PaymentInstrument: model.PaymentInstrument{
				HandlerID: handlerID,
				Type:      "card",
				Credential: &model.PaymentCredential{
					Token: &model.TokenCredential{Type: "sim_token", Token: "sim-tok-" + checkoutID},
				},
			},
			Selected: true,
		}},
	}
	body := map[string]any{"payment": payment}

	headers := map[string]string{"Idempotency-Key": fmt.Sprintf("sim-checkout-complete-%s", checkoutID)}
	var resp CompleteCheckoutResult
	url := fmt.Sprintf("%s/checkout-sessions/%s/complete", a.checkoutBaseURL(storeID), checkoutID)
	if err := a.doJSON(ctx, http.MethodPost, url, body, headers, &resp); err != nil {
		return nil, fmt.Errorf("complete checkout: %w", err)
	}
	return &resp, nil
}

// TrackedOrder is what TrackOrder reads back — Sarvex's own record of the
// order (sarvex-v1-control-plane.yaml `order` schema), not a UCP resource:
// V1 has no agent-facing order-read capability (order status-sync is a
// documented gap — architecture G2), so tracking an order after checkout
// means asking Sarvex what it recorded, the same way the console would.
type TrackedOrder struct {
	OrderID           string `json:"order_id"`
	CheckoutSessionID string `json:"checkout_session_id"`
	StoreOrderRef     string `json:"store_order_ref"`
	Status            string `json:"status"`
	SyncState         string `json:"sync_state"`
	Value             struct {
		AmountMinor int64  `json:"amount_minor"`
		Currency    string `json:"currency"`
	} `json:"value"`
	PlacedAt time.Time `json:"placed_at"`
}

// TrackOrder reads back an order Sarvex recorded from a completed checkout,
// via the control plane's operator/merchant-facing order-tracking endpoint
// (GET /stores/{id}/orders/{order_id}) — see internal/planes/control/api
// listOrders/getOrder and internal/planes/sync/checkout_order_intent.go for
// how it gets written.
func (a *Agent) TrackOrder(ctx context.Context, storeID, orderID string) (*TrackedOrder, error) {
	merchantID, err := a.resolveMerchant(ctx, storeID)
	if err != nil {
		return nil, err
	}
	token, err := DevBearerToken(RoleOwner, merchantID)
	if err != nil {
		return nil, fmt.Errorf("mint dev token: %w", err)
	}

	url := fmt.Sprintf("%s/stores/%s/orders/%s", a.controlBaseURL, storeID, orderID)
	headers := map[string]string{"Authorization": "Bearer " + token}
	var order TrackedOrder
	if err := a.doJSON(ctx, http.MethodGet, url, nil, headers, &order); err != nil {
		return nil, fmt.Errorf("track order: %w", err)
	}
	return &order, nil
}

func (a *Agent) resolveMerchant(ctx context.Context, storeID string) (string, error) {
	if a.merchants == nil {
		return "", fmt.Errorf("track order: agent was built without a MerchantResolver")
	}
	return a.merchants.MerchantForStore(ctx, storeID)
}

// doJSON performs an HTTP request with an optional JSON body and decodes a
// JSON response, returning an error that includes the response body on any
// non-2xx status so a failed sim run tells the operator exactly what the
// serving/control plane said instead of just a status code.
func (a *Agent) doJSON(ctx context.Context, method, url string, body any, headers map[string]string, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: status %d: %s", method, url, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
