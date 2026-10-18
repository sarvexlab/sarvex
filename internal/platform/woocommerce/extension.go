// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package woocommerce

import (
	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// ApplyFulfillmentExtension enriches resp with the checkout fulfillment
// object: the shipping method the buyer is choosing among, the destination(s)
// on it, and the shipping-rate options grouped under it.
//
// A checkout must *offer* fulfillment, not merely echo a completed selection:
// once a shipping destination is known the store can quote rates, and the
// agent picks one by setting selected_option_id on a group. So this builds a
// method whenever there is a destination, a selection, or an advertised rate —
// returning early only for a brand-new checkout that has none of those yet.
func (m mapper) ApplyFulfillmentExtension(resp *base.CheckoutResponse, order *wcOrder) {
	// A checkout with items always offers a shipping method so an agent can
	// see fulfillment is required and drive it, even before an address is
	// known — the destinations (and rate options) simply stay empty until one
	// is. Nothing to offer for an empty cart.
	if len(resp.LineItems) == 0 {
		return
	}
	hasAddress := orderHasShippingAddress(order.ShippingAddress)

	lineItemIDs := make([]string, 0, len(resp.LineItems))
	for _, li := range resp.LineItems {
		lineItemIDs = append(lineItemIDs, li.ID)
	}

	// Options: every rate the store advertises for this destination, plus the
	// currently-selected method if it isn't already among them — so
	// selected_option_id always names an option actually present in the group.
	var options []model.FulfillmentOption
	seen := map[string]bool{}
	for _, r := range order.AvailableShippingRates {
		options = append(options, shippingOption(r.MethodID, r.MethodTitle, r.Total))
		seen[r.MethodID] = true
	}
	var selectedOptionID *string
	if len(order.ShippingLines) > 0 {
		sel := order.ShippingLines[0]
		id := sel.MethodID
		selectedOptionID = &id
		if !seen[sel.MethodID] {
			options = append(options, shippingOption(sel.MethodID, sel.MethodTitle, sel.Total))
		}
	}

	var destinations []model.FulfillmentDestination
	var selectedDestinationID *string
	if hasAddress {
		destID := "dest_1"
		destinations = []model.FulfillmentDestination{{
			Shipping: &model.ShippingDestination{
				ID:            destID,
				PostalAddress: wcAddressToPostalAddress(order.ShippingAddress),
			},
		}}
		selectedDestinationID = &destID
	}

	now := "now"
	resp.Fulfillment = &model.Fulfillment{
		Methods: []model.FulfillmentMethod{{
			ID:                    "method_1",
			Type:                  model.FulfillmentMethodTypeShipping,
			LineItemIDs:           lineItemIDs,
			Destinations:          destinations,
			SelectedDestinationID: selectedDestinationID,
			Groups: []model.FulfillmentGroup{{
				ID:               "group_1",
				LineItemIDs:      lineItemIDs,
				Options:          options,
				SelectedOptionID: selectedOptionID,
			}},
		}},
		AvailableMethods: []model.FulfillmentAvailableMethod{{
			Type:          model.FulfillmentMethodTypeShipping,
			LineItemIDs:   lineItemIDs,
			FulfillableOn: &now,
		}},
	}
}

// ApplyPickup adds the pickup method to resp's fulfillment object when the
// store offers pickup: one retail-location destination and one zero-cost
// option, selected when the order was placed for pickup. A pickup order's
// shipping address is the pickup location's, so the shipping method must not
// present it as a chosen shipping destination or rate.
func (m mapper) ApplyPickup(resp *base.CheckoutResponse, order *wcOrder, pickup *base.PickupInfo) {
	if pickup == nil || !pickup.Enabled || pickup.Location == nil || resp.Fulfillment == nil || len(resp.LineItems) == 0 {
		return
	}

	lineItemIDs := make([]string, 0, len(resp.LineItems))
	for _, li := range resp.LineItems {
		lineItemIDs = append(lineItemIDs, li.ID)
	}
	isPickupOrder := len(order.ShippingLines) > 0 && isPickupMethod(order.ShippingLines[0].MethodID)

	pickupMethod := model.FulfillmentMethod{
		ID:           "method_pickup",
		Type:         model.FulfillmentMethodTypePickup,
		LineItemIDs:  lineItemIDs,
		Destinations: []model.FulfillmentDestination{{Retail: pickup.Location}},
		Groups: []model.FulfillmentGroup{{
			ID:          "group_pickup",
			LineItemIDs: lineItemIDs,
			Options:     []model.FulfillmentOption{shippingOption(pickupOptionID, pickupTitle, "0")},
		}},
	}

	if isPickupOrder {
		destID, optID := pickup.Location.ID, pickupOptionID
		pickupMethod.SelectedDestinationID = &destID
		pickupMethod.Groups[0].SelectedOptionID = &optID

		// Undo what the shipping method inferred from the order's shipping
		// line and address (both of which are the pickup's).
		for i := range resp.Fulfillment.Methods {
			sm := &resp.Fulfillment.Methods[i]
			if sm.Type != model.FulfillmentMethodTypeShipping {
				continue
			}
			sm.Destinations, sm.SelectedDestinationID = nil, nil
			for g := range sm.Groups {
				kept := sm.Groups[g].Options[:0]
				for _, o := range sm.Groups[g].Options {
					if !isPickupMethod(o.ID) {
						kept = append(kept, o)
					}
				}
				sm.Groups[g].Options, sm.Groups[g].SelectedOptionID = kept, nil
			}
		}
	}

	now := "now"
	resp.Fulfillment.Methods = append(resp.Fulfillment.Methods, pickupMethod)
	resp.Fulfillment.AvailableMethods = append(resp.Fulfillment.AvailableMethods, model.FulfillmentAvailableMethod{
		Type:          model.FulfillmentMethodTypePickup,
		LineItemIDs:   lineItemIDs,
		FulfillableOn: &now,
	})
}

// shippingOption builds a fulfillment option from a shipping rate's id, title,
// and amount-as-string (minor units resolved via priceToCents).
func shippingOption(id, title, total string) model.FulfillmentOption {
	return model.NewFulfillmentOption(id, title, priceToCents(total))
}

// orderHasShippingAddress reports whether the order carries enough of a
// shipping address to quote against (country, street, or postcode).
func orderHasShippingAddress(a wcAddress) bool {
	return a.Country != "" || a.Address1 != "" || a.Postcode != ""
}

// wcAddressToPostalAddress maps a WooCommerce address back to the canonical
// PostalAddress — the inverse of ucpAddressToWCAddress — for echoing a
// selected destination in the checkout fulfillment object.
func wcAddressToPostalAddress(a wcAddress) model.PostalAddress {
	return model.PostalAddress{
		FirstName:       a.FirstName,
		LastName:        a.LastName,
		StreetAddress:   a.Address1,
		ExtendedAddress: a.Address2,
		AddressLocality: a.City,
		AddressRegion:   a.State,
		PostalCode:      a.Postcode,
		AddressCountry:  a.Country,
		PhoneNumber:     a.Phone,
	}
}

// ApplyDiscountExtension enriches resp with discount data from the WooCommerce
// order: one applied discount per coupon line (carrying the code the agent
// submitted, canonicalized by the store) plus the pooled amount.
func (m mapper) ApplyDiscountExtension(resp *base.CheckoutResponse, order *wcOrder) {
	if priceToCents(order.DiscountTotal) <= 0 {
		return
	}

	resp.Discounts = &model.DiscountsObject{Applied: appliedDiscounts(order)}

	// Reflect the reduction in the checkout totals: a negative `discount`
	// entry (total.json types it exclusiveMax 0). The order's own total
	// already nets the discount out, so no other entry changes.
	resp.Totals = append(resp.Totals, model.Total{
		Type:   model.TotalTypeDiscount,
		Amount: model.SignedAmount(-priceToCents(order.DiscountTotal)),
	})
}

// appliedDiscounts lists one applied discount per coupon line (carrying the
// code the agent submitted, canonicalized by the store).
func appliedDiscounts(order *wcOrder) []model.AppliedDiscount {
	applied := make([]model.AppliedDiscount, 0, len(order.CouponLines))
	for _, cl := range order.CouponLines {
		applied = append(applied, model.AppliedDiscount{
			Code:   cl.Code,
			Title:  cl.Code,
			Amount: model.Amount(priceToCents(cl.Discount)),
		})
	}
	// A discount total with no itemized coupon lines (shouldn't happen for the
	// code path, but keep the object well-formed) still carries the pooled sum.
	if len(applied) == 0 {
		applied = append(applied, model.AppliedDiscount{
			Title:  "Discount",
			Amount: model.Amount(priceToCents(order.DiscountTotal)),
		})
	}
	return applied
}

// ApplyCartDiscountExtension enriches a cart response with the same applied
// discounts checkout reports: the discount extension extends the cart
// capability as well as checkout, and the order the cart is backed by already
// carries the coupon lines and their reduction. The cart's totals already
// include a discount entry (orderToCartResponse), so only the applied list is
// added here.
func (m mapper) ApplyCartDiscountExtension(resp *base.CartResponse, order *wcOrder) {
	if priceToCents(order.DiscountTotal) <= 0 {
		return
	}
	resp.Discounts = &model.DiscountsObject{Applied: appliedDiscounts(order)}
}
