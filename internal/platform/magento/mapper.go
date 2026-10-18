// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package magento

import (
	"fmt"
	"math"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// mapper is a stateless collection of translation functions from Magento
// native types to UCP canonical types. All functions are pure — no IO.
type mapper struct{}

// ─── Catalog ──────────────────────────────────────────────────────────────────

func (m mapper) productToUCPItem(p mgProduct, currency string, baseURL string) model.Product {
	product := model.Product{
		ID:    p.SKU,
		Title: p.Name,
		PriceRange: model.PriceRange{
			Min: model.Price{Amount: model.Amount(priceToMinorUnits(p.Price)), Currency: currency},
			Max: model.Price{Amount: model.Amount(priceToMinorUnits(p.Price)), Currency: currency},
		},
	}

	desc := m.customAttributeValue(p.CustomAttributes, "short_description")
	if desc != "" {
		product.Description = model.Description{Plain: desc}
	}

	urlKey := m.customAttributeValue(p.CustomAttributes, "url_key")
	if urlKey != "" {
		product.URL = baseURL + "/" + urlKey + ".html"
	}

	if len(p.MediaGalleryEntries) > 0 {
		product.Media = []model.Media{{
			Type: model.MediaTypeImage,
			URL:  baseURL + "/media/catalog/product" + p.MediaGalleryEntries[0].URL,
		}}
	}

	available := m.isProductAvailable(p)
	product.Variants = []model.Variant{{
		ID:    p.SKU,
		Title: p.Name,
		Price: model.Price{Amount: model.Amount(priceToMinorUnits(p.Price)), Currency: currency},
		Availability: &model.VariantAvailability{
			Available: available,
		},
	}}

	return product
}

func (m mapper) isProductAvailable(p mgProduct) bool {
	if p.Status != 1 {
		return false
	}
	if p.ExtensionAttributes != nil && p.ExtensionAttributes.StockItem != nil {
		return p.ExtensionAttributes.StockItem.IsInStock && p.ExtensionAttributes.StockItem.Qty > 0
	}
	return true
}

func (m mapper) customAttributeValue(attrs []mgCustomAttribute, code string) string {
	for _, attr := range attrs {
		if attr.AttributeCode == code {
			if val, ok := attr.Value.(string); ok {
				return val
			}
			return fmt.Sprintf("%v", attr.Value)
		}
	}
	return ""
}

// ─── Cart / Checkout ──────────────────────────────────────────────────────────

// cartLineItems maps a quote's items to canonical line items.
func (m mapper) cartLineItems(cart *mgCart) []model.LineItem {
	var out []model.LineItem
	for _, item := range cart.Items {
		price := model.Amount(priceToMinorUnits(item.Price))
		out = append(out, model.LineItem{
			ID: fmt.Sprintf("%d", item.ItemID),
			Item: model.Item{
				ID:    item.SKU,
				Title: item.Name,
				Price: &price,
			},
			Quantity: int(item.Qty),
			Totals:   lineItemTotals(priceToMinorUnits(item.Price * item.Qty)),
		})
	}
	return out
}

// cartTotals builds the canonical totals from a quote's totals. A quote whose
// totals could not be read still yields a well-formed set (the line items'
// sum), so a response never ships without the subtotal/total pair the schema
// requires. The discount entry appears only when a discount applies: the spec
// types it as strictly negative, so a zero entry would be invalid.
func (m mapper) cartTotals(cart *mgCart) model.Totals {
	t := cart.Totals
	if t == nil {
		var sum int64
		for _, item := range cart.Items {
			sum += priceToMinorUnits(item.Price * item.Qty)
		}
		return model.Totals{
			model.Total{Type: model.TotalTypeSubtotal, Amount: model.SignedAmount(sum)},
			model.Total{Type: model.TotalTypeTotal, Amount: model.SignedAmount(sum)},
		}
	}
	return orderTotals(t.Subtotal, t.TaxAmount, t.ShippingAmount, t.DiscountAmount, t.GrandTotal)
}

// orderTotals builds canonical totals from Magento's amounts. Magento reports
// a discount as a negative number on quotes and orders alike (some versions
// report it positive), so its magnitude is taken and the canonical entry is
// always negative.
func orderTotals(subtotal, tax, shipping, discount, grand float64) model.Totals {
	totals := model.Totals{
		model.Total{Type: model.TotalTypeSubtotal, Amount: model.SignedAmount(priceToMinorUnits(subtotal))},
		model.Total{Type: model.TotalTypeTax, Amount: model.SignedAmount(priceToMinorUnits(tax))},
		model.Total{Type: model.TotalTypeFulfillment, Amount: model.SignedAmount(priceToMinorUnits(shipping))},
	}
	if d := discountMinorUnits(discount); d > 0 {
		totals = append(totals, model.Total{Type: model.TotalTypeDiscount, Amount: model.SignedAmount(-d)})
	}
	return append(totals, model.Total{Type: model.TotalTypeTotal, Amount: model.SignedAmount(priceToMinorUnits(grand))})
}

// cartBuyer is the contact a quote has recorded, from its billing address or
// (for a guest) its customer email. Nil when none is recorded yet.
func (m mapper) cartBuyer(cart *mgCart) *model.Buyer {
	b := &model.Buyer{}
	if cart.BillingAddress != nil {
		b.Email = cart.BillingAddress.Email
		b.FirstName = cart.BillingAddress.FirstName
		b.LastName = cart.BillingAddress.LastName
		b.PhoneNumber = cart.BillingAddress.Telephone
	}
	if b.Email == "" && cart.Customer != nil {
		b.Email = cart.Customer.Email
	}
	if b.Email == "" && b.FirstName == "" && b.LastName == "" && b.PhoneNumber == "" {
		return nil
	}
	return b
}

// cartToCartResponse maps a quote to a cart response under the id the caller
// addresses the cart by (Magento's masked id, not the quote's own numeric id).
func (m mapper) cartToCartResponse(cart *mgCart, cartID string, currency string) *base.CartResponse {
	return &base.CartResponse{
		ID:        cartID,
		Currency:  currency,
		LineItems: m.cartLineItems(cart),
		Totals:    m.cartTotals(cart),
		Buyer:     m.cartBuyer(cart),
	}
}

// cartToCheckoutResponse maps a quote to a not-yet-completed checkout response.
func (m mapper) cartToCheckoutResponse(cart *mgCart, checkoutID string, currency string) *base.CheckoutResponse {
	resp := &base.CheckoutResponse{
		ID:        checkoutID,
		Status:    base.CheckoutStatusIncomplete,
		Currency:  currency,
		LineItems: m.cartLineItems(cart),
		Totals:    m.cartTotals(cart),
	}
	if b := m.cartBuyer(cart); b != nil {
		resp.Buyer = &model.BuyerWithConsent{Buyer: *b}
	}
	return resp
}

// orderToCheckoutResponse maps a placed order to the completed checkout
// response. checkoutID is the id the caller addresses the checkout by; the
// order's own ids are reported on the confirmation.
func (m mapper) orderToCheckoutResponse(order *mgOrder, checkoutID string, currency, baseURL string) *base.CheckoutResponse {
	resp := &base.CheckoutResponse{
		ID:       checkoutID,
		Status:   m.orderStatusToCheckoutStatus(order.Status),
		Currency: currency,
	}

	for _, item := range order.Items {
		price := model.Amount(priceToMinorUnits(item.Price))
		lineTotal := priceToMinorUnits(item.Price * item.Qty)
		resp.LineItems = append(resp.LineItems, model.LineItem{
			ID: fmt.Sprintf("%d", item.ItemID),
			Item: model.Item{
				ID:    item.SKU,
				Title: item.Name,
				Price: &price,
			},
			Quantity: int(item.Qty),
			Totals:   lineItemTotals(lineTotal),
		})
	}

	resp.Totals = orderTotals(order.Subtotal, order.TaxAmount, order.ShippingAmount, order.DiscountAmount, order.GrandTotal)

	if order.BillingAddress != nil {
		resp.Buyer = &model.BuyerWithConsent{
			Buyer: model.Buyer{
				Email:       order.BillingAddress.Email,
				FirstName:   order.BillingAddress.FirstName,
				LastName:    order.BillingAddress.LastName,
				PhoneNumber: order.BillingAddress.Telephone,
			},
		}
	}

	// The placed order, once there is one: the serving plane reads
	// Order.ID into the order-intent event the sync plane records the sale
	// from. The id is the entity id (what /orders/{id} is addressed by, so the
	// sync plane can look the order up again); increment_id is the
	// merchant-facing number.
	if resp.Status == base.CheckoutStatusCompleted {
		resp.Order = &model.OrderConfirmation{
			ID:           fmt.Sprintf("%d", order.EntityID),
			Label:        order.IncrementID,
			PermalinkURL: baseURL + "/sales/guest/form/",
		}
	}
	return resp
}

// orderStatusToCheckoutStatus maps a Magento order status. "pending" is what
// offline payment methods leave a freshly placed order in, so it reads as
// incomplete here; CheckoutComplete, which knows the order was just placed,
// overrides it.
func (m mapper) orderStatusToCheckoutStatus(status string) base.CheckoutStatus {
	switch status {
	case "complete", "processing", "closed":
		return base.CheckoutStatusCompleted
	case "canceled", "cancelled":
		return base.CheckoutStatusCanceled
	default:
		return base.CheckoutStatusIncomplete
	}
}

// ─── Helpers ───────────────────────────────────────────────────────────────────

// priceToMinorUnits converts a price (in major units, e.g., dollars) to minor units (cents).
// It rounds: truncating loses a cent to floating-point error (19.99*100 is
// 1998.999…).
func priceToMinorUnits(price float64) int64 {
	return int64(math.Round(price * 100))
}

// discountMinorUnits is the magnitude of a Magento discount amount in minor
// units (Magento reports discounts as negative numbers).
func discountMinorUnits(amount float64) int64 {
	d := priceToMinorUnits(amount)
	if d < 0 {
		return -d
	}
	return d
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

// ucpAddressToMgAddress converts a UCP PostalAddress to Magento format.
func ucpAddressToMgAddress(addr *model.PostalAddress) *mgAddress {
	if addr == nil {
		return nil
	}
	street := []string{addr.StreetAddress}
	if addr.ExtendedAddress != "" {
		street = append(street, addr.ExtendedAddress)
	}
	return &mgAddress{
		FirstName:  addr.FirstName,
		LastName:   addr.LastName,
		Street:     street,
		City:       addr.AddressLocality,
		Region:     addr.AddressRegion,
		PostalCode: addr.PostalCode,
		Country:    addr.AddressCountry,
		Telephone:  addr.PhoneNumber,
	}
}

// mgAddressToPostalAddress converts a Magento address to a UCP PostalAddress.
func mgAddressToPostalAddress(a *mgAddress) model.PostalAddress {
	p := model.PostalAddress{
		FirstName:       a.FirstName,
		LastName:        a.LastName,
		AddressLocality: a.City,
		AddressRegion:   a.Region,
		PostalCode:      a.PostalCode,
		AddressCountry:  a.Country,
		PhoneNumber:     a.Telephone,
	}
	if len(a.Street) > 0 {
		p.StreetAddress = a.Street[0]
	}
	if len(a.Street) > 1 {
		p.ExtendedAddress = a.Street[1]
	}
	return p
}
