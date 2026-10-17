// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package magento

// REST API types for Magento 2.x

// mgProduct represents a product from Magento REST API.
type mgProduct struct {
	ID                  int                    `json:"id"`
	SKU                 string                 `json:"sku"`
	Name                string                 `json:"name"`
	Price               float64                `json:"price"`
	TypeID              string                 `json:"type_id"` // "simple", "configurable", etc.
	Visibility          int                    `json:"visibility"`
	Status              int                    `json:"status"` // 1 = enabled
	Weight              float64                `json:"weight,omitempty"`
	CustomAttributes    []mgCustomAttribute    `json:"custom_attributes,omitempty"`
	ExtensionAttributes *mgExtensionAttributes `json:"extension_attributes,omitempty"`
	MediaGalleryEntries []mgMediaGalleryEntry  `json:"media_gallery_entries,omitempty"`
}

// mgExtensionAttributes holds extension attribute data for products.
type mgExtensionAttributes struct {
	StockItem *mgStockItem `json:"stock_item,omitempty"`
}

// mgStockItem represents inventory information.
type mgStockItem struct {
	IsInStock bool    `json:"is_in_stock"`
	Qty       float64 `json:"qty"`
}

// mgCustomAttribute represents a product custom attribute (name, description, etc.).
type mgCustomAttribute struct {
	AttributeCode string      `json:"attribute_code"`
	Value         interface{} `json:"value"`
}

// mgMediaGalleryEntry represents a product image.
type mgMediaGalleryEntry struct {
	ID        int    `json:"id"`
	MediaType string `json:"media_type"` // "image", "video", etc.
	URL       string `json:"file"`       // Relative path; construct absolute URL from BaseURL
	Label     string `json:"label,omitempty"`
}

// mgCart represents a guest cart from Magento REST API.
type mgCart struct {
	ID    string       `json:"id"` // Quote id; the masked id a cart is addressed by is not echoed here
	Items []mgCartItem `json:"items,omitempty"`
	// Totals is not part of Magento's quote resource: it is read from the
	// separate /totals endpoint (see Connector.loadCart). Stores that embed it
	// in the cart body are still honoured.
	Totals         *mgCartTotals         `json:"totals,omitempty"`
	BillingAddress *mgAddress            `json:"billing_address,omitempty"`
	Customer       *mgCartCustomer       `json:"customer,omitempty"`
	ExtensionAttrs *mgCartExtensionAttrs `json:"extension_attributes,omitempty"`
}

// mgCartCustomer is the quote's customer block; for a guest it carries the
// email once one has been recorded.
type mgCartCustomer struct {
	Email string `json:"email,omitempty"`
}

// mgCartExtensionAttrs carries the quote's shipping assignment: the chosen
// shipping address and method.
type mgCartExtensionAttrs struct {
	ShippingAssignments []mgShippingAssignment `json:"shipping_assignments,omitempty"`
}

// mgCartItem represents a line item in a cart.
type mgCartItem struct {
	ItemID    int           `json:"item_id"`
	SKU       string        `json:"sku"`
	Qty       float64       `json:"qty"`
	Name      string        `json:"name"`
	Price     float64       `json:"price"`
	ProductID int           `json:"product_id"`
	Totals    *mgCartTotals `json:"totals,omitempty"`
}

// mgCartTotals represents totals (subtotal, tax, shipping, etc.) in a cart.
type mgCartTotals struct {
	GrandTotal                    float64                  `json:"grand_total"`
	Subtotal                      float64                  `json:"subtotal"`
	SubtotalWithTax               float64                  `json:"subtotal_with_tax"`
	TaxAmount                     float64                  `json:"tax_amount"`
	ShippingAmount                float64                  `json:"shipping_amount"`
	ShippingInclTax               float64                  `json:"shipping_incl_tax"`
	DiscountAmount                float64                  `json:"discount_amount"`
	DiscountTaxCompensationAmount float64                  `json:"discount_tax_compensation_amount"`
	BaseCurrencyCode              string                   `json:"base_currency_code"`
	QuoteCurrencyCode             string                   `json:"quote_currency_code"`
	CouponCode                    string                   `json:"coupon_code,omitempty"`
	Segments                      []map[string]interface{} `json:"segments,omitempty"`
}

// mgAddress represents a billing or shipping address.
type mgAddress struct {
	ID               int                 `json:"id,omitempty"`
	FirstName        string              `json:"firstname"`
	LastName         string              `json:"lastname"`
	Company          string              `json:"company,omitempty"`
	Street           []string            `json:"street"` // Array: [0] = street, [1] = ext address
	City             string              `json:"city"`
	Region           string              `json:"region"` // State/province
	RegionCode       string              `json:"region_code,omitempty"`
	PostalCode       string              `json:"postcode"`
	Country          string              `json:"country_id"` // ISO 3166-1 alpha-2
	Email            string              `json:"email,omitempty"`
	Telephone        string              `json:"telephone,omitempty"`
	CustomAttributes []mgCustomAttribute `json:"custom_attributes,omitempty"`
	// ExtensionAttributes carries the in-store-pickup location code on a
	// pickup shipping address.
	ExtensionAttributes *mgAddressExtension `json:"extension_attributes,omitempty"`
}

// mgAddressExtension holds an address's extension attributes; only the MSI
// pickup location is used.
type mgAddressExtension struct {
	PickupLocationCode string `json:"pickup_location_code,omitempty"`
}

// mgShippingMethod represents an available shipping method.
type mgShippingMethod struct {
	CarrierCode string  `json:"carrier_code"`
	MethodCode  string  `json:"method_code"`
	MethodTitle string  `json:"method_title"`
	Amount      float64 `json:"amount"`
	BaseAmount  float64 `json:"base_amount,omitempty"`
}

// mgOrder represents an order from Magento REST API.
type mgOrder struct {
	EntityID            int                         `json:"entity_id"`
	IncrementID         string                      `json:"increment_id"` // Order number
	Status              string                      `json:"status"`
	State               string                      `json:"state"` // Internal state
	Currency            string                      `json:"order_currency_code"`
	CreatedAt           string                      `json:"created_at"` // ISO 8601 datetime
	UpdatedAt           string                      `json:"updated_at"`
	GrandTotal          float64                     `json:"grand_total"`
	Subtotal            float64                     `json:"subtotal"`
	TaxAmount           float64                     `json:"tax_amount"`
	ShippingAmount      float64                     `json:"shipping_amount"`
	DiscountAmount      float64                     `json:"discount_amount"`
	CouponCode          string                      `json:"coupon_code,omitempty"`
	Items               []mgOrderItem               `json:"items,omitempty"`
	BillingAddress      *mgAddress                  `json:"billing_address,omitempty"`
	ShippingAddress     *mgAddress                  `json:"shipping_address,omitempty"`
	ExtensionAttributes *mgOrderExtensionAttributes `json:"extension_attributes,omitempty"`
}

// mgOrderItem represents a line item in an order.
type mgOrderItem struct {
	ItemID    int     `json:"item_id"`
	SKU       string  `json:"sku"`
	ProductID int     `json:"product_id"`
	Name      string  `json:"name"`
	Qty       float64 `json:"qty_ordered"`
	Price     float64 `json:"price"`
	RowTotal  float64 `json:"row_total"`
}

// mgOrderExtensionAttributes holds extension attributes for orders.
type mgOrderExtensionAttributes struct {
	ShippingAssignments []mgShippingAssignment `json:"shipping_assignments,omitempty"`
}

// mgShippingAssignment represents shipping information in an order.
type mgShippingAssignment struct {
	Shipping *mgShippingInfo `json:"shipping,omitempty"`
	Items    []mgOrderItem   `json:"items,omitempty"`
}

// mgShippingInfo is the selected shipping address and method of a quote or
// order ("carrier_method", e.g. "flatrate_flatrate").
type mgShippingInfo struct {
	Address *mgAddress `json:"address,omitempty"`
	Method  string     `json:"method,omitempty"`
}

// mgSource is an MSI inventory source. A source flagged as a pickup location
// is where a buyer can collect an order in store.
type mgSource struct {
	SourceCode string `json:"source_code"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Country    string `json:"country_id"`
	Region     string `json:"region,omitempty"`
	City       string `json:"city"`
	Street     string `json:"street"`
	Postcode   string `json:"postcode"`
	Phone      string `json:"phone,omitempty"`

	ExtensionAttributes *mgSourceExtension `json:"extension_attributes,omitempty"`
}

// mgSourceExtension holds the in-store-pickup flags MSI adds to a source.
type mgSourceExtension struct {
	IsPickupLocationActive bool   `json:"is_pickup_location_active"`
	FrontendName           string `json:"frontend_name,omitempty"`
}

// mgSourceList is the response of GET /inventory/sources.
type mgSourceList struct {
	Items []mgSource `json:"items"`
}

// mgSearchResponse represents a product search response with search criteria.
type mgSearchResponse struct {
	Items          []mgProduct            `json:"items"`
	SearchCriteria map[string]interface{} `json:"search_criteria,omitempty"`
	TotalCount     int                    `json:"total_count"`
}
