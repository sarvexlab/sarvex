// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package woocommerce

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/commerce/model"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/connectors/base"
)

// mapper is a stateless collection of translation functions from WooCommerce
// native types to UCP canonical types. All functions are pure — no IO.
type mapper struct{}

// ─── Catalog ──────────────────────────────────────────────────────────────────

func (m mapper) productToUCPItem(p wcProduct, currency string) model.Product {
	price := priceToCents(p.Price)
	if price == 0 && p.RegularPrice != "" {
		price = priceToCents(p.RegularPrice)
	}

	product := model.Product{
		ID:    fmt.Sprintf("%d", p.ID),
		Title: p.Name,
		Description: model.Description{
			Plain: p.ShortDescription,
		},
		URL: p.Permalink,
		PriceRange: model.PriceRange{
			Min: model.Price{Amount: model.Amount(price), Currency: currency},
			Max: model.Price{Amount: model.Amount(price), Currency: currency},
		},
	}

	if len(p.Images) > 0 {
		product.Media = []model.Media{{
			Type: model.MediaTypeImage,
			URL:  p.Images[0].Src,
		}}
	}

	return product
}

func (m mapper) variationToUCPVariant(v wcVariation, currency string) model.Variant {
	price := priceToCents(v.Price)

	variant := model.Variant{
		ID:    fmt.Sprintf("%d", v.ID),
		Title: m.buildVariantTitle(v.Attributes),
		Price: model.Price{Amount: model.Amount(price), Currency: currency},
		Availability: &model.VariantAvailability{
			Available: v.InStock && v.Quantity > 0,
		},
	}

	for _, attr := range v.Attributes {
		variant.Options = append(variant.Options, model.SelectedOption{
			Name:  attr.Name,
			Label: attr.Option,
		})
	}

	return variant
}

func (m mapper) buildVariantTitle(attrs []wcAttribute) string {
	if len(attrs) == 0 {
		return "Default"
	}
	parts := make([]string, 0, len(attrs))
	for _, a := range attrs {
		parts = append(parts, a.Option)
	}
	return strings.Join(parts, " / ")
}

// ─── Cart / Order ─────────────────────────────────────────────────────────────

func (m mapper) orderToCartResponse(order *wcOrder, currency string) *base.CartResponse {
	resp := &base.CartResponse{
		ID:       fmt.Sprintf("%d", order.ID),
		Currency: currency,
	}

	subtotal := int64(0)
	for _, li := range order.LineItems {
		total := priceToCents(li.Total)
		quantity := int64(li.Quantity)
		unitPrice := total / maxInt64(quantity, 1)
		up := model.Amount(unitPrice)

		resp.LineItems = append(resp.LineItems, model.LineItem{
			ID: fmt.Sprintf("wc-line-%d", li.ID),
			Item: model.Item{
				ID:    fmt.Sprintf("%d", li.ProductID),
				Title: li.Name,
				Price: &up,
			},
			Quantity: li.Quantity,
			Totals:   lineItemTotals(total),
		})
		subtotal += total
	}

	// The discount entry appears only when a discount applies: the spec types it
	// as strictly negative, so a zero entry would be invalid.
	resp.Totals = model.Totals{
		model.Total{Type: model.TotalTypeSubtotal, Amount: model.SignedAmount(subtotal)},
		model.Total{Type: model.TotalTypeTax, Amount: model.SignedAmount(priceToCents(order.TaxTotal))},
		model.Total{Type: model.TotalTypeFulfillment, Amount: model.SignedAmount(priceToCents(order.ShippingTotal))},
	}
	if d := priceToCents(order.DiscountTotal); d > 0 {
		resp.Totals = append(resp.Totals, model.Total{Type: model.TotalTypeDiscount, Amount: model.SignedAmount(-d)})
	}
	resp.Totals = append(resp.Totals, model.Total{Type: model.TotalTypeTotal, Amount: model.SignedAmount(priceToCents(order.Total))})

	if order.BillingAddress.Email != "" {
		resp.Buyer = &model.Buyer{
			Email:       order.BillingAddress.Email,
			FirstName:   order.BillingAddress.FirstName,
			LastName:    order.BillingAddress.LastName,
			PhoneNumber: order.BillingAddress.Phone,
		}
	}

	return resp
}

func (m mapper) orderToCheckoutResponse(order *wcOrder, currency, baseURL string) *base.CheckoutResponse {
	resp := &base.CheckoutResponse{
		ID:       fmt.Sprintf("%d", order.ID),
		Status:   m.orderStatusToCheckoutStatus(order.Status),
		Currency: currency,
	}

	subtotal := int64(0)
	for _, li := range order.LineItems {
		total := priceToCents(li.Total)
		quantity := int64(li.Quantity)
		unitPrice := total / maxInt64(quantity, 1)
		up := model.Amount(unitPrice)

		resp.LineItems = append(resp.LineItems, model.LineItem{
			ID: fmt.Sprintf("wc-line-%d", li.ID),
			Item: model.Item{
				ID:    fmt.Sprintf("%d", li.ProductID),
				Title: li.Name,
				Price: &up,
			},
			Quantity: li.Quantity,
			Totals:   lineItemTotals(total),
		})
		subtotal += total
	}

	resp.Totals = model.Totals{
		model.Total{Type: model.TotalTypeSubtotal, Amount: model.SignedAmount(subtotal)},
		model.Total{Type: model.TotalTypeTax, Amount: model.SignedAmount(priceToCents(order.TaxTotal))},
		model.Total{Type: model.TotalTypeFulfillment, Amount: model.SignedAmount(priceToCents(order.ShippingTotal))},
		model.Total{Type: model.TotalTypeTotal, Amount: model.SignedAmount(priceToCents(order.Total))},
	}

	// The placed order, once there is one. Without this the response carried
	// no order identifiers at all: the serving plane reads
	// checkoutResp.Order.ID into serving.checkout.order_intent, so the sync
	// plane rejected every completed checkout with "order intent: missing
	// order identifiers" and recorded no order — leaving the console's agent
	// order counts at zero after a sale that really happened at the merchant.
	//
	// Gated on the checkout being complete rather than on the WooCommerce
	// order merely existing: WooCommerce creates the order at checkout-create
	// time in "pending", and an order confirmation for a checkout the buyer
	// has not completed would be a lie to the agent.
	if resp.Status == base.CheckoutStatusCompleted {
		resp.Order = &model.OrderConfirmation{
			ID:           fmt.Sprintf("%d", order.ID),
			PermalinkURL: orderPermalink(baseURL, order.ID),
		}
		// order_number is WooCommerce's merchant-facing label and differs from
		// the post id on stores with a numbering plugin; omitted when unset so
		// the field stays absent rather than reading "0".
		if order.OrderNumber != 0 {
			resp.Order.Label = fmt.Sprintf("%d", order.OrderNumber)
		}
	}

	consent := consentFromMeta(order.MetaData)
	if order.BillingAddress.Email != "" || consent != nil {
		resp.Buyer = &model.BuyerWithConsent{
			Buyer: model.Buyer{
				Email:       order.BillingAddress.Email,
				FirstName:   order.BillingAddress.FirstName,
				LastName:    order.BillingAddress.LastName,
				PhoneNumber: order.BillingAddress.Phone,
			},
			Consent: consent,
		}
	}

	return resp
}

// orderPermalink is where the buyer can see the order on the merchant's site.
// Matches the continue_url the connector already builds for a live checkout.
func orderPermalink(baseURL string, orderID int) string {
	if baseURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/checkout/order-received/%d/", baseURL, orderID)
}

func (m mapper) orderStatusToCheckoutStatus(status string) base.CheckoutStatus {
	switch status {
	case "processing":
		return base.CheckoutStatusCompleted
	case "completed":
		return base.CheckoutStatusCompleted
	case "pending":
		return base.CheckoutStatusIncomplete
	case "cancelled":
		return base.CheckoutStatusCanceled
	default:
		return base.CheckoutStatusIncomplete
	}
}

// ─── Helpers ───────────────────────────────────────────────────────────────────

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// lineItemTotals is a LineItem's own cost breakdown: subtotal and total
// both equal the line's extended amount (unit price × quantity, before
// order-level tax/shipping/discount) — the minimum shape the Totals
// invariant (exactly one subtotal, one total entry) allows, and the
// canonical Checkout schema requires every line item to carry one.
func lineItemTotals(amount int64) model.Totals {
	return model.Totals{
		model.Total{Type: model.TotalTypeSubtotal, Amount: model.SignedAmount(amount)},
		model.Total{Type: model.TotalTypeTotal, Amount: model.SignedAmount(amount)},
	}
}

// ucpAddressToWCAddress converts a UCP postal address to WooCommerce format.
func ucpAddressToWCAddress(addr *model.PostalAddress) map[string]any {
	if addr == nil {
		return map[string]any{}
	}
	return map[string]any{
		"first_name": addr.FirstName,
		"last_name":  addr.LastName,
		"address_1":  addr.StreetAddress,
		"address_2":  addr.ExtendedAddress,
		"city":       addr.AddressLocality,
		"state":      addr.AddressRegion,
		"postcode":   addr.PostalCode,
		"country":    addr.AddressCountry,
		"phone":      addr.PhoneNumber,
	}
}

// consentFromMeta recovers the buyer_consent extension's consent object from
// an order's meta_data. Nil when none was stored (or it was cleared), so the
// response omits consent rather than reporting an all-unset object.
func consentFromMeta(meta []wcMetaData) *model.Consent {
	for _, m := range meta {
		if m.Key != metaKeyBuyerConsent {
			continue
		}
		raw, ok := m.Value.(string)
		if !ok || raw == "" {
			return nil
		}
		var c model.Consent
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			return nil
		}
		return &c
	}
	return nil
}
