// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package woocommerce

// REST API v3 response types for WooCommerce.

// wcProduct represents a WooCommerce product from REST API v3.
type wcProduct struct {
	ID               int                  `json:"id"`
	Name             string               `json:"name"`
	Slug             string               `json:"slug"`
	SKU              string               `json:"sku"`
	Description      string               `json:"description"`
	Price            string               `json:"price"` // String representation like "29.99"
	Images           []wcImage            `json:"images"`
	Variations       []int                `json:"variations"` // Variant IDs
	InStock          bool                 `json:"in_stock"`
	StockStatus      string               `json:"stock_status"`   // "instock" or "outofstock"
	StockQuantity    *int                 `json:"stock_quantity"` // null when manage_stock is false
	ManageStock      bool                 `json:"manage_stock"`
	RegularPrice     string               `json:"regular_price"`
	SalePrice        string               `json:"sale_price"`
	ShortDescription string               `json:"short_description"`
	Permalink        string               `json:"permalink"`
	Type             string               `json:"type"` // "simple", "variable", etc.
	Weight           string               `json:"weight"`
	Dimensions       wcDims               `json:"dimensions"`
	Categories       []wcTerm             `json:"categories"`
	Attributes       []wcProductAttribute `json:"attributes"`
}

// wcDims is the product's package dimensions, as strings (WooCommerce serves
// "" for an unset dimension).
type wcDims struct {
	Length string `json:"length"`
	Width  string `json:"width"`
	Height string `json:"height"`
}

// wcTerm is a category (or tag) reference.
type wcTerm struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// wcProductAttribute is one product-level attribute (colour, size, ...) with
// its option values, distinct from wcAttribute (a variation's chosen option).
type wcProductAttribute struct {
	Name    string   `json:"name"`
	Options []string `json:"options"`
}

type wcImage struct {
	ID   int    `json:"id"`
	Src  string `json:"src"`
	Name string `json:"name"`
	Alt  string `json:"alt"`
}

// wcVariation represents a product variation from REST API v3.
type wcVariation struct {
	ID         int           `json:"id"`
	SKU        string        `json:"sku"`
	Price      string        `json:"price"`
	InStock    bool          `json:"in_stock"`
	Quantity   int           `json:"stock_quantity"`
	Attributes []wcAttribute `json:"attributes"`
	Images     []wcImage     `json:"images"`
}

type wcAttribute struct {
	Name   string `json:"name"`
	Option string `json:"option"`
}

// wcOrder represents a WooCommerce order from REST API v3.
type wcOrder struct {
	ID              int              `json:"id"`
	OrderNumber     int              `json:"order_number"`
	Status          string           `json:"status"`
	Currency        string           `json:"currency"`
	DateCreated     string           `json:"date_created"`
	Total           string           `json:"total"`
	Subtotal        string           `json:"subtotal"`
	ShippingTotal   string           `json:"shipping_total"`
	TaxTotal        string           `json:"tax_total"`
	DiscountTotal   string           `json:"discount_total"`
	LineItems       []wcLineItem     `json:"line_items"`
	ShippingLines   []wcShippingLine `json:"shipping_lines"`
	CouponLines     []wcCouponLine   `json:"coupon_lines,omitempty"`
	BillingAddress  wcAddress        `json:"billing"`
	ShippingAddress wcAddress        `json:"shipping"`
	// MetaData is WooCommerce's standard order meta_data; Sarvex keeps the
	// buyer_consent extension's state under metaKeyBuyerConsent.
	MetaData []wcMetaData `json:"meta_data,omitempty"`
	// AvailableShippingRates carries the rates the store can fulfil the
	// current destination with. Not a stock WooCommerce order field — the sim
	// store (internal/planes/sync/sim) surfaces it here so checkout can offer
	// fulfillment options; a real deployment sources the same information from
	// the store's shipping configuration.
	AvailableShippingRates []wcShippingRate `json:"available_shipping_rates,omitempty"`
}

// wcMetaData is one WooCommerce order meta_data entry.
type wcMetaData struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// wcShippingRate is one offerable shipping rate (see wcOrder.AvailableShippingRates).
type wcShippingRate struct {
	MethodID    string `json:"method_id"`
	MethodTitle string `json:"method_title"`
	Total       string `json:"total"`
}

// wcCouponLine is one applied discount code and its attributed reduction.
type wcCouponLine struct {
	Code     string `json:"code"`
	Discount string `json:"discount"`
}

type wcLineItem struct {
	ID          int    `json:"id"`
	ProductID   int    `json:"product_id"`
	VariationID int    `json:"variation_id,omitempty"`
	Quantity    int    `json:"quantity"`
	Price       string `json:"price"`
	Name        string `json:"name"`
	SKU         string `json:"sku"`
	Total       string `json:"total"`
}

type wcShippingLine struct {
	ID          int     `json:"id"`
	MethodID    string  `json:"method_id"`
	MethodTitle string  `json:"method_title"`
	Total       string  `json:"total"`
	Taxes       []wcTax `json:"taxes"`
}

type wcTax struct {
	ID       int    `json:"id"`
	Total    string `json:"total"`
	Subtotal string `json:"subtotal"`
}

type wcAddress struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Company   string `json:"company"`
	Address1  string `json:"address_1"`
	Address2  string `json:"address_2"`
	City      string `json:"city"`
	State     string `json:"state"`
	Postcode  string `json:"postcode"`
	Country   string `json:"country"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
}
