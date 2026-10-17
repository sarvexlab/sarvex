// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package shopify

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// mapper is a stateless collection of translation functions from Shopify
// native types to UCP canonical types. All functions are pure — no IO.
//
// Responsibility boundaries after the extension refactor:
//
//	mapper.go     — base capability fields only: line items, totals, buyer
//	                identity (email/name/phone), order events, error messages,
//	                address translation.
//
//	extension.go  — fulfillment extension (methods/destinations/groups/options)
//	                and discount extension (applied[], allocations[]).
//	                Called by connector.go AFTER mapper builds the base response.
//
// The mapper intentionally does NOT populate resp.Fulfillment or resp.Discounts.
// Those are populated by ApplyFulfillmentExtension and ApplyDiscountExtension.
// The mapper also does NOT set resp.Buyer.Consent — that is carried by
// connector.carryConsent after the checkout response is fully assembled.
type mapper struct{}

// ─── Catalog ──────────────────────────────────────────────────────────────────

func (m mapper) productToUCPItem(p shopifyProduct) model.Product {
	currency := p.PriceRange.MinVariantPrice.CurrencyCode
	minPrice := model.Amount(moneyToCents(p.PriceRange.MinVariantPrice.Amount))

	product := model.Product{
		ID:    p.ID,
		Title: p.Title,
		Description: model.Description{
			Plain: p.Description,
		},
		URL: p.OnlineStoreURL,
		PriceRange: model.PriceRange{
			Min: model.Price{Amount: minPrice, Currency: currency},
			Max: model.Price{Amount: minPrice, Currency: currency},
		},
	}

	if p.FeaturedImage != nil {
		product.Media = []model.Media{{
			Type: model.MediaTypeImage,
			URL:  p.FeaturedImage.URL,
		}}
	}

	for _, edge := range p.Variants.Edges {
		v := edge.Node
		price := model.Amount(moneyToCents(v.PriceV2.Amount))
		if price == 0 {
			price = model.Amount(moneyToCents(v.Price.Amount))
		}

		variant := model.Variant{
			ID:    v.ID,
			Title: v.Title,
			Price: model.Price{Amount: price, Currency: currency},
			Availability: &model.VariantAvailability{
				Available: v.AvailableForSale,
			},
		}
		for _, opt := range v.SelectedOptions {
			variant.Options = append(variant.Options, model.SelectedOption{
				Name:  opt.Name,
				Label: opt.Value,
			})
		}
		product.Variants = append(product.Variants, variant)
	}

	return product
}

// ─── Cart ─────────────────────────────────────────────────────────────────────

// cartToUCPResponse builds the base CartResponse from a Shopify Cart object.
//
// NOTE: resp.Discounts is NOT set here. The discount extension (cart.discountCodes)
// is applied afterwards by mapper.ApplyCartDiscountExtension in extension.go.
func (m mapper) cartToUCPResponse(cart *shopifyCart) *base.CartResponse {
	resp := &base.CartResponse{
		ID:          cart.ID,
		ContinueURL: cart.CheckoutURL,
	}

	for _, edge := range cart.Lines.Edges {
		line := edge.Node
		unitPrice := model.Amount(moneyToCents(line.Merchandise.Price.Amount))
		li := model.LineItem{
			ID:       line.ID,
			Quantity: line.Quantity,
			Item: model.Item{
				ID:    line.Merchandise.ID,
				Title: line.Merchandise.Title,
				Price: &unitPrice,
			},
			Totals: model.Totals{
				{Type: model.TotalTypeSubtotal, Amount: model.SignedAmount(moneyToCents(line.Cost.SubtotalAmount.Amount))},
				{Type: model.TotalTypeTotal, Amount: model.SignedAmount(moneyToCents(line.Cost.TotalAmount.Amount))},
			},
		}
		if line.Merchandise.Product.FeaturedImage != nil {
			li.Item.ImageURL = line.Merchandise.Product.FeaturedImage.URL
		}
		resp.LineItems = append(resp.LineItems, li)
	}

	resp.Totals = m.cartCostToTotals(
		cart.Cost.SubtotalAmount,
		cart.Cost.TotalTaxAmount,
		cart.Cost.TotalAmount,
	)

	if cart.BuyerIdentity.Email != "" {
		resp.Buyer = &model.Buyer{Email: cart.BuyerIdentity.Email}
	}

	return resp
}

func (m mapper) cartCostToTotals(subtotal shopifyMoney, tax *shopifyMoney, total shopifyMoney) model.Totals {
	totals := model.Totals{
		{Type: model.TotalTypeSubtotal, Amount: model.SignedAmount(moneyToCents(subtotal.Amount))},
	}
	if tax != nil && tax.Amount != "" && tax.Amount != "0.0" {
		totals = append(totals, model.Total{Type: model.TotalTypeTax, Amount: model.SignedAmount(moneyToCents(tax.Amount))})
	}
	totals = append(totals, model.Total{Type: model.TotalTypeTotal, Amount: model.SignedAmount(moneyToCents(total.Amount))})
	return totals
}

func (m mapper) cartUserErrorsToMessages(errs []shopifyUserError) []model.Message {
	msgs := make([]model.Message, 0, len(errs))
	for _, e := range errs {
		msgs = append(msgs, model.NewErrorMessage(model.MessageError{
			Code:     model.ErrorCode(m.normaliseCartErrorCode(e.Code)),
			Content:  e.Message,
			Severity: model.MessageSeverityRecoverable,
			Path:     m.fieldPathToJSONPath(e.Field),
		}))
	}
	return msgs
}

// ─── Checkout ─────────────────────────────────────────────────────────────────

// checkoutToUCPResponse builds the base CheckoutResponse from a Shopify
// Checkout object. It populates the following fields:
//
//	ID, Status, ContinueURL
//	Order (on completed checkouts)
//	LineItems — with per-line subtotal and post-discount total
//	Totals    — subtotal, tax, shipping, total
//	Buyer     — email, first/last name, phone (Consent is NOT set here)
//	Messages  — recoverable missing-field errors
//	Links     — terms_of_service, privacy_policy
//
// The following fields are intentionally left nil by this function:
//
//	Fulfillment — set by mapper.ApplyFulfillmentExtension (extension.go)
//	Discounts   — set by mapper.ApplyDiscountExtension (extension.go)
func (m mapper) checkoutToUCPResponse(co *shopifyCheckout, cfg MerchantConfig) *base.CheckoutResponse {
	resp := &base.CheckoutResponse{
		ID:          co.ID,
		ContinueURL: co.WebURL,
	}

	resp.Status = m.deriveCheckoutStatus(co)

	// Completed checkout — surface order confirmation.
	// co.Order.ID is a Shopify GID (e.g. "gid://shopify/Order/1234567890");
	// strip to the numeric tail so the Admin REST endpoint receives a plain ID.
	if co.Order != nil {
		resp.Order = &model.OrderConfirmation{
			ID:           gidToNumericID(co.Order.ID),
			Label:        co.Order.Name,
			PermalinkURL: co.Order.StatusURL,
		}
	}

	// Line items.
	// The "total" entry on each line reflects the post-discount amount.
	// The full discount breakdown lives in the Discounts extension (set later).
	for _, edge := range co.LineItems.Edges {
		node := edge.Node
		li := model.LineItem{
			ID:       node.ID,
			Quantity: node.Quantity,
			Item: model.Item{
				ID:    node.ID,
				Title: node.Title,
			},
		}
		if node.Variant != nil {
			li.Item.ID = node.Variant.ID
			unitPrice := model.Amount(moneyToCents(node.Variant.PriceV2.Amount))
			li.Item.Price = &unitPrice
			if node.Variant.Product.FeaturedImage != nil {
				li.Item.ImageURL = node.Variant.Product.FeaturedImage.URL
			}
			subtotal := int64(unitPrice) * int64(node.Quantity)
			// Sum per-line discount allocations to compute post-discount total.
			var allocated int64
			for _, d := range node.DiscountAllocations {
				allocated += moneyToCents(d.AllocatedAmount.Amount)
			}
			li.Totals = model.Totals{
				{Type: model.TotalTypeSubtotal, Amount: model.SignedAmount(subtotal)},
				{Type: model.TotalTypeTotal, Amount: model.SignedAmount(subtotal - allocated)},
			}
		}
		resp.LineItems = append(resp.LineItems, li)
	}

	// Order-level totals.
	resp.Totals = model.Totals{
		{Type: model.TotalTypeSubtotal, Amount: model.SignedAmount(moneyToCents(co.SubtotalPriceV2.Amount))},
	}
	if co.TotalTaxV2 != nil {
		resp.Totals = append(resp.Totals, model.Total{
			Type:   model.TotalTypeTax,
			Amount: model.SignedAmount(moneyToCents(co.TotalTaxV2.Amount)),
		})
	}
	if co.ShippingLine != nil {
		resp.Totals = append(resp.Totals, model.Total{
			Type:        model.TotalType("shipping"),
			DisplayText: co.ShippingLine.Title,
			Amount:      model.SignedAmount(moneyToCents(co.ShippingLine.PriceV2.Amount)),
		})
	}
	resp.Totals = append(resp.Totals, model.Total{
		Type:   model.TotalTypeTotal,
		Amount: model.SignedAmount(moneyToCents(co.TotalPriceV2.Amount)),
	})

	// Buyer — identity only. Consent is applied separately by connector.carryConsent.
	resp.Buyer = m.checkoutBuyer(co)

	// Messages using extension-accurate JSONPaths.
	resp.Messages = m.deriveCheckoutMessages(co)

	// Links — both spec-required link types.
	resp.Links = []model.Link{
		{Type: model.LinkTypeTermsOfService, URL: cfg.StoreBaseURL + "/policies/terms-of-service"},
		{Type: model.LinkTypePrivacyPolicy, URL: cfg.StoreBaseURL + "/policies/privacy-policy"},
	}

	return resp
}

// checkoutBuyer assembles a BuyerWithConsent from the checkout email and shipping address.
// Returns nil when no buyer information is present at all.
// Consent is intentionally NOT set here.
func (m mapper) checkoutBuyer(co *shopifyCheckout) *model.BuyerWithConsent {
	if co.Email == "" && co.ShippingAddress == nil {
		return nil
	}
	buyer := &model.BuyerWithConsent{}
	if co.Email != "" {
		buyer.Email = co.Email
	}
	if co.ShippingAddress != nil {
		buyer.FirstName = co.ShippingAddress.FirstName
		buyer.LastName = co.ShippingAddress.LastName
		buyer.PhoneNumber = co.ShippingAddress.Phone
	}
	return buyer
}

func (m mapper) deriveCheckoutStatus(co *shopifyCheckout) base.CheckoutStatus {
	if co.CompletedAt != nil && *co.CompletedAt != "" {
		return base.CheckoutStatusCompleted
	}

	missingEmail := co.Email == ""
	missingAddress := co.RequiresShipping && co.ShippingAddress == nil
	missingShippingSelection := co.RequiresShipping && co.ShippingLine == nil

	if missingEmail || missingAddress || missingShippingSelection {
		return base.CheckoutStatusIncomplete
	}

	return base.CheckoutStatusReadyForComplete
}

// deriveCheckoutMessages produces recoverable error messages for missing
// required fields. JSONPaths use the fulfillment extension schema paths
// (methods[0]/groups[0]) so platforms can target the exact field.
func (m mapper) deriveCheckoutMessages(co *shopifyCheckout) []model.Message {
	var msgs []model.Message

	if co.Email == "" {
		msgs = append(msgs, model.NewErrorMessage(model.MessageError{
			Code:     model.ErrorCode("missing"),
			Path:     "$.buyer.email",
			Content:  "Buyer email is required to proceed",
			Severity: model.MessageSeverityRecoverable,
		}))
	}

	if co.RequiresShipping {
		if co.ShippingAddress == nil {
			msgs = append(msgs, model.NewErrorMessage(model.MessageError{
				Code:     model.ErrorCode("missing"),
				Path:     "$.fulfillment.methods[0].selected_destination_id",
				Content:  "Shipping address is required",
				Severity: model.MessageSeverityRecoverable,
			}))
		} else if co.ShippingLine == nil {
			if co.AvailableShippingRates != nil && !co.AvailableShippingRates.Ready {
				msgs = append(msgs, model.NewInfoMessage(model.MessageInfo{
					Code:    model.InfoCode("shipping_rates_loading"),
					Content: "Shipping rates are being calculated for this address",
				}))
			} else {
				msgs = append(msgs, model.NewErrorMessage(model.MessageError{
					Code:     model.ErrorCode("missing"),
					Path:     "$.fulfillment.methods[0].groups[0].selected_option_id",
					Content:  "A shipping method must be selected",
					Severity: model.MessageSeverityRecoverable,
				}))
			}
		}
	}

	return msgs
}

func (m mapper) checkoutUserErrorsToMessages(errs []shopifyCheckoutError) []model.Message {
	msgs := make([]model.Message, 0, len(errs))
	for _, e := range errs {
		msgs = append(msgs, model.NewErrorMessage(model.MessageError{
			Code:     model.ErrorCode(m.normaliseCheckoutErrorCode(e.Code)),
			Content:  e.Message,
			Path:     m.fieldPathToJSONPath(e.Field),
			Severity: m.checkoutErrorSeverity(e.Code),
		}))
	}
	return msgs
}

// ─── Addresses ────────────────────────────────────────────────────────────────

// ucpAddressToShopifyMailingInput converts a UCP PostalAddress to the map
// shape Shopify expects for MailingAddressInput in GraphQL mutations.
func (m mapper) ucpAddressToShopifyMailingInput(addr *model.PostalAddress) map[string]any {
	if addr == nil {
		return nil
	}
	return map[string]any{
		"firstName": addr.FirstName,
		"lastName":  addr.LastName,
		"address1":  addr.StreetAddress,
		"address2":  addr.ExtendedAddress,
		"city":      addr.AddressLocality,
		"province":  addr.AddressRegion,
		"country":   addr.AddressCountry,
		"zip":       addr.PostalCode,
		"phone":     addr.PhoneNumber,
	}
}

// ─── Order events ─────────────────────────────────────────────────────────────

func (m mapper) adminOrderToUCPEvent(order shopifyAdminOrder) *base.OrderEvent {
	event := &base.OrderEvent{
		OrderID: fmt.Sprintf("%d", order.ID),
		Status:  m.normaliseOrderStatus(order.FinancialStatus, order.FulfillmentStatus),
	}
	for _, li := range order.LineItems {
		event.Items = append(event.Items, base.OrderItem{
			LineItemID: fmt.Sprintf("%d", li.ID),
			Quantity:   li.Quantity,
			Status:     li.FulfillmentStatus,
		})
	}
	if len(order.Fulfillments) > 0 {
		f := order.Fulfillments[0]
		event.Tracking = &base.TrackingInfo{
			Carrier:        f.TrackingCompany,
			TrackingNumber: f.TrackingNumber,
			TrackingURL:    f.TrackingURL,
		}
	}
	return event
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// moneyToCents converts a Shopify decimal amount string ("12.50") to integer
// minor units (1250). Returns 0 on parse error or empty input.
func moneyToCents(amount string) int64 {
	if amount == "" {
		return 0
	}
	f, err := strconv.ParseFloat(amount, 64)
	if err != nil {
		return 0
	}
	return int64(math.Round(f * 100))
}

func (m mapper) fieldPathToJSONPath(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	return "$." + strings.Join(fields, ".")
}

func (m mapper) normaliseCartErrorCode(code string) string {
	switch code {
	case "INVALID":
		return "invalid"
	case "LESS_THAN":
		return "quantity_invalid"
	case "INVALID_MERCHANDISE_LINE":
		return "item_unavailable"
	default:
		return strings.ToLower(code)
	}
}

func (m mapper) normaliseCheckoutErrorCode(code string) string {
	switch code {
	case "CART_DOES_NOT_MEET_DISCOUNT_REQUIREMENTS_NOTICE":
		return "discount_invalid"
	case "DELIVERY_ADDRESS_REQUIRED":
		return "missing"
	case "GIFT_CARD_NOT_APPLICABLE":
		return "payment_failed"
	case "INVALID":
		return "invalid"
	case "NOT_ENOUGH_IN_STOCK":
		return "out_of_stock"
	case "PRODUCT_UNAVAILABLE":
		return "item_unavailable"
	case "PAYMENT_ERROR":
		return "payment_failed"
	// Discount rejection codes — mapped to spec-defined discount error codes.
	case "DISCOUNT_NOT_FOUND":
		return "discount_code_invalid"
	case "DISCOUNT_ALREADY_APPLIED":
		return "discount_code_already_applied"
	case "DISCOUNT_EXPIRED":
		return "discount_code_expired"
	case "CUSTOMER_ALREADY_USED_ONCE_PER_CUSTOMER_DISCOUNT_NOTICE":
		return "discount_code_user_ineligible"
	default:
		return strings.ToLower(code)
	}
}

func (m mapper) checkoutErrorSeverity(code string) model.MessageSeverity {
	switch code {
	case "NOT_ENOUGH_IN_STOCK", "PRODUCT_UNAVAILABLE":
		return model.MessageSeverityRecoverable
	case "PAYMENT_ERROR":
		return model.MessageSeverityRecoverable
	case "INVALID_COUNTRY_AND_CURRENCY":
		return model.MessageSeverityUnrecoverable
	// Discount rejections are warnings per spec: the buyer expects a discount
	// but won't receive it, so they must be informed.
	case "DISCOUNT_NOT_FOUND",
		"DISCOUNT_ALREADY_APPLIED",
		"DISCOUNT_EXPIRED",
		"CUSTOMER_ALREADY_USED_ONCE_PER_CUSTOMER_DISCOUNT_NOTICE":
		return model.MessageSeverityRecoverable
	default:
		return model.MessageSeverityRecoverable
	}
}

// gidToNumericID extracts the numeric tail from a Shopify Global ID.
// "gid://shopify/Order/1234567890" → "1234567890"
// Returns the input unchanged if it is not a GID.
func gidToNumericID(gid string) string {
	if i := strings.LastIndex(gid, "/"); i >= 0 && i < len(gid)-1 {
		return gid[i+1:]
	}
	return gid
}

func (m mapper) normaliseOrderStatus(financial, fulfillment string) string {
	if fulfillment == "fulfilled" {
		return "fulfilled"
	}
	if fulfillment == "partial" {
		return "partially_fulfilled"
	}
	if financial == "paid" {
		return "processing"
	}
	if financial == "pending" {
		return "pending"
	}
	return "unknown"
}
