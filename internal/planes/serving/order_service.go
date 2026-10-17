// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
)

// ErrOrderNotFound is returned when no order exists for the given order ID.
var ErrOrderNotFound = errors.New("serving: order not found")

// OrderService projects a completed CheckoutSession as the spec's Order
// resource (GET/PUT /orders/{id}) and applies the only two things an Order
// PUT can add: a fulfillment event and/or an adjustment
// (api/order_contract.go's OrderUpdateRequest) — an order's line items,
// fulfillment expectations, and totals are fixed at Complete, the same
// live-priced data the checkout itself returned, not re-derived here. A
// completed checkout session doubles as Sarvex's record of the order this
// resource represents, addressed by SessionStore.GetByOrderID, rather than
// a second, separately-owned copy of the same data (see session.go's
// OrderID/Adjustments/FulfillmentEvents fields).
type OrderService struct {
	sessions SessionStore
	// webhooks delivers the order-event webhook on every Update (order.md:
	// businesses MUST send the full entity on updates too). A nil
	// *WebhookDeliverer no-ops, so omitting it — every pre-existing caller of
	// NewOrderService — is unchanged behavior.
	webhooks *WebhookDeliverer
}

// NewOrderService constructs an OrderService. webhooks is optional
// (trailing, variadic) — pass one to enable order-event webhook delivery on
// Update; omit it for behavior identical to before webhooks existed.
func NewOrderService(sessions SessionStore, webhooks ...*WebhookDeliverer) *OrderService {
	var wh *WebhookDeliverer
	if len(webhooks) > 0 {
		wh = webhooks[0]
	}
	return &OrderService{sessions: sessions, webhooks: wh}
}

// Get retrieves the order placed by a checkout session's Complete.
func (s *OrderService) Get(ctx context.Context, storeID, orderID string) (*model.Order, error) {
	session, err := s.sessions.GetByOrderID(ctx, storeID, orderID)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, ErrOrderNotFound
		}
		return nil, err
	}
	return buildOrder(session), nil
}

// OrderUpdateInput is the caller-supplied portion of an order PUT — new
// fulfillment events and/or adjustments to append.
type OrderUpdateInput struct {
	FulfillmentEvents []model.FulfillmentEvent
	Adjustments       []model.Adjustment
}

// Update appends new fulfillment events and/or adjustments to the order.
// Both are append-only logs per the spec, so a request naming an ID the
// order already has is a no-op for that entry (safe to resend the whole
// known set) rather than a duplicate or a conflict.
func (s *OrderService) Update(ctx context.Context, storeID, orderID string, in OrderUpdateInput) (*model.Order, error) {
	session, err := s.sessions.GetByOrderID(ctx, storeID, orderID)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, ErrOrderNotFound
		}
		return nil, err
	}

	updated := *session
	updated.FulfillmentEvents = appendNewFulfillmentEvents(session.FulfillmentEvents, in.FulfillmentEvents)
	updated.Adjustments = appendNewAdjustments(session.Adjustments, in.Adjustments)
	updated.UpdatedAt = time.Now().UTC()

	if err := s.sessions.Update(ctx, &updated); err != nil {
		return nil, fmt.Errorf("persist order update: %w", err)
	}

	order := buildOrder(&updated)
	// order.md: businesses MUST send the full order entity on updates too,
	// never an incremental delta — the same buildOrder snapshot this call
	// returns is what gets delivered.
	s.webhooks.DeliverOrderEvent(ctx, updated.WebhookURL, order, updated.SpecVersion)
	return order, nil
}

// appendNewFulfillmentEvents returns existing plus any incoming event whose
// ID isn't already present.
func appendNewFulfillmentEvents(existing, incoming []model.FulfillmentEvent) []model.FulfillmentEvent {
	seen := make(map[string]bool, len(existing))
	for _, e := range existing {
		seen[e.ID] = true
	}
	out := append([]model.FulfillmentEvent{}, existing...)
	for _, e := range incoming {
		if !seen[e.ID] {
			out = append(out, e)
			seen[e.ID] = true
		}
	}
	return out
}

// appendNewAdjustments returns existing plus any incoming adjustment whose
// ID isn't already present.
func appendNewAdjustments(existing, incoming []model.Adjustment) []model.Adjustment {
	seen := make(map[string]bool, len(existing))
	for _, a := range existing {
		seen[a.ID] = true
	}
	out := append([]model.Adjustment{}, existing...)
	for _, a := range incoming {
		if !seen[a.ID] {
			out = append(out, a)
			seen[a.ID] = true
		}
	}
	return out
}

// buildOrder projects a completed CheckoutSession into the spec's Order
// resource (model.Order).
func buildOrder(session *CheckoutSession) *model.Order {
	order := &model.Order{
		ID:         session.OrderID,
		CheckoutID: session.SessionID,
		Fulfillment: &model.OrderFulfillment{
			Events: session.FulfillmentEvents,
		},
		Adjustments: session.Adjustments,
	}

	if session.LastResponse != nil {
		order.Currency = session.LastResponse.Currency
		order.Totals = session.LastResponse.Totals
		order.LineItems = orderLineItemsFromCheckout(session.LastResponse.LineItems)
		order.Fulfillment.Expectations = expectationsFromFulfillment(session.LastResponse.Fulfillment)
		if session.LastResponse.Order != nil {
			order.Label = session.LastResponse.Order.Label
			order.PermalinkURL = session.LastResponse.Order.PermalinkURL
		}
	}

	return order
}

// orderLineItemsFromCheckout carries a completed checkout's priced line
// items over to the order as-is (quantity fully active, nothing fulfilled
// yet) — an order's line items start as exactly what was purchased.
func orderLineItemsFromCheckout(items []model.LineItem) []model.OrderLineItem {
	out := make([]model.OrderLineItem, len(items))
	for i, item := range items {
		out[i] = model.OrderLineItem{
			ID:   item.ID,
			Item: item.Item,
			Quantity: model.OrderLineItemQuantity{
				Original: originalQuantityPtr(item.Quantity),
				Total:    item.Quantity,
			},
			Totals:   item.Totals,
			Status:   model.OrderLineItemStatusProcessing,
			ParentID: item.ParentID,
		}
	}
	return out
}

// expectationsFromFulfillment derives buyer-facing fulfillment
// expectations from the checkout's selected methods/destinations — one
// expectation per method that has a selected destination. A method with no
// selection yet (should not happen for a completed checkout, but is not
// this code's place to enforce that) is skipped rather than guessed at.
func expectationsFromFulfillment(f *model.Fulfillment) []model.Expectation {
	if f == nil {
		return nil
	}

	var expectations []model.Expectation
	for _, method := range f.Methods {
		if method.SelectedDestinationID == nil {
			continue
		}
		dest := findDestination(method.Destinations, *method.SelectedDestinationID)
		if dest == nil || dest.Shipping == nil {
			// Retail pickup has no postal address to report as a shipping
			// expectation's destination — the spec models pickup
			// expectations separately, out of V1's checkout-first scope.
			continue
		}

		lineItems := make([]model.ExpectationLineItem, len(method.LineItemIDs))
		for i, id := range method.LineItemIDs {
			lineItems[i] = model.ExpectationLineItem{ID: id, Quantity: 1}
		}

		expectations = append(expectations, model.Expectation{
			ID:            method.ID,
			LineItems:     lineItems,
			MethodType:    model.ExpectationMethodType(method.Type),
			Description:   selectedOptionTitle(method),
			Destination:   dest.Shipping.PostalAddress,
			FulfillableOn: "now",
		})
	}
	return expectations
}

// selectedOptionTitle returns the title of the fulfillment option the buyer
// selected on the method (the shipping choice, e.g. "Standard Shipping"), which
// the order records as the buyer-facing expectation description. Empty when no
// option is selected.
func selectedOptionTitle(method model.FulfillmentMethod) string {
	for _, g := range method.Groups {
		if g.SelectedOptionID == nil {
			continue
		}
		for _, o := range g.Options {
			if o.ID == *g.SelectedOptionID {
				return o.Title
			}
		}
	}
	return ""
}

// findDestination returns the destination with the given ID, or nil.
func findDestination(destinations []model.FulfillmentDestination, id string) *model.FulfillmentDestination {
	for i := range destinations {
		if destinations[i].Shipping != nil && destinations[i].Shipping.ID == id {
			return &destinations[i]
		}
		if destinations[i].Retail != nil && destinations[i].Retail.ID == id {
			return &destinations[i]
		}
	}
	return nil
}

// originalQuantityPtr returns a pointer to n — OrderLineItemQuantity.Original
// distinguishes "unset" from an explicit value, and a line item fresh off a
// completed checkout always has one.
func originalQuantityPtr(n int) *int { return &n }
