// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package shopify

// extension.go implements the two UCP checkout extensions for Shopify:
//
//   dev.ucp.shopping.fulfillment  https://base.dev/2026-04-08/specification/fulfillment/
//   dev.ucp.shopping.discount     https://base.dev/2026-04-08/specification/discount/
//
// Both extensions are purely additive — they enrich the CheckoutResponse and
// CartResponse that connector.go already builds, without touching core fields.
// The connector calls the Apply* functions at the end of every checkout/cart
// response construction path.
//
// ── Fulfillment mapping ───────────────────────────────────────────────────────
//
// Shopify's flat shipping rate model maps to the UCP extension shape like this:
//
//   Shopify concept                       UCP fulfillment extension
//   ─────────────────────────────────     ─────────────────────────────────────────
//   checkout.requiresShipping             whether to include fulfillment at all
//   checkout.shippingAddress              methods[0].destinations[0] (shipping dest)
//   checkout.availableShippingRates[]     methods[0].groups[0].options[]
//   checkout.shippingLine.handle          methods[0].groups[0].selected_option_id
//   checkout.lineItems[].id              methods[0].line_item_ids[]
//
// Shopify does not expose pickup locations via the Storefront API, so the
// extension only ever emits a single "shipping" method.
//
// ── Discount mapping ─────────────────────────────────────────────────────────
//
// Shopify checkout discounts:
//
//   Shopify concept                       UCP discount extension
//   ─────────────────────────────────     ─────────────────────────────────────────
//   checkout.discountApplications[]       discounts.applied[]
//   discountApplication.code             applied[].code   (empty for automatic)
//   discountApplication.title            applied[].title
//   discountApplication.value            applied[].amount (resolved to cents)
//   discountApplication.allocationMethod applied[].method ("each" | "across")
//   lineItem.discountAllocations[]       applied[].allocations[]
//   cart.discountCodes[]                 discounts.codes[]  (input echo on cart)

import (
	"fmt"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// ─── Fulfillment extension ────────────────────────────────────────────────────

// ApplyFulfillmentExtension enriches resp with the spec-accurate fulfillment
// extension block built from co. It replaces the simplified FulfillmentResponse
// that mapper.go populates with the full methods/destinations/groups/options shape.
// No-ops when the checkout has no physical items or no rates ready.
func (m mapper) ApplyFulfillmentExtension(resp *base.CheckoutResponse, co *shopifyCheckout) {
	ext := m.buildFulfillmentExtension(co)
	if ext == nil {
		return
	}
	resp.Fulfillment = ext
}

func (m mapper) buildFulfillmentExtension(co *shopifyCheckout) *model.Fulfillment {
	if !co.RequiresShipping {
		return nil
	}

	// Collect the IDs of all physical line items in this checkout.
	var lineItemIDs []string
	for _, edge := range co.LineItems.Edges {
		lineItemIDs = append(lineItemIDs, edge.Node.ID)
	}
	if len(lineItemIDs) == 0 {
		return nil
	}

	method := model.FulfillmentMethod{
		ID:          "shipping",
		Type:        model.FulfillmentMethodTypeShipping,
		LineItemIDs: lineItemIDs,
	}

	// Destination — the buyer's current shipping address, if set.
	if co.ShippingAddress != nil {
		dest := m.mailingAddressToDestination(co.ShippingAddress)
		method.Destinations = []model.FulfillmentDestination{dest}
		if dest.Shipping != nil {
			method.SelectedDestinationID = &dest.Shipping.ID
		}
	}

	// Group — one package containing all physical items, with an option per
	// available shipping rate. This matches Shopify's single-package model.
	if co.AvailableShippingRates != nil && co.AvailableShippingRates.Ready &&
		len(co.AvailableShippingRates.ShippingRates) > 0 {

		group := model.FulfillmentGroup{
			ID:          "group_1",
			LineItemIDs: lineItemIDs,
		}

		for _, sr := range co.AvailableShippingRates.ShippingRates {
			group.Options = append(group.Options, model.FulfillmentOption{
				ID:    sr.Handle,
				Title: sr.Title,
				Totals: model.Totals{
					{Type: model.TotalTypeTotal, Amount: model.SignedAmount(moneyToCents(sr.PriceV2.Amount))},
				},
			})
		}

		if co.ShippingLine != nil {
			handle := co.ShippingLine.Handle
			group.SelectedOptionID = &handle
		}

		method.Groups = []model.FulfillmentGroup{group}
	}

	// Available methods hint — all physical items can be shipped immediately.
	now := "now"
	availableMethods := []model.FulfillmentAvailableMethod{{
		Type:          model.FulfillmentMethodTypeShipping,
		LineItemIDs:   lineItemIDs,
		FulfillableOn: &now,
	}}

	return &model.Fulfillment{
		Methods:          []model.FulfillmentMethod{method},
		AvailableMethods: availableMethods,
	}
}

// mailingAddressToDestination converts a Shopify mailing address to a
// UCP FulfillmentDestination with a stable deterministic ID derived from
// postal code + country so repeated fetches produce the same ID.
func (m mapper) mailingAddressToDestination(addr *shopifyMailingAddress) model.FulfillmentDestination {
	// Prefer the ISO alpha-2 country code; fall back to the full country name.
	country := addr.CountryCode
	if country == "" {
		country = addr.Country
	}
	id := fmt.Sprintf("dest_%s_%s", addr.Zip, country)

	return model.FulfillmentDestination{
		Shipping: &model.ShippingDestination{
			PostalAddress: model.PostalAddress{
				FirstName:       addr.FirstName,
				LastName:        addr.LastName,
				PhoneNumber:     addr.Phone,
				StreetAddress:   addr.Address1,
				ExtendedAddress: addr.Address2,
				AddressLocality: addr.City,
				AddressRegion:   addr.Province,
				PostalCode:      addr.Zip,
				AddressCountry:  country,
			},
			ID: id,
		},
	}
}

// ─── Discount extension ───────────────────────────────────────────────────────

// ApplyDiscountExtension enriches resp with the spec-accurate discount
// extension block built from co's discountApplications and line item allocations.
// No-ops when no discount applications are present.
func (m mapper) ApplyDiscountExtension(resp *base.CheckoutResponse, co *shopifyCheckout) {
	ext := m.buildCheckoutDiscountExtension(co)
	if ext == nil {
		return
	}
	resp.Discounts = ext
}

// ApplyCartDiscountExtension enriches resp with discount codes echoed from the
// Shopify cart's discountCodes field.
func (m mapper) ApplyCartDiscountExtension(resp *base.CartResponse, cart *shopifyCart) {
	ext := m.buildCartDiscountExtension(cart)
	if ext == nil {
		return
	}
	resp.Discounts = ext
}

func (m mapper) buildCheckoutDiscountExtension(co *shopifyCheckout) *model.DiscountsObject {
	if len(co.DiscountApplications.Edges) == 0 {
		return nil
	}

	d := &model.DiscountsObject{}

	// Build a map of line item IDs indexed by position for JSONPath construction.
	lineItemPos := make(map[string]int, len(co.LineItems.Edges))
	for i, edge := range co.LineItems.Edges {
		lineItemPos[edge.Node.ID] = i
	}

	for i, edge := range co.DiscountApplications.Edges {
		app := edge.Node

		applied := model.AppliedDiscount{
			Title:  app.Title,
			Amount: model.Amount(resolveDiscountAmount(app, co)),
		}

		// Code-based vs automatic.
		if app.Code != "" {
			applied.Code = app.Code
		} else {
			applied.Automatic = true
		}

		// Allocation method mapping.
		switch app.AllocationMethod {
		case "EACH":
			applied.Method = model.AllocationMethodEach
		case "ACROSS":
			applied.Method = model.AllocationMethodAcross
		}

		// Build per-line-item allocations by cross-referencing ApplicationIndex.
		for _, liEdge := range co.LineItems.Edges {
			for _, alloc := range liEdge.Node.DiscountAllocations {
				if alloc.ApplicationIndex == i {
					pos, ok := lineItemPos[liEdge.Node.ID]
					if !ok {
						continue
					}
					applied.Allocations = append(applied.Allocations, model.Allocation{
						Path:   fmt.Sprintf("$.line_items[%d]", pos),
						Amount: model.Amount(moneyToCents(alloc.AllocatedAmount.Amount)),
					})
				}
			}
		}

		d.Applied = append(d.Applied, applied)
	}

	return d
}

func (m mapper) buildCartDiscountExtension(cart *shopifyCart) *model.DiscountsObject {
	if len(cart.DiscountCodes) == 0 {
		return nil
	}

	d := &model.DiscountsObject{}
	for _, dc := range cart.DiscountCodes {
		// Echo all codes back, both applicable and not.
		// Non-applicable codes produce a rejected-code message via mapper.go;
		// here we just surface them in the discount block for transparency.
		d.Codes = append(d.Codes, dc.Code)
	}
	return d
}

// resolveDiscountAmount extracts the total discount amount in cents from a
// Shopify DiscountApplication. Shopify returns either a MoneyV2 amount or a
// PricingPercentageValue. For percentage discounts we sum the per-line
// allocations instead, since the total depends on the cart contents.
func resolveDiscountAmount(app shopifyDiscountApplication, co *shopifyCheckout) int64 {
	// If the value field is a plain decimal string, treat it as a money amount.
	if cents := moneyToCents(app.Value); cents > 0 {
		return cents
	}
	// For percentage discounts (value is e.g. "10" for 10%), sum allocations
	// that reference this application index.
	var total int64
	for i, edge := range co.DiscountApplications.Edges {
		if edge.Node.Code != app.Code && edge.Node.Title != app.Title {
			continue
		}
		for _, liEdge := range co.LineItems.Edges {
			for _, alloc := range liEdge.Node.DiscountAllocations {
				if alloc.ApplicationIndex == i {
					total += moneyToCents(alloc.AllocatedAmount.Amount)
				}
			}
		}
		break
	}
	return total
}

// ─── Discount mutation helpers ────────────────────────────────────────────────

// discountCodeApplyArgs returns the GQL mutation and variables for applying
// a single discount code to a Shopify checkout.
func discountCodeApplyArgs(checkoutID, code string) (string, map[string]any) {
	return gqlCheckoutDiscountCodeApply, map[string]any{
		"checkoutId":   checkoutID,
		"discountCode": code,
	}
}

// discountCodeRemoveArgs returns the GQL mutation and variables for removing
// all discount codes from a Shopify checkout.
func discountCodeRemoveArgs(checkoutID string) (string, map[string]any) {
	return gqlCheckoutDiscountCodeRemove, map[string]any{
		"checkoutId": checkoutID,
	}
}

// ─── Buyer Consent — Shopify storage ─────────────────────────────────────────

// ConsentToCustomAttributes converts a BuyerConsent struct into Shopify
// checkout custom attributes so the consent choices are stored on the
// Shopify order for the merchant's compliance records.
//
// Custom attribute keys follow the convention "_ucp_consent_{field}" with
// a leading underscore to hide them from storefront display.
func ConsentToCustomAttributes(consent *model.Consent) []map[string]string {
	if consent == nil {
		return nil
	}
	attrs := []map[string]string{}
	if consent.Analytics != nil {
		attrs = append(attrs, map[string]string{
			"key":   "_ucp_consent_analytics",
			"value": boolToString(*consent.Analytics),
		})
	}
	if consent.Preferences != nil {
		attrs = append(attrs, map[string]string{
			"key":   "_ucp_consent_preferences",
			"value": boolToString(*consent.Preferences),
		})
	}
	if consent.Marketing != nil {
		attrs = append(attrs, map[string]string{
			"key":   "_ucp_consent_marketing",
			"value": boolToString(*consent.Marketing),
		})
	}
	if consent.SaleOfData != nil {
		attrs = append(attrs, map[string]string{
			"key":   "_ucp_consent_sale_of_data",
			"value": boolToString(*consent.SaleOfData),
		})
	}
	return attrs
}

func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
