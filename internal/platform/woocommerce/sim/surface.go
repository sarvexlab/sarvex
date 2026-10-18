// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package sim is WooCommerce's simulated store: the WooCommerce REST API v3
// surface (products, orders, shipping zones, general settings) over the
// platform-neutral simstore.Store model, served by the dev/test simulator
// (internal/planes/sync/sim) for every store_id under /stores/{store_id}/.
// The real WooCommerce connector talks to it exactly as it would to a store.
package sim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/simstore"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/resilience"
	"github.com/sarvexlab/sarvex/internal/platform/woocommerce"
)

// localPickupMethodID is WooCommerce's Local Pickup shipping method id.
const localPickupMethodID = "local_pickup"

// Surface is WooCommerce's simstore.Surface.
type Surface struct{}

var _ simstore.Surface = Surface{}

// server holds what the REST handlers need from the hosting simulator.
type server struct {
	stores  simstore.StoreRepository
	baseURL string
	log     *slog.Logger
}

// Register implements simstore.Surface.
func (Surface) Register(mux *http.ServeMux, env simstore.Env) {
	s := &server{stores: env.Stores, baseURL: strings.TrimRight(env.BaseURL, "/"), log: env.Log}
	mux.HandleFunc("GET /stores/{store_id}/wp-json/wc/v3/products", s.withAuth(s.listProducts))
	mux.HandleFunc("GET /stores/{store_id}/wp-json/wc/v3/products/{id}", s.withAuth(s.getProduct))
	mux.HandleFunc("PUT /stores/{store_id}/wp-json/wc/v3/products/{id}", s.withAuth(s.updateProduct))
	mux.HandleFunc("POST /stores/{store_id}/wp-json/wc/v3/orders", s.withAuth(s.createOrder))
	mux.HandleFunc("GET /stores/{store_id}/wp-json/wc/v3/orders/{id}", s.withAuth(s.getOrder))
	mux.HandleFunc("PUT /stores/{store_id}/wp-json/wc/v3/orders/{id}", s.withAuth(s.updateOrder))
	// Shipping configuration and store settings, read by pickup detection.
	mux.HandleFunc("GET /stores/{store_id}/wp-json/wc/v3/shipping/zones", s.withAuth(s.listShippingZones))
	mux.HandleFunc("GET /stores/{store_id}/wp-json/wc/v3/shipping/zones/{zone_id}/methods", s.withAuth(s.listZoneMethods))
	mux.HandleFunc("GET /stores/{store_id}/wp-json/wc/v3/settings/general", s.withAuth(s.getGeneralSettings))
}

// ConsumerKey deterministically derives the WooCommerce REST consumer key a
// simulated store's connector is paired with, from its store_id alone.
func ConsumerKey(storeID string) string {
	return "ck_sim_" + storeID
}

// ConsumerSecret deterministically derives the WooCommerce REST consumer
// secret, from its store_id alone.
func ConsumerSecret(storeID string) string {
	return "cs_sim_" + storeID
}

// Credentials implements simstore.Surface: the store_credentials_woocommerce
// payload (sarvex-v1-control-plane.yaml).
func (Surface) Credentials(simServerBaseURL, storeID string) map[string]any {
	return map[string]any{
		"store_base_url":  simstore.StoreBaseURL(simServerBaseURL, storeID),
		"consumer_key":    ConsumerKey(storeID),
		"consumer_secret": ConsumerSecret(storeID),
		"webhook_secret":  simstore.WebhookSecret(storeID),
		"store_currency":  "USD",
	}
}

// Connector implements simstore.Surface.
func (Surface) Connector(simServerBaseURL, merchantID, storeID string) (base.Connector, error) {
	return woocommerce.NewConnector(woocommerce.MerchantConfig{
		MerchantID: merchantID, StoreID: storeID,
		StoreBaseURL:   simstore.StoreBaseURL(simServerBaseURL, storeID),
		ConsumerKey:    ConsumerKey(storeID),
		ConsumerSecret: ConsumerSecret(storeID),
		StoreCurrency:  "USD", APIVersion: "v3",
		RequestTimeout: 5 * time.Second, MaxRetries: 1,
	}, nil, resilience.Config{}, nil)
}

// CatalogID implements simstore.Surface: WooCommerce's catalog is keyed by
// numeric product id.
func (Surface) CatalogID(p *simstore.Product) string { return strconv.Itoa(p.ID) }

// PickupLocationID implements simstore.Surface.
func (Surface) PickupLocationID() string { return "pickup_store" }

func (s *server) storeBaseURL(storeID string) string {
	return simstore.StoreBaseURL(s.baseURL, storeID)
}

// storeAddress is simstore.Address as WooCommerce's general settings report it.
var storeAddress = []struct{ id, value string }{
	{"woocommerce_store_address", simstore.Address.Street},
	{"woocommerce_store_address_2", simstore.Address.Street2},
	{"woocommerce_store_city", simstore.Address.City},
	{"woocommerce_default_country", simstore.Address.Country + ":" + simstore.Address.Region},
	{"woocommerce_store_postcode", simstore.Address.Postcode},
}

// The simulated store model, shared by every platform's surface.
type (
	Store         = simstore.Store
	Product       = simstore.Product
	Order         = simstore.Order
	OrderLineItem = simstore.OrderLineItem
	OrderAddress  = simstore.OrderAddress
)

// wcProduct is the WooCommerce REST API v3 product wire shape, matching
// internal/platform/woocommerce's wcProduct exactly — this server
// exists to be pulled by that real connector, so the shapes must agree.
type wcProduct struct {
	ID               int       `json:"id"`
	Name             string    `json:"name"`
	Slug             string    `json:"slug"`
	SKU              string    `json:"sku"`
	Description      string    `json:"description"`
	Price            string    `json:"price"`
	Images           []wcImage `json:"images"`
	Variations       []int     `json:"variations"`
	InStock          bool      `json:"in_stock"`
	StockStatus      string    `json:"stock_status"`
	StockQuantity    *int      `json:"stock_quantity"`
	ManageStock      bool      `json:"manage_stock"`
	RegularPrice     string    `json:"regular_price"`
	SalePrice        string    `json:"sale_price"`
	ShortDescription string    `json:"short_description"`
	Permalink        string    `json:"permalink"`
	Type             string    `json:"type"`
	Weight           string    `json:"weight"`
	Dimensions       wcDims    `json:"dimensions"`
	Categories       []wcTerm  `json:"categories"`
}

// wcImage, wcDims and wcTerm match internal/platform/woocommerce's shapes.
type wcImage struct {
	ID  int    `json:"id"`
	Src string `json:"src"`
}

type wcDims struct {
	Length string `json:"length"`
	Width  string `json:"width"`
	Height string `json:"height"`
}

type wcTerm struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// wcOrder is the WooCommerce REST API v3 order wire shape, matching
// internal/platform/woocommerce's wcOrder exactly — the real
// connector's entire checkout/cart lifecycle (create/get/update/complete/
// cancel) is implemented as POST/GET/PUT calls against this resource, so
// this server must serve and accept exactly this shape.
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
	MetaData        []wcMetaData     `json:"meta_data,omitempty"`
	// AvailableShippingRates is a Sarvex-simulator extension to the WooCommerce
	// order shape (not a field the real REST API returns on an order): a real
	// WooCommerce store surfaces available rates via its Store API cart once an
	// address is known, which this sim has no equivalent of, so it advertises
	// the offerable rates here instead. The real woocommerce.Connector reads
	// this to build the checkout's fulfillment options; both sides keep the
	// same shape, exactly as the wcOrder doc comment requires.
	AvailableShippingRates []wcShippingRate `json:"available_shipping_rates,omitempty"`
}

// wcMetaData is one WooCommerce order meta_data entry.
type wcMetaData struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// metaKeyBuyerConsent mirrors the woocommerce connector's key for the
// buyer_consent extension's stored consent object.
const metaKeyBuyerConsent = "_ucp_buyer_consent"

// wcShippingRate is one offerable shipping rate (see wcOrder.AvailableShippingRates).
type wcShippingRate struct {
	MethodID    string `json:"method_id"`
	MethodTitle string `json:"method_title"`
	Total       string `json:"total"`
}

// wcCouponLine is one applied discount code and the reduction attributed to it
// (matching the real WooCommerce order shape's coupon_lines).
type wcCouponLine struct {
	Code     string `json:"code"`
	Discount string `json:"discount"`
}

type wcLineItem struct {
	ID        int    `json:"id"`
	ProductID int    `json:"product_id"`
	Quantity  int    `json:"quantity"`
	Price     string `json:"price"`
	Name      string `json:"name"`
	SKU       string `json:"sku"`
	Total     string `json:"total"`
}

type wcShippingLine struct {
	ID          int    `json:"id"`
	MethodID    string `json:"method_id"`
	MethodTitle string `json:"method_title"`
	Total       string `json:"total"`
}

type wcAddress struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Address1  string `json:"address_1"`
	Address2  string `json:"address_2"`
	City      string `json:"city"`
	State     string `json:"state"`
	Postcode  string `json:"postcode"`
	Country   string `json:"country"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
}

// shippingTitle is the human title of a shipping method.
func shippingTitle(methodID string) string {
	if methodID == localPickupMethodID {
		return "Local pickup"
	}
	return "Standard Shipping"
}

func formatMinor(minor int64) string {
	return fmt.Sprintf("%.2f", float64(minor)/100)
}

func toWCAddress(a OrderAddress) wcAddress {
	return wcAddress{
		FirstName: a.FirstName,
		LastName:  a.LastName,
		Address1:  a.Address1,
		Address2:  a.Address2,
		City:      a.City,
		State:     a.State,
		Postcode:  a.Postcode,
		Country:   a.Country,
		Email:     a.Email,
		Phone:     a.Phone,
	}
}

func fromWCAddress(a wcAddress) OrderAddress {
	return OrderAddress{
		FirstName: a.FirstName,
		LastName:  a.LastName,
		Address1:  a.Address1,
		Address2:  a.Address2,
		City:      a.City,
		State:     a.State,
		Postcode:  a.Postcode,
		Country:   a.Country,
		Email:     a.Email,
		Phone:     a.Phone,
	}
}

// toWCOrder renders a persisted Order in the exact wire shape the real
// woocommerce.Connector's mapper reads (mapper.go orderToCheckoutResponse):
// line item .Total/.ProductID/.Quantity/.Name and order .Total/.TaxTotal/
// .ShippingTotal drive the checkout response's totals and line items.
func toWCOrder(o *Order) wcOrder {
	subtotalMinor := int64(0)
	lineItems := make([]wcLineItem, len(o.LineItems))
	for i, li := range o.LineItems {
		lineTotalMinor := li.PriceMinor * int64(li.Quantity)
		subtotalMinor += lineTotalMinor
		lineItems[i] = wcLineItem{
			ID:        i + 1,
			ProductID: li.ProductID,
			Quantity:  li.Quantity,
			Price:     formatMinor(li.PriceMinor),
			Name:      li.Name,
			SKU:       li.SKU,
			Total:     formatMinor(lineTotalMinor),
		}
	}

	var shippingLines []wcShippingLine
	if o.ShippingMethodID != "" {
		shippingLines = []wcShippingLine{{
			ID:          1,
			MethodID:    o.ShippingMethodID,
			MethodTitle: shippingTitle(o.ShippingMethodID),
			Total:       formatMinor(o.ShippingLineMinor),
		}}
	}

	totalMinor := subtotalMinor + o.ShippingLineMinor + o.TaxMinor - o.DiscountMinor

	// Applied coupon lines, one per accepted discount code, carrying the whole
	// order-level reduction on the first line (the sim models a single pooled
	// discount, not a per-code split).
	var couponLines []wcCouponLine
	for i, code := range o.DiscountCodes {
		amount := int64(0)
		if i == 0 {
			amount = o.DiscountMinor
		}
		couponLines = append(couponLines, wcCouponLine{Code: code, Discount: formatMinor(amount)})
	}

	// Once a shipping destination is known, advertise the rate(s) the store
	// can fulfil it with — the single flat rate this sim models, priced at
	// simstore.FlatRateShippingMinor so the offered price matches what selecting it
	// actually charges (CreateOrder/UpdateOrder). No address, no offer, the
	// same way a real store can't quote shipping without one.
	var availableRates []wcShippingRate
	if simstore.HasShippingAddress(o.ShippingAddress) {
		availableRates = []wcShippingRate{{
			MethodID:    "flat_rate",
			MethodTitle: "Standard Shipping",
			Total:       formatMinor(simstore.FlatRateShippingMinor),
		}}
	}

	return wcOrder{
		ID:                     o.ID,
		OrderNumber:            o.ID,
		Status:                 o.Status,
		Currency:               o.Currency,
		DateCreated:            o.CreatedAt.Format(time.RFC3339),
		Total:                  formatMinor(totalMinor),
		Subtotal:               formatMinor(subtotalMinor),
		ShippingTotal:          formatMinor(o.ShippingLineMinor),
		TaxTotal:               formatMinor(o.TaxMinor),
		DiscountTotal:          formatMinor(o.DiscountMinor),
		LineItems:              lineItems,
		ShippingLines:          shippingLines,
		CouponLines:            couponLines,
		BillingAddress:         toWCAddress(o.BillingAddress),
		ShippingAddress:        toWCAddress(o.ShippingAddress),
		AvailableShippingRates: availableRates,
		MetaData:               consentMeta(o.BuyerConsent),
	}
}

func consentMeta(consent string) []wcMetaData {
	if consent == "" {
		return nil
	}
	return []wcMetaData{{Key: metaKeyBuyerConsent, Value: consent}}
}

// wcOrderWriteRequest is the subset of the WooCommerce order create/update
// wire body the real connector actually sends (connector.go CheckoutCreate/
// Update/Complete/Cancel and CartCreate/Update): status, line items by
// product_id, billing/shipping addresses, and a single shipping method
// selection. Fields absent from a PUT are left nil/zero and must not
// overwrite the existing order (matching updateProduct's tolerance).
type wcOrderWriteRequest struct {
	Status    *string `json:"status"`
	LineItems []struct {
		ProductID int `json:"product_id"`
		Quantity  int `json:"quantity"`
	} `json:"line_items"`
	Billing       *wcAddress `json:"billing"`
	Shipping      *wcAddress `json:"shipping"`
	ShippingLines []struct {
		MethodID string `json:"method_id"`
	} `json:"shipping_lines"`
	// CouponLines is a pointer so an absent key (leave coupons unchanged) is
	// distinguishable from a present, possibly-empty one (replace/clear them).
	CouponLines *[]struct {
		Code string `json:"code"`
	} `json:"coupon_lines"`
	// PaymentToken is the opaque token the connector forwards at completion so
	// this simulated backend can decide the charge outcome. Not a stock
	// WooCommerce order field; the connector sends it only on complete.
	PaymentToken string `json:"payment_token"`
	// MetaData carries the buyer_consent extension's consent object; an entry
	// with an empty value clears it. A nil slice leaves consent unchanged.
	MetaData []wcMetaData `json:"meta_data"`
}

// buyerConsent returns the submitted consent replace-value: nil when no
// buyer_consent meta entry was sent (leave unchanged), otherwise the (possibly
// empty) JSON string.
func (r *wcOrderWriteRequest) buyerConsent() *string {
	for _, m := range r.MetaData {
		if m.Key != metaKeyBuyerConsent {
			continue
		}
		v, _ := m.Value.(string)
		return &v
	}
	return nil
}

func (r *wcOrderWriteRequest) lineItemMap() map[int]int {
	if len(r.LineItems) == 0 {
		return nil
	}
	m := make(map[int]int, len(r.LineItems))
	for _, li := range r.LineItems {
		m[li.ProductID] = li.Quantity
	}
	return m
}

func (r *wcOrderWriteRequest) shippingMethodID() *string {
	if len(r.ShippingLines) == 0 {
		return nil
	}
	return &r.ShippingLines[0].MethodID
}

// couponCodes returns the submitted discount codes as a replace-set: nil when
// no coupon_lines key was sent (leave unchanged), otherwise the (possibly
// empty) list of codes.
func (r *wcOrderWriteRequest) couponCodes() *[]string {
	if r.CouponLines == nil {
		return nil
	}
	codes := make([]string, 0, len(*r.CouponLines))
	for _, cl := range *r.CouponLines {
		codes = append(codes, cl.Code)
	}
	return &codes
}

func toWCProduct(p *Product, storeBaseURL string) wcProduct {
	price := fmt.Sprintf("%.2f", float64(p.PriceMinor)/100)
	if p.PriceMinor <= 0 {
		price = ""
	}
	stockStatus := "outofstock"
	if p.InStock {
		stockStatus = "instock"
	}
	images := make([]wcImage, 0, len(p.ImageURLs))
	for i, u := range p.ImageURLs {
		images = append(images, wcImage{ID: i + 1, Src: u})
	}
	categories := []wcTerm{}
	if p.Category != "" {
		categories = append(categories, wcTerm{ID: 1, Name: p.Category})
	}
	return wcProduct{
		ID:               p.ID,
		Name:             p.Name,
		Slug:             p.Slug,
		SKU:              p.SKU,
		Description:      p.Description,
		Price:            price,
		Images:           images,
		Weight:           p.Weight,
		Dimensions:       wcDims{Length: p.Length, Width: p.Width, Height: p.Height},
		Categories:       categories,
		Variations:       []int{},
		InStock:          p.InStock,
		StockStatus:      stockStatus,
		StockQuantity:    p.StockQuantity,
		ManageStock:      p.ManageStock,
		RegularPrice:     price,
		SalePrice:        "",
		ShortDescription: p.ShortDescription,
		Permalink:        fmt.Sprintf("%s/product/%s/", storeBaseURL, p.Slug),
		Type:             p.Type,
	}
}

// withAuth enforces HTTP Basic auth against the deterministic credentials
// for the store_id in the path, exactly as a real WooCommerce store would
// enforce Consumer Key/Secret — the real connector always sends them
// (client.go's setAuthHeadersForConfig), so this is a genuine check, not a
// formality.
func (s *server) withAuth(next func(w http.ResponseWriter, r *http.Request, storeID string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		storeID := r.PathValue("store_id")
		key, secret, ok := r.BasicAuth()
		if !ok || key != ConsumerKey(storeID) || secret != ConsumerSecret(storeID) {
			writeWCError(w, http.StatusUnauthorized, "woocommerce_rest_authentication_error", "Invalid consumer key or secret.")
			return
		}
		next(w, r, storeID)
	}
}

func writeWCError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": message, "data": map[string]any{"status": status}})
}

func (s *server) loadStore(ctx context.Context, w http.ResponseWriter, storeID string) (*Store, bool) {
	store, err := s.stores.Get(ctx, storeID)
	if errors.Is(err, simstore.ErrStoreNotFound) {
		writeWCError(w, http.StatusNotFound, "woocommerce_rest_shop_not_found", "No simulated store has been seeded for this store_id yet — run `sim seed --store "+storeID+"`.")
		return nil, false
	}
	if err != nil {
		s.log.Error("sim store: load failed", "store_id", storeID, "error", err)
		writeWCError(w, http.StatusInternalServerError, "woocommerce_rest_internal_error", err.Error())
		return nil, false
	}
	return store, true
}

// listProducts implements GET /products, matching every query shape the
// real connector issues: ?search=, ?sku=, ?per_page=&page=.
func (s *server) listProducts(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadStore(ctx, w, storeID)
	if !ok {
		return
	}

	skus := make([]string, 0, len(store.Products))
	for sku := range store.Products {
		skus = append(skus, sku)
	}
	sort.Strings(skus)

	var matched []*Product
	if sku := r.URL.Query().Get("sku"); sku != "" {
		if p, ok := store.Products[sku]; ok {
			matched = append(matched, p)
		}
	} else if search := r.URL.Query().Get("search"); search != "" && search != "*" {
		for _, sku := range skus {
			p := store.Products[sku]
			if strings.Contains(strings.ToLower(p.Name), strings.ToLower(search)) {
				matched = append(matched, p)
			}
		}
	} else {
		for _, sku := range skus {
			matched = append(matched, store.Products[sku])
		}
	}

	perPage := simstore.QueryInt(r, "per_page", len(matched))
	if perPage <= 0 {
		perPage = len(matched)
	}
	page := simstore.QueryInt(r, "page", 1)
	if page <= 0 {
		page = 1
	}
	start := (page - 1) * perPage
	end := start + perPage
	if start > len(matched) {
		start = len(matched)
	}
	if end > len(matched) {
		end = len(matched)
	}

	out := make([]wcProduct, 0, end-start)
	for _, p := range matched[start:end] {
		out = append(out, toWCProduct(p, s.storeBaseURL(storeID)))
	}

	simstore.WriteJSON(w, http.StatusOK, out)
}

func (s *server) getProduct(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadStore(ctx, w, storeID)
	if !ok {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeWCError(w, http.StatusNotFound, "woocommerce_rest_product_invalid_id", "Invalid product id.")
		return
	}
	for _, p := range store.Products {
		if p.ID == id {
			simstore.WriteJSON(w, http.StatusOK, toWCProduct(p, s.storeBaseURL(storeID)))
			return
		}
	}
	writeWCError(w, http.StatusNotFound, "woocommerce_rest_product_invalid_id", "Invalid product id.")
}

// updateProduct implements PUT /products/{id}: the remediation write-back
// path (woocommerce.Connector.UpdateProduct PUTs here). Only the fields the
// real connector actually sends are applied (name, description,
// short_description, regular_price, stock_quantity, manage_stock);
// everything else is accepted and ignored, matching a real WooCommerce
// store's tolerance of a partial PUT payload.
func (s *server) updateProduct(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadStore(ctx, w, storeID)
	if !ok {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeWCError(w, http.StatusNotFound, "woocommerce_rest_product_invalid_id", "Invalid product id.")
		return
	}

	var target *Product
	for _, p := range store.Products {
		if p.ID == id {
			target = p
			break
		}
	}
	if target == nil {
		writeWCError(w, http.StatusNotFound, "woocommerce_rest_product_invalid_id", "Invalid product id.")
		return
	}

	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeWCError(w, http.StatusBadRequest, "rest_invalid_param", "Invalid JSON body.")
		return
	}

	if v, ok := body["name"].(string); ok {
		target.Name = v
	}
	if v, ok := body["description"].(string); ok {
		target.Description = v
	}
	if v, ok := body["short_description"].(string); ok {
		target.ShortDescription = v
	}
	if v, ok := body["regular_price"].(string); ok {
		var f float64
		if _, err := fmt.Sscanf(v, "%f", &f); err == nil {
			target.PriceMinor = int64(f * 100)
		}
	}
	if v, ok := body["stock_quantity"].(float64); ok {
		q := int(v)
		target.StockQuantity = &q
		target.InStock = q > 0
	}
	if v, ok := body["manage_stock"].(bool); ok {
		target.ManageStock = v
	}

	target.UpdatedAt = store.UpdatedAt
	store.UpdatedAt = target.UpdatedAt

	if err := s.stores.Upsert(ctx, store); err != nil {
		s.log.Error("sim store: persist update failed", "store_id", storeID, "error", err)
		writeWCError(w, http.StatusInternalServerError, "woocommerce_rest_internal_error", err.Error())
		return
	}

	simstore.WriteJSON(w, http.StatusOK, toWCProduct(target, s.storeBaseURL(storeID)))
}

// createOrder implements POST /orders: the connector's CheckoutCreate,
// CheckoutCreate (cart), and CartCreate all POST here with a "pending"
// status and the cart's product_id/quantity line items.
func (s *server) createOrder(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadStore(ctx, w, storeID)
	if !ok {
		return
	}

	var req wcOrderWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeWCError(w, http.StatusBadRequest, "rest_invalid_param", "Invalid JSON body.")
		return
	}

	var billing, shipping OrderAddress
	if req.Billing != nil {
		billing = fromWCAddress(*req.Billing)
	}
	if req.Shipping != nil {
		shipping = fromWCAddress(*req.Shipping)
	}
	var shippingMethodID string
	if id := req.shippingMethodID(); id != nil {
		shippingMethodID = *id
	}

	var couponCodes []string
	if codes := req.couponCodes(); codes != nil {
		couponCodes = *codes
	}

	order, err := store.CreateOrder(req.lineItemMap(), billing, shipping, shippingMethodID, couponCodes)
	if err != nil {
		writeWCError(w, http.StatusBadRequest, "woocommerce_rest_cannot_create_order", err.Error())
		return
	}
	if consent := req.buyerConsent(); consent != nil {
		order.BuyerConsent = *consent
	}

	if err := s.stores.Upsert(ctx, store); err != nil {
		s.log.Error("sim store: persist order create failed", "store_id", storeID, "error", err)
		writeWCError(w, http.StatusInternalServerError, "woocommerce_rest_internal_error", err.Error())
		return
	}

	simstore.WriteJSON(w, http.StatusCreated, toWCOrder(order))
}

// getOrder implements GET /orders/{id}: the connector's CheckoutGet,
// CartGet, idempotent-replay path, and OrderGet (post-completion polling)
// all read here.
func (s *server) getOrder(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadStore(ctx, w, storeID)
	if !ok {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeWCError(w, http.StatusNotFound, "woocommerce_rest_shop_order_invalid_id", "Invalid order id.")
		return
	}
	order, ok := store.GetOrder(id)
	if !ok {
		writeWCError(w, http.StatusNotFound, "woocommerce_rest_shop_order_invalid_id", "Invalid order id.")
		return
	}
	simstore.WriteJSON(w, http.StatusOK, toWCOrder(order))
}

// updateOrder implements PUT /orders/{id}: the connector's CheckoutUpdate
// (buyer/fulfillment), CheckoutComplete (status -> "processing"), and
// CheckoutCancel (status -> "cancelled") all PUT here — WooCommerce order
// status IS checkout session state, there is no separate resource.
func (s *server) updateOrder(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadStore(ctx, w, storeID)
	if !ok {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeWCError(w, http.StatusNotFound, "woocommerce_rest_shop_order_invalid_id", "Invalid order id.")
		return
	}

	var req wcOrderWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeWCError(w, http.StatusBadRequest, "rest_invalid_param", "Invalid JSON body.")
		return
	}

	var status string
	if req.Status != nil {
		status = *req.Status
	}

	// A completion (status -> processing) carrying the reserved failure token
	// is declined, the way a real payment processor would reject a bad
	// instrument — a business outcome (400), not a store fault. "fail_token"
	// is the official conformance suite's convention for the failure path.
	if status == "processing" && req.PaymentToken == "fail_token" {
		writeWCError(w, http.StatusBadRequest, "woocommerce_rest_payment_failed", "payment was declined by the processor")
		return
	}

	var billing, shipping *OrderAddress
	if req.Billing != nil {
		a := fromWCAddress(*req.Billing)
		billing = &a
	}
	if req.Shipping != nil {
		a := fromWCAddress(*req.Shipping)
		shipping = &a
	}

	order, err := store.UpdateOrder(id, status, req.lineItemMap(), billing, shipping, req.shippingMethodID(), req.couponCodes())
	if err == nil {
		if consent := req.buyerConsent(); consent != nil {
			order.BuyerConsent = *consent
		}
	}
	if err != nil {
		if errors.Is(err, simstore.ErrProductNotFound) {
			writeWCError(w, http.StatusBadRequest, "woocommerce_rest_cannot_update_order", err.Error())
			return
		}
		if errors.Is(err, simstore.ErrFulfillmentRequired) {
			writeWCError(w, http.StatusBadRequest, "woocommerce_rest_missing_fulfillment", err.Error())
			return
		}
		writeWCError(w, http.StatusNotFound, "woocommerce_rest_shop_order_invalid_id", err.Error())
		return
	}

	if err := s.stores.Upsert(ctx, store); err != nil {
		s.log.Error("sim store: persist order update failed", "store_id", storeID, "error", err)
		writeWCError(w, http.StatusInternalServerError, "woocommerce_rest_internal_error", err.Error())
		return
	}

	simstore.WriteJSON(w, http.StatusOK, toWCOrder(order))
}

// listShippingZones implements GET /shipping/zones: one zone covering
// everywhere. (WooCommerce's implicit zone 0, "Locations not covered", is not
// part of this list, as on a real store.)
func (s *server) listShippingZones(w http.ResponseWriter, r *http.Request, storeID string) {
	if _, ok := s.loadStore(r.Context(), w, storeID); !ok {
		return
	}
	simstore.WriteJSON(w, http.StatusOK, []map[string]any{{"id": 1, "name": "Everywhere"}})
}

// listZoneMethods implements GET /shipping/zones/{id}/methods: the flat rate,
// plus Local Pickup when the merchant has it enabled.
func (s *server) listZoneMethods(w http.ResponseWriter, r *http.Request, storeID string) {
	store, ok := s.loadStore(r.Context(), w, storeID)
	if !ok {
		return
	}
	methods := []map[string]any{}
	if r.PathValue("zone_id") == "1" {
		methods = append(methods, map[string]any{"method_id": "flat_rate", "enabled": true})
		if store.LocalPickup {
			methods = append(methods, map[string]any{"method_id": localPickupMethodID, "enabled": true})
		}
	}
	simstore.WriteJSON(w, http.StatusOK, methods)
}

// getGeneralSettings implements GET /settings/general: the store address.
func (s *server) getGeneralSettings(w http.ResponseWriter, r *http.Request, storeID string) {
	if _, ok := s.loadStore(r.Context(), w, storeID); !ok {
		return
	}
	out := make([]map[string]any, 0, len(storeAddress))
	for _, a := range storeAddress {
		out = append(out, map[string]any{"id": a.id, "value": a.value})
	}
	simstore.WriteJSON(w, http.StatusOK, out)
}
