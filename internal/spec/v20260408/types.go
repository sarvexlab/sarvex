// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package v20260408 provides typed Go representations of every schema on
// the UCP Schema Reference for spec version 2026-04-08
// (https://ucp.dev/2026-04-08/specification/reference/). Types are
// hand-written against the JSON Schema sources in
// github.com/Universal-Commerce-Protocol/ucp at the release/2026-04-08
// branch.
//
// This package has no dependency on anything outside the standard
// library: it sits below ucpsig/ucpnego, both of which may import it.
package v20260408

import (
	"fmt"
	"regexp"
	"time"
)

// Amount is a monetary amount in the currency's minor unit as defined by
// ISO 4217 (e.g. cents for USD). Refer to the currency's exponent to
// determine the minor-to-major ratio (2 for USD, 0 for JPY, 3 for KWD).
// Amount is always non-negative; see SignedAmount for values that carry a
// sign (discounts, refunds).
type Amount int64

// SignedAmount is a monetary amount in the currency's minor unit as
// defined by ISO 4217. Unlike Amount, SignedAmount may be negative — the
// sign is intrinsic to the value (e.g. discounts and refunds are
// negative, charges are positive).
type SignedAmount int64

// ReverseDomainName is a collision-safe identifier used to namespace
// capabilities, services, handlers, eligibility claims, and
// extension-contributed keys (e.g. "dev.ucp.shopping.checkout",
// "com.example.loyalty_gold").
type ReverseDomainName string

var reverseDomainNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9_]*)+$`)

// Validate reports whether n matches the reverse-domain-name pattern
// (at least two dot-separated segments, lowercase alphanumeric with
// underscores).
func (n ReverseDomainName) Validate() error {
	if !reverseDomainNamePattern.MatchString(string(n)) {
		return fmt.Errorf("ucp: invalid reverse domain name %q", string(n))
	}
	return nil
}

// AccountInfo carries non-sensitive backend identifiers for linking a
// payment instrument across tokens (Payment Account Info in the spec).
type AccountInfo struct {
	// PaymentAccountReference is the EMVCo PAR: a unique identifier
	// linking a payment card to a specific account, enabling tracking
	// across tokens (Apple Pay, physical card, etc).
	PaymentAccountReference string `json:"payment_account_reference,omitempty"`
}

// Attribution carries platform-emitted referral and conversion-event
// context (campaign identifiers, click IDs, source/medium markers). Keys
// and values mirror what platforms would send as URL query parameters in
// browser-based flows; numeric or boolean values are string-encoded.
type Attribution map[string]string

// Buyer represents the buyer placing an order.
type Buyer struct {
	FirstName   string `json:"first_name,omitempty"`
	LastName    string `json:"last_name,omitempty"`
	Email       string `json:"email,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"` // E.164.

	// Extra carries any additional buyer fields not modeled above; the
	// schema allows additional properties.
	Extra map[string]any `json:"-"`
}

// Category is a product category with an optional taxonomy identifier.
type Category struct {
	// Value is the category value or path (e.g. "Apparel > Shirts", "1604").
	Value string `json:"value"`
	// Taxonomy is the source taxonomy. Well-known values: google_product_category, shopify, merchant.
	Taxonomy string `json:"taxonomy,omitempty"`
}

// Context carries provisional buyer signals for relevance and
// localization. These are not authoritative: eligibility and policy
// enforcement MUST occur at checkout time using binding transaction data.
type Context struct {
	AddressCountry string `json:"address_country,omitempty"`
	AddressRegion  string `json:"address_region,omitempty"`
	PostalCode     string `json:"postal_code,omitempty"`
	Intent         string `json:"intent,omitempty"`
	// Language is an IETF BCP 47 language tag (e.g. "en", "fr-CA", "zh-Hans").
	Language string `json:"language,omitempty"`
	// Currency is an ISO 4217 currency hint (e.g. "EUR", "USD").
	Currency string `json:"currency,omitempty"`
	// Eligibility is a set of buyer claims about eligible benefits
	// (loyalty membership, payment instrument perks, etc). Values MUST be
	// non-identifying reverse-domain names.
	Eligibility []ReverseDomainName `json:"eligibility,omitempty"`

	// Extra carries any additional context fields not modeled above; the
	// schema allows additional properties.
	Extra map[string]any `json:"-"`
}

// Description holds descriptive content in one or more formats. At least
// one of Plain, HTML, or Markdown must be present per the spec.
type Description struct {
	Plain    string `json:"plain,omitempty"`
	HTML     string `json:"html,omitempty"`
	Markdown string `json:"markdown,omitempty"`
}

// Validate reports an error if none of Plain, HTML, or Markdown is set.
func (d Description) Validate() error {
	if d.Plain == "" && d.HTML == "" && d.Markdown == "" {
		return fmt.Errorf("ucp: description requires at least one of plain, html, or markdown")
	}
	return nil
}

// OptionValue is a selectable value for a ProductOption.
type OptionValue struct {
	// ID is an optional server-assigned identifier. When present in a
	// SelectedOption, the server SHOULD use it for matching instead of Label.
	ID string `json:"id,omitempty"`
	// Label is the display text for this option value (e.g. "Small", "Blue").
	Label string `json:"label"`
}

// DetailOptionValue is an OptionValue with availability signals relative
// to the current selections, used in get_product responses.
type DetailOptionValue struct {
	OptionValue
	// Available reports whether a variant matching this value and the
	// current option selections is purchasable. A pointer distinguishes
	// "not stated" from an explicit false.
	Available *bool `json:"available,omitempty"`
	// Exists reports whether a variant matching this value and the
	// current option selections exists in the catalog.
	Exists *bool `json:"exists,omitempty"`
}

// ErrorCodeExamples documents the well-known ErrorCode values; see the
// ErrorCode type in enums.go for the typed constants.

// ErrorResponse is a generic error response used when no valid resource
// can be established for a request.
type ErrorResponse struct {
	UCP ErrorResponseMetadata `json:"ucp"`
	// Messages describes why the operation failed. Must contain at least
	// one entry.
	Messages []Message `json:"messages"`
	// ContinueURL is a URL for buyer handoff or session recovery.
	ContinueURL string `json:"continue_url,omitempty"`
}

// Validate reports an error if Messages is empty, as required by the spec.
func (r ErrorResponse) Validate() error {
	if len(r.Messages) == 0 {
		return fmt.Errorf("ucp: error response requires at least one message")
	}
	return nil
}

// ExpectationLineItem references a line item and the quantity it
// contributes to an Expectation, Adjustment, or FulfillmentEvent.
type ExpectationLineItem struct {
	ID       string `json:"id"`
	Quantity int    `json:"quantity"`
}

// Expectation is a buyer-facing fulfillment grouping (e.g. "package")
// representing logical groupings of items. Expectations can be split,
// merged, or adjusted post-order to set buyer expectations for when/how
// items arrive.
type Expectation struct {
	ID          string                `json:"id"`
	LineItems   []ExpectationLineItem `json:"line_items"`
	MethodType  ExpectationMethodType `json:"method_type"`
	Destination PostalAddress         `json:"destination"`
	Description string                `json:"description,omitempty"`
	// FulfillableOn is "now" or an ISO 8601 timestamp for a future date
	// (backorder, pre-order).
	FulfillableOn string `json:"fulfillable_on,omitempty"`
}

// Time parses FulfillableOn as an RFC 3339 timestamp. It returns an error
// if FulfillableOn is the literal "now" or otherwise not a timestamp —
// callers must check for "now" themselves before calling Time.
func (e Expectation) Time() (time.Time, error) {
	return time.Parse(time.RFC3339, e.FulfillableOn)
}

// InputCorrelation maps a request identifier to the variant it resolved
// to, with match semantics.
type InputCorrelation struct {
	// ID is the identifier from the lookup request that resolved to this variant.
	ID string `json:"id"`
	// Match describes how the request identifier resolved to this
	// variant. See InputCorrelationMatch for well-known values.
	Match InputCorrelationMatch `json:"match,omitempty"`
}

// Item identifies the product being purchased in a LineItem.
type Item struct {
	// ID is the product identifier, often the SKU, required to resolve
	// product details. Recognized by both platform and business.
	ID string `json:"id"`
	// Title is the product title.
	Title string `json:"title,omitempty"`
	// Price is the unit price in ISO 4217 minor units. A pointer
	// distinguishes an unset price from an explicit free (zero) price.
	Price *Amount `json:"price,omitempty"`
	// ImageURL is the product image URI.
	ImageURL string `json:"image_url,omitempty"`
}

// LineItem is a line item in a Cart or Checkout, expected to use the
// currency of its parent object.
type LineItem struct {
	ID       string `json:"id,omitempty"`
	Item     Item   `json:"item"`
	Quantity int    `json:"quantity"`
	Totals   Totals `json:"totals,omitempty"`
	// ParentID references a parent line item for nested structures (e.g.
	// bundles).
	ParentID string `json:"parent_id,omitempty"`
}

// Link is a merchant-provided link (policies, FAQs, etc) to be displayed
// by the platform.
type Link struct {
	// Type categorizes the link. See LinkType for well-known values.
	Type LinkType `json:"type"`
	URL  string   `json:"url"`
	// Title is optional display text; when provided, use it instead of
	// generating text from Type.
	Title string `json:"title,omitempty"`
}

// Media is a product media item (image, video, 3D model, etc).
type Media struct {
	// Type categorizes the media. See MediaType for well-known values.
	Type    MediaType `json:"type"`
	URL     string    `json:"url"`
	AltText string    `json:"alt_text,omitempty"`
	Width   int       `json:"width,omitempty"`
	Height  int       `json:"height,omitempty"`
}

// OrderConfirmation carries order details available at checkout
// completion time.
type OrderConfirmation struct {
	ID string `json:"id"`
	// Label is a human-readable order label. MUST only be provided by the business.
	Label string `json:"label,omitempty"`
	// PermalinkURL is a permalink to access the order on the merchant site.
	PermalinkURL string `json:"permalink_url"`
}

// OrderLineItemQuantity tracks quantity over the lifetime of an
// OrderLineItem.
type OrderLineItemQuantity struct {
	// Original is the quantity from the original checkout.
	Original *int `json:"original,omitempty"`
	// Total is the current total active quantity; may differ from
	// Original due to post-order modifications (returns, cancellations).
	Total int `json:"total"`
	// Fulfilled is the quantity fulfilled so far.
	Fulfilled int `json:"fulfilled"`
}

// OrderLineItem is a line item on an Order.
type OrderLineItem struct {
	ID       string                `json:"id"`
	Item     Item                  `json:"item"`
	Quantity OrderLineItemQuantity `json:"quantity"`
	Totals   Totals                `json:"totals"`
	// Status is derived: removed if Quantity.Total == 0, fulfilled if
	// Quantity.Total > 0 and Quantity.Fulfilled == Quantity.Total,
	// partial if Quantity.Total > 0 and Quantity.Fulfilled > 0,
	// otherwise processing.
	Status   OrderLineItemStatus `json:"status"`
	ParentID string              `json:"parent_id,omitempty"`
}

// PaginationRequest carries cursor-based pagination parameters for list
// requests.
type PaginationRequest struct {
	// Cursor is an opaque cursor from a previous response.
	Cursor string `json:"cursor,omitempty"`
	// Limit is the requested page size. Implementations MAY clamp to a
	// lower maximum. A pointer distinguishes "unset" (server default,
	// documented as 10) from an explicit value.
	Limit *int `json:"limit,omitempty"`
}

// PaginationResponse carries cursor-based pagination state in list
// responses.
type PaginationResponse struct {
	// Cursor fetches the next page. MUST be present when HasNextPage is true.
	Cursor      string `json:"cursor,omitempty"`
	HasNextPage bool   `json:"has_next_page"`
	// TotalCount is the total number of matching items, if available.
	TotalCount *int `json:"total_count,omitempty"`
}

// Validate reports an error if HasNextPage is true but Cursor is empty,
// as required by the spec.
func (p PaginationResponse) Validate() error {
	if p.HasNextPage && p.Cursor == "" {
		return fmt.Errorf("ucp: pagination response requires cursor when has_next_page is true")
	}
	return nil
}

// PostalAddress is a physical mailing address.
type PostalAddress struct {
	ExtendedAddress string `json:"extended_address,omitempty"`
	StreetAddress   string `json:"street_address,omitempty"`
	AddressLocality string `json:"address_locality,omitempty"`
	// AddressRegion is required for applicable countries (state in US,
	// province in CA).
	AddressRegion string `json:"address_region,omitempty"`
	// AddressCountry is recommended as ISO 3166-1 alpha-2 (e.g. "US");
	// alpha-3 or full country names are accepted for backward compatibility.
	AddressCountry string `json:"address_country,omitempty"`
	PostalCode     string `json:"postal_code,omitempty"`
	FirstName      string `json:"first_name,omitempty"`
	LastName       string `json:"last_name,omitempty"`
	PhoneNumber    string `json:"phone_number,omitempty"`
}

// Price is a monetary amount with explicit currency.
type Price struct {
	// Amount is in ISO 4217 minor units. Use 0 for free items.
	Amount   Amount `json:"amount"`
	Currency string `json:"currency"`
}

// PriceFilter is a price range filter denominated in Context.Currency.
type PriceFilter struct {
	Min *Amount `json:"min,omitempty"`
	Max *Amount `json:"max,omitempty"`
}

// PriceRange represents a minimum and maximum Price, e.g. across product
// variants.
type PriceRange struct {
	Min Price `json:"min"`
	Max Price `json:"max"`
}

// ProductOption is a product option such as size, color, or material.
type ProductOption struct {
	Name   string        `json:"name"`
	Values []OptionValue `json:"values"`
}

// Rating is an aggregate product or variant rating.
type Rating struct {
	Value float64 `json:"value"`
	// ScaleMin is the minimum value on the rating scale. Defaults to 1
	// when absent; a pointer distinguishes absence from an explicit 0.
	ScaleMin *float64 `json:"scale_min,omitempty"`
	ScaleMax float64  `json:"scale_max"`
	// Count is the number of reviews contributing to the rating. A
	// pointer distinguishes absence from an explicit 0.
	Count *int `json:"count,omitempty"`
}

// RetailLocation is a pickup location (retail store, locker, etc). It is
// one of the two variants of FulfillmentDestination.
type RetailLocation struct {
	ID      string         `json:"id,omitempty"`
	Name    string         `json:"name"`
	Address *PostalAddress `json:"address,omitempty"`

	// Extra carries any additional fields not modeled above; the schema
	// allows additional properties.
	Extra map[string]any `json:"-"`
}

// SearchFilters narrows catalog search results. All specified filters
// combine with AND logic.
type SearchFilters struct {
	// Categories filters by product category (OR logic within this field).
	Categories []string     `json:"categories,omitempty"`
	Price      *PriceFilter `json:"price,omitempty"`

	// Extra carries any additional filter fields not modeled above; the
	// schema allows additional properties.
	Extra map[string]any `json:"-"`
}

// SelectedOption is a specific option selection on a variant (e.g. Size: Large).
type SelectedOption struct {
	Name string `json:"name"`
	// ID is an optional option_value.id; when present the server SHOULD
	// use it for matching.
	ID    string `json:"id,omitempty"`
	Label string `json:"label"`
}

// ShippingDestination is a shipping address, one of the two variants of
// FulfillmentDestination. It embeds PostalAddress and requires an ID.
type ShippingDestination struct {
	PostalAddress
	// ID identifies this shipping destination.
	ID string `json:"id"`
}

// Signals carries environment data provided by the platform to support
// authorization and abuse prevention. Values MUST NOT be buyer-asserted
// claims. All keys use reverse-domain naming; unknown keys are preserved
// via Extra (see json.go for the custom (Un)MarshalJSON implementation).
type Signals struct {
	// BuyerIP is dev.ucp.buyer_ip: the client's IP address (IPv4 or IPv6).
	BuyerIP string `json:"dev.ucp.buyer_ip,omitempty"`
	// UserAgent is dev.ucp.user_agent: the client's HTTP User-Agent header
	// or equivalent.
	UserAgent string `json:"dev.ucp.user_agent,omitempty"`
	// Extra carries any additional reverse-domain-namespaced signal keys.
	// (Un)MarshalJSON implementations live in json.go.
	Extra map[string]any `json:"-"`
}

// Total is a single cost breakdown entry with a category, amount, and
// optional display text and itemized sub-lines.
type Total struct {
	// Type categorizes the entry. See TotalType for well-known values.
	Type        TotalType    `json:"type"`
	DisplayText string       `json:"display_text,omitempty"`
	Amount      SignedAmount `json:"amount"`
	// Lines is an optional itemized breakdown. The parent entry is always
	// rendered; lines are supplementary. The sum of line amounts MUST
	// equal the parent entry's Amount.
	Lines []TotalLine `json:"lines,omitempty"`
}

// TotalLine is a itemized sub-line entry within a Total.
type TotalLine struct {
	DisplayText string       `json:"display_text"`
	Amount      SignedAmount `json:"amount"`
}

// Totals is a pricing breakdown provided by the business. It MUST contain
// exactly one subtotal and one total entry; detail types (tax, fee,
// discount, fulfillment) may repeat for itemization.
type Totals []Total

// Validate enforces the Totals invariant: exactly one entry of type
// "subtotal" and exactly one of type "total".
func (t Totals) Validate() error {
	var subtotals, totals int
	for _, entry := range t {
		switch entry.Type {
		case TotalTypeSubtotal:
			subtotals++
		case TotalTypeTotal:
			totals++
		}
	}
	if subtotals != 1 {
		return fmt.Errorf("ucp: totals must contain exactly one subtotal entry, got %d", subtotals)
	}
	if totals != 1 {
		return fmt.Errorf("ucp: totals must contain exactly one total entry, got %d", totals)
	}
	return nil
}

// VariantBarcode is an industry-standard product identifier.
type VariantBarcode struct {
	// Type is the barcode standard. See BarcodeType for well-known values.
	Type  BarcodeType `json:"type"`
	Value string      `json:"value"`
}

// VariantUnitPrice is price per standard unit of measurement (e.g. per
// 100ml, per 1kg).
type VariantUnitPrice struct {
	Amount    Amount                   `json:"amount"`
	Currency  string                   `json:"currency"`
	Measure   VariantUnitPriceQuantity `json:"measure"`
	Reference VariantUnitPriceQuantity `json:"reference"`
}

// VariantUnitPriceQuantity is a quantity with a unit of measurement, used
// by VariantUnitPrice's Measure and Reference fields.
type VariantUnitPriceQuantity struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// VariantAvailability describes whether a Variant can be purchased.
type VariantAvailability struct {
	Available bool `json:"available,omitempty"`
	// Status qualifies Available with fulfillment state. See
	// VariantAvailabilityStatus for well-known values.
	Status VariantAvailabilityStatus `json:"status,omitempty"`
}

// VariantSeller is optional seller context for a Variant.
type VariantSeller struct {
	Name  string `json:"name,omitempty"`
	Links []Link `json:"links,omitempty"`
}

// Variant is a purchasable variant of a Product with specific option
// selections.
type Variant struct {
	// ID is the Global ID (GID) uniquely identifying this variant. Used
	// as Item.ID in checkout.
	ID           string               `json:"id"`
	SKU          string               `json:"sku,omitempty"`
	Barcodes     []VariantBarcode     `json:"barcodes,omitempty"`
	Handle       string               `json:"handle,omitempty"`
	Title        string               `json:"title"`
	Description  Description          `json:"description"`
	URL          string               `json:"url,omitempty"`
	Categories   []Category           `json:"categories,omitempty"`
	Price        Price                `json:"price"`
	ListPrice    *Price               `json:"list_price,omitempty"`
	UnitPrice    *VariantUnitPrice    `json:"unit_price,omitempty"`
	Availability *VariantAvailability `json:"availability,omitempty"`
	Options      []SelectedOption     `json:"options,omitempty"`
	Media        []Media              `json:"media,omitempty"`
	Rating       *Rating              `json:"rating,omitempty"`
	Tags         []string             `json:"tags,omitempty"`
	Metadata     map[string]any       `json:"metadata,omitempty"`
	Seller       *VariantSeller       `json:"seller,omitempty"`
}

// LookupVariant is a Variant extended with the request-identifier
// correlation metadata returned by catalog lookup operations.
type LookupVariant struct {
	Variant
	// Inputs records which request identifiers resolved to this variant,
	// and how. Required, at least one entry.
	Inputs []InputCorrelation `json:"inputs"`
}

// Product is a product in the catalog with variants and options.
type Product struct {
	// ID is the Global ID (GID) uniquely identifying this product.
	ID string `json:"id"`
	// Handle is a URL-safe slug for SEO-friendly URLs. Use ID for stable
	// API references.
	Handle      string      `json:"handle,omitempty"`
	Title       string      `json:"title"`
	Description Description `json:"description"`
	URL         string      `json:"url,omitempty"`
	Categories  []Category  `json:"categories,omitempty"`
	PriceRange  PriceRange  `json:"price_range"`
	// ListPriceRange is the list price range before discounts (for
	// strikethrough display).
	ListPriceRange *PriceRange     `json:"list_price_range,omitempty"`
	Media          []Media         `json:"media,omitempty"`
	Options        []ProductOption `json:"options,omitempty"`
	// Variants are purchasable variants of this product. First item is
	// the featured variant for listings. At least one required.
	Variants []Variant      `json:"variants"`
	Rating   *Rating        `json:"rating,omitempty"`
	Tags     []string       `json:"tags,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// AdjustmentLineItem references a line item and the signed quantity an
// Adjustment affects.
type AdjustmentLineItem struct {
	ID string `json:"id"`
	// Quantity is signed: negative for reductions (returns), positive for
	// additions (exchanges).
	Quantity int `json:"quantity"`
}

// Adjustment is a post-order event that exists independently of
// fulfillment — typically a money movement, but can be any post-order
// change. Polymorphic; can optionally reference line items.
type Adjustment struct {
	ID string `json:"id"`
	// Type is an open string. See AdjustmentType for well-known values.
	Type       AdjustmentType       `json:"type"`
	OccurredAt string               `json:"occurred_at"`
	Status     AdjustmentStatus     `json:"status"`
	LineItems  []AdjustmentLineItem `json:"line_items,omitempty"`
	// Totals carries signed values: negative for money returned to the
	// buyer (refunds, credits), positive for additional charges (exchanges).
	Totals      Totals `json:"totals,omitempty"`
	Description string `json:"description,omitempty"`
}

// Time parses OccurredAt as an RFC 3339 timestamp.
func (a Adjustment) Time() (time.Time, error) {
	return time.Parse(time.RFC3339, a.OccurredAt)
}
