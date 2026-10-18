// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package simstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrProductNotFound is returned when an order references a product_id that
// doesn't exist in this store's catalog.
var ErrProductNotFound = errors.New("sim store: product not found")

// ErrFulfillmentRequired is returned when a completion is attempted on an
// order that has physical line items but no fulfillment method selected — the
// merchant has nothing to ship against. A business rejection, not a fault.
var ErrFulfillmentRequired = errors.New("sim store: a fulfillment method must be selected before completing checkout")

// ErrOutOfStock is returned when an order line item requests more units of
// a stock-managed product than it currently has available — matching how a
// real WooCommerce store enforces "manage_stock" at order create/update
// time, rather than silently accepting an order it can never fulfill.
var ErrOutOfStock = errors.New("sim store: insufficient stock")

// Product is one item in a simulated store's catalog, shaped to round-trip
// through each platform's REST surface (internal/platform/<name>/sim) so each
// platform's real connector can genuinely pull it — no field here exists only
// for Sarvex's convenience.
type Product struct {
	// ID is the WooCommerce-native numeric product id. The real connector
	// looks products up by numeric id for CatalogLookup/UpdateProduct once
	// it has resolved SKU -> id via a /products?sku= search, exactly as it
	// would against a real store.
	ID               int       `bson:"id"`
	SKU              string    `bson:"sku"`
	Name             string    `bson:"name"`
	Slug             string    `bson:"slug"`
	Type             string    `bson:"type"`
	Description      string    `bson:"description"`
	ShortDescription string    `bson:"short_description"`
	Category         string    `bson:"category"`
	ImageURLs        []string  `bson:"image_urls"`
	Weight           string    `bson:"weight"`      // kg, decimal string as stores serve it ("" = not set)
	Length           string    `bson:"length"`      // cm
	Width            string    `bson:"width"`       // cm
	Height           string    `bson:"height"`      // cm
	PriceMinor       int64     `bson:"price_minor"` // 0 means "no price" (messy seed / mutate stock to 0 uses this)
	Currency         string    `bson:"currency"`
	InStock          bool      `bson:"in_stock"`
	ManageStock      bool      `bson:"manage_stock"`
	StockQuantity    *int      `bson:"stock_quantity,omitempty"`
	UpdatedAt        time.Time `bson:"updated_at"`
}

// OrderLineItem is one line in a simulated order. Price is snapshotted at
// order-creation time from the product's current price — a later price
// mutation must not retroactively change an already-placed order, matching
// how a real WooCommerce store behaves.
type OrderLineItem struct {
	ProductID  int    `bson:"product_id"`
	SKU        string `bson:"sku"`
	Name       string `bson:"name"`
	Quantity   int    `bson:"quantity"`
	PriceMinor int64  `bson:"price_minor"`
}

// OrderAddress is a simulated order's billing or shipping address, shaped to
// round-trip through the WooCommerce REST API v3 wire format (see
// internal/platform/woocommerce's wcAddress).
type OrderAddress struct {
	FirstName string `bson:"first_name"`
	LastName  string `bson:"last_name"`
	Address1  string `bson:"address_1"`
	Address2  string `bson:"address_2"`
	City      string `bson:"city"`
	State     string `bson:"state"`
	Postcode  string `bson:"postcode"`
	Country   string `bson:"country"`
	Email     string `bson:"email"`
	Phone     string `bson:"phone"`
}

// Order is one simulated store's order. The real woocommerce.Connector's
// entire checkout lifecycle — CheckoutCreate/Update/Complete/Cancel and
// CartCreate/Update alike — is implemented as POST/PUT calls against the
// WooCommerce Orders REST endpoint (connector.go): a checkout session IS a
// WooCommerce order from "pending" through "processing"/"cancelled", there
// is no separate cart resource on the wire. This type exists so the sim
// store can genuinely serve that surface instead of 404ing on it.
type Order struct {
	ID                int             `bson:"id"`
	Status            string          `bson:"status"` // pending, processing, cancelled
	Currency          string          `bson:"currency"`
	LineItems         []OrderLineItem `bson:"line_items"`
	ShippingMethodID  string          `bson:"shipping_method_id"`
	ShippingLineMinor int64           `bson:"shipping_line_minor"`
	TaxMinor          int64           `bson:"tax_minor"`
	// DiscountCodes are the canonical (upper-cased) discount codes accepted on
	// this order; DiscountMinor is the total reduction they produce, recomputed
	// against the current subtotal on every change.
	DiscountCodes   []string     `bson:"discount_codes,omitempty"`
	DiscountMinor   int64        `bson:"discount_minor"`
	BillingAddress  OrderAddress `bson:"billing"`
	ShippingAddress OrderAddress `bson:"shipping"`
	// BuyerConsent is the JSON-encoded buyer_consent object the connector
	// stores as order meta_data (empty when none or cleared).
	BuyerConsent string    `bson:"buyer_consent,omitempty"`
	CreatedAt    time.Time `bson:"created_at"`
	UpdatedAt    time.Time `bson:"updated_at"`
}

// Store is one simulated merchant store's persistent catalog and the
// deterministic WooCommerce REST credentials it accepts, keyed by store_id.
type Store struct {
	StoreID       string              `bson:"_id"`
	Products      map[string]*Product `bson:"products"` // keyed by SKU
	NextProductID int                 `bson:"next_product_id"`
	// Orders is keyed by the order's id, stringified — BSON documents
	// require string field names, so a map[int]*Order would fail to
	// round-trip through Mongo (unlike encoding/json, which tolerates int
	// map keys).
	Orders      map[string]*Order `bson:"orders"`
	NextOrderID int               `bson:"next_order_id"`
	// LocalPickup is whether the simulated merchant has WooCommerce's Local
	// Pickup shipping method enabled. Off by default: stores advertise pickup
	// only once a merchant turns it on, and the simulator mirrors that.
	LocalPickup bool      `bson:"local_pickup,omitempty"`
	CreatedAt   time.Time `bson:"created_at"`
	UpdatedAt   time.Time `bson:"updated_at"`
}

// NewStore creates an empty persistent store for storeID.
func NewStore(storeID string) *Store {
	now := time.Now().UTC()
	return &Store{
		StoreID:       storeID,
		Products:      make(map[string]*Product),
		NextProductID: 1000,
		Orders:        make(map[string]*Order),
		NextOrderID:   5000,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// skuFor generates a stable, readable SKU for the i-th seeded product of a store.
func skuFor(storeID string, i int) string {
	suffix := storeID
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	return fmt.Sprintf("SIM-%s-%04d", suffix, i)
}

// Seed populates n products into the store. When messy is true, a fixed
// fraction of products are seeded with real, detectable readiness problems
// (matching the rules the live scoring rubric actually evaluates —
// internal/pkg/ucp/catalogue/v1/scoring_rubric_data.go — rather than
// fabricated ones): missing price (rule_price_missing, blocks checkout),
// missing title (rule_title_missing, blocks checkout), and an
// in-stock/quantity inconsistency (feeds the consistency engine). Products
// not hit by one of those every-5th/7th/11th slots are clean.
func (s *Store) Seed(n int, messy bool) {
	for i := 1; i <= n; i++ {
		sku := skuFor(s.StoreID, i)
		id := s.NextProductID
		s.NextProductID++

		p := &Product{
			ID:            id,
			SKU:           sku,
			Name:          fmt.Sprintf("Sim Product %d", i),
			Slug:          fmt.Sprintf("sim-product-%d", i),
			Type:          "simple",
			Description:   fmt.Sprintf("A simulated catalog item, item #%d, seeded for local testing.", i),
			Category:      "Simulated goods",
			ImageURLs:     []string{fmt.Sprintf("https://sim.example.test/images/%s.jpg", skuFor(s.StoreID, i))},
			Weight:        "0.5",
			Length:        "10",
			Width:         "10",
			Height:        "10",
			PriceMinor:    1999 + int64(i)*100,
			Currency:      "USD",
			InStock:       true,
			ManageStock:   true,
			StockQuantity: intPtr(25),
			UpdatedAt:     time.Now().UTC(),
		}

		if messy {
			switch {
			case i%5 == 0:
				// rule_price_missing (blocks_checkout: true)
				p.PriceMinor = 0
			case i%7 == 0:
				// rule_title_missing (blocks_checkout: true)
				p.Name = ""
			case i%11 == 0:
				// stock inconsistency: marked out of stock but positive quantity
				p.InStock = false
				p.StockQuantity = intPtr(10)
			case i%3 == 0:
				// no shipping data: rule_shipping_weight_missing /
				// rule_shipping_dimensions_missing (non-blocking)
				p.Weight, p.Length, p.Width, p.Height = "", "", "", ""
			}
		}

		s.Products[sku] = p
	}
	s.UpdatedAt = time.Now().UTC()
}

// SetPrice mutates a product's price in place. priceMinor <= 0 recreates the
// "missing price" condition on demand (sim mutate price --to 0).
func (s *Store) SetPrice(sku string, priceMinor int64) (*Product, error) {
	p, ok := s.Products[sku]
	if !ok {
		return nil, fmt.Errorf("sim store %s: no such sku %q", s.StoreID, sku)
	}
	p.PriceMinor = priceMinor
	p.UpdatedAt = time.Now().UTC()
	s.UpdatedAt = p.UpdatedAt
	return p, nil
}

// SetStock mutates a product's stock quantity in place, deriving InStock
// from the new quantity (0 => out of stock), matching how a merchant
// actually flips a product's availability.
func (s *Store) SetStock(sku string, quantity int) (*Product, error) {
	p, ok := s.Products[sku]
	if !ok {
		return nil, fmt.Errorf("sim store %s: no such sku %q", s.StoreID, sku)
	}
	p.StockQuantity = intPtr(quantity)
	p.InStock = quantity > 0
	p.UpdatedAt = time.Now().UTC()
	s.UpdatedAt = p.UpdatedAt
	return p, nil
}

// BreakAttr sets field to a deliberately bad value the live scoring rubric
// detects, mirroring the rules in scoring_rubric_data.go that key off the
// fields the real connector actually normalises (title, price). Returns an
// error for a field the rubric can't currently evaluate for a
// WooCommerce-sourced product, rather than silently no-op'ing.
func (s *Store) BreakAttr(sku, field string) (*Product, error) {
	p, ok := s.Products[sku]
	if !ok {
		return nil, fmt.Errorf("sim store %s: no such sku %q", s.StoreID, sku)
	}
	switch field {
	case "title":
		p.Name = ""
	case "price":
		p.PriceMinor = 0
	case "currency":
		p.Currency = ""
	default:
		return nil, fmt.Errorf("sim store %s: field %q has no real, rubric-visible break for a simulated product (only title, price, currency)", s.StoreID, field)
	}
	p.UpdatedAt = time.Now().UTC()
	s.UpdatedAt = p.UpdatedAt
	return p, nil
}

// Fix sets field back to a good value on sku, the inverse of BreakAttr /
// SetPrice(0) / SetStock(0).
func (s *Store) Fix(sku, field string) (*Product, error) {
	p, ok := s.Products[sku]
	if !ok {
		return nil, fmt.Errorf("sim store %s: no such sku %q", s.StoreID, sku)
	}
	switch field {
	case "title":
		if p.Name == "" {
			p.Name = fmt.Sprintf("Sim Product (%s)", sku)
		}
	case "price":
		if p.PriceMinor <= 0 {
			p.PriceMinor = 1999
		}
	case "stock":
		p.StockQuantity = intPtr(25)
		p.InStock = true
	case "currency":
		if p.Currency == "" {
			p.Currency = "USD"
		}
	default:
		return nil, fmt.Errorf("sim store %s: unknown field %q (want title, price, stock, or currency)", s.StoreID, field)
	}
	p.UpdatedAt = time.Now().UTC()
	s.UpdatedAt = p.UpdatedAt
	return p, nil
}

// productByID looks up a product by its WooCommerce-native numeric id
// (order line items reference products this way, never by SKU).
func (s *Store) productByID(id int) *Product {
	for _, p := range s.Products {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// checkStock rejects a line item quantity a stock-managed product cannot
// fulfill, matching how a real WooCommerce store's "manage_stock" enforces
// availability at order create/update time rather than silently placing an
// order for something it doesn't have. A product with ManageStock false is
// never stock-checked, same as WooCommerce's own opt-in behavior.
func checkStock(p *Product, qty int) error {
	if !p.ManageStock {
		return nil
	}
	if !p.InStock || (p.StockQuantity != nil && qty > *p.StockQuantity) {
		return fmt.Errorf("%w: sku %s", ErrOutOfStock, p.SKU)
	}
	return nil
}

// FlatRateShippingMinor is the sim's single simulated shipping rate,
// applied whenever an order carries a selected shipping method — the fake
// store doesn't model shipping zones/rates, only that selecting one has a
// real, non-zero cost the connector's totals must reflect.
const FlatRateShippingMinor = int64(500)

// IsPickupMethod reports whether a shipping method id means "collect in
// store". Every simulated platform's pickup method id ends in "_pickup"
// (WooCommerce's local_pickup, Magento's instore_pickup), which keeps this
// model free of any one platform's ids.
func IsPickupMethod(methodID string) bool {
	return strings.HasSuffix(methodID, "_pickup")
}

// shippingCostMinor is what selecting the given shipping method costs:
// collecting an order in store is free, anything else pays the flat rate.
func shippingCostMinor(methodID string) int64 {
	if IsPickupMethod(methodID) {
		return 0
	}
	return FlatRateShippingMinor
}

// simDiscountCodes are the discount codes this simulated store honors, keyed by
// canonical (upper-case) code -> percent off the subtotal. Matching is
// case-insensitive and normalizes to these forms. SPRING20 mirrors the
// official conformance suite's default discount code.
var simDiscountCodes = map[string]int{"SPRING20": 20}

// ApplyCoupons resolves submitted codes against the known discount codes,
// returning the canonical codes accepted and the total reduction in minor
// units off subtotalMinor. Unknown or duplicate codes are dropped.
func ApplyCoupons(codes []string, subtotalMinor int64) (accepted []string, discountMinor int64) {
	seen := map[string]bool{}
	for _, raw := range codes {
		code := strings.ToUpper(strings.TrimSpace(raw))
		pct, ok := simDiscountCodes[code]
		if !ok || seen[code] {
			continue
		}
		seen[code] = true
		accepted = append(accepted, code)
		discountMinor += subtotalMinor * int64(pct) / 100
	}
	return accepted, discountMinor
}

// OrderSubtotalMinor is the pre-discount sum of the order's line items.
func OrderSubtotalMinor(items []OrderLineItem) int64 {
	var subtotal int64
	for _, li := range items {
		subtotal += li.PriceMinor * int64(li.Quantity)
	}
	return subtotal
}

// CreateOrder places a new order ("pending", matching a fresh WooCommerce
// checkout session) from the given product_id -> quantity line items,
// snapshotting each product's current price and currency.
func (s *Store) CreateOrder(lineItems map[int]int, billing, shipping OrderAddress, shippingMethodID string, couponCodes []string) (*Order, error) {
	if len(lineItems) == 0 {
		return nil, fmt.Errorf("sim store %s: order requires at least one line item", s.StoreID)
	}

	items := make([]OrderLineItem, 0, len(lineItems))
	currency := "USD"
	for productID, qty := range lineItems {
		p := s.productByID(productID)
		if p == nil {
			return nil, fmt.Errorf("%w: product_id %d", ErrProductNotFound, productID)
		}
		if err := checkStock(p, qty); err != nil {
			return nil, err
		}
		if p.Currency != "" {
			currency = p.Currency
		}
		items = append(items, OrderLineItem{
			ProductID:  productID,
			SKU:        p.SKU,
			Name:       p.Name,
			Quantity:   qty,
			PriceMinor: p.PriceMinor,
		})
	}

	id := s.NextOrderID
	s.NextOrderID++
	now := time.Now().UTC()
	order := &Order{
		ID:               id,
		Status:           "pending",
		Currency:         currency,
		LineItems:        items,
		ShippingMethodID: shippingMethodID,
		BillingAddress:   billing,
		ShippingAddress:  shipping,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if shippingMethodID != "" {
		order.ShippingLineMinor = shippingCostMinor(shippingMethodID)
	}
	order.DiscountCodes, order.DiscountMinor = ApplyCoupons(couponCodes, OrderSubtotalMinor(items))

	if s.Orders == nil {
		s.Orders = make(map[string]*Order)
	}
	s.Orders[strconv.Itoa(id)] = order
	s.UpdatedAt = now
	return order, nil
}

// GetOrder retrieves an order by its numeric id.
func (s *Store) GetOrder(id int) (*Order, bool) {
	o, ok := s.Orders[strconv.Itoa(id)]
	return o, ok
}

// UpdateOrder applies a partial update to an existing order — matching
// WooCommerce's PUT tolerance of a partial payload (the WooCommerce surface's
// updateProduct does the same for products): a nil/empty argument leaves that
// field unchanged. lineItems, when non-empty, fully replaces the order's
// line items (the connector always sends the full set on update/complete).
func (s *Store) UpdateOrder(id int, status string, lineItems map[int]int, billing, shipping *OrderAddress, shippingMethodID *string, couponCodes *[]string) (*Order, error) {
	o, ok := s.Orders[strconv.Itoa(id)]
	if !ok {
		return nil, fmt.Errorf("sim store %s: no such order %d", s.StoreID, id)
	}

	// A completion needs a fulfillment method — a physical-goods order cannot
	// ship without one. Reject before mutating, so a checkout that never chose
	// fulfillment gets the missing-selection business error rather than being
	// marked processing. The effective method accounts for one selected in this
	// same update.
	if status == "processing" {
		effectiveMethod := o.ShippingMethodID
		if shippingMethodID != nil {
			effectiveMethod = *shippingMethodID
		}
		if effectiveMethod == "" && len(o.LineItems) > 0 {
			return nil, ErrFulfillmentRequired
		}
	}

	if status != "" {
		o.Status = status
	}

	if len(lineItems) > 0 {
		items := make([]OrderLineItem, 0, len(lineItems))
		for productID, qty := range lineItems {
			p := s.productByID(productID)
			if p == nil {
				return nil, fmt.Errorf("%w: product_id %d", ErrProductNotFound, productID)
			}
			if err := checkStock(p, qty); err != nil {
				return nil, err
			}
			items = append(items, OrderLineItem{
				ProductID:  productID,
				SKU:        p.SKU,
				Name:       p.Name,
				Quantity:   qty,
				PriceMinor: p.PriceMinor,
			})
		}
		o.LineItems = items
	}

	if billing != nil {
		o.BillingAddress = *billing
	}
	if shipping != nil {
		o.ShippingAddress = *shipping
	}
	if shippingMethodID != nil {
		o.ShippingMethodID = *shippingMethodID
		if *shippingMethodID != "" {
			o.ShippingLineMinor = shippingCostMinor(*shippingMethodID)
		}
	}

	// Coupons are PUT-replace: a non-nil (even empty) set replaces the order's
	// codes. Recompute the reduction against the current subtotal whenever the
	// codes or the line items may have changed.
	subtotal := OrderSubtotalMinor(o.LineItems)
	if couponCodes != nil {
		o.DiscountCodes, o.DiscountMinor = ApplyCoupons(*couponCodes, subtotal)
	} else {
		_, o.DiscountMinor = ApplyCoupons(o.DiscountCodes, subtotal)
	}

	o.UpdatedAt = time.Now().UTC()
	s.UpdatedAt = o.UpdatedAt
	return o, nil
}

func intPtr(i int) *int { return &i }

var ErrStoreNotFound = errors.New("sim: store not found")

// StoreRepository persists simulated stores, keyed by store_id, so they
// survive a restart of both the sim-store HTTP server and the sync-plane
// service (acceptance requirement: nothing needs redoing after a restart).
type StoreRepository interface {
	Get(ctx context.Context, storeID string) (*Store, error)
	Upsert(ctx context.Context, s *Store) error
	List(ctx context.Context) ([]*Store, error)
}
