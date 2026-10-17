// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package magento

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/resilience"
)

// defaultCatalogPageSize is used by FetchCatalog when the caller passes
// pageSize <= 0.
const defaultCatalogPageSize = 50

// Connector implements base.Connector for Magento. Like WooCommerce, it uses
// a per-store resilience.Guard for pull methods (FetchProduct, FetchCatalog, Ping, ...)
// behind a per-store retry/circuit-breaker/rate-limit guard.
type Connector struct {
	cfg         MerchantConfig
	client      *httpClient
	mapper      mapper
	credentials base.CredentialsProvider
	guard       *resilience.Guard
	// pullClients caches one httpClient (and its cached admin token) per
	// store for the store-API pull path, keyed by storeID. c.client (bound
	// to this connector's construction-time cfg) is never reused for pull
	// calls: a connector instance can keep serving a store after its
	// credentials rotate at re-pairing, so the pull path always resolves
	// credentials fresh via credentials (see configForStore) rather than
	// trusting what was baked in at construction.
	pullClients   map[string]*httpClient
	pullClientsMu sync.Mutex
	logger        *slog.Logger
	// pickup memoises checkout-time pickup detection (see pickup.go).
	pickup pickupCache
}

// NewConnector creates and validates a new Magento connector for one merchant.
// credentials resolves a store's decrypted secrets fresh for store-API pull methods;
// it may be nil if the caller never invokes them (e.g. the serving plane).
// guardCfg configures the pull methods' shared retry/breaker/rate-limit policy
// (zero value uses documented defaults).
func NewConnector(cfg MerchantConfig, credentials base.CredentialsProvider, guardCfg resilience.Config, logger *slog.Logger) (*Connector, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cfg = cfg.WithDefaults()

	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("connector", "magento", "merchant_id", cfg.MerchantID)

	return &Connector{
		cfg:         cfg,
		client:      newHTTPClient(cfg, logger),
		mapper:      mapper{},
		credentials: credentials,
		guard:       resilience.NewGuard(guardCfg),
		pullClients: make(map[string]*httpClient),
		logger:      logger,
	}, nil
}

// Ensure Connector satisfies base.Connector at compile time.
var _ base.Connector = (*Connector)(nil)

// CatalogSearch executes a product search.
func (c *Connector) CatalogSearch(ctx context.Context, storeID string, req *base.CatalogSearchRequest) (*base.CatalogSearchResponse, error) {
	query := req.Query
	if query == "" {
		query = "*"
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}

	// Magento searchCriteria format: filter_groups with filters
	path := fmt.Sprintf("/products?searchCriteria[filter_groups][0][filters][0][field]=name&searchCriteria[filter_groups][0][filters][0][value]=%s&searchCriteria[filter_groups][0][filters][0][condition_type]=like&searchCriteria[page_size]=%d&searchCriteria[current_page]=1", query, limit)

	var searchResp mgSearchResponse
	if err := c.client.GET(ctx, path, &searchResp); err != nil {
		return nil, fmt.Errorf("magento: catalog search: %w", err)
	}

	resp := &base.CatalogSearchResponse{}

	for _, p := range searchResp.Items {
		resp.Products = append(resp.Products, c.mapper.productToUCPItem(p, c.cfg.StoreCurrency, c.cfg.BaseURL))
	}

	return resp, nil
}

// CatalogLookup retrieves a single product by SKU.
func (c *Connector) CatalogLookup(ctx context.Context, storeID string, itemID string) (*base.CatalogLookupResponse, error) {
	path := fmt.Sprintf("/products/%s", itemID)

	var product mgProduct
	if err := c.client.GET(ctx, path, &product); err != nil {
		return nil, fmt.Errorf("%w: product %s", ErrNotFound, itemID)
	}

	item := c.mapper.productToUCPItem(product, c.cfg.StoreCurrency, c.cfg.BaseURL)

	return &base.CatalogLookupResponse{
		Product: &item,
	}, nil
}

// UpdateProduct writes corrected product fields back to the merchant's store
// via remediation. itemID is the product SKU; updates are field→value pairs
// using the same canonical field vocabulary the sync-plane remediation rules
// use for every connector (name, description, short_description, sku,
// regular_price, status, manage_stock, stock_quantity) — see
// woocommerce.Connector.UpdateProduct for the shared vocabulary. Magento
// products are addressed by SKU directly, so no separate lookup step is
// needed before the write.
func (c *Connector) UpdateProduct(ctx context.Context, storeID string, itemID string, updates map[string]interface{}) error {
	if itemID == "" {
		return fmt.Errorf("product ID (SKU) required")
	}
	if len(updates) == 0 {
		return nil
	}

	product := map[string]any{"sku": itemID}
	var customAttrs []map[string]any
	var stockItem map[string]any

	for field, value := range updates {
		switch field {
		case "name":
			product["name"] = value
		case "sku":
			// Magento addresses products by SKU in the URL path; renaming a
			// SKU via this PUT is unsupported here.
		case "regular_price", "price":
			product["price"] = magentoPrice(value)
		case "status":
			product["status"] = magentoStatus(value)
		case "description":
			customAttrs = append(customAttrs, map[string]any{"attribute_code": "description", "value": fmt.Sprintf("%v", value)})
		case "short_description":
			customAttrs = append(customAttrs, map[string]any{"attribute_code": "short_description", "value": fmt.Sprintf("%v", value)})
		case "manage_stock":
			if b, ok := value.(bool); ok {
				if stockItem == nil {
					stockItem = map[string]any{}
				}
				stockItem["manage_stock"] = b
			}
		case "stock_quantity":
			if stockItem == nil {
				stockItem = map[string]any{}
			}
			switch q := value.(type) {
			case float64:
				stockItem["qty"] = q
				stockItem["is_in_stock"] = q > 0
			case int:
				stockItem["qty"] = q
				stockItem["is_in_stock"] = q > 0
			}
		default:
			// Pass through other fields (categories, custom attributes, ...).
			customAttrs = append(customAttrs, map[string]any{"attribute_code": field, "value": value})
		}
	}

	if len(customAttrs) > 0 {
		product["custom_attributes"] = customAttrs
	}
	if stockItem != nil {
		product["extension_attributes"] = map[string]any{"stock_item": stockItem}
	}

	path := fmt.Sprintf("/products/%s", url.PathEscape(itemID))
	var result mgProduct
	if err := c.client.PUT(ctx, path, map[string]any{"product": product}, &result); err != nil {
		return fmt.Errorf("update product (sku %s): %w", itemID, err)
	}

	c.logger.Debug("product updated via remediation",
		"sku", itemID,
		"fields_updated", len(updates),
	)

	return nil
}

// magentoPrice coerces a remediation value (typically a string, matching
// WooCommerce's regular_price convention) into the float64 Magento's price
// attribute expects.
func magentoPrice(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		var f float64
		if _, err := fmt.Sscanf(v, "%f", &f); err != nil {
			return 0
		}
		return f
	default:
		return 0
	}
}

// magentoStatus maps a remediation status value onto Magento's product
// status attribute (1 = enabled, 2 = disabled).
func magentoStatus(value any) int {
	switch v := value.(type) {
	case bool:
		if v {
			return 1
		}
		return 2
	case int:
		if v == 1 {
			return 1
		}
		return 2
	case float64:
		if v == 1 {
			return 1
		}
		return 2
	case string:
		switch strings.ToLower(v) {
		case "publish", "enabled", "active", "1", "true":
			return 1
		default:
			return 2
		}
	default:
		return 2
	}
}

// ─── Store-API pull methods ────────────────────────────────────────────────
// These use a store's credentials resolved fresh on every call (never c.cfg
// directly — a connector instance keeps serving a store across a credential
// rotation at re-pairing), behind a per-store resilience.Guard that owns all
// retry/circuit-breaking/rate-limiting. Each guarded attempt issues exactly
// one HTTP request (httpClient.doOnce) rather than going through
// client.GET/POST/PUT's own internal retry loop, so a failing store's
// failure budget is counted once per logical call.

// FetchProduct implements base.Connector.
func (c *Connector) FetchProduct(ctx context.Context, storeID string, sku string) (*base.CatalogEntity, error) {
	cfg, p, err := c.lookupBySKU(ctx, storeID, sku, "FetchProduct")
	if err != nil {
		return nil, err
	}
	entity := productToCatalogEntity(*p, cfg.StoreCurrency, cfg.BaseURL)
	return &entity, nil
}

// FetchInventory implements base.Connector.
func (c *Connector) FetchInventory(ctx context.Context, storeID string, sku string) (*base.InventoryEntity, error) {
	_, p, err := c.lookupBySKU(ctx, storeID, sku, "FetchInventory")
	if err != nil {
		return nil, err
	}
	inStock, qty := stockFromProduct(*p)
	return &base.InventoryEntity{SKU: p.SKU, InStock: inStock, Quantity: qty}, nil
}

// FetchPricing implements base.Connector.
func (c *Connector) FetchPricing(ctx context.Context, storeID string, sku string) (*base.PricingEntity, error) {
	cfg, p, err := c.lookupBySKU(ctx, storeID, sku, "FetchPricing")
	if err != nil {
		return nil, err
	}
	return &base.PricingEntity{
		SKU:        p.SKU,
		PriceMinor: priceToMinorUnits(p.Price),
		Currency:   cfg.StoreCurrency,
	}, nil
}

// FetchCatalog implements base.Connector: pages through /products via
// Magento's searchCriteria collection until a short page signals the end,
// yielding to any concurrent high-priority call between pages, and returns
// the full normalised set.
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
		path := fmt.Sprintf("/products?searchCriteria[page_size]=%d&searchCriteria[current_page]=%d", pageSize, page)

		var searchResp mgSearchResponse
		if err := c.pullCall(ctx, storeID, cfg, resilience.PriorityBulk, http.MethodGet, path, nil, &searchResp); err != nil {
			return nil, c.classifyConnectorError(storeID, "FetchCatalog", err)
		}

		for _, p := range searchResp.Items {
			all = append(all, productToCatalogEntity(p, cfg.StoreCurrency, cfg.BaseURL))
		}

		if len(searchResp.Items) < pageSize {
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

	path := "/products?searchCriteria[page_size]=1&searchCriteria[current_page]=1"
	err = c.pullCall(ctx, storeID, cfg, resilience.PriorityHigh, http.MethodGet, path, nil, nil)
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
func (c *Connector) lookupBySKU(ctx context.Context, storeID, sku, op string) (MerchantConfig, *mgProduct, error) {
	if sku == "" {
		return MerchantConfig{}, nil, &base.ConnectorError{Kind: base.ConnectorErrNotFound, StoreID: storeID, Op: op, Err: errors.New("sku is required")}
	}

	cfg, err := c.configForStore(ctx, storeID)
	if err != nil {
		return MerchantConfig{}, nil, c.classifyConnectorError(storeID, op, err)
	}

	var product mgProduct
	path := fmt.Sprintf("/products/%s", url.PathEscape(sku))
	if err := c.pullCall(ctx, storeID, cfg, resilience.PriorityHigh, http.MethodGet, path, nil, &product); err != nil {
		return MerchantConfig{}, nil, c.classifyConnectorError(storeID, op, err)
	}

	return cfg, &product, nil
}

// configForStore resolves storeID's own credentials into a MerchantConfig
// for the store-API pull path. The pull methods must never reuse c.cfg — see
// woocommerce.Connector.configForStore's doc comment for why (credential
// rotation and multi-store safety both depend on this).
func (c *Connector) configForStore(ctx context.Context, storeID string) (MerchantConfig, error) {
	if c.credentials == nil {
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
	if err := cfg.Validate(); err != nil {
		return MerchantConfig{}, fmt.Errorf("%w: invalid credentials for store %s: %w", ErrUnauthorized, storeID, err)
	}
	return cfg, nil
}

// pullClientFor returns the cached httpClient (and its cached admin token)
// for cfg.StoreID, creating one on first use. Caching per store means
// FetchCatalog's page loop — and repeated calls for the same store over the
// connector's lifetime — reuse one admin token instead of re-authenticating
// on every request.
func (c *Connector) pullClientFor(cfg MerchantConfig) *httpClient {
	c.pullClientsMu.Lock()
	defer c.pullClientsMu.Unlock()

	if hc, ok := c.pullClients[cfg.StoreID]; ok {
		return hc
	}
	hc := newHTTPClient(cfg, c.logger)
	c.pullClients[cfg.StoreID] = hc
	return hc
}

// pullCall runs one guarded, retried, per-attempt-timed-out request against
// path, decoding the JSON response into out (nil skips decoding, used by
// Ping and item-update calls).
func (c *Connector) pullCall(ctx context.Context, storeID string, cfg MerchantConfig, priority resilience.Priority, method, path string, body, out any) error {
	hc := c.pullClientFor(cfg)
	return c.guard.Do(ctx, storeID, priority, isRetryablePullError, func(callCtx context.Context) error {
		attemptCtx, cancel := context.WithTimeout(callCtx, cfg.RequestTimeout)
		defer cancel()
		return hc.doOnce(attemptCtx, method, path, body, out)
	})
}

// classifyConnectorError maps an error surfaced by the resilience guard (or
// the underlying Magento classification) into a typed base.ConnectorError.
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
	case errors.Is(err, ErrUnauthorized), errors.Is(err, ErrForbidden), errors.Is(err, ErrTokenRefresh):
		return &base.ConnectorError{Kind: base.ConnectorErrCredentials, StoreID: storeID, Op: op, Err: err}
	default:
		return &base.ConnectorError{Kind: base.ConnectorErrUnavailable, StoreID: storeID, Op: op, Err: err}
	}
}

// isRetryablePullError classifies whether an error from a single Magento API
// attempt is worth retrying.
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

// productToCatalogEntity maps a Magento product to the normalised catalog
// entity, matching the sync-plane catalog store model's shape.
//
// Description and images are core product data and travel typed (the image
// URL is the store's media base + the gallery entry's relative path, the same
// construction the serving mapper uses). Everything else the store knows —
// weight, its ts_dimensions_* package dimensions, and its other scalar custom
// attributes — travels as raw attributes under Magento's own names for the
// ontology to interpret. Nothing here is interpreted, scored or defaulted.
func productToCatalogEntity(p mgProduct, currency, baseURL string) base.CatalogEntity {
	inStock, qty := stockFromProduct(p)
	m := mapper{}

	raw := map[string]string{
		"type_id": p.TypeID,
	}
	if p.Weight > 0 {
		raw["weight"] = strconv.FormatFloat(p.Weight, 'f', -1, 64)
	}
	l := m.customAttributeValue(p.CustomAttributes, "ts_dimensions_length")
	w := m.customAttributeValue(p.CustomAttributes, "ts_dimensions_width")
	h := m.customAttributeValue(p.CustomAttributes, "ts_dimensions_height")
	if l != "" && w != "" && h != "" {
		raw["dimensions"] = l + "x" + w + "x" + h
	}
	for _, attr := range p.CustomAttributes {
		if _, skip := nonAttributeCodes[attr.AttributeCode]; skip || strings.HasPrefix(attr.AttributeCode, "meta_") {
			continue
		}
		if _, taken := raw[attr.AttributeCode]; taken {
			continue
		}
		if v, ok := scalarString(attr.Value); ok {
			raw[attr.AttributeCode] = v
		}
	}

	description := m.customAttributeValue(p.CustomAttributes, "description")
	if strings.TrimSpace(description) == "" {
		description = m.customAttributeValue(p.CustomAttributes, "short_description")
	}
	var images []string
	for _, e := range p.MediaGalleryEntries {
		if e.MediaType != "" && e.MediaType != "image" {
			continue
		}
		if e.URL != "" {
			images = append(images, strings.TrimRight(baseURL, "/")+"/media/catalog/product"+e.URL)
		}
	}

	return base.CatalogEntity{
		SKU:           p.SKU,
		Name:          p.Name,
		Description:   description,
		Images:        images,
		PriceMinor:    priceToMinorUnits(p.Price),
		Currency:      currency,
		InStock:       inStock,
		Quantity:      qty,
		RawAttributes: raw,
	}
}

// nonAttributeCodes are custom attribute codes that are either carried typed
// (descriptions), consumed above (dimensions), or Magento plumbing (urls,
// image roles, layout) rather than merchandising attributes.
var nonAttributeCodes = map[string]struct{}{
	"description": {}, "short_description": {}, "url_key": {},
	"ts_dimensions_length": {}, "ts_dimensions_width": {}, "ts_dimensions_height": {},
	"image": {}, "small_image": {}, "thumbnail": {}, "swatch_image": {},
	"options_container": {}, "required_options": {}, "has_options": {},
	"tax_class_id": {}, "category_ids": {}, "gift_message_available": {},
}

// maxRawAttributeLen bounds a pass-through attribute value: merchandising
// attributes are short; anything longer is content, not an attribute.
const maxRawAttributeLen = 256

// scalarString renders a custom attribute value when it is a short scalar.
func scalarString(v interface{}) (string, bool) {
	var s string
	switch t := v.(type) {
	case string:
		s = strings.TrimSpace(t)
	case float64:
		s = strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		s = strconv.FormatBool(t)
	default:
		return "", false
	}
	if s == "" || len(s) > maxRawAttributeLen {
		return "", false
	}
	return s, true
}

// stockFromProduct derives availability and quantity from a Magento
// product's extension_attributes.stock_item, falling back to the product's
// enabled/disabled status when no stock item is present.
func stockFromProduct(p mgProduct) (inStock bool, qty *int) {
	available := mapper{}.isProductAvailable(p)
	if p.ExtensionAttributes != nil && p.ExtensionAttributes.StockItem != nil {
		q := int(p.ExtensionAttributes.StockItem.Qty)
		return available, &q
	}
	return available, nil
}

// parseShippingMethod parses a shipping method ID (format: "carrier_method") into carrier and method codes.
func parseShippingMethod(methodID string) (string, string) {
	parts := strings.Split(methodID, "_")
	if len(parts) >= 2 {
		return parts[0], strings.Join(parts[1:], "_")
	}
	return methodID, methodID
}
