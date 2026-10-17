// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package magento

import (
	"strings"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/commerce/model"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/connectors/base"
)

// shippingState is what a checkout response needs to know about fulfillment:
// where the order is going, the rates the store offers for that destination,
// and what the buyer has chosen.
type shippingState struct {
	// Address is the destination; nil until one is known.
	Address *mgAddress
	// Rates are the shipping methods the store offers for Address.
	Rates []mgShippingMethod
	// SelectedMethod is the chosen method's id ("carrier_method"), "" if none.
	SelectedMethod string
}

// methodID is the id a fulfillment option carries for a shipping method.
func methodID(m mgShippingMethod) string { return m.CarrierCode + "_" + m.MethodCode }

// ApplyFulfillmentExtension enriches resp with the checkout's fulfillment
// object: the shipping method the buyer is choosing among, the destination on
// it, and the shipping-rate options grouped under it.
//
// A checkout with items always offers a shipping method so an agent can see
// fulfillment is required and drive it, even before an address is known — the
// destination and rate options simply stay empty until one is. Nothing is
// offered for an empty cart.
func (m mapper) ApplyFulfillmentExtension(resp *base.CheckoutResponse, st shippingState) {
	if len(resp.LineItems) == 0 {
		return
	}
	lineItemIDs := make([]string, 0, len(resp.LineItems))
	for _, li := range resp.LineItems {
		lineItemIDs = append(lineItemIDs, li.ID)
	}

	// Options: every rate the store offers for this destination, plus the
	// chosen method if it is not already among them, so selected_option_id
	// always names an option present in the group. Pickup is offered as its own
	// method, never as a shipping rate.
	var options []model.FulfillmentOption
	seen := map[string]bool{}
	for _, r := range st.Rates {
		id := methodID(r)
		if isPickupMethod(id) {
			continue
		}
		options = append(options, shippingOption(id, r.MethodTitle, priceToMinorUnits(r.Amount)))
		seen[id] = true
	}
	var selectedOptionID *string
	if st.SelectedMethod != "" && !isPickupMethod(st.SelectedMethod) {
		id := st.SelectedMethod
		selectedOptionID = &id
		if !seen[id] {
			options = append(options, shippingOption(id, id, 0))
		}
	}

	var destinations []model.FulfillmentDestination
	var selectedDestinationID *string
	if st.Address != nil && !isPickupAddress(st.Address) {
		destID := "dest_1"
		destinations = []model.FulfillmentDestination{{
			Shipping: &model.ShippingDestination{
				ID:            destID,
				PostalAddress: mgAddressToPostalAddress(st.Address),
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
// option, selected when the buyer chose pickup.
func (m mapper) ApplyPickup(resp *base.CheckoutResponse, st shippingState, pickup *base.PickupInfo) {
	if pickup == nil || !pickup.Enabled || pickup.Location == nil || resp.Fulfillment == nil || len(resp.LineItems) == 0 {
		return
	}

	lineItemIDs := make([]string, 0, len(resp.LineItems))
	for _, li := range resp.LineItems {
		lineItemIDs = append(lineItemIDs, li.ID)
	}

	method := model.FulfillmentMethod{
		ID:           "method_pickup",
		Type:         model.FulfillmentMethodTypePickup,
		LineItemIDs:  lineItemIDs,
		Destinations: []model.FulfillmentDestination{{Retail: pickup.Location}},
		Groups: []model.FulfillmentGroup{{
			ID:          "group_pickup",
			LineItemIDs: lineItemIDs,
			Options:     []model.FulfillmentOption{shippingOption(pickupOptionID, pickupTitle, 0)},
		}},
	}
	if isPickupMethod(st.SelectedMethod) {
		destID, optID := pickup.Location.ID, pickupOptionID
		method.SelectedDestinationID = &destID
		method.Groups[0].SelectedOptionID = &optID
	}

	now := "now"
	resp.Fulfillment.Methods = append(resp.Fulfillment.Methods, method)
	resp.Fulfillment.AvailableMethods = append(resp.Fulfillment.AvailableMethods, model.FulfillmentAvailableMethod{
		Type:          model.FulfillmentMethodTypePickup,
		LineItemIDs:   lineItemIDs,
		FulfillableOn: &now,
	})
}

// shippingOption builds a fulfillment option from a rate's id, title and price.
func shippingOption(id, title string, amountMinor int64) model.FulfillmentOption {
	return model.NewFulfillmentOption(id, title, amountMinor)
}

// isPickupMethod reports whether a shipping method id ("carrier_method") is
// Magento's in-store-pickup carrier.
func isPickupMethod(id string) bool { return strings.HasPrefix(id, pickupCarrierCode+"_") }

// isPickupAddress reports whether an address is a pickup location's (a pickup
// order's shipping address is the location's, not the buyer's).
func isPickupAddress(a *mgAddress) bool {
	return a != nil && a.ExtensionAttributes != nil && a.ExtensionAttributes.PickupLocationCode != ""
}

// appliedDiscounts lists the discount the quote/order carries: one entry
// carrying the coupon code the agent submitted (Magento applies one coupon per
// cart) and the reduction.
func appliedDiscounts(couponCode string, discountAmount float64) []model.AppliedDiscount {
	d := discountMinorUnits(discountAmount)
	if d <= 0 {
		return nil
	}
	title := "Discount"
	if couponCode != "" {
		title = couponCode
	}
	return []model.AppliedDiscount{{
		Code:   couponCode,
		Title:  title,
		Amount: model.Amount(d),
	}}
}

// ApplyDiscountExtension reports the applied discounts on a checkout response;
// the reduction itself is already in the totals (mapper.cartTotals).
func (m mapper) ApplyDiscountExtension(resp *base.CheckoutResponse, t *mgCartTotals) {
	if t == nil {
		return
	}
	if applied := appliedDiscounts(t.CouponCode, t.DiscountAmount); len(applied) > 0 {
		resp.Discounts = &model.DiscountsObject{Applied: applied}
	}
}

// ApplyCartDiscountExtension reports the applied discounts on a cart response.
func (m mapper) ApplyCartDiscountExtension(resp *base.CartResponse, t *mgCartTotals) {
	if t == nil {
		return
	}
	if applied := appliedDiscounts(t.CouponCode, t.DiscountAmount); len(applied) > 0 {
		resp.Discounts = &model.DiscountsObject{Applied: applied}
	}
}
