// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package v20260825 provides typed Go representations of every schema on
// the UCP Schema Reference for spec version 2026-08-25
// (https://ucp.dev/2026-08-25/specification/reference/). Types are
// hand-written against the JSON Schema sources in
// github.com/Universal-Commerce-Protocol/ucp at the release/2026-08-25
// branch, commit cd78fb3.
//
// This package has no dependency on anything outside the standard
// library, and no dependency on any other package in this module —
// including the sibling v20260408 package, which it does not import and
// is not compatible with on the wire. See DISCREPANCIES.md for the
// modeling decisions taken while porting the schema, in particular the
// breaking changes from v20260408: the payment credential split (card
// deprecated in favor of pan/network_token), buyer consent's move from
// booleans to a reverse-DNS-keyed map, the fulfillment destination
// discriminator, and the profile document's signing_keys -> keys rename.
package v20260825

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
// "com.example.loyalty_gold"). The pattern is looser than v20260408's:
// segments after the first may start with a digit and carry interior
// hyphens (e.g. "com.example-shop.checkout", "com.2example.cart"), to
// support domain- and identifier-derived segments; only the first
// (reversed top-level domain) segment is restricted to letters, digits,
// and interior hyphens (for punycode TLDs such as "xn--p1ai").
type ReverseDomainName string

var reverseDomainNamePattern = regexp.MustCompile(`^[a-z](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9_-]*[a-z0-9_])?)+$`)

// Validate reports whether n matches the reverse-domain-name pattern
// (at least two dot-separated segments).
func (n ReverseDomainName) Validate() error {
	if !reverseDomainNamePattern.MatchString(string(n)) {
		return fmt.Errorf("ucp: invalid reverse domain name %q", string(n))
	}
	return nil
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

// ContextPaymentPreference names a buyer-preferred payment handler, and
// optionally the preferred instrument types within it, in priority order.
// Context.Payment carries a list of these, most preferred first.
type ContextPaymentPreference struct {
	// Handler is the payment handler registry key advertised in the
	// business profile's ucp.payment_handlers.
	Handler ReverseDomainName `json:"handler"`
	// Types is an optional list of preferred instrument types for this
	// handler, in priority order (e.g. "card", "bank").
	Types []string `json:"types,omitempty"`
}

// Context carries provisional buyer signals for relevance and
// localization. These are not authoritative: eligibility and policy
// enforcement MUST occur at checkout time using binding transaction data.
//
// New in this spec version versus v20260408: Location (an opaque
// business-scoped Location id hint, distinct from the coarse locality
// fields) and Payment (buyer-preferred payment handlers/instrument
// types). The locality fields (AddressCountry/AddressRegion/PostalCode)
// now come from the shared "locality" schema fragment, composed in
// alongside Context's own fields, but are represented here as before
// (flat fields on Context) since the wire shape is unchanged.
type Context struct {
	AddressCountry string `json:"address_country,omitempty"`
	AddressRegion  string `json:"address_region,omitempty"`
	PostalCode     string `json:"postal_code,omitempty"`
	// Location is a stable, opaque identifier for a Location in the
	// business's namespace (e.g. a saved default store). Distinct from
	// the buyer's locality; the operation spec or active
	// capability/extension defines its effects.
	Location string `json:"location,omitempty"`
	Intent   string `json:"intent,omitempty"`
	// Language is an IETF BCP 47 language tag (e.g. "en", "fr-CA", "zh-Hans").
	Language string `json:"language,omitempty"`
	// Currency is an ISO 4217 currency hint (e.g. "EUR", "USD").
	Currency string `json:"currency,omitempty"`
	// Eligibility is a set of buyer claims about eligible benefits
	// (loyalty membership, payment instrument perks, etc). Values MUST be
	// non-identifying reverse-domain names.
	Eligibility []ReverseDomainName `json:"eligibility,omitempty"`
	// Payment is buyer-preferred payment handlers in priority order.
	Payment []ContextPaymentPreference `json:"payment,omitempty"`

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
// contributes to an Expectation or FulfillmentEvent.
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
	// Price is the unit price in ISO 4217 minor units, per one whole
	// QuantityUnit.Unit (or per "each" when QuantityUnit is absent). A
	// pointer distinguishes an unset price from an explicit free (zero)
	// price.
	Price *Amount `json:"price,omitempty"`
	// QuantityUnit is the sale basis this item's LineItem.Quantity is
	// denominated in. New in this spec version: on an authoritative
	// business response, absence encodes the default "each" machine
	// identity (unit "C62", scale 0); on platform requests, absence
	// makes no assertion. See Unit and QuantityUnit.
	QuantityUnit *QuantityUnit `json:"quantity_unit,omitempty"`
	// UnitPrice is the pricing basis for this item, present when it
	// differs from the sale basis (e.g. priced per pound, sold per each).
	UnitPrice *UnitPrice `json:"unit_price,omitempty"`
	// ImageURL is the product image URI.
	ImageURL string `json:"image_url,omitempty"`
}

// LineItem is a line item in a Cart or Checkout, expected to use the
// currency of its parent object. Quantity remains an integer step count
// in this spec version — see Item.QuantityUnit for how fractional sale
// bases (e.g. price-per-pound) are represented: quantity always counts
// integer steps of the item's quantity unit, rather than becoming a
// polymorphic integer-or-object shape.
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

// LocationSummary is a summary of a physical business location: a stable
// id, a display name, and an optional address. It is the shallow
// location shape this package models — see DISCREPANCIES.md for why the
// richer Location schema (geo coordinates, operating hours, amenities —
// used by the out-of-scope Location Search/Lookup capabilities) is not
// modeled here.
type LocationSummary struct {
	// ID is a stable, opaque, business-scoped Location identifier.
	ID string `json:"id"`
	// Name is the buyer-facing, business-owned display name.
	Name string `json:"name,omitempty"`
	// Address is the physical address of the location.
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
// Unlike v20260408, the wire shape now also carries an explicit "type":
// "shipping_address" discriminator; FulfillmentDestination's
// (Un)MarshalJSON owns adding/stripping that discriminator (see
// fulfillment.go), so it is not a field on this type.
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

// Unit is a reusable unit descriptor for quantities and measures. Its
// unit-descriptor machine identity is (Unit, effective Scale), where
// effective Scale is the provided Scale or 0; DisplayText does not
// participate in identity. New in this spec version, replacing
// v20260408's ad hoc VariantUnitPriceQuantity{Value float64, Unit
// string} with a shared, scale-aware descriptor used throughout the
// fractional-quantity machinery (Measure, QuantityUnit).
type Unit struct {
	// Unit is the stable machine identifier. Businesses SHOULD use the
	// exact UN/CEFACT Rec20 Common Code when one applies (e.g. "C62" for
	// "each"); otherwise a custom identifier, used consistently.
	Unit string `json:"unit"`
	// Scale means one step equals 10^-Scale of Unit. 0-15; defaults to 0.
	Scale int `json:"scale,omitempty"`
	// DisplayText is the required printable unit label.
	DisplayText string `json:"display_text"`
}

// Measure is a measure composed of an integer value and a Unit
// descriptor: the integer count of 10^-Scale units of Unit. Used for
// VariantUnitPrice's Measure/Reference denominators and for the
// settlement measurement on a measurement-priced AdjustmentLineItem.
type Measure struct {
	Unit
	// Value is the integer count of 10^-Scale units of Unit.
	Value int64 `json:"value"`
}

// QuantityUnit is Item's sale-basis descriptor: the shared Unit
// descriptor plus the business's ordering-increment policy. Its
// unit-descriptor machine identity is (Unit, effective Scale); Increment
// (like DisplayText) is excluded from identity and mismatch comparison.
type QuantityUnit struct {
	Unit
	// Increment is the ordering granularity in steps: the business sells
	// in integer multiples of Increment steps. Defaults to 1.
	Increment int `json:"increment,omitempty"`
}

// UnitPrice is price per standard unit of measurement (e.g. per 100ml,
// per 1kg), shared between Item and Variant (both reference the same
// unit_price.json schema in this spec version; v20260408 only exposed
// this on Variant, as VariantUnitPrice with float/string measure
// quantities — see DISCREPANCIES.md).
type UnitPrice struct {
	Amount    Amount  `json:"amount"`
	Currency  string  `json:"currency"`
	Measure   Measure `json:"measure"`
	Reference Measure `json:"reference"`
}

// AvailabilityStatus qualifies Availability.Available with fulfillment
// state. Open string; well-known values provided as constants.
type AvailabilityStatus string

// Availability describes whether an item (a Variant, or a catalog
// fulfillment method) can be obtained. Named "Availability" rather than
// v20260408's "VariantAvailability" since this spec version's schema
// (shopping/types/availability.json) is shared beyond Variant — see the
// fulfillment extension's CatalogFulfillmentMethod.
type Availability struct {
	Available bool `json:"available,omitempty"`
	// Status qualifies Available with fulfillment state. See
	// AvailabilityStatus for well-known values.
	Status AvailabilityStatus `json:"status,omitempty"`
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
	ID       string           `json:"id"`
	SKU      string           `json:"sku,omitempty"`
	Barcodes []VariantBarcode `json:"barcodes,omitempty"`
	Handle   string           `json:"handle,omitempty"`
	Title    string           `json:"title"`
	// Description is required in this spec version (v20260408 left it
	// optional at the Product/Variant level, though Description.Validate
	// always required at least one populated format).
	Description Description `json:"description"`
	URL         string      `json:"url,omitempty"`
	Categories  []Category  `json:"categories,omitempty"`
	Price       Price       `json:"price"`
	// QuantityUnit is the sale basis this variant's quantity is
	// denominated in; absence encodes the default "each" basis. New in
	// this spec version.
	QuantityUnit *QuantityUnit    `json:"quantity_unit,omitempty"`
	ListPrice    *Price           `json:"list_price,omitempty"`
	UnitPrice    *UnitPrice       `json:"unit_price,omitempty"`
	Availability *Availability    `json:"availability,omitempty"`
	Options      []SelectedOption `json:"options,omitempty"`
	Media        []Media          `json:"media,omitempty"`
	Rating       *Rating          `json:"rating,omitempty"`
	Tags         []string         `json:"tags,omitempty"`
	Metadata     map[string]any   `json:"metadata,omitempty"`
	Seller       *VariantSeller   `json:"seller,omitempty"`
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
	Handle string `json:"handle,omitempty"`
	Title  string `json:"title"`
	// Description is required on Product in this spec version
	// (v20260408 required it only via Description.Validate's internal
	// invariant, not at the Product level).
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
	// additions (exchanges). Counts integer steps of the referenced line
	// item's quantity unit.
	Quantity int `json:"quantity"`
	// Measure is the settled measurement this adjustment reconciles (e.g.
	// actual picked weight), present when the line's price settles by
	// measurement. New in this spec version. Its unit identity MUST
	// match the line's pricing basis (Item.UnitPrice's Measure/Reference
	// unit) — no unit conversion is performed. A pure price settlement
	// uses Quantity 0 together with Measure and a totals delta.
	Measure *Measure `json:"measure,omitempty"`
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

// Policy is a durable business rule about the items in a response —
// return/refund terms, warranty, and the like — at the time of purchase.
// New in this spec version: embedded directly on Cart/Checkout/Order/the
// catalog responses as a "policies snapshot" (see capability.go).
type Policy struct {
	// Type is the policy type discriminator: an open reverse-DNS
	// vocabulary. Well-known values: "dev.ucp.shopping.policy.return",
	// "dev.ucp.shopping.policy.warranty".
	Type ReverseDomainName `json:"type"`
	// Description is a human-readable policy summary in one or more
	// formats, required so a platform can present it without
	// understanding any type-specific fields.
	Description Description `json:"description"`
	// AppliesTo is a list of RFC 9535 JSONPath expressions identifying
	// the nodes this policy applies to, relative to the embedding
	// response root. When absent, the policy applies to the entire
	// response.
	AppliesTo []string `json:"applies_to,omitempty"`
	// URL is an optional link to the full policy document.
	URL string `json:"url,omitempty"`

	// Extra carries any additional, type-specific fields not modeled
	// above; the schema allows additional properties gated by Type.
	Extra map[string]any `json:"-"`
}

// ActionInstance is one outstanding instance of an extension-defined
// Action type, keyed by reverse-domain Action type in Actions. New in
// this spec version.
type ActionInstance struct {
	// ID identifies this Action instance.
	ID string `json:"id"`
	// Config is configuration defined by the extension that declares
	// this Action type.
	Config map[string]any `json:"config,omitempty"`

	// Extra carries any additional fields not modeled above; the schema
	// allows additional properties.
	Extra map[string]any `json:"-"`
}

// Actions is a set of outstanding extension-defined Action instances,
// keyed by reverse-domain Action type (not by extension name). New in
// this spec version; embedded on Cart/Checkout/the catalog responses
// (see capability.go). Non-empty per type when present: the spec
// requires each key's array to have at least one element, but this
// package does not reject an empty slice on unmarshal per the
// module-wide convention of not validating implicitly.
type Actions map[ReverseDomainName][]ActionInstance

// PaymentIdentity identifies a participant for token binding. AccessToken
// uniquely identifies the participant tokens should be bound to. Used by
// the (out-of-scope, transport-level) tokenization handler alongside
// Binding — see payment.go and DISCREPANCIES.md.
type PaymentIdentity struct {
	AccessToken string `json:"access_token"`
}

// Binding binds a credential or token to a specific capability resource,
// preventing reuse across different resources. Breaking change from
// v20260408: that version's Binding was checkout-specific
// ({checkout_id, identity}); this version generalizes to any capability
// resource ({type: capability reverse-domain name, id: opaque resource
// id}), with the participant Identity now a sibling field alongside
// Binding rather than nested inside it (see the tokenization handler's
// request shape, out of scope per DISCREPANCIES.md).
type Binding struct {
	// Type is the capability that owns the bound resource (e.g.
	// "dev.ucp.shopping.checkout"). MUST be a capability name declared in
	// the UCP namespace.
	Type ReverseDomainName `json:"type"`
	// ID is the opaque identifier of the bound resource within the
	// owning capability (e.g. a checkout id).
	ID string `json:"id"`
}
