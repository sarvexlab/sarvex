// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package sim is Magento's simulated store: the subset of the Magento 2.x REST
// API the Magento connector calls — admin-token issuance, product
// get/search/paging/SKU lookup, product PUT (remediation write-back), the
// guest-cart lifecycle (create, add items, shipping-information,
// estimate-shipping-methods, payment-information) and order GET — over the
// same platform-neutral simstore.Store model every simulated platform serves:
// one simulated merchant catalog, one wire adapter per platform, matching how
// Sarvex models "merchant -> store -> connector" (sarvex-v1-architecture.md).
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
	"github.com/sarvexlab/sarvex/internal/platform/magento"
)

// Surface is Magento's simstore.Surface.
type Surface struct{}

var _ simstore.Surface = Surface{}

// server holds what the REST handlers need from the hosting simulator.
type server struct {
	stores simstore.StoreRepository
	log    *slog.Logger
}

// AdminUser deterministically derives the Magento admin API username a
// simulated store's connector authenticates with, from its store_id alone.
func AdminUser(storeID string) string {
	return "admin_sim_" + storeID
}

// AdminPassword deterministically derives the Magento admin API password,
// from its store_id alone.
func AdminPassword(storeID string) string {
	return "pw_sim_" + storeID
}

// StoreCode is the Magento store view code every simulated store serves
// under — the sim doesn't model multiple store views per store_id.
const StoreCode = "default"

// Credentials implements simstore.Surface: the store_credentials_magento
// payload (sarvex-v1-control-plane.yaml).
func (Surface) Credentials(simServerBaseURL, storeID string) map[string]any {
	return map[string]any{
		"base_url":       simstore.StoreBaseURL(simServerBaseURL, storeID),
		"admin_user":     AdminUser(storeID),
		"admin_password": AdminPassword(storeID),
		"store_code":     StoreCode,
		"webhook_secret": simstore.WebhookSecret(storeID),
		"store_currency": "USD",
	}
}

// Connector implements simstore.Surface.
func (Surface) Connector(simServerBaseURL, merchantID, storeID string) (base.Connector, error) {
	return magento.NewConnector(magento.MerchantConfig{
		MerchantID: merchantID, StoreID: storeID,
		BaseURL:   simstore.StoreBaseURL(simServerBaseURL, storeID),
		AdminUser: AdminUser(storeID), AdminPassword: AdminPassword(storeID),
		StoreCode: StoreCode, StoreCurrency: "USD",
		WebhookSecret: simstore.WebhookSecret(storeID), PaymentMethodCode: "checkmo",
		RequestTimeout: 5 * time.Second, MaxRetries: 1,
	}, nil, resilience.Config{}, nil)
}

// CatalogID implements simstore.Surface: Magento's catalog is keyed by SKU.
func (Surface) CatalogID(p *simstore.Product) string { return p.SKU }

// PickupLocationID implements simstore.Surface.
func (Surface) PickupLocationID() string { return simPickupSourceCode }

// The simulated store model, shared by every platform's surface.
type (
	Store         = simstore.Store
	Product       = simstore.Product
	Order         = simstore.Order
	OrderLineItem = simstore.OrderLineItem
	OrderAddress  = simstore.OrderAddress
)

// mgProduct is the Magento REST API product wire shape, matching
// internal/platform/magento's mgProduct exactly — this server
// exists to be pulled by that real connector, so the shapes must agree.
type mgProduct struct {
	ID                  int                    `json:"id"`
	SKU                 string                 `json:"sku"`
	Name                string                 `json:"name"`
	Price               float64                `json:"price"`
	TypeID              string                 `json:"type_id"`
	Visibility          int                    `json:"visibility"`
	Status              int                    `json:"status"`
	Weight              float64                `json:"weight,omitempty"`
	CustomAttributes    []mgCustomAttribute    `json:"custom_attributes,omitempty"`
	ExtensionAttributes *mgExtensionAttributes `json:"extension_attributes,omitempty"`
	MediaGalleryEntries []mgMediaGalleryEntry  `json:"media_gallery_entries,omitempty"`
}

type mgExtensionAttributes struct {
	StockItem *mgStockItem `json:"stock_item,omitempty"`
}

type mgStockItem struct {
	IsInStock   bool    `json:"is_in_stock"`
	Qty         float64 `json:"qty"`
	ManageStock bool    `json:"manage_stock,omitempty"`
}

type mgCustomAttribute struct {
	AttributeCode string `json:"attribute_code"`
	Value         any    `json:"value"`
}

type mgMediaGalleryEntry struct {
	ID        int    `json:"id"`
	MediaType string `json:"media_type"`
	URL       string `json:"file"`
	Label     string `json:"label,omitempty"`
}

type mgSearchResponse struct {
	Items      []mgProduct    `json:"items"`
	TotalCount int            `json:"total_count"`
	Meta       map[string]any `json:"search_criteria,omitempty"`
}

// mgCart is the quote resource. Like Magento's own, it carries no totals (they
// are a separate endpoint) but does carry the shipping assignment and the
// guest's contact once recorded.
type mgCart struct {
	ID             string                `json:"id"`
	Items          []mgCartItem          `json:"items,omitempty"`
	BillingAddress *mgAddress            `json:"billing_address,omitempty"`
	Customer       *mgCartCustomer       `json:"customer,omitempty"`
	ExtensionAttrs *mgCartExtensionAttrs `json:"extension_attributes,omitempty"`
}

type mgCartCustomer struct {
	Email string `json:"email,omitempty"`
}

type mgCartExtensionAttrs struct {
	ShippingAssignments []mgShippingAssignment `json:"shipping_assignments,omitempty"`
}

type mgShippingAssignment struct {
	Shipping *mgShippingInfo `json:"shipping,omitempty"`
}

type mgShippingInfo struct {
	Address *mgAddress `json:"address,omitempty"`
	Method  string     `json:"method,omitempty"`
}

type mgCartItem struct {
	ItemID    int     `json:"item_id"`
	SKU       string  `json:"sku"`
	Qty       float64 `json:"qty"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	ProductID int     `json:"product_id"`
}

// mgCartTotals is the /totals resource. As on a real store, discount_amount is
// negative.
type mgCartTotals struct {
	GrandTotal       float64 `json:"grand_total"`
	Subtotal         float64 `json:"subtotal"`
	TaxAmount        float64 `json:"tax_amount"`
	ShippingAmount   float64 `json:"shipping_amount"`
	DiscountAmount   float64 `json:"discount_amount"`
	CouponCode       string  `json:"coupon_code,omitempty"`
	BaseCurrencyCode string  `json:"base_currency_code"`
}

type mgAddress struct {
	FirstName  string   `json:"firstname"`
	LastName   string   `json:"lastname"`
	Street     []string `json:"street"`
	City       string   `json:"city"`
	Region     string   `json:"region"`
	PostalCode string   `json:"postcode"`
	Country    string   `json:"country_id"`
	Email      string   `json:"email,omitempty"`
	Telephone  string   `json:"telephone,omitempty"`

	ExtensionAttributes *mgAddressExtension `json:"extension_attributes,omitempty"`
}

type mgAddressExtension struct {
	PickupLocationCode string `json:"pickup_location_code,omitempty"`
}

type mgShippingMethod struct {
	CarrierCode string  `json:"carrier_code"`
	MethodCode  string  `json:"method_code"`
	MethodTitle string  `json:"method_title"`
	Amount      float64 `json:"amount"`
}

type mgOrder struct {
	EntityID       int           `json:"entity_id"`
	IncrementID    string        `json:"increment_id"`
	Status         string        `json:"status"`
	State          string        `json:"state"`
	Currency       string        `json:"order_currency_code"`
	CreatedAt      string        `json:"created_at"`
	UpdatedAt      string        `json:"updated_at"`
	GrandTotal     float64       `json:"grand_total"`
	Subtotal       float64       `json:"subtotal"`
	TaxAmount      float64       `json:"tax_amount"`
	ShippingAmount float64       `json:"shipping_amount"`
	DiscountAmount float64       `json:"discount_amount"`
	CouponCode     string        `json:"coupon_code,omitempty"`
	Items          []mgOrderItem `json:"items,omitempty"`
	BillingAddress *mgAddress    `json:"billing_address,omitempty"`

	ExtensionAttributes *mgOrderExtensionAttributes `json:"extension_attributes,omitempty"`
}

type mgOrderExtensionAttributes struct {
	ShippingAssignments []mgShippingAssignment `json:"shipping_assignments,omitempty"`
}

type mgOrderItem struct {
	ItemID    int     `json:"item_id"`
	SKU       string  `json:"sku"`
	ProductID int     `json:"product_id"`
	Name      string  `json:"name"`
	Qty       float64 `json:"qty_ordered"`
	Price     float64 `json:"price"`
	RowTotal  float64 `json:"row_total"`
}

// mgCartItemRequest is the body shape of POST /guest-carts/{id}/items and
// PUT /guest-carts/{id}/items/{item_id}: one cart item, addressed by SKU (add)
// or by item id (set quantity).
type mgCartItemRequest struct {
	CartItem struct {
		SKU string  `json:"sku"`
		Qty float64 `json:"qty"`
	} `json:"cartItem"`
}

// mgShippingInformationRequest is the body shape of POST
// /guest-carts/{id}/shipping-information.
type mgShippingInformationRequest struct {
	AddressInformation struct {
		ShippingAddress     *mgAddress `json:"shipping_address"`
		ShippingCarrierCode string     `json:"shipping_carrier_code"`
		ShippingMethodCode  string     `json:"shipping_method_code"`
	} `json:"addressInformation"`
}

// mgPaymentInformationRequest is the body shape of POST
// /guest-carts/{id}/payment-information.
type mgPaymentInformationRequest struct {
	Email         string `json:"email"`
	PaymentMethod struct {
		Method         string         `json:"method"`
		AdditionalData map[string]any `json:"additional_data"`
	} `json:"paymentMethod"`
	BillingAddress *mgAddress `json:"billingAddress"`
}

// mgProductWriteRequest is the body shape of PUT /products/{sku} — the
// remediation write-back path (magento.Connector.UpdateProduct). Only the
// fields the real connector actually sends are applied (name, price via
// custom_attributes description/short_description, and
// extension_attributes.stock_item.{qty,manage_stock}); everything else is
// accepted and ignored, matching a real Magento store's tolerance of a
// partial PUT payload — see the WooCommerce surface's updateProduct (internal/platform/woocommerce/sim) for the WooCommerce
// equivalent this mirrors.
type mgProductWriteRequest struct {
	Product struct {
		SKU                 string                 `json:"sku"`
		Name                string                 `json:"name"`
		Price               *float64               `json:"price"`
		CustomAttributes    []mgCustomAttribute    `json:"custom_attributes,omitempty"`
		ExtensionAttributes *mgExtensionAttributes `json:"extension_attributes,omitempty"`
	} `json:"product"`
}

// toMgProduct renders a persisted Product in the Magento wire shape. Status
// is always 1 ("enabled") — the sim has no separate enabled/disabled concept;
// availability lives entirely on the stock item, matching how the real
// magento.Connector derives availability (mapper.go's isProductAvailable
// checks StockItem, falling back to Status only when no stock item is
// present at all).
func toMgProduct(p *Product) mgProduct {
	product := mgProduct{
		ID:     p.ID,
		SKU:    p.SKU,
		Name:   p.Name,
		Price:  float64(p.PriceMinor) / 100,
		TypeID: firstNonEmpty(p.Type, "simple"),
		Status: 1,
		CustomAttributes: []mgCustomAttribute{
			{AttributeCode: "short_description", Value: p.ShortDescription},
			{AttributeCode: "description", Value: p.Description},
			{AttributeCode: "url_key", Value: p.Slug},
		},
	}
	if p.Weight != "" {
		if w, err := strconv.ParseFloat(p.Weight, 64); err == nil {
			product.Weight = w
		}
	}
	if p.Length != "" && p.Width != "" && p.Height != "" {
		product.CustomAttributes = append(product.CustomAttributes,
			mgCustomAttribute{AttributeCode: "ts_dimensions_length", Value: p.Length},
			mgCustomAttribute{AttributeCode: "ts_dimensions_width", Value: p.Width},
			mgCustomAttribute{AttributeCode: "ts_dimensions_height", Value: p.Height},
		)
	}
	for i, u := range p.ImageURLs {
		product.MediaGalleryEntries = append(product.MediaGalleryEntries, mgMediaGalleryEntry{
			ID: i + 1, MediaType: "image", URL: imagePath(u),
		})
	}
	if p.StockQuantity != nil {
		product.ExtensionAttributes = &mgExtensionAttributes{
			StockItem: &mgStockItem{
				IsInStock: p.InStock,
				Qty:       float64(*p.StockQuantity),
			},
		}
	} else {
		product.ExtensionAttributes = &mgExtensionAttributes{
			StockItem: &mgStockItem{IsInStock: p.InStock},
		}
	}
	return product
}

// imagePath turns a seeded absolute image URL into the relative gallery path
// Magento serves ("/<file>"); the connector rebuilds the absolute URL from
// the store's media base.
func imagePath(u string) string {
	if i := strings.LastIndex(u, "/"); i >= 0 {
		u = u[i+1:]
	}
	return "/" + u
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// simPickupSourceCode / simPickupSourceName identify the simulated store's one
// inventory source, which is a pickup location when the merchant enables pickup.
const (
	simPickupSourceCode = "sim_store"
	simPickupSourceName = "Simulated Store"
)

// toMgShippingAssignment is the quote's / order's shipping assignment: the
// chosen method and the destination. A pickup order's address carries the
// pickup location code.
func toMgShippingAssignment(o *Order) []mgShippingAssignment {
	if o.ShippingMethodID == "" && !simstore.HasShippingAddress(o.ShippingAddress) {
		return nil
	}
	info := &mgShippingInfo{Method: o.ShippingMethodID}
	if addr := toMgAddressPtr(o.ShippingAddress); addr != nil {
		if simstore.IsPickupMethod(o.ShippingMethodID) {
			addr.ExtensionAttributes = &mgAddressExtension{PickupLocationCode: simPickupSourceCode}
		}
		info.Address = addr
	}
	return []mgShippingAssignment{{Shipping: info}}
}

// mgItemID is a cart line's item id: the product id, stable across other
// lines being added or removed (as a real quote item's id is).
func mgItemID(li OrderLineItem) int { return li.ProductID }

// toMgCart renders a persisted Order as the quote resource.
func toMgCart(o *Order) mgCart {
	items := make([]mgCartItem, len(o.LineItems))
	for i, li := range o.LineItems {
		items[i] = mgCartItem{
			ItemID:    mgItemID(li),
			SKU:       li.SKU,
			Qty:       float64(li.Quantity),
			Name:      li.Name,
			Price:     float64(li.PriceMinor) / 100,
			ProductID: li.ProductID,
		}
	}
	cart := mgCart{
		ID:             strconv.Itoa(o.ID),
		Items:          items,
		BillingAddress: toMgAddressPtr(o.BillingAddress),
	}
	if o.BillingAddress.Email != "" {
		cart.Customer = &mgCartCustomer{Email: o.BillingAddress.Email}
	}
	if sa := toMgShippingAssignment(o); sa != nil {
		cart.ExtensionAttrs = &mgCartExtensionAttrs{ShippingAssignments: sa}
	}
	return cart
}

// toMgTotals renders the /totals resource.
func toMgTotals(o *Order) mgCartTotals {
	subtotalMinor := simstore.OrderSubtotalMinor(o.LineItems)
	grandTotalMinor := subtotalMinor + o.ShippingLineMinor + o.TaxMinor - o.DiscountMinor
	code := ""
	if len(o.DiscountCodes) > 0 {
		code = o.DiscountCodes[0]
	}
	return mgCartTotals{
		GrandTotal:       float64(grandTotalMinor) / 100,
		Subtotal:         float64(subtotalMinor) / 100,
		TaxAmount:        float64(o.TaxMinor) / 100,
		ShippingAmount:   float64(o.ShippingLineMinor) / 100,
		DiscountAmount:   -float64(o.DiscountMinor) / 100,
		CouponCode:       code,
		BaseCurrencyCode: o.Currency,
	}
}

func toMgOrder(o *Order) mgOrder {
	items := make([]mgOrderItem, len(o.LineItems))
	for i, li := range o.LineItems {
		items[i] = mgOrderItem{
			ItemID:    mgItemID(li),
			SKU:       li.SKU,
			ProductID: li.ProductID,
			Name:      li.Name,
			Qty:       float64(li.Quantity),
			Price:     float64(li.PriceMinor) / 100,
			RowTotal:  float64(li.PriceMinor*int64(li.Quantity)) / 100,
		}
	}
	t := toMgTotals(o)
	idStr := strconv.Itoa(o.ID)
	out := mgOrder{
		EntityID:       o.ID,
		IncrementID:    idStr,
		Status:         magentoOrderStatus(o.Status),
		State:          magentoOrderStatus(o.Status),
		Currency:       o.Currency,
		CreatedAt:      o.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      o.UpdatedAt.Format(time.RFC3339),
		GrandTotal:     t.GrandTotal,
		Subtotal:       t.Subtotal,
		TaxAmount:      t.TaxAmount,
		ShippingAmount: t.ShippingAmount,
		DiscountAmount: t.DiscountAmount,
		CouponCode:     t.CouponCode,
		Items:          items,
		BillingAddress: toMgAddressPtr(o.BillingAddress),
	}
	if sa := toMgShippingAssignment(o); sa != nil {
		out.ExtensionAttributes = &mgOrderExtensionAttributes{ShippingAssignments: sa}
	}
	return out
}

// magentoOrderStatus maps the sim's platform-agnostic order status
// (pending/processing/cancelled, shared with the WooCommerce surface) onto
// Magento's own status vocabulary, which magento.Connector's mapper
// understands (mapper.go's orderStatusToCheckoutStatus).
func magentoOrderStatus(status string) string {
	switch status {
	case "processing":
		return "processing"
	case "cancelled":
		return "cancelled"
	default:
		return "pending"
	}
}

func toMgAddress(a OrderAddress) mgAddress {
	street := []string{a.Address1}
	if a.Address2 != "" {
		street = append(street, a.Address2)
	}
	return mgAddress{
		FirstName:  a.FirstName,
		LastName:   a.LastName,
		Street:     street,
		City:       a.City,
		Region:     a.State,
		PostalCode: a.Postcode,
		Country:    a.Country,
		Email:      a.Email,
		Telephone:  a.Phone,
	}
}

func toMgAddressPtr(a OrderAddress) *mgAddress {
	if !simstore.HasShippingAddress(a) {
		return nil
	}
	addr := toMgAddress(a)
	return &addr
}

func fromMgAddress(a mgAddress) OrderAddress {
	addr1, addr2 := "", ""
	if len(a.Street) > 0 {
		addr1 = a.Street[0]
	}
	if len(a.Street) > 1 {
		addr2 = a.Street[1]
	}
	return OrderAddress{
		FirstName: a.FirstName,
		LastName:  a.LastName,
		Address1:  addr1,
		Address2:  addr2,
		City:      a.City,
		State:     a.Region,
		Postcode:  a.PostalCode,
		Country:   a.Country,
		Email:     a.Email,
		Phone:     a.Telephone,
	}
}

// registerMagentoRoutes adds the Magento REST surface to mux, serving the
// same store repository the WooCommerce routes in NewServer do. Path shape
// mirrors a real Magento instance's own routing
// (BaseURL + "/rest/" + StoreCode + "/V1" + ...), with the store_id segment
// this sim multiplexes on standing in for BaseURL: a simulated store's
// magento.MerchantConfig.BaseURL is simstore.StoreBaseURL(simServerURL,
// storeID), so /rest/all/V1/... and /rest/{store_code}/V1/... land under
// /stores/{store_id}/... exactly like every other route this server serves.
// Register implements simstore.Surface.
func (Surface) Register(mux *http.ServeMux, env simstore.Env) {
	s := &server{stores: env.Stores, log: env.Log}
	mux.HandleFunc("POST /stores/{store_id}/rest/all/V1/integration/admin/token", s.magentoIssueToken)
	mux.HandleFunc("GET /stores/{store_id}/rest/{store_code}/V1/products", s.withMagentoAuth(s.magentoListProducts))
	mux.HandleFunc("GET /stores/{store_id}/rest/{store_code}/V1/products/{sku}", s.withMagentoAuth(s.magentoGetProduct))
	mux.HandleFunc("PUT /stores/{store_id}/rest/{store_code}/V1/products/{sku}", s.withMagentoAuth(s.magentoUpdateProduct))
	mux.HandleFunc("POST /stores/{store_id}/rest/{store_code}/V1/guest-carts", s.withMagentoAuth(s.magentoCreateCart))
	mux.HandleFunc("GET /stores/{store_id}/rest/{store_code}/V1/guest-carts/{cart_id}", s.withMagentoAuth(s.magentoGetCart))
	mux.HandleFunc("GET /stores/{store_id}/rest/{store_code}/V1/guest-carts/{cart_id}/totals", s.withMagentoAuth(s.magentoGetCartTotals))
	mux.HandleFunc("POST /stores/{store_id}/rest/{store_code}/V1/guest-carts/{cart_id}/items", s.withMagentoAuth(s.magentoAddCartItem))
	mux.HandleFunc("PUT /stores/{store_id}/rest/{store_code}/V1/guest-carts/{cart_id}/items/{item_id}", s.withMagentoAuth(s.magentoSetCartItemQty))
	mux.HandleFunc("DELETE /stores/{store_id}/rest/{store_code}/V1/guest-carts/{cart_id}/items/{item_id}", s.withMagentoAuth(s.magentoDeleteCartItem))
	mux.HandleFunc("PUT /stores/{store_id}/rest/{store_code}/V1/guest-carts/{cart_id}/coupons/{code}", s.withMagentoAuth(s.magentoApplyCoupon))
	mux.HandleFunc("DELETE /stores/{store_id}/rest/{store_code}/V1/guest-carts/{cart_id}/coupons", s.withMagentoAuth(s.magentoClearCoupon))
	mux.HandleFunc("GET /stores/{store_id}/rest/{store_code}/V1/inventory/sources", s.withMagentoAuth(s.magentoListSources))
	mux.HandleFunc("POST /stores/{store_id}/rest/{store_code}/V1/orders/{order_id}/comments", s.withMagentoAuth(s.magentoAddOrderComment))
	mux.HandleFunc("POST /stores/{store_id}/rest/{store_code}/V1/guest-carts/{cart_id}/shipping-information", s.withMagentoAuth(s.magentoSetShippingInformation))
	mux.HandleFunc("POST /stores/{store_id}/rest/{store_code}/V1/guest-carts/{cart_id}/estimate-shipping-methods", s.withMagentoAuth(s.magentoEstimateShippingMethods))
	mux.HandleFunc("POST /stores/{store_id}/rest/{store_code}/V1/guest-carts/{cart_id}/payment-information", s.withMagentoAuth(s.magentoPaymentInformation))
	mux.HandleFunc("GET /stores/{store_id}/rest/{store_code}/V1/orders/{order_id}", s.withMagentoAuth(s.magentoGetOrder))
}

func writeMgError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"message": message})
}

// magentoIssueToken implements POST /integration/admin/token: validates the
// deterministic sim admin credentials for store_id and, on success, returns
// a deterministic bearer token as a bare JSON string — matching the real
// Magento admin-token endpoint's actual (non-object) response shape, which
// magento.Connector's httpClient.refreshToken specifically unmarshals as
// such.
func (s *server) magentoIssueToken(w http.ResponseWriter, r *http.Request) {
	storeID := r.PathValue("store_id")

	var creds struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		writeMgError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}

	if creds.Username != AdminUser(storeID) || creds.Password != AdminPassword(storeID) {
		writeMgError(w, http.StatusUnauthorized, "The account sign-in was incorrect or your account is disabled temporarily.")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(magentoBearerToken(storeID))
}

func magentoBearerToken(storeID string) string {
	return "admintok_" + storeID
}

// withMagentoAuth enforces the Bearer admin token every authenticated
// Magento request carries (client.go's setAuthHeaders), exactly as a real
// Magento instance would reject a missing/stale token.
func (s *server) withMagentoAuth(next func(w http.ResponseWriter, r *http.Request, storeID string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		storeID := r.PathValue("store_id")
		auth := r.Header.Get("Authorization")
		want := "Bearer " + magentoBearerToken(storeID)
		if auth != want {
			writeMgError(w, http.StatusUnauthorized, "Consumer is not authorized to access %resources%")
			return
		}
		next(w, r, storeID)
	}
}

// magentoListProducts implements GET /products, matching both query shapes
// the real connector issues: a name "like" filter_groups search
// (CatalogSearch) and a plain page_size/current_page listing
// (FetchCatalog/Ping). A literal "*" filter value (CatalogSearch's default
// when no query is given) is treated as no-filter, the same convention
// the WooCommerce surface's listProducts uses for its own "*" search default.
func (s *server) magentoListProducts(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadMgStore(ctx, w, storeID)
	if !ok {
		return
	}

	skus := make([]string, 0, len(store.Products))
	for sku := range store.Products {
		skus = append(skus, sku)
	}
	sort.Strings(skus)

	nameFilter := r.URL.Query().Get("searchCriteria[filter_groups][0][filters][0][value]")

	var matched []*Product
	if nameFilter != "" && nameFilter != "*" {
		for _, sku := range skus {
			p := store.Products[sku]
			if strings.Contains(strings.ToLower(p.Name), strings.ToLower(nameFilter)) {
				matched = append(matched, p)
			}
		}
	} else {
		for _, sku := range skus {
			matched = append(matched, store.Products[sku])
		}
	}

	pageSize := simstore.QueryInt(r, "searchCriteria[page_size]", len(matched))
	if pageSize <= 0 {
		pageSize = len(matched)
	}
	page := simstore.QueryInt(r, "searchCriteria[current_page]", 1)
	if page <= 0 {
		page = 1
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > len(matched) {
		start = len(matched)
	}
	if end > len(matched) {
		end = len(matched)
	}

	items := make([]mgProduct, 0, end-start)
	for _, p := range matched[start:end] {
		items = append(items, toMgProduct(p))
	}

	simstore.WriteJSON(w, http.StatusOK, mgSearchResponse{Items: items, TotalCount: len(matched)})
}

func (s *server) magentoGetProduct(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadMgStore(ctx, w, storeID)
	if !ok {
		return
	}
	p, ok := store.Products[r.PathValue("sku")]
	if !ok {
		writeMgError(w, http.StatusNotFound, "The product that was requested doesn't exist. Verify the product and try again.")
		return
	}
	simstore.WriteJSON(w, http.StatusOK, toMgProduct(p))
}

// magentoUpdateProduct implements PUT /products/{sku}, the remediation
// write-back path (magento.Connector.UpdateProduct).
func (s *server) magentoUpdateProduct(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadMgStore(ctx, w, storeID)
	if !ok {
		return
	}
	sku := r.PathValue("sku")
	target, ok := store.Products[sku]
	if !ok {
		writeMgError(w, http.StatusNotFound, "The product that was requested doesn't exist. Verify the product and try again.")
		return
	}

	var req mgProductWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMgError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}

	if req.Product.Name != "" {
		target.Name = req.Product.Name
	}
	if req.Product.Price != nil {
		target.PriceMinor = int64(*req.Product.Price * 100)
	}
	for _, attr := range req.Product.CustomAttributes {
		val, _ := attr.Value.(string)
		switch attr.AttributeCode {
		case "description":
			target.Description = val
		case "short_description":
			target.ShortDescription = val
		}
	}
	if req.Product.ExtensionAttributes != nil && req.Product.ExtensionAttributes.StockItem != nil {
		stock := req.Product.ExtensionAttributes.StockItem
		q := int(stock.Qty)
		target.StockQuantity = &q
		target.InStock = stock.IsInStock
		target.ManageStock = stock.ManageStock
	}

	target.UpdatedAt = time.Now().UTC()
	store.UpdatedAt = target.UpdatedAt

	if err := s.stores.Upsert(ctx, store); err != nil {
		s.log.Error("sim store: persist magento product update failed", "store_id", storeID, "error", err)
		writeMgError(w, http.StatusInternalServerError, err.Error())
		return
	}

	simstore.WriteJSON(w, http.StatusOK, toMgProduct(target))
}

// magentoCreateCart implements POST /guest-carts: creates an empty pending
// order directly (bypassing Store.CreateOrder, which requires at least one
// line item — a real Magento guest cart is created empty, then populated by
// a separate POST .../items call, so that validation doesn't apply here).
// Returns the masked cart id as a bare JSON string, matching Magento's own
// non-object response shape for this endpoint.
func (s *server) magentoCreateCart(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadMgStore(ctx, w, storeID)
	if !ok {
		return
	}

	id := store.NextOrderID
	store.NextOrderID++
	now := time.Now().UTC()
	order := &Order{
		ID:        id,
		Status:    "pending",
		Currency:  "USD",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if store.Orders == nil {
		store.Orders = make(map[string]*Order)
	}
	store.Orders[strconv.Itoa(id)] = order
	store.UpdatedAt = now

	if err := s.stores.Upsert(ctx, store); err != nil {
		s.log.Error("sim store: persist magento cart create failed", "store_id", storeID, "error", err)
		writeMgError(w, http.StatusInternalServerError, err.Error())
		return
	}

	simstore.WriteJSON(w, http.StatusOK, strconv.Itoa(id))
}

func (s *server) magentoGetCart(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadMgStore(ctx, w, storeID)
	if !ok {
		return
	}
	order, ok := s.loadMgCart(w, store, r.PathValue("cart_id"))
	if !ok {
		return
	}
	simstore.WriteJSON(w, http.StatusOK, toMgCart(order))
}

// magentoGetCartTotals implements GET /guest-carts/{id}/totals.
func (s *server) magentoGetCartTotals(w http.ResponseWriter, r *http.Request, storeID string) {
	store, ok := s.loadMgStore(r.Context(), w, storeID)
	if !ok {
		return
	}
	order, ok := s.loadMgCart(w, store, r.PathValue("cart_id"))
	if !ok {
		return
	}
	simstore.WriteJSON(w, http.StatusOK, toMgTotals(order))
}

// persistMg saves the store after a mutation, answering 500 on failure.
func (s *server) persistMg(ctx context.Context, w http.ResponseWriter, store *Store, what string) bool {
	if err := s.stores.Upsert(ctx, store); err != nil {
		s.log.Error("sim store: persist magento "+what+" failed", "store_id", store.StoreID, "error", err)
		writeMgError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	return true
}

// currentLines is the order's lines as a product_id -> quantity map.
func currentLines(o *Order) map[int]int {
	m := make(map[int]int, len(o.LineItems))
	for _, li := range o.LineItems {
		m[li.ProductID] = li.Quantity
	}
	return m
}

// setLines replaces the order's lines with lines. UpdateOrder ignores an empty
// set, so emptying a cart is done here directly.
func setLines(store *Store, o *Order, lines map[int]int) error {
	if len(lines) == 0 {
		o.LineItems = nil
		_, o.DiscountMinor = simstore.ApplyCoupons(o.DiscountCodes, 0)
		o.UpdatedAt = time.Now().UTC()
		store.UpdatedAt = o.UpdatedAt
		return nil
	}
	_, err := store.UpdateOrder(o.ID, "", lines, nil, nil, nil, nil)
	return err
}

// magentoAddCartItem implements POST /guest-carts/{id}/items: adds one item by
// SKU. As on a real store, adding a SKU already in the cart increases its
// quantity rather than replacing it.
func (s *server) magentoAddCartItem(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadMgStore(ctx, w, storeID)
	if !ok {
		return
	}
	order, ok := s.loadMgCart(w, store, r.PathValue("cart_id"))
	if !ok {
		return
	}

	var req mgCartItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMgError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	p, ok := store.Products[req.CartItem.SKU]
	if !ok {
		writeMgError(w, http.StatusBadRequest, fmt.Sprintf("Could not find a product with SKU %q", req.CartItem.SKU))
		return
	}

	lines := currentLines(order)
	lines[p.ID] += int(req.CartItem.Qty)
	if err := setLines(store, order, lines); err != nil {
		writeMgError(w, http.StatusBadRequest, magentoStockMessage(err))
		return
	}
	if !s.persistMg(ctx, w, store, "cart item") {
		return
	}
	simstore.WriteJSON(w, http.StatusOK, map[string]any{"sku": p.SKU, "qty": lines[p.ID]})
}

// magentoStockMessage renders a store error the way Magento words it.
func magentoStockMessage(err error) string {
	if errors.Is(err, simstore.ErrOutOfStock) {
		return "The requested qty is not available"
	}
	return err.Error()
}

// cartItemByID finds a cart line by its item id.
func cartItemByID(o *Order, itemID string) (OrderLineItem, bool) {
	id, err := strconv.Atoi(itemID)
	if err != nil {
		return OrderLineItem{}, false
	}
	for _, li := range o.LineItems {
		if mgItemID(li) == id {
			return li, true
		}
	}
	return OrderLineItem{}, false
}

// magentoSetCartItemQty implements PUT /guest-carts/{id}/items/{item_id}.
func (s *server) magentoSetCartItemQty(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadMgStore(ctx, w, storeID)
	if !ok {
		return
	}
	order, ok := s.loadMgCart(w, store, r.PathValue("cart_id"))
	if !ok {
		return
	}
	li, ok := cartItemByID(order, r.PathValue("item_id"))
	if !ok {
		writeMgError(w, http.StatusNotFound, "The %1 Cart doesn't contain the %2 item.")
		return
	}
	var req mgCartItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMgError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	lines := currentLines(order)
	lines[li.ProductID] = int(req.CartItem.Qty)
	if err := setLines(store, order, lines); err != nil {
		writeMgError(w, http.StatusBadRequest, magentoStockMessage(err))
		return
	}
	if !s.persistMg(ctx, w, store, "cart item qty") {
		return
	}
	simstore.WriteJSON(w, http.StatusOK, map[string]any{"item_id": li.ProductID, "qty": lines[li.ProductID]})
}

// magentoDeleteCartItem implements DELETE /guest-carts/{id}/items/{item_id}.
func (s *server) magentoDeleteCartItem(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadMgStore(ctx, w, storeID)
	if !ok {
		return
	}
	order, ok := s.loadMgCart(w, store, r.PathValue("cart_id"))
	if !ok {
		return
	}
	li, ok := cartItemByID(order, r.PathValue("item_id"))
	if !ok {
		writeMgError(w, http.StatusNotFound, "The %1 Cart doesn't contain the %2 item.")
		return
	}
	lines := currentLines(order)
	delete(lines, li.ProductID)
	if err := setLines(store, order, lines); err != nil {
		writeMgError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.persistMg(ctx, w, store, "cart item delete") {
		return
	}
	simstore.WriteJSON(w, http.StatusOK, true)
}

// magentoApplyCoupon implements PUT /guest-carts/{id}/coupons/{code}: one
// coupon per cart, an unknown code is refused with 404 as a real store does.
func (s *server) magentoApplyCoupon(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadMgStore(ctx, w, storeID)
	if !ok {
		return
	}
	order, ok := s.loadMgCart(w, store, r.PathValue("cart_id"))
	if !ok {
		return
	}
	code := r.PathValue("code")
	if accepted, _ := simstore.ApplyCoupons([]string{code}, simstore.OrderSubtotalMinor(order.LineItems)); len(accepted) == 0 {
		writeMgError(w, http.StatusNotFound, "The coupon code isn't valid. Verify the code and try again.")
		return
	}
	if _, err := store.UpdateOrder(order.ID, "", nil, nil, nil, nil, &[]string{code}); err != nil {
		writeMgError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.persistMg(ctx, w, store, "coupon") {
		return
	}
	simstore.WriteJSON(w, http.StatusOK, true)
}

// magentoClearCoupon implements DELETE /guest-carts/{id}/coupons. A cart with
// no coupon answers 404, like a real store.
func (s *server) magentoClearCoupon(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadMgStore(ctx, w, storeID)
	if !ok {
		return
	}
	order, ok := s.loadMgCart(w, store, r.PathValue("cart_id"))
	if !ok {
		return
	}
	if len(order.DiscountCodes) == 0 {
		writeMgError(w, http.StatusNotFound, "The cart doesn't contain coupons")
		return
	}
	if _, err := store.UpdateOrder(order.ID, "", nil, nil, nil, nil, &[]string{}); err != nil {
		writeMgError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.persistMg(ctx, w, store, "coupon clear") {
		return
	}
	simstore.WriteJSON(w, http.StatusOK, true)
}

// magentoSetShippingInformation implements POST
// /guest-carts/{id}/shipping-information: stores the shipping address and
// selected method on the underlying order. The in-store-pickup carrier is
// accepted only when the merchant has pickup enabled, and only for the pickup
// source's own address.
func (s *server) magentoSetShippingInformation(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadMgStore(ctx, w, storeID)
	if !ok {
		return
	}
	order, ok := s.loadMgCart(w, store, r.PathValue("cart_id"))
	if !ok {
		return
	}

	var req mgShippingInformationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMgError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	info := req.AddressInformation
	if info.ShippingAddress == nil || info.ShippingCarrierCode == "" || info.ShippingMethodCode == "" {
		writeMgError(w, http.StatusBadRequest, "The shipping address and shipping method are required.")
		return
	}

	methodID := info.ShippingCarrierCode + "_" + info.ShippingMethodCode
	if simstore.IsPickupMethod(methodID) {
		ext := info.ShippingAddress.ExtensionAttributes
		if !store.LocalPickup || ext == nil || ext.PickupLocationCode != simPickupSourceCode {
			writeMgError(w, http.StatusBadRequest, "Carrier with such method not found: instore, pickup")
			return
		}
	}

	a := fromMgAddress(*info.ShippingAddress)
	if _, err := store.UpdateOrder(order.ID, "", nil, nil, &a, &methodID, nil); err != nil {
		writeMgError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.persistMg(ctx, w, store, "shipping-information") {
		return
	}
	simstore.WriteJSON(w, http.StatusOK, map[string]any{"payment_methods": []any{}, "totals": toMgTotals(order)})
}

// magentoEstimateShippingMethods implements POST
// /guest-carts/{id}/estimate-shipping-methods: offers the sim's single flat
// rate for a destination (the body's address, else the cart's own), matching
// the WooCommerce surface's flat-rate convention.
func (s *server) magentoEstimateShippingMethods(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadMgStore(ctx, w, storeID)
	if !ok {
		return
	}
	order, ok := s.loadMgCart(w, store, r.PathValue("cart_id"))
	if !ok {
		return
	}

	var req struct {
		Address *mgAddress `json:"address"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // an absent body falls back to the cart's address

	hasDestination := simstore.HasShippingAddress(order.ShippingAddress)
	if req.Address != nil {
		hasDestination = req.Address.Country != ""
	}

	methods := []mgShippingMethod{}
	if hasDestination {
		methods = append(methods, mgShippingMethod{
			CarrierCode: "flatrate",
			MethodCode:  "flatrate",
			MethodTitle: "Standard Shipping",
			Amount:      float64(simstore.FlatRateShippingMinor) / 100,
		})
	}
	simstore.WriteJSON(w, http.StatusOK, methods)
}

// magentoListSources implements GET /inventory/sources: the store's one
// inventory source, flagged as a pickup location when the merchant has
// enabled pickup.
func (s *server) magentoListSources(w http.ResponseWriter, r *http.Request, storeID string) {
	store, ok := s.loadMgStore(r.Context(), w, storeID)
	if !ok {
		return
	}
	addr := simstore.Address
	source := map[string]any{
		"source_code": simPickupSourceCode,
		"name":        simPickupSourceName,
		"enabled":     true,
		"country_id":  addr.Country,
		"region":      addr.Region,
		"city":        addr.City,
		"street":      addr.Street,
		"postcode":    addr.Postcode,
		"extension_attributes": map[string]any{
			"is_pickup_location_active": store.LocalPickup,
		},
	}
	simstore.WriteJSON(w, http.StatusOK, map[string]any{"items": []any{source}, "total_count": 1})
}

// magentoPaymentInformation implements POST
// /guest-carts/{id}/payment-information: places the order. A guest order needs
// the buyer's email and a billing address, and a fulfillment method; a payment
// token of "fail_token" is declined, as the WooCommerce surface does.
func (s *server) magentoPaymentInformation(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadMgStore(ctx, w, storeID)
	if !ok {
		return
	}
	order, ok := s.loadMgCart(w, store, r.PathValue("cart_id"))
	if !ok {
		return
	}

	var req mgPaymentInformationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMgError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	if req.Email == "" {
		writeMgError(w, http.StatusBadRequest, "Invalid value of \"\" provided for the email field.")
		return
	}
	if token, _ := req.PaymentMethod.AdditionalData["payment_token"].(string); token == "fail_token" {
		writeMgError(w, http.StatusBadRequest, "Payment failed: the payment was declined by the processor")
		return
	}

	var billing *OrderAddress
	if req.BillingAddress != nil {
		b := fromMgAddress(*req.BillingAddress)
		b.Email = req.Email
		billing = &b
	}

	updated, err := store.UpdateOrder(order.ID, "processing", nil, billing, nil, nil, nil)
	if err != nil {
		writeMgError(w, http.StatusBadRequest, magentoCompleteMessage(err))
		return
	}
	if !s.persistMg(ctx, w, store, "payment-information") {
		return
	}
	simstore.WriteJSON(w, http.StatusOK, updated.ID)
}

// magentoCompleteMessage words a completion refusal the way Magento does.
func magentoCompleteMessage(err error) string {
	if errors.Is(err, simstore.ErrFulfillmentRequired) {
		return "The shipping method is missing. Select the shipping method and try again."
	}
	return err.Error()
}

// magentoAddOrderComment implements POST /orders/{id}/comments: an admin-side
// order comment. A comment that carries buyer consent is also kept as the
// order's consent record, which is how a test reads back what was stored.
func (s *server) magentoAddOrderComment(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadMgStore(ctx, w, storeID)
	if !ok {
		return
	}
	order, ok := s.loadMgCart(w, store, r.PathValue("order_id"))
	if !ok {
		return
	}
	var req struct {
		StatusHistory struct {
			Comment string `json:"comment"`
		} `json:"statusHistory"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMgError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	if consent, found := strings.CutPrefix(req.StatusHistory.Comment, "UCP buyer consent: "); found {
		order.BuyerConsent = consent
	}
	if !s.persistMg(ctx, w, store, "order comment") {
		return
	}
	simstore.WriteJSON(w, http.StatusOK, true)
}

func (s *server) magentoGetOrder(w http.ResponseWriter, r *http.Request, storeID string) {
	ctx := r.Context()
	store, ok := s.loadMgStore(ctx, w, storeID)
	if !ok {
		return
	}
	order, ok := s.loadMgCart(w, store, r.PathValue("order_id"))
	if !ok {
		return
	}
	simstore.WriteJSON(w, http.StatusOK, toMgOrder(order))
}

func (s *server) loadMgStore(ctx context.Context, w http.ResponseWriter, storeID string) (*Store, bool) {
	store, err := s.stores.Get(ctx, storeID)
	if errors.Is(err, simstore.ErrStoreNotFound) {
		writeMgError(w, http.StatusNotFound, "No simulated store has been seeded for this store_id yet — run `sim seed --store "+storeID+"`.")
		return nil, false
	}
	if err != nil {
		s.log.Error("sim store: load failed", "store_id", storeID, "error", err)
		writeMgError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	return store, true
}

func (s *server) loadMgCart(w http.ResponseWriter, store *Store, cartID string) (*Order, bool) {
	id, err := strconv.Atoi(cartID)
	if err != nil {
		writeMgError(w, http.StatusNotFound, "Invalid cart id.")
		return nil, false
	}
	order, ok := store.GetOrder(id)
	if !ok {
		writeMgError(w, http.StatusNotFound, fmt.Sprintf("No such entity with cartId = %s", cartID))
		return nil, false
	}
	return order, true
}
