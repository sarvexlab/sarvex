// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package woocommerce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/resilience"
)

// metaKeyBuyerConsent is the WooCommerce order meta_data key under which the
// buyer_consent extension's consent object is stored (JSON-encoded), so it
// persists with the order at the merchant and round-trips into responses.
const metaKeyBuyerConsent = "_ucp_buyer_consent"

// defaultCatalogPageSize is used by FetchCatalog when the caller passes
// pageSize <= 0.
const defaultCatalogPageSize = 50

// errTransport marks a network/transport-level failure (as opposed to a
// classified HTTP error response), so the retry classifier can treat it as
// transient.
var errTransport = errors.New("woocommerce: transport error")

// Connector implements base.Connector for WooCommerce. The checkout/catalog/
// cart/order methods use cfg (baked in at construction, from the cache-backed
// merchant configuration); the store-API pull methods (FetchProduct,
// FetchCatalog, Ping, ...) instead resolve that store's credentials fresh on
// every call via credentials, behind a per-store retry/circuit-breaker/
// rate-limit guard, so a credential rotation at pairing takes effect on the
// very next pull call and one store's outage never affects another's.
type Connector struct {
	cfg            MerchantConfig
	client         *httpClient
	mapper         mapper
	credentials    base.CredentialsProvider
	guard          *resilience.Guard
	pullHTTPClient *http.Client
	// productIDs memoises SKU -> WooCommerce product id; see resolveProductID.
	productIDs *productIDCache
	// pickup memoises the checkout-time pickup detection (see checkoutPickup).
	pickup pickupCache
	logger *slog.Logger
}

// NewConnector creates and validates a new WooCommerce connector for one
// merchant. credentials resolves a store's decrypted secrets fresh for the
// store-API pull methods; it may be nil if the caller never invokes them
// (e.g. the serving plane, which must never make pull calls). guardCfg
// configures the pull methods' shared retry/breaker/rate-limit policy (zero
// value uses documented defaults).
func NewConnector(cfg MerchantConfig, credentials base.CredentialsProvider, guardCfg resilience.Config, logger *slog.Logger) (*Connector, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cfg = cfg.WithDefaults()

	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("connector", "woocommerce", "merchant_id", cfg.MerchantID)

	return &Connector{
		cfg:            cfg,
		client:         newHTTPClient(cfg, logger),
		mapper:         mapper{},
		credentials:    credentials,
		guard:          resilience.NewGuard(guardCfg),
		pullHTTPClient: &http.Client{Timeout: 30 * time.Second},
		productIDs:     newProductIDCache(),
		logger:         logger,
	}, nil
}

// Ensure Connector satisfies base.Connector at compile time.
var _ base.Connector = (*Connector)(nil)

// CatalogSearch executes a product search against the backend catalog.
func (c *Connector) CatalogSearch(ctx context.Context, storeID string, req *base.CatalogSearchRequest) (*base.CatalogSearchResponse, error) {
	query := req.Query
	if query == "" {
		query = "*"
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}

	var products []wcProduct
	path := fmt.Sprintf("/products?search=%s&per_page=%d", query, limit)

	if err := c.client.GET(ctx, path, &products); err != nil {
		return nil, fmt.Errorf("woocommerce: catalog search: %w", err)
	}

	resp := &base.CatalogSearchResponse{}

	for _, p := range products {
		resp.Products = append(resp.Products, c.mapper.productToUCPItem(p, c.cfg.StoreCurrency))
	}

	return resp, nil
}

// CatalogLookup retrieves a single product by ID.
func (c *Connector) CatalogLookup(ctx context.Context, storeID string, itemID string) (*base.CatalogLookupResponse, error) {
	productID, err := strconv.Atoi(itemID)
	if err != nil {
		return nil, fmt.Errorf("woocommerce: invalid product id: %w", err)
	}

	var product wcProduct
	path := fmt.Sprintf("/products/%d", productID)

	if err := c.client.GET(ctx, path, &product); err != nil {
		return nil, fmt.Errorf("%w: product %s", ErrNotFound, itemID)
	}

	item := c.mapper.productToUCPItem(product, c.cfg.StoreCurrency)

	// Fetch variations if any exist.
	if len(product.Variations) > 0 {
		for _, varID := range product.Variations {
			var variation wcVariation
			varPath := fmt.Sprintf("/products/%d/variations/%d", productID, varID)
			if err := c.client.GET(ctx, varPath, &variation); err != nil {
				c.logger.WarnContext(ctx, "failed to fetch variation",
					"product_id", productID,
					"variation_id", varID,
					"error", err,
				)
				continue
			}
			item.Variants = append(item.Variants, c.mapper.variationToUCPVariant(variation, c.cfg.StoreCurrency))
		}
	}

	return &base.CatalogLookupResponse{
		Product: &item,
	}, nil
}

// CartCreate creates a new draft order as a cart.
func (c *Connector) CartCreate(ctx context.Context, storeID string, req *base.CreateCartRequest) (*base.CartResponse, error) {
	payload := map[string]any{
		"status": "pending",
	}

	// Add line items if provided. An unresolvable item id (unknown SKU) is a
	// business rejection the buyer must fix, not a store outage.
	if len(req.LineItems) > 0 {
		lineItems, err := c.wcLineItems(ctx, createLineItems(req.LineItems))
		if err != nil {
			if rej := checkoutRejection(err); rej != nil {
				return nil, rej
			}
			return nil, err
		}
		payload["line_items"] = lineItems
	}
	applyCartBuyer(payload, req.Buyer)
	if req.Discounts != nil {
		payload["coupon_lines"] = couponLines(req.Discounts.Codes)
	}

	// Creating an order is not idempotent at the store, so this must not be
	// retried transparently: a retry after a lost response would leave a second
	// pending order behind (checkout create uses the same no-retry POST).
	var order wcOrder
	if err := c.client.POSTNoRetry(ctx, "/orders", payload, &order); err != nil {
		if rej := checkoutRejection(err); rej != nil {
			return nil, rej
		}
		return nil, fmt.Errorf("woocommerce: cart create: %w", err)
	}

	return c.cartResponse(&order), nil
}

// CartGet retrieves a cart by order ID.
func (c *Connector) CartGet(ctx context.Context, storeID string, cartID string) (*base.CartResponse, error) {
	orderID, err := strconv.Atoi(cartID)
	if err != nil {
		return nil, fmt.Errorf("woocommerce: invalid cart id: %w", err)
	}

	var order wcOrder
	path := fmt.Sprintf("/orders/%d", orderID)

	if err := c.client.GET(ctx, path, &order); err != nil {
		return nil, fmt.Errorf("%w: cart %s", ErrNotFound, cartID)
	}

	return c.cartResponse(&order), nil
}

// CartUpdate updates line items in a cart (PUT semantics).
func (c *Connector) CartUpdate(ctx context.Context, storeID string, cartID string, req *base.UpdateCartRequest) (*base.CartResponse, error) {
	orderID, err := strconv.Atoi(cartID)
	if err != nil {
		return nil, fmt.Errorf("woocommerce: invalid cart id: %w", err)
	}

	payload := map[string]any{}

	// Update line items.
	if len(req.LineItems) > 0 {
		lineItems, err := c.wcLineItems(ctx, updateLineItems(req.LineItems))
		if err != nil {
			if rej := checkoutRejection(err); rej != nil {
				return nil, rej
			}
			return nil, err
		}
		payload["line_items"] = lineItems
	}
	applyCartBuyer(payload, req.Buyer)
	if req.Discounts != nil {
		payload["coupon_lines"] = couponLines(req.Discounts.Codes)
	}

	var order wcOrder
	path := fmt.Sprintf("/orders/%d", orderID)

	if err := c.client.PUT(ctx, path, payload, &order); err != nil {
		if rej := checkoutRejection(err); rej != nil {
			return nil, rej
		}
		return nil, fmt.Errorf("woocommerce: cart update: %w", err)
	}

	return c.cartResponse(&order), nil
}

// cartResponse maps a backing order to the cart response, including the
// discount extension's applied discounts.
func (c *Connector) cartResponse(order *wcOrder) *base.CartResponse {
	resp := c.mapper.orderToCartResponse(order, c.cfg.StoreCurrency)
	c.mapper.ApplyCartDiscountExtension(resp, order)
	return resp
}

// applyCartBuyer records the cart's buyer as the order's billing contact, so
// estimates can be personalised and the buyer is carried into checkout.
func applyCartBuyer(payload map[string]any, buyer *model.Buyer) {
	if buyer == nil {
		return
	}
	payload["billing"] = map[string]any{
		"first_name": buyer.FirstName,
		"last_name":  buyer.LastName,
		"email":      buyer.Email,
		"phone":      buyer.PhoneNumber,
	}
}

// CheckoutCreate creates a new checkout session.
func (c *Connector) CheckoutCreate(ctx context.Context, storeID string, req *base.CreateCheckoutRequest) (*base.CheckoutResponse, error) {
	payload := map[string]any{
		"status": "pending",
	}

	// Add line items if provided. An unresolvable item id (unknown SKU) is a
	// business rejection the buyer must fix, not a store outage.
	if len(req.LineItems) > 0 {
		lineItems, err := c.wcLineItems(ctx, createLineItems(req.LineItems))
		if err != nil {
			if rej := checkoutRejection(err); rej != nil {
				return nil, rej
			}
			return nil, err
		}
		payload["line_items"] = lineItems
	}

	// Add billing address and buyer consent if provided.
	if req.Buyer != nil {
		payload["billing"] = map[string]any{
			"first_name": req.Buyer.FirstName,
			"last_name":  req.Buyer.LastName,
			"email":      req.Buyer.Email,
			"phone":      req.Buyer.PhoneNumber,
		}
		payload["meta_data"] = buyerConsentMeta(req.Buyer.Consent)
	}

	// Record the fulfillment choice: a shipping destination and rate, or pickup.
	if err := c.applyFulfillmentSelection(ctx, payload, req.Fulfillment); err != nil {
		return nil, err
	}

	if req.Discounts != nil {
		payload["coupon_lines"] = couponLines(req.Discounts.Codes)
	}

	var order wcOrder
	if err := c.client.POSTNoRetry(ctx, "/orders", payload, &order); err != nil {
		if rej := checkoutRejection(err); rej != nil {
			return nil, rej
		}
		return nil, fmt.Errorf("woocommerce: checkout create: %w", err)
	}

	resp := c.mapper.orderToCheckoutResponse(&order, c.cfg.StoreCurrency, c.cfg.StoreBaseURL)
	c.applyFulfillment(ctx, resp, &order)
	mapper{}.ApplyDiscountExtension(resp, &order)
	resp.ContinueURL = c.cfg.StoreBaseURL + "/checkout/order-pay/" + resp.ID + "/"

	return resp, nil
}

// CheckoutGet retrieves a checkout by order ID.
func (c *Connector) CheckoutGet(ctx context.Context, storeID string, checkoutID string) (*base.CheckoutResponse, error) {
	orderID, err := strconv.Atoi(checkoutID)
	if err != nil {
		return nil, fmt.Errorf("woocommerce: invalid checkout id: %w", err)
	}

	var order wcOrder
	path := fmt.Sprintf("/orders/%d", orderID)

	if err := c.client.GET(ctx, path, &order); err != nil {
		return nil, fmt.Errorf("%w: checkout %s", ErrNotFound, checkoutID)
	}

	resp := c.mapper.orderToCheckoutResponse(&order, c.cfg.StoreCurrency, c.cfg.StoreBaseURL)
	c.applyFulfillment(ctx, resp, &order)
	mapper{}.ApplyDiscountExtension(resp, &order)
	resp.ContinueURL = c.cfg.StoreBaseURL + "/checkout/order-pay/" + resp.ID + "/"

	return resp, nil
}

// CheckoutUpdate updates a checkout (PUT semantics).
func (c *Connector) CheckoutUpdate(ctx context.Context, storeID string, checkoutID string, req *base.UpdateCheckoutRequest) (*base.CheckoutResponse, error) {
	orderID, err := strconv.Atoi(checkoutID)
	if err != nil {
		return nil, fmt.Errorf("woocommerce: invalid checkout id: %w", err)
	}

	payload := map[string]any{}

	// Update line items if provided.
	if len(req.LineItems) > 0 {
		lineItems, err := c.wcLineItems(ctx, updateLineItems(req.LineItems))
		if err != nil {
			return nil, err
		}
		payload["line_items"] = lineItems
	}

	// Update billing address and buyer consent if provided.
	if req.Buyer != nil {
		payload["billing"] = map[string]any{
			"first_name": req.Buyer.FirstName,
			"last_name":  req.Buyer.LastName,
			"email":      req.Buyer.Email,
			"phone":      req.Buyer.PhoneNumber,
		}
		payload["meta_data"] = buyerConsentMeta(req.Buyer.Consent)
	}

	// Record the fulfillment choice: a shipping destination and rate, or pickup.
	if err := c.applyFulfillmentSelection(ctx, payload, req.Fulfillment); err != nil {
		return nil, err
	}

	if req.Discounts != nil {
		payload["coupon_lines"] = couponLines(req.Discounts.Codes)
	}

	var order wcOrder
	path := fmt.Sprintf("/orders/%d", orderID)

	if err := c.client.PUT(ctx, path, payload, &order); err != nil {
		if rej := checkoutRejection(err); rej != nil {
			return nil, rej
		}
		return nil, fmt.Errorf("woocommerce: checkout update: %w", err)
	}

	resp := c.mapper.orderToCheckoutResponse(&order, c.cfg.StoreCurrency, c.cfg.StoreBaseURL)
	c.applyFulfillment(ctx, resp, &order)
	mapper{}.ApplyDiscountExtension(resp, &order)
	resp.ContinueURL = c.cfg.StoreBaseURL + "/checkout/order-pay/" + resp.ID + "/"

	return resp, nil
}

// CheckoutComplete completes a checkout by setting status to processing.
func (c *Connector) CheckoutComplete(ctx context.Context, storeID string, checkoutID string, req *base.CompleteCheckoutRequest) (*base.CheckoutResponse, error) {
	orderID, err := strconv.Atoi(checkoutID)
	if err != nil {
		return nil, fmt.Errorf("woocommerce: invalid checkout id: %w", err)
	}

	payload := map[string]any{
		"status": "processing",
	}
	// Forward the opaque payment token to the merchant backend so it can run
	// the charge — the merchant is the party that processes payment. Sarvex
	// never stores or interprets it (invariant 8); it is passed through only.
	if token := paymentToken(req); token != "" {
		payload["payment_token"] = token
	}

	var order wcOrder
	path := fmt.Sprintf("/orders/%d", orderID)

	if err := c.client.PUTNoRetry(ctx, path, payload, &order); err != nil {
		if rej := checkoutRejection(err); rej != nil {
			return nil, rej
		}
		return nil, fmt.Errorf("woocommerce: checkout complete: %w", err)
	}

	resp := c.mapper.orderToCheckoutResponse(&order, c.cfg.StoreCurrency, c.cfg.StoreBaseURL)
	c.applyFulfillment(ctx, resp, &order)
	mapper{}.ApplyDiscountExtension(resp, &order)
	resp.ContinueURL = c.cfg.StoreBaseURL + "/checkout/order-pay/" + resp.ID + "/"

	return resp, nil
}

// buyerConsentMeta renders the buyer_consent extension's consent object as a
// WooCommerce meta_data list. The buyer object is PUT-replace, so an absent
// consent clears any previously stored value (empty string) rather than
// leaving stale consent behind.
func buyerConsentMeta(c *model.Consent) []map[string]any {
	value := ""
	if c != nil {
		if b, err := json.Marshal(c); err == nil {
			value = string(b)
		}
	}
	return []map[string]any{{"key": metaKeyBuyerConsent, "value": value}}
}

// couponLines maps UCP discount codes to the WooCommerce coupon_lines shape
// the backend understands. A non-nil (possibly empty) slice is always
// returned so an empty codes list clears previously applied coupons, matching
// the PUT-replace semantics of the discounts object.
func couponLines(codes []string) []map[string]any {
	lines := make([]map[string]any, 0, len(codes))
	for _, code := range codes {
		lines = append(lines, map[string]any{"code": code})
	}
	return lines
}

// paymentToken pulls the opaque token from the selected payment instrument's
// token credential, if the completion request carries one. Empty when there is
// no token credential (e.g. a card handler that tokenizes elsewhere).
func paymentToken(req *base.CompleteCheckoutRequest) string {
	if req == nil || req.Payment == nil {
		return ""
	}
	instruments := req.Payment.Instruments
	selected := -1
	for i, inst := range instruments {
		if inst.Selected {
			selected = i
			break
		}
	}
	if selected < 0 && len(instruments) > 0 {
		selected = 0
	}
	if selected < 0 {
		return ""
	}
	cred := instruments[selected].Credential
	if cred == nil || cred.Token == nil {
		return ""
	}
	return cred.Token.Token
}

// CheckoutCancel cancels a checkout by setting status to cancelled.
func (c *Connector) CheckoutCancel(ctx context.Context, storeID string, checkoutID string) (*base.CheckoutResponse, error) {
	orderID, err := strconv.Atoi(checkoutID)
	if err != nil {
		return nil, fmt.Errorf("woocommerce: invalid checkout id: %w", err)
	}

	payload := map[string]any{
		"status": "cancelled",
	}

	var order wcOrder
	path := fmt.Sprintf("/orders/%d", orderID)

	if err := c.client.PUT(ctx, path, payload, &order); err != nil {
		return nil, fmt.Errorf("woocommerce: checkout cancel: %w", err)
	}

	resp := c.mapper.orderToCheckoutResponse(&order, c.cfg.StoreCurrency, c.cfg.StoreBaseURL)
	c.applyFulfillment(ctx, resp, &order)
	mapper{}.ApplyDiscountExtension(resp, &order)
	resp.ContinueURL = c.cfg.StoreBaseURL + "/checkout/order-pay/" + resp.ID + "/"

	return resp, nil
}

// CheckoutURL returns the web checkout URL for this WooCommerce order.
// WooCommerce uses the order-pay URL so the buyer can pay for an existing order.
func (c *Connector) CheckoutURL(_ context.Context, _ string, checkoutID string) (string, error) {
	if checkoutID == "" {
		return "", fmt.Errorf("woocommerce: checkout ID must not be empty")
	}
	return c.cfg.StoreBaseURL + "/checkout/order-pay/" + checkoutID + "/", nil
}

// OrderGet retrieves an order.
func (c *Connector) OrderGet(ctx context.Context, storeID string, orderID string) (*base.OrderEvent, error) {
	id, err := strconv.Atoi(orderID)
	if err != nil {
		return nil, fmt.Errorf("woocommerce: invalid order id: %w", err)
	}

	var order wcOrder
	path := fmt.Sprintf("/orders/%d", id)

	if err := c.client.GET(ctx, path, &order); err != nil {
		return nil, fmt.Errorf("%w: order %s", ErrNotFound, orderID)
	}

	event := &base.OrderEvent{
		OrderID: orderID,
		Status:  string(c.mapper.orderStatusToCheckoutStatus(order.Status)),
	}

	return event, nil
}

// UpdateProduct writes corrected product fields back to the merchant's store via remediation.
// itemID is the product SKU; updates are field→value pairs from remediation rules.
// Maps remediation fields to WooCommerce API fields and performs atomic update.
func (c *Connector) UpdateProduct(ctx context.Context, storeID string, itemID string, updates map[string]interface{}) error {
	if itemID == "" {
		return fmt.Errorf("product ID (SKU) required")
	}
	if len(updates) == 0 {
		return nil
	}

	// Find product by SKU
	var products []wcProduct
	path := fmt.Sprintf("/products?sku=%s", itemID)
	if err := c.client.GET(ctx, path, &products); err != nil {
		return fmt.Errorf("find product by sku %s: %w", itemID, err)
	}
	target := selectBySKU(products, itemID)
	if target == nil {
		return fmt.Errorf("product not found with sku: %s", itemID)
	}

	productID := target.ID

	// Map remediation fields to WooCommerce API format
	payload := make(map[string]interface{})
	for field, value := range updates {
		switch field {
		case "name":
			payload["name"] = value
		case "description":
			payload["description"] = value
		case "short_description":
			payload["short_description"] = value
		case "sku":
			payload["sku"] = value
		case "regular_price":
			// Remediation stores price as string, WooCommerce expects string in API
			payload["regular_price"] = fmt.Sprintf("%v", value)
		case "status":
			payload["status"] = value
		case "manage_stock":
			// Attempt boolean parse
			if b, ok := value.(bool); ok {
				payload["manage_stock"] = b
			}
		case "stock_quantity":
			// Attempt int parse
			if q, ok := value.(float64); ok {
				payload["stock_quantity"] = int(q)
			} else if q, ok := value.(int); ok {
				payload["stock_quantity"] = q
			}
		default:
			// Pass through other fields (meta, categories, tags, etc.)
			payload[field] = value
		}
	}

	// Perform atomic update via PUT /products/{id}
	var result wcProduct
	path = fmt.Sprintf("/products/%d", productID)
	if err := c.client.PUT(ctx, path, payload, &result); err != nil {
		return fmt.Errorf("update product %d (sku %s): %w", productID, itemID, err)
	}

	c.logger.Debug("product updated via remediation",
		"product_id", productID,
		"sku", itemID,
		"fields_updated", len(payload),
	)

	return nil
}

// ─── Store-API pull methods ────────────────────────────────────────────────

// FetchProduct implements base.Connector.
func (c *Connector) FetchProduct(ctx context.Context, storeID string, sku string) (*base.CatalogEntity, error) {
	cfg, p, err := c.lookupBySKU(ctx, storeID, sku, "FetchProduct")
	if err != nil {
		return nil, err
	}
	entity := productToCatalogEntity(*p, cfg.StoreCurrency)
	return &entity, nil
}

// FetchInventory implements base.Connector.
func (c *Connector) FetchInventory(ctx context.Context, storeID string, sku string) (*base.InventoryEntity, error) {
	_, p, err := c.lookupBySKU(ctx, storeID, sku, "FetchInventory")
	if err != nil {
		return nil, err
	}
	return &base.InventoryEntity{
		SKU:      p.SKU,
		InStock:  p.InStock && p.StockStatus == "instock",
		Quantity: p.StockQuantity,
	}, nil
}

// FetchPricing implements base.Connector.
func (c *Connector) FetchPricing(ctx context.Context, storeID string, sku string) (*base.PricingEntity, error) {
	cfg, p, err := c.lookupBySKU(ctx, storeID, sku, "FetchPricing")
	if err != nil {
		return nil, err
	}
	return &base.PricingEntity{
		SKU:        p.SKU,
		PriceMinor: productPriceMinor(*p),
		Currency:   cfg.StoreCurrency,
	}, nil
}

// FetchCatalog implements base.Connector: pages through /products until a
// short page signals the end, yielding to any concurrent high-priority call
// between pages, and returns the full normalised set.
func (c *Connector) FetchCatalog(ctx context.Context, storeID string, pageSize int) ([]base.CatalogEntity, error) {
	if pageSize <= 0 {
		pageSize = defaultCatalogPageSize
	}

	cfg, err := c.configForStore(ctx, storeID)
	if err != nil {
		return nil, c.classifyConnectorError(storeID, "FetchCatalog", err)
	}

	var all []base.CatalogEntity
	for page := 1; ; page++ {
		var products []wcProduct
		path := fmt.Sprintf("/products?per_page=%d&page=%d", pageSize, page)

		if err := c.pullCall(ctx, storeID, cfg, resilience.PriorityBulk, path, &products); err != nil {
			return nil, c.classifyConnectorError(storeID, "FetchCatalog", err)
		}

		for _, p := range products {
			all = append(all, productToCatalogEntity(p, cfg.StoreCurrency))
		}

		if len(products) < pageSize {
			break
		}
	}
	return all, nil
}

// Ping implements base.Connector: a cheap, bounded read used for connector
// health reporting.
func (c *Connector) Ping(ctx context.Context, storeID string) (base.HealthStatus, error) {
	cfg, err := c.configForStore(ctx, storeID)
	if err != nil {
		return base.HealthFailed, c.classifyConnectorError(storeID, "Ping", err)
	}

	err = c.pullCall(ctx, storeID, cfg, resilience.PriorityHigh, "/products?per_page=1", nil)
	if err == nil {
		return base.HealthHealthy, nil
	}

	connErr := c.classifyConnectorError(storeID, "Ping", err)
	var ce *base.ConnectorError
	if errors.As(connErr, &ce) && ce.Kind == base.ConnectorErrCircuitOpen {
		return base.HealthFailed, connErr
	}
	return base.HealthDegraded, connErr
}

// ─── pull internals ────────────────────────────────────────────────────────

// lookupBySKU resolves storeID's credentials and fetches the product with
// the given sku, returning a typed base.ConnectorError on any failure
// (credential resolution, transport, not-found).
func (c *Connector) lookupBySKU(ctx context.Context, storeID, sku, op string) (MerchantConfig, *wcProduct, error) {
	if sku == "" {
		return MerchantConfig{}, nil, &base.ConnectorError{Kind: base.ConnectorErrNotFound, StoreID: storeID, Op: op, Err: errors.New("sku is required")}
	}

	cfg, err := c.configForStore(ctx, storeID)
	if err != nil {
		return MerchantConfig{}, nil, c.classifyConnectorError(storeID, op, err)
	}

	var products []wcProduct
	path := fmt.Sprintf("/products?sku=%s", url.QueryEscape(sku))
	if err := c.pullCall(ctx, storeID, cfg, resilience.PriorityHigh, path, &products); err != nil {
		return MerchantConfig{}, nil, c.classifyConnectorError(storeID, op, err)
	}
	p := selectBySKU(products, sku)
	if p == nil {
		return MerchantConfig{}, nil, &base.ConnectorError{Kind: base.ConnectorErrNotFound, StoreID: storeID, Op: op, Err: fmt.Errorf("%w: sku %s", ErrNotFound, sku)}
	}

	return cfg, p, nil
}

// selectBySKU picks the product a /products?sku= lookup was for. WooCommerce
// filters by sku, but the response is only a hint: a store, proxy or plugin that
// ignores the parameter returns the whole catalog, and trusting the first entry
// would read — or write — a different product under this SKU. An exact SKU match
// wins; a lone result that does not match is accepted (the store resolved the
// SKU itself, e.g. a variation to its parent); several results with no match
// means the filter was ignored, which is not found.
func selectBySKU(products []wcProduct, sku string) *wcProduct {
	for i := range products {
		if products[i].SKU == sku {
			return &products[i]
		}
	}
	if len(products) == 1 {
		return &products[0]
	}
	return nil
}

// configForStore resolves storeID's own credentials into a MerchantConfig
// for the store-API pull path.
//
// The pull methods must never reuse c.cfg: that config is the one this
// connector instance was constructed with, and connector instances are
// shared across stores. Using it meant every store's pull call went to the
// first store's base URL with the first store's consumer key — which is
// what TestConnector_CredentialIsolation catches, and what this connector's
// own doc comment has always claimed it does not do.
//
// Resolving per call (rather than caching) is also what makes a credential
// rotation at pairing take effect on the very next call.
func (c *Connector) configForStore(ctx context.Context, storeID string) (MerchantConfig, error) {
	if c.credentials == nil {
		// A connector built without a credentials provider serves exactly
		// one store: the one it was constructed for. Falling back to c.cfg
		// for that store keeps the single-store construction path working
		// (the catalog-import path and the sim integration test both use
		// it) without weakening isolation — a *different* store still
		// errors rather than silently borrowing these credentials, which is
		// the failure mode this whole function exists to prevent.
		if storeID != "" && storeID == c.cfg.StoreID {
			return c.cfg, nil
		}
		return MerchantConfig{}, fmt.Errorf("%w: no credentials provider configured for store %s", ErrUnauthorized, storeID)
	}

	raw, err := c.credentials.GetStoreCredentials(ctx, storeID)
	if err != nil {
		return MerchantConfig{}, fmt.Errorf("%w: resolve credentials for store %s: %w", ErrUnauthorized, storeID, err)
	}

	encoded, err := json.Marshal(raw)
	if err != nil {
		return MerchantConfig{}, fmt.Errorf("%w: encode credentials for store %s: %w", ErrUnauthorized, storeID, err)
	}
	var cfg MerchantConfig
	if err := json.Unmarshal(encoded, &cfg); err != nil {
		return MerchantConfig{}, fmt.Errorf("%w: decode credentials for store %s: %w", ErrUnauthorized, storeID, err)
	}

	cfg = cfg.WithDefaults()
	cfg.MerchantID = c.cfg.MerchantID
	cfg.StoreID = storeID
	cfg.Platform = c.cfg.Platform
	if err := cfg.Validate(); err != nil {
		return MerchantConfig{}, fmt.Errorf("%w: invalid credentials for store %s: %w", ErrUnauthorized, storeID, err)
	}
	return cfg, nil
}

// pullCall runs one guarded, retried, per-attempt-timed-out GET against path,
// decoding the JSON response into out (nil skips decoding, used by Ping).
func (c *Connector) pullCall(ctx context.Context, storeID string, cfg MerchantConfig, priority resilience.Priority, path string, out any) error {
	return c.guard.Do(ctx, storeID, priority, isRetryablePullError, func(callCtx context.Context) error {
		attemptCtx, cancel := context.WithTimeout(callCtx, cfg.RequestTimeout)
		defer cancel()
		return doPullRequest(attemptCtx, c.pullHTTPClient, cfg, http.MethodGet, path, out)
	})
}

// classifyConnectorError maps an error surfaced by the resilience guard (or
// the underlying WooCommerce classification) into a typed base.ConnectorError.
func (c *Connector) classifyConnectorError(storeID, op string, err error) error {
	switch {
	case errors.Is(err, resilience.ErrCircuitOpen):
		return &base.ConnectorError{Kind: base.ConnectorErrCircuitOpen, StoreID: storeID, Op: op, Err: err}
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return &base.ConnectorError{Kind: base.ConnectorErrUnavailable, StoreID: storeID, Op: op, Err: err}
	case errors.Is(err, ErrRateLimited):
		return &base.ConnectorError{Kind: base.ConnectorErrRateLimited, StoreID: storeID, Op: op, Err: err}
	case errors.Is(err, ErrNotFound):
		return &base.ConnectorError{Kind: base.ConnectorErrNotFound, StoreID: storeID, Op: op, Err: err}
	case errors.Is(err, ErrUnauthorized), errors.Is(err, ErrForbidden):
		return &base.ConnectorError{Kind: base.ConnectorErrCredentials, StoreID: storeID, Op: op, Err: err}
	default:
		return &base.ConnectorError{Kind: base.ConnectorErrUnavailable, StoreID: storeID, Op: op, Err: err}
	}
}

// isRetryablePullError classifies whether an error from a single WooCommerce
// API attempt is worth retrying.
func isRetryablePullError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errTransport) || errors.Is(err, ErrRateLimited) {
		return true
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.IsRetryable()
	}
	return false
}

// productToCatalogEntity maps a WooCommerce product to the normalised
// catalog entity, matching the sync-plane catalog store model's shape.
//
// Description, category and images are core product data and travel typed;
// everything else the store knows (weight, dimensions, its own attribute
// names) travels as raw attributes under the store's own names — the
// ontology, not this connector, decides what they mean. Nothing here is
// interpreted, scored or defaulted.
func productToCatalogEntity(p wcProduct, currency string) base.CatalogEntity {
	raw := map[string]string{
		"type": p.Type,
		"slug": p.Slug,
	}
	if w := strings.TrimSpace(p.Weight); w != "" {
		raw["weight"] = w
	}
	if d := dimensionsString(p.Dimensions); d != "" {
		raw["dimensions"] = d
	}
	for _, a := range p.Attributes {
		name := strings.TrimSpace(a.Name)
		if name == "" || len(a.Options) == 0 {
			continue
		}
		if _, taken := raw[strings.ToLower(name)]; taken {
			continue
		}
		raw[name] = strings.Join(a.Options, ", ")
	}

	description := p.Description
	if strings.TrimSpace(description) == "" {
		description = p.ShortDescription
	}
	var category string
	if len(p.Categories) > 0 {
		category = p.Categories[0].Name
	}
	var images []string
	for _, img := range p.Images {
		if img.Src != "" {
			images = append(images, img.Src)
		}
	}

	return base.CatalogEntity{
		SKU:           p.SKU,
		Name:          p.Name,
		Description:   description,
		Category:      category,
		Images:        images,
		PriceMinor:    productPriceMinor(p),
		Currency:      currency,
		InStock:       p.InStock && p.StockStatus == "instock",
		Quantity:      p.StockQuantity,
		RawAttributes: raw,
	}
}

// dimensionsString renders length x width x height, or "" unless all three
// are set (a partial set cannot be shipped against).
func dimensionsString(d wcDims) string {
	l, w, h := strings.TrimSpace(d.Length), strings.TrimSpace(d.Width), strings.TrimSpace(d.Height)
	if l == "" || w == "" || h == "" {
		return ""
	}
	return l + "x" + w + "x" + h
}

func productPriceMinor(p wcProduct) int64 {
	price := priceToCents(p.Price)
	if price == 0 && p.RegularPrice != "" {
		price = priceToCents(p.RegularPrice)
	}
	return price
}

// doPullRequest performs exactly one HTTP attempt against a store's
// WooCommerce REST API using the given (freshly resolved) config. No retry,
// no circuit breaking — that is the resilience.Guard's job one layer up.
func doPullRequest(ctx context.Context, httpc *http.Client, cfg MerchantConfig, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, restURLForConfig(cfg, path), nil)
	if err != nil {
		return fmt.Errorf("woocommerce: build request: %w", err)
	}
	setAuthHeadersForConfig(req, cfg)

	resp, err := httpc.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", errTransport, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%w: read response body: %v", errTransport, err)
	}

	if err := classifyHTTPError(resp.StatusCode, string(body)); err != nil {
		return err
	}

	if out != nil && len(body) > 0 {
		return json.Unmarshal(body, out)
	}
	return nil
}
