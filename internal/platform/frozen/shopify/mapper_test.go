// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package shopify

import (
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
	ucp "github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// All tests in this file are pure unit tests — no network, no file I/O.
// Run with: go test ./internal/connector/shopify/ -run TestMapper

var m mapper // zero value is valid

// ─── moneyToCents ─────────────────────────────────────────────────────────────

func TestMoneyToCents(t *testing.T) {
	cases := []struct {
		input string
		want  int64
	}{
		{"12.50", 1250},
		{"0.00", 0},
		{"100.00", 10000},
		{"9.99", 999},
		{"1.001", 100}, // rounds to nearest cent
		{"", 0},
		{"abc", 0},
		{"1999.99", 199999},
	}
	for _, tc := range cases {
		got := moneyToCents(tc.input)
		if got != tc.want {
			t.Errorf("moneyToCents(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

// ─── productToUCPItem ─────────────────────────────────────────────────────────

func TestProductToUCPItem_BasicMapping(t *testing.T) {
	imgURL := "https://cdn.shopify.com/s/files/image.jpg"
	p := shopifyProduct{
		ID:               "gid://shopify/Product/123",
		Title:            "Test Shirt",
		Description:      "A great shirt",
		Handle:           "test-shirt",
		OnlineStoreURL:   "https://store.example.com/products/test-shirt",
		AvailableForSale: true,
		PriceRange: struct {
			MinVariantPrice shopifyMoney `json:"minVariantPrice"`
		}{
			MinVariantPrice: shopifyMoney{Amount: "29.99", CurrencyCode: "USD"},
		},
		FeaturedImage: &struct {
			URL string `json:"url"`
		}{URL: imgURL},
	}
	p.Variants.Edges = []struct {
		Node shopifyVariant `json:"node"`
	}{
		{Node: shopifyVariant{
			ID:               "gid://shopify/ProductVariant/456",
			Title:            "M / Blue",
			AvailableForSale: true,
			Price:            shopifyMoney{Amount: "29.99", CurrencyCode: "USD"},
			SelectedOptions: []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			}{
				{Name: "Size", Value: "M"},
				{Name: "Color", Value: "Blue"},
			},
		}},
	}

	item := m.productToUCPItem(p)

	if item.ID != p.ID {
		t.Errorf("ID: got %s, want %s", item.ID, p.ID)
	}
	if item.Title != "Test Shirt" {
		t.Errorf("Title: got %s", item.Title)
	}
	if item.PriceRange.Min.Amount != 2999 {
		t.Errorf("Price: got %d, want 2999", item.PriceRange.Min.Amount)
	}
	if item.PriceRange.Min.Currency != "USD" {
		t.Errorf("Currency: got %s, want USD", item.PriceRange.Min.Currency)
	}
	if len(item.Media) == 0 || item.Media[0].URL != imgURL {
		t.Errorf("ImageURL: got %v, want %s", item.Media, imgURL)
	}
	if len(item.Variants) != 1 {
		t.Fatalf("Variants: got %d, want 1", len(item.Variants))
	}
	v := item.Variants[0]
	if v.Availability == nil || !v.Availability.Available {
		t.Error("Available should be true")
	}
	findOpt := func(name string) string {
		for _, o := range v.Options {
			if o.Name == name {
				return o.Label
			}
		}
		return ""
	}
	if findOpt("Size") != "M" || findOpt("Color") != "Blue" {
		t.Errorf("Variant options incorrect: %v", v.Options)
	}
}

func TestProductToUCPItem_NoImage(t *testing.T) {
	p := shopifyProduct{
		ID:    "gid://shopify/Product/1",
		Title: "Invisible Product",
		PriceRange: struct {
			MinVariantPrice shopifyMoney `json:"minVariantPrice"`
		}{MinVariantPrice: shopifyMoney{Amount: "0.00", CurrencyCode: "USD"}},
	}
	item := m.productToUCPItem(p)
	if len(item.Media) != 0 {
		t.Errorf("expected no media, got %v", item.Media)
	}
}

// ─── deriveCheckoutStatus ─────────────────────────────────────────────────────

func TestDeriveCheckoutStatus_Incomplete_MissingEmail(t *testing.T) {
	co := &shopifyCheckout{
		Email:            "",
		RequiresShipping: false,
	}
	got := m.deriveCheckoutStatus(co)
	if got != ucp.CheckoutStatusIncomplete {
		t.Errorf("got %s, want %s", got, ucp.CheckoutStatusIncomplete)
	}
}

func TestDeriveCheckoutStatus_Incomplete_MissingShipping(t *testing.T) {
	co := &shopifyCheckout{
		Email:            "buyer@example.com",
		RequiresShipping: true,
		ShippingAddress:  nil,
	}
	got := m.deriveCheckoutStatus(co)
	if got != ucp.CheckoutStatusIncomplete {
		t.Errorf("got %s, want %s", got, ucp.CheckoutStatusIncomplete)
	}
}

func TestDeriveCheckoutStatus_Incomplete_NoShippingRateSelected(t *testing.T) {
	co := &shopifyCheckout{
		Email:            "buyer@example.com",
		RequiresShipping: true,
		ShippingAddress:  &shopifyMailingAddress{City: "Sydney"},
		ShippingLine:     nil,
	}
	got := m.deriveCheckoutStatus(co)
	if got != ucp.CheckoutStatusIncomplete {
		t.Errorf("got %s, want %s", got, ucp.CheckoutStatusIncomplete)
	}
}

func TestDeriveCheckoutStatus_ReadyForComplete(t *testing.T) {
	co := &shopifyCheckout{
		Email:            "buyer@example.com",
		RequiresShipping: true,
		ShippingAddress:  &shopifyMailingAddress{City: "Sydney"},
		ShippingLine: &struct {
			Handle  string       `json:"handle"`
			Title   string       `json:"title"`
			PriceV2 shopifyMoney `json:"priceV2"`
		}{Handle: "express", Title: "Express", PriceV2: shopifyMoney{Amount: "9.99"}},
	}
	got := m.deriveCheckoutStatus(co)
	if got != ucp.CheckoutStatusReadyForComplete {
		t.Errorf("got %s, want %s", got, ucp.CheckoutStatusReadyForComplete)
	}
}

func TestDeriveCheckoutStatus_Completed(t *testing.T) {
	completedAt := "2026-04-08T10:00:00Z"
	co := &shopifyCheckout{
		Email:       "buyer@example.com",
		CompletedAt: &completedAt,
	}
	got := m.deriveCheckoutStatus(co)
	if got != ucp.CheckoutStatusCompleted {
		t.Errorf("got %s, want %s", got, ucp.CheckoutStatusCompleted)
	}
}

func TestDeriveCheckoutStatus_DigitalGoods_ReadyWithNoShipping(t *testing.T) {
	co := &shopifyCheckout{
		Email:            "buyer@example.com",
		RequiresShipping: false,
	}
	got := m.deriveCheckoutStatus(co)
	if got != ucp.CheckoutStatusReadyForComplete {
		t.Errorf("got %s, want %s", got, ucp.CheckoutStatusReadyForComplete)
	}
}

// ─── deriveCheckoutMessages ───────────────────────────────────────────────────

func TestDeriveCheckoutMessages_MissingEmail(t *testing.T) {
	co := &shopifyCheckout{
		Email:            "",
		RequiresShipping: false,
	}
	msgs := m.deriveCheckoutMessages(co)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	err0, ok := msgs[0].AsError()
	if !ok {
		t.Fatal("expected error message")
	}
	if err0.Code != "missing" || err0.Path != "$.buyer.email" {
		t.Errorf("unexpected message: %+v", err0)
	}
	if err0.Severity != model.MessageSeverityRecoverable {
		t.Errorf("expected recoverable severity, got %s", err0.Severity)
	}
}

func TestDeriveCheckoutMessages_MissingShippingAddress(t *testing.T) {
	co := &shopifyCheckout{
		Email:            "buyer@example.com",
		RequiresShipping: true,
		ShippingAddress:  nil,
	}
	msgs := m.deriveCheckoutMessages(co)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d: %+v", len(msgs), msgs)
	}
	err0, ok := msgs[0].AsError()
	if !ok {
		t.Fatal("expected error message")
	}
	if err0.Path != "$.fulfillment.methods[0].selected_destination_id" {
		t.Errorf("unexpected path: %s", err0.Path)
	}
}

func TestDeriveCheckoutMessages_Clean(t *testing.T) {
	co := &shopifyCheckout{
		Email:            "buyer@example.com",
		RequiresShipping: true,
		ShippingAddress:  &shopifyMailingAddress{City: "Melbourne"},
		ShippingLine: &struct {
			Handle  string       `json:"handle"`
			Title   string       `json:"title"`
			PriceV2 shopifyMoney `json:"priceV2"`
		}{Handle: "standard"},
	}
	msgs := m.deriveCheckoutMessages(co)
	if len(msgs) != 0 {
		t.Errorf("expected no messages, got %d: %+v", len(msgs), msgs)
	}
}

// ─── checkoutToUCPResponse totals ─────────────────────────────────────────────

func TestCheckoutToUCPResponse_Totals(t *testing.T) {
	cfg := MerchantConfig{
		MerchantID:   "m1",
		StoreBaseURL: "https://store.example.com",
	}
	co := &shopifyCheckout{
		ID:              "gid://shopify/Checkout/abc",
		Email:           "buyer@example.com",
		CurrencyCode:    "AUD",
		SubtotalPriceV2: shopifyMoney{Amount: "100.00", CurrencyCode: "AUD"},
		TotalTaxV2:      &shopifyMoney{Amount: "10.00", CurrencyCode: "AUD"},
		TotalPriceV2:    shopifyMoney{Amount: "110.00", CurrencyCode: "AUD"},
		ShippingLine: &struct {
			Handle  string       `json:"handle"`
			Title   string       `json:"title"`
			PriceV2 shopifyMoney `json:"priceV2"`
		}{Handle: "h1", Title: "Standard", PriceV2: shopifyMoney{Amount: "0.00"}},
	}

	resp := m.checkoutToUCPResponse(co, cfg)

	totalsByType := make(map[string]int64)
	for _, tot := range resp.Totals {
		totalsByType[string(tot.Type)] = int64(tot.Amount)
	}

	if totalsByType["subtotal"] != 10000 {
		t.Errorf("subtotal: got %d, want 10000", totalsByType["subtotal"])
	}
	if totalsByType["tax"] != 1000 {
		t.Errorf("tax: got %d, want 1000", totalsByType["tax"])
	}
	if totalsByType["total"] != 11000 {
		t.Errorf("total: got %d, want 11000", totalsByType["total"])
	}
}

// ─── normaliseCheckoutErrorCode ───────────────────────────────────────────────

func TestNormaliseCheckoutErrorCode(t *testing.T) {
	cases := []struct{ in, want string }{
		{"NOT_ENOUGH_IN_STOCK", "out_of_stock"},
		{"PRODUCT_UNAVAILABLE", "item_unavailable"},
		{"PAYMENT_ERROR", "payment_failed"},
		{"INVALID", "invalid"},
		{"SOME_NEW_CODE", "some_new_code"},
	}
	for _, tc := range cases {
		got := m.normaliseCheckoutErrorCode(tc.in)
		if got != tc.want {
			t.Errorf("normaliseCheckoutErrorCode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ─── ucpAddressToShopifyMailingInput ─────────────────────────────────────────

func TestUCPAddressToShopifyMailingInput(t *testing.T) {
	addr := &model.PostalAddress{
		FirstName:       "Jane",
		LastName:        "Doe",
		StreetAddress:   "123 Main St",
		ExtendedAddress: "Suite 4",
		AddressLocality: "Sydney",
		AddressRegion:   "NSW",
		PostalCode:      "2000",
		AddressCountry:  "AU",
		PhoneNumber:     "+61400000000",
	}

	got := m.ucpAddressToShopifyMailingInput(addr)

	if got["firstName"] != "Jane" {
		t.Errorf("firstName: %v", got["firstName"])
	}
	if got["city"] != "Sydney" {
		t.Errorf("city: %v", got["city"])
	}
	if got["country"] != "AU" {
		t.Errorf("country: %v", got["country"])
	}
	if got["zip"] != "2000" {
		t.Errorf("zip: %v", got["zip"])
	}
}

func TestUCPAddressToShopifyMailingInput_Nil(t *testing.T) {
	got := m.ucpAddressToShopifyMailingInput(nil)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// ─── hasUnrecoverable ─────────────────────────────────────────────────────────

func TestHasUnrecoverable(t *testing.T) {
	cases := []struct {
		msgs []model.Message
		want bool
	}{
		{[]model.Message{model.NewErrorMessage(model.MessageError{Severity: model.MessageSeverityRecoverable})}, false},
		{[]model.Message{model.NewErrorMessage(model.MessageError{Severity: model.MessageSeverityUnrecoverable})}, true},
		{[]model.Message{
			model.NewErrorMessage(model.MessageError{Severity: model.MessageSeverityRecoverable}),
			model.NewErrorMessage(model.MessageError{Severity: model.MessageSeverityUnrecoverable}),
		}, true},
		{nil, false},
	}
	for _, tc := range cases {
		got := hasUnrecoverable(tc.msgs)
		if got != tc.want {
			t.Errorf("hasUnrecoverable: got %v, want %v", got, tc.want)
		}
	}
}

// ─── normaliseOrderStatus ─────────────────────────────────────────────────────

func TestNormaliseOrderStatus(t *testing.T) {
	cases := []struct {
		financial, fulfillment, want string
	}{
		{"paid", "fulfilled", "fulfilled"},
		{"paid", "partial", "partially_fulfilled"},
		{"paid", "", "processing"},
		{"pending", "", "pending"},
		{"", "", "unknown"},
	}
	for _, tc := range cases {
		got := m.normaliseOrderStatus(tc.financial, tc.fulfillment)
		if got != tc.want {
			t.Errorf("normaliseOrderStatus(%q, %q) = %q, want %q",
				tc.financial, tc.fulfillment, got, tc.want)
		}
	}
}

// ─── fieldPathToJSONPath ──────────────────────────────────────────────────────

func TestFieldPathToJSONPath(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"email"}, "$.email"},
		{[]string{"buyer", "email"}, "$.buyer.email"},
		{nil, ""},
		{[]string{}, ""},
	}
	for _, tc := range cases {
		got := m.fieldPathToJSONPath(tc.in)
		if got != tc.want {
			t.Errorf("fieldPathToJSONPath(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestGidToNumericID(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"gid://shopify/Order/1234567890", "1234567890"},
		{"gid://shopify/Order/order-17", "order-17"},
		{"gid://shopify/Product/987", "987"},
		{"1234567890", "1234567890"}, // already numeric — pass through
		{"", ""},
	}
	for _, tc := range cases {
		got := gidToNumericID(tc.input)
		if got != tc.want {
			t.Errorf("gidToNumericID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
