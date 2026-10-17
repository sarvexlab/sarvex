// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package magento

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// Carts and checkouts are both Magento guest carts (quotes), addressed by the
// masked id POST /guest-carts returns. Cart and checkout responses are built by
// one shared path (cartResponse / checkoutResponse) so every capability and
// extension — line items, discounts, buyer, fulfillment including pickup —
// behaves the same on every call.

// quotePath is the path of a guest cart resource.
func quotePath(cartID string, suffix ...string) string {
	p := "/guest-carts/" + url.PathEscape(cartID)
	for _, s := range suffix {
		p += "/" + s
	}
	return p
}

// lineWant is one line the caller wants on the cart.
type lineWant struct {
	SKU string
	Qty int
}

func createLineWants(items []base.LineItemCreateRequest) []lineWant {
	out := make([]lineWant, 0, len(items))
	for _, li := range items {
		out = append(out, lineWant{SKU: li.Item.ID, Qty: li.Quantity})
	}
	return out
}

func updateLineWants(items []base.LineItemUpdateRequest) []lineWant {
	out := make([]lineWant, 0, len(items))
	for _, li := range items {
		out = append(out, lineWant{SKU: li.Item.ID, Qty: li.Quantity})
	}
	return out
}

// createQuote opens an empty guest cart and returns its masked id. Creating a
// cart is not idempotent at the store, so it is never retried.
func (c *Connector) createQuote(ctx context.Context) (string, error) {
	// Magento's guest-carts POST returns a bare quoted string (the masked id).
	var cartID string
	if err := c.client.POSTNoRetry(ctx, "/guest-carts", nil, &cartID); err != nil {
		return "", fmt.Errorf("magento: create cart: %w", err)
	}
	if cartID == "" {
		return "", errors.New("magento: empty cart id returned")
	}
	return cartID, nil
}

// loadCart reads the quote and its totals. Totals live on their own endpoint
// in Magento; a store that embeds them in the cart body is also honoured, and
// a cart whose totals cannot be read still maps (see mapper.cartTotals).
func (c *Connector) loadCart(ctx context.Context, cartID string) (*mgCart, error) {
	var cart mgCart
	if err := c.client.GET(ctx, quotePath(cartID), &cart); err != nil {
		return nil, err
	}
	var totals mgCartTotals
	switch err := c.client.GET(ctx, quotePath(cartID, "totals"), &totals); {
	case err == nil:
		cart.Totals = &totals
	case errors.Is(err, ErrNotFound):
		// keep any embedded totals
	default:
		return nil, err
	}
	return &cart, nil
}

// syncLineItems makes the cart hold exactly the wanted lines (PUT semantics):
// lines not wanted are removed, changed quantities are set, new SKUs are added.
// A quantity of 0 removes the line. Magento's add-item call adds to an existing
// line rather than replacing it, so an existing SKU is changed through the
// item's own endpoint.
func (c *Connector) syncLineItems(ctx context.Context, cartID string, wanted []lineWant) error {
	want := map[string]int{}
	var order []string
	for _, w := range wanted {
		if _, ok := want[w.SKU]; !ok {
			order = append(order, w.SKU)
		}
		want[w.SKU] += w.Qty
	}

	var existing mgCart
	if err := c.client.GET(ctx, quotePath(cartID), &existing); err != nil {
		return err
	}
	have := map[string]bool{}
	for _, item := range existing.Items {
		qty, wanted := want[item.SKU]
		have[item.SKU] = true
		itemPath := quotePath(cartID, "items", fmt.Sprintf("%d", item.ItemID))
		switch {
		case !wanted || qty <= 0:
			if err := c.client.DELETE(ctx, itemPath); err != nil {
				return err
			}
		case float64(qty) != item.Qty:
			body := map[string]any{"cartItem": map[string]any{"item_id": item.ItemID, "qty": qty, "quote_id": cartID}}
			if err := c.client.PUT(ctx, itemPath, body, nil); err != nil {
				return err
			}
		}
	}
	for _, sku := range order {
		if have[sku] || want[sku] <= 0 {
			continue
		}
		body := map[string]any{"cartItem": map[string]any{"sku": sku, "qty": want[sku], "quote_id": cartID}}
		if err := c.client.POST(ctx, quotePath(cartID, "items"), body, nil); err != nil {
			return err
		}
	}
	return nil
}

// applyDiscounts sets the cart's discount codes (PUT-replace: a non-nil object,
// even with no codes, replaces what was applied). Magento applies one coupon
// per cart, so the first code the store accepts is the one applied; a code the
// store refuses is left out, which the agent sees as its absence from the
// applied discounts.
func (c *Connector) applyDiscounts(ctx context.Context, cartID string, d *model.DiscountsObject) error {
	if d == nil {
		return nil
	}
	// Clear the current coupon. A cart without one answers with a client error,
	// which is fine; only a store/transport failure matters.
	if err := c.client.DELETE(ctx, quotePath(cartID, "coupons")); err != nil && !isClientError(err) {
		return err
	}
	for _, code := range d.Codes {
		err := c.client.PUT(ctx, quotePath(cartID, "coupons", url.PathEscape(code)), nil, nil)
		if err == nil {
			return nil
		}
		if !isClientError(err) {
			return err
		}
	}
	return nil
}

// isClientError reports whether err is the store refusing the request (a 4xx),
// as opposed to a store/transport failure.
func isClientError(err error) bool {
	var apiErr *APIError
	return errors.Is(err, ErrNotFound) || (errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500)
}

// setShippingInformation records the shipping destination and chosen method on
// the quote.
func (c *Connector) setShippingInformation(ctx context.Context, cartID string, addr *mgAddress, carrier, method string) error {
	payload := map[string]any{"addressInformation": map[string]any{
		"shipping_address":      addr,
		"shipping_carrier_code": carrier,
		"shipping_method_code":  method,
	}}
	return c.client.POST(ctx, quotePath(cartID, "shipping-information"), payload, nil)
}

// reject turns a store refusal into the business rejection the agent can act
// on; anything else is wrapped as a store failure.
func reject(op string, err error) error {
	if rej := checkoutRejection(err); rej != nil {
		return rej
	}
	return fmt.Errorf("magento: %s: %w", op, err)
}

// ─── Cart ─────────────────────────────────────────────────────────────────────

// CartCreate creates a new guest cart.
func (c *Connector) CartCreate(ctx context.Context, storeID string, req *base.CreateCartRequest) (*base.CartResponse, error) {
	cartID, err := c.createQuote(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.syncLineItems(ctx, cartID, createLineWants(req.LineItems)); err != nil {
		return nil, reject("add cart items", err)
	}
	if err := c.applyDiscounts(ctx, cartID, req.Discounts); err != nil {
		return nil, reject("apply discounts", err)
	}
	return c.cartResponse(ctx, cartID, req.Buyer)
}

// CartGet retrieves a cart.
func (c *Connector) CartGet(ctx context.Context, storeID string, cartID string) (*base.CartResponse, error) {
	resp, err := c.cartResponse(ctx, cartID, nil)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: cart %s", ErrNotFound, cartID)
		}
		return nil, err
	}
	return resp, nil
}

// CartUpdate replaces the cart's line items (and discounts/buyer when supplied)
// and returns the re-priced cart.
func (c *Connector) CartUpdate(ctx context.Context, storeID string, cartID string, req *base.UpdateCartRequest) (*base.CartResponse, error) {
	if len(req.LineItems) > 0 {
		if err := c.syncLineItems(ctx, cartID, updateLineWants(req.LineItems)); err != nil {
			return nil, reject("update cart items", err)
		}
	}
	if err := c.applyDiscounts(ctx, cartID, req.Discounts); err != nil {
		return nil, reject("apply discounts", err)
	}
	return c.cartResponse(ctx, cartID, req.Buyer)
}

// cartResponse reads the cart and maps it, with its discounts. buyer is the
// contact the caller just supplied: a guest cart cannot hold one before a full
// address exists, so it is echoed here and carried into checkout by the
// serving plane.
func (c *Connector) cartResponse(ctx context.Context, cartID string, buyer *model.Buyer) (*base.CartResponse, error) {
	cart, err := c.loadCart(ctx, cartID)
	if err != nil {
		return nil, readError("fetch cart", err)
	}
	resp := c.mapper.cartToCartResponse(cart, cartID, c.cfg.StoreCurrency)
	c.mapper.ApplyCartDiscountExtension(resp, cart.Totals)
	if buyer != nil {
		resp.Buyer = buyer
	}
	return resp, nil
}

// ─── Checkout ─────────────────────────────────────────────────────────────────

// CheckoutCreate creates a new checkout from the requested line items.
func (c *Connector) CheckoutCreate(ctx context.Context, storeID string, req *base.CreateCheckoutRequest) (*base.CheckoutResponse, error) {
	cartID, err := c.createQuote(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.syncLineItems(ctx, cartID, createLineWants(req.LineItems)); err != nil {
		return nil, reject("add checkout items", err)
	}
	choice, err := c.applyFulfillmentSelection(ctx, cartID, req.Fulfillment)
	if err != nil {
		return nil, rejectOrWrap("set fulfillment", err)
	}
	if err := c.applyDiscounts(ctx, cartID, req.Discounts); err != nil {
		return nil, reject("apply discounts", err)
	}
	return c.checkoutResponse(ctx, cartID, choice, req.Buyer)
}

// CheckoutGet retrieves a checkout.
func (c *Connector) CheckoutGet(ctx context.Context, storeID string, checkoutID string) (*base.CheckoutResponse, error) {
	resp, err := c.checkoutResponse(ctx, checkoutID, nil, nil)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: checkout %s", ErrNotFound, checkoutID)
		}
		return nil, err
	}
	return resp, nil
}

// CheckoutUpdate updates a checkout (PUT semantics).
func (c *Connector) CheckoutUpdate(ctx context.Context, storeID string, checkoutID string, req *base.UpdateCheckoutRequest) (*base.CheckoutResponse, error) {
	if len(req.LineItems) > 0 {
		if err := c.syncLineItems(ctx, checkoutID, updateLineWants(req.LineItems)); err != nil {
			return nil, reject("update checkout items", err)
		}
	}
	choice, err := c.applyFulfillmentSelection(ctx, checkoutID, req.Fulfillment)
	if err != nil {
		return nil, rejectOrWrap("update fulfillment", err)
	}
	if err := c.applyDiscounts(ctx, checkoutID, req.Discounts); err != nil {
		return nil, reject("apply discounts", err)
	}
	return c.checkoutResponse(ctx, checkoutID, choice, req.Buyer)
}

// readError classifies a failure to read a cart back: a cart the store no
// longer knows stays ErrNotFound (the caller decides what a missing cart
// means), anything else is a store failure.
func readError(op string, err error) error {
	if errors.Is(err, ErrNotFound) {
		return err
	}
	return fmt.Errorf("magento: %s: %w", op, err)
}

// rejectOrWrap passes an already-formed rejection through and classifies any
// other error.
func rejectOrWrap(op string, err error) error {
	var rej *base.CheckoutRejectedError
	if errors.As(err, &rej) {
		return err
	}
	return reject(op, err)
}

// checkoutResponse reads the quote and builds the checkout response: line
// items, totals, the buyer, discounts and the fulfillment object (shipping
// rates for the destination, plus pickup where the store offers it). buyer is
// the contact the caller just supplied; it is echoed because a guest cart can
// only record it at completion.
func (c *Connector) checkoutResponse(ctx context.Context, cartID string, choice *fulfillmentChoice, buyer *model.BuyerWithConsent) (*base.CheckoutResponse, error) {
	cart, err := c.loadCart(ctx, cartID)
	if err != nil {
		return nil, readError("fetch checkout", err)
	}

	resp := c.mapper.cartToCheckoutResponse(cart, cartID, c.cfg.StoreCurrency)
	st := c.shippingState(ctx, cartID, cart, choice)
	c.mapper.ApplyFulfillmentExtension(resp, st)
	if len(resp.LineItems) > 0 {
		c.mapper.ApplyPickup(resp, st, c.checkoutPickup(ctx))
	}
	c.mapper.ApplyDiscountExtension(resp, cart.Totals)
	if buyer != nil {
		resp.Buyer = buyer
	}
	resp.ContinueURL = c.cfg.BaseURL + "/checkout"
	return resp, nil
}

// shippingState gathers the destination, offered rates and chosen method for a
// quote. The destination is the quote's own once a rate has been chosen, else
// the one the caller just supplied; rates are estimated for it.
func (c *Connector) shippingState(ctx context.Context, cartID string, cart *mgCart, choice *fulfillmentChoice) shippingState {
	var st shippingState
	if cart.ExtensionAttrs != nil {
		for _, sa := range cart.ExtensionAttrs.ShippingAssignments {
			if sa.Shipping != nil {
				st.Address, st.SelectedMethod = sa.Shipping.Address, sa.Shipping.Method
				break
			}
		}
	}
	if (st.Address == nil || st.Address.Country == "") && choice != nil && choice.Destination != nil {
		st.Address = choice.Destination
	}
	if st.Address != nil && st.Address.Country != "" && !isPickupAddress(st.Address) {
		var rates []mgShippingMethod
		body := map[string]any{"address": st.Address}
		if err := c.client.POST(ctx, quotePath(cartID, "estimate-shipping-methods"), body, &rates); err != nil {
			// Rates are an enrichment: without them the response still carries the
			// destination and any chosen method.
			c.logger.Debug("estimate shipping methods failed", "error", err)
		} else {
			st.Rates = rates
		}
	}
	return st
}

// CheckoutComplete completes a checkout by placing the order. A guest order
// needs the buyer's email and a billing address: they come from the request's
// buyer and, for the address, the destination already on the quote (for a
// pickup order, the pickup location's).
func (c *Connector) CheckoutComplete(ctx context.Context, storeID string, checkoutID string, req *base.CompleteCheckoutRequest) (*base.CheckoutResponse, error) {
	cart, err := c.loadCart(ctx, checkoutID)
	if err != nil {
		return nil, reject("fetch checkout", err)
	}

	var buyer *model.BuyerWithConsent
	if req != nil {
		buyer = req.Buyer
	}
	email, billing := billingFor(buyer, cart)

	paymentMethod := map[string]any{"method": c.cfg.PaymentMethodCode}
	// Forward the opaque payment token to the merchant backend so its payment
	// method can run the charge — the merchant is the party that processes
	// payment. Sarvex never stores or interprets it (invariant 8).
	if token := paymentToken(req); token != "" {
		paymentMethod["additional_data"] = map[string]any{"payment_token": token}
	}
	payload := map[string]any{
		"email":          email,
		"paymentMethod":  paymentMethod,
		"billingAddress": billing,
	}

	// Placing an order is not idempotent at the store: never replayed.
	var orderID int64
	if err := c.client.POSTNoRetry(ctx, quotePath(checkoutID, "payment-information"), payload, &orderID); err != nil {
		return nil, reject("checkout complete", err)
	}

	var order mgOrder
	if err := c.client.GET(ctx, fmt.Sprintf("/orders/%d", orderID), &order); err != nil {
		return nil, fmt.Errorf("magento: fetch completed order: %w", err)
	}

	resp := c.mapper.orderToCheckoutResponse(&order, checkoutID, c.cfg.StoreCurrency, c.cfg.BaseURL)
	// payment-information succeeded, so the order is placed even when the store
	// leaves it "pending" (what offline payment methods do).
	if resp.Status == base.CheckoutStatusIncomplete {
		resp.Status = base.CheckoutStatusCompleted
		resp.Order = &model.OrderConfirmation{
			ID:           fmt.Sprintf("%d", order.EntityID),
			Label:        order.IncrementID,
			PermalinkURL: c.cfg.BaseURL + "/sales/guest/form/",
		}
	}

	st := orderShippingState(&order)
	c.mapper.ApplyFulfillmentExtension(resp, st)
	if len(resp.LineItems) > 0 {
		c.mapper.ApplyPickup(resp, st, c.checkoutPickup(ctx))
	}
	c.mapper.ApplyDiscountExtension(resp, &mgCartTotals{CouponCode: order.CouponCode, DiscountAmount: order.DiscountAmount})

	if buyer != nil && buyer.Consent != nil {
		if resp.Buyer == nil {
			resp.Buyer = &model.BuyerWithConsent{}
		}
		resp.Buyer.Consent = buyer.Consent
		c.recordConsent(ctx, &order, buyer.Consent)
	}
	resp.ContinueURL = c.cfg.BaseURL + "/checkout"
	return resp, nil
}

// orderShippingState is the destination and method a placed order carries.
func orderShippingState(o *mgOrder) shippingState {
	var st shippingState
	if o.ExtensionAttributes != nil {
		for _, sa := range o.ExtensionAttributes.ShippingAssignments {
			if sa.Shipping != nil {
				st.Address, st.SelectedMethod = sa.Shipping.Address, sa.Shipping.Method
				break
			}
		}
	}
	return st
}

// billingFor resolves the email and billing address for a guest order. The
// buyer supplies the contact; Magento's address fields come from the quote's
// shipping destination since a UCP buyer carries no postal address.
func billingFor(buyer *model.BuyerWithConsent, cart *mgCart) (string, *mgAddress) {
	contact := model.Buyer{}
	if buyer != nil {
		contact = buyer.Buyer
	} else if b := (mapper{}).cartBuyer(cart); b != nil {
		contact = *b
	}

	addr := &mgAddress{}
	if cart.ExtensionAttrs != nil {
		for _, sa := range cart.ExtensionAttrs.ShippingAssignments {
			if sa.Shipping != nil && sa.Shipping.Address != nil {
				cp := *sa.Shipping.Address
				cp.ID = 0
				cp.ExtensionAttributes = nil // a pickup location code belongs to shipping only
				addr = &cp
				break
			}
		}
	}
	if contact.FirstName != "" {
		addr.FirstName = contact.FirstName
	}
	if contact.LastName != "" {
		addr.LastName = contact.LastName
	}
	if contact.PhoneNumber != "" {
		addr.Telephone = contact.PhoneNumber
	}
	addr.Email = contact.Email
	return contact.Email, addr
}

// recordConsent stores the buyer's consent with the order as an internal
// (admin-only, not customer-visible) order comment — Magento has no consent
// field. The order is already placed, so a failure here is logged rather than
// failing the checkout. The comment holds the consent flags only, no PII.
func (c *Connector) recordConsent(ctx context.Context, order *mgOrder, consent *model.Consent) {
	encoded, err := json.Marshal(consent)
	if err != nil {
		c.logger.Warn("encode buyer consent", "error", err)
		return
	}
	body := map[string]any{"statusHistory": map[string]any{
		"comment":              "UCP buyer consent: " + string(encoded),
		"is_customer_notified": 0,
		"is_visible_on_front":  0,
		"parent_id":            order.EntityID,
		"status":               order.Status,
	}}
	if err := c.client.POSTNoRetry(ctx, fmt.Sprintf("/orders/%d/comments", order.EntityID), body, nil); err != nil {
		c.logger.Warn("record buyer consent on order failed", "order_id", order.EntityID, "error", err)
	}
}

// paymentToken pulls the opaque token from the selected payment instrument's
// token credential, if the completion request carries one. Empty when there is
// no token credential (e.g. a card handler that tokenizes elsewhere).
func paymentToken(req *base.CompleteCheckoutRequest) string {
	if req == nil || req.Payment == nil {
		return ""
	}
	instruments := req.Payment.Instruments
	selected := -1
	for i, inst := range instruments {
		if inst.Selected {
			selected = i
			break
		}
	}
	if selected < 0 && len(instruments) > 0 {
		selected = 0
	}
	if selected < 0 {
		return ""
	}
	cred := instruments[selected].Credential
	if cred == nil || cred.Token == nil {
		return ""
	}
	return cred.Token.Token
}

// CheckoutCancel cancels a checkout (Magento carts auto-expire, so this is a no-op).
func (c *Connector) CheckoutCancel(ctx context.Context, storeID string, checkoutID string) (*base.CheckoutResponse, error) {
	// Magento doesn't have explicit cart deletion, carts auto-expire.
	// Return a synthetic canceled response.
	resp := &base.CheckoutResponse{
		ID:       checkoutID,
		Status:   base.CheckoutStatusCanceled,
		Currency: c.cfg.StoreCurrency,
	}
	return resp, nil
}

// CheckoutURL returns the web checkout URL for this Magento guest cart.
// Magento does not embed the cart/quote ID in the checkout URL; the checkout
// state is managed server-side via session. The store's generic checkout page
// is the best available fallback.
func (c *Connector) CheckoutURL(_ context.Context, _ string, _ string) (string, error) {
	return c.cfg.BaseURL + "/checkout", nil
}

// OrderGet retrieves an order by its entity id (the id reported on the
// checkout's order confirmation).
func (c *Connector) OrderGet(ctx context.Context, storeID string, orderID string) (*base.OrderEvent, error) {
	var order mgOrder
	path := "/orders/" + url.PathEscape(orderID)

	if err := c.client.GET(ctx, path, &order); err != nil {
		return nil, fmt.Errorf("%w: order %s", ErrNotFound, orderID)
	}

	return &base.OrderEvent{
		OrderID: orderID,
		Status:  string(mapper{}.orderStatusToCheckoutStatus(order.Status)),
	}, nil
}
