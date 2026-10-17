// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package shopify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// Option is a functional option for NewConnector.
type Option func(*Connector)

// WithHTTPClient replaces the underlying http.Client used for Shopify API calls.
// Intended for testing — production callers should use the default.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Connector) {
		c.http.underlying = client
	}
}

// Connector is the Shopify implementation of base.Connector. It translates all
// UCP capability operations into Shopify Storefront GraphQL or Admin REST calls
// and returns UCP-normalised responses.
//
// One Connector instance handles exactly one merchant. The gateway instantiates
// one per registered merchant and caches them by merchantID.
//
// Thread-safety: Connector is safe for concurrent use after construction.
type Connector struct {
	cfg            MerchantConfig
	http           *httpClient
	mapper         mapper
	gatewayBaseURL string
	logger         *slog.Logger
}

// NewConnector creates and validates a new Shopify connector for one merchant.
//
// gatewayBaseURL is the public base URL of the Sarvex gateway itself, used
// when constructing the endpoint field in the UCP profile. E.g.
// "https://gateway.sarvex.example.com".
func NewConnector(cfg MerchantConfig, logger *slog.Logger, opts ...Option) (*Connector, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg = cfg.WithDefaults()

	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("connector", "shopify", "merchant_id", cfg.MerchantID)

	conn := &Connector{
		cfg:    cfg,
		http:   newHTTPClient(cfg, logger),
		logger: logger,
	}
	for _, opt := range opts {
		opt(conn)
	}
	return conn, nil
}

// Ensure Connector satisfies all connector interfaces at compile time.
var _ base.Connector = (*Connector)(nil)
var _ base.SimulationConnector = (*Connector)(nil)

// ─── Catalog ──────────────────────────────────────────────────────────────────

func (c *Connector) CatalogSearch(ctx context.Context, storeID string, req *base.CatalogSearchRequest) (*base.CatalogSearchResponse, error) {
	pageSize := req.Limit
	if pageSize <= 0 || pageSize > 50 {
		pageSize = 20
	}

	vars := map[string]any{
		"query": req.Query,
		"first": pageSize,
	}

	var data shopifyProductSearchData
	if err := c.http.StorefrontGQL(ctx, gqlProductSearch, vars, &data); err != nil {
		return nil, fmt.Errorf("shopify: catalog search: %w", err)
	}

	resp := &base.CatalogSearchResponse{}

	for _, edge := range data.Products.Edges {
		resp.Products = append(resp.Products, c.mapper.productToUCPItem(edge.Node))
	}

	return resp, nil
}

func (c *Connector) CatalogLookup(ctx context.Context, storeID string, itemID string) (*base.CatalogLookupResponse, error) {
	vars := map[string]any{"id": itemID}

	var data shopifyProductByIDData
	if err := c.http.StorefrontGQL(ctx, gqlProductByID, vars, &data); err != nil {
		return nil, fmt.Errorf("shopify: catalog lookup %s: %w", itemID, err)
	}

	if data.Product == nil {
		return nil, fmt.Errorf("%w: product %s", ErrNotFound, itemID)
	}

	product := c.mapper.productToUCPItem(*data.Product)
	return &base.CatalogLookupResponse{
		Product: &product,
	}, nil
}

// ─── Cart ─────────────────────────────────────────────────────────────────────

func (c *Connector) CartCreate(ctx context.Context, storeID string, req *base.CreateCartRequest) (*base.CartResponse, error) {
	input := map[string]any{
		"lines": c.buildCartLineInputs(req.LineItems),
	}
	if req.Buyer != nil && req.Buyer.Email != "" {
		input["buyerIdentity"] = map[string]any{"email": req.Buyer.Email}
	}

	vars := map[string]any{"input": input}

	var data shopifyCartCreateData
	if err := c.http.StorefrontGQL(ctx, gqlCartCreate, vars, &data); err != nil {
		return nil, fmt.Errorf("shopify: cart create: %w", err)
	}

	if len(data.CartCreate.UserErrors) > 0 {
		msgs := c.mapper.cartUserErrorsToMessages(data.CartCreate.UserErrors)
		return c.cartErrorResponse(msgs), nil
	}

	if data.CartCreate.Cart == nil {
		return nil, errors.New("shopify: cart create returned nil cart with no user errors")
	}

	resp := c.mapper.cartToUCPResponse(data.CartCreate.Cart)
	c.mapper.ApplyCartDiscountExtension(resp, data.CartCreate.Cart)
	return resp, nil
}

func (c *Connector) CartGet(ctx context.Context, storeID string, cartID string) (*base.CartResponse, error) {
	vars := map[string]any{"cartId": cartID}

	var data shopifyCartGetData
	if err := c.http.StorefrontGQL(ctx, gqlCartGet, vars, &data); err != nil {
		return nil, fmt.Errorf("shopify: cart get %s: %w", cartID, err)
	}

	if data.Cart == nil {
		return nil, fmt.Errorf("%w: cart %s", ErrNotFound, cartID)
	}

	resp := c.mapper.cartToUCPResponse(data.Cart)
	c.mapper.ApplyCartDiscountExtension(resp, data.Cart)
	return resp, nil
}

// CartUpdate replaces the cart contents with the provided line items (PUT semantics).
// Because Shopify does not expose a single "replace all lines" mutation, the
// implementation fetches the current cart, diffs the lines, and applies
// the appropriate add/update mutations.
func (c *Connector) CartUpdate(ctx context.Context, storeID string, cartID string, req *base.UpdateCartRequest) (*base.CartResponse, error) {
	// Fetch current state to diff.
	var currentData shopifyCartGetData
	if err := c.http.StorefrontGQL(ctx, gqlCartGet, map[string]any{"cartId": cartID}, &currentData); err != nil {
		return nil, fmt.Errorf("shopify: cart get for update %s: %w", cartID, err)
	}
	if currentData.Cart == nil {
		return nil, fmt.Errorf("%w: cart %s", ErrNotFound, cartID)
	}

	// Build maps of existing line IDs by variant ID for diffing.
	existingLines := make(map[string]string) // variantID -> lineID
	for _, edge := range currentData.Cart.Lines.Edges {
		existingLines[edge.Node.Merchandise.ID] = edge.Node.ID
	}

	// Separate into adds vs updates.
	var addLines []map[string]any
	var updateLines []map[string]any

	for _, li := range req.LineItems {
		// In UCP, item.ID maps to the Shopify variant GID.
		variantID := li.Item.ID
		if existingLineID, exists := existingLines[variantID]; exists {
			updateLines = append(updateLines, map[string]any{
				"id":       existingLineID,
				"quantity": li.Quantity,
			})
		} else {
			addLines = append(addLines, map[string]any{
				"merchandiseId": variantID,
				"quantity":      li.Quantity,
			})
		}
	}

	var finalCart *shopifyCart

	if len(addLines) > 0 {
		var data shopifyCartLinesAddData
		if err := c.http.StorefrontGQL(ctx, gqlCartLinesAdd, map[string]any{
			"cartId": cartID, "lines": addLines,
		}, &data); err != nil {
			return nil, fmt.Errorf("shopify: cart lines add: %w", err)
		}
		if len(data.CartLinesAdd.UserErrors) > 0 {
			return c.cartErrorResponse(c.mapper.cartUserErrorsToMessages(data.CartLinesAdd.UserErrors)), nil
		}
		finalCart = data.CartLinesAdd.Cart
	}

	if len(updateLines) > 0 {
		var data shopifyCartLinesUpdateData
		if err := c.http.StorefrontGQL(ctx, gqlCartLinesUpdate, map[string]any{
			"cartId": cartID, "lines": updateLines,
		}, &data); err != nil {
			return nil, fmt.Errorf("shopify: cart lines update: %w", err)
		}
		if len(data.CartLinesUpdate.UserErrors) > 0 {
			return c.cartErrorResponse(c.mapper.cartUserErrorsToMessages(data.CartLinesUpdate.UserErrors)), nil
		}
		finalCart = data.CartLinesUpdate.Cart
	}

	if finalCart == nil {
		// No mutations were needed — return current cart.
		return c.mapper.cartToUCPResponse(currentData.Cart), nil
	}

	return c.mapper.cartToUCPResponse(finalCart), nil
}

// ─── Checkout ─────────────────────────────────────────────────────────────────

func (c *Connector) CheckoutCreate(ctx context.Context, storeID string, req *base.CreateCheckoutRequest) (*base.CheckoutResponse, error) {
	input := map[string]any{
		"lineItems": c.buildCheckoutLineInputs(req.LineItems),
	}

	if req.Buyer != nil {
		if req.Buyer.Email != "" {
			input["email"] = req.Buyer.Email
		}
		// Store consent choices as Shopify custom attributes so they are
		// preserved on the resulting order for the merchant's compliance records.
		if req.Buyer.Consent != nil {
			if attrs := ConsentToCustomAttributes(req.Buyer.Consent); len(attrs) > 0 {
				input["customAttributes"] = attrs
			}
		}
	}

	if req.Fulfillment != nil && req.Fulfillment.SelectedDestination != nil {
		input["shippingAddress"] = c.mapper.ucpAddressToShopifyMailingInput(req.Fulfillment.SelectedDestination)
	}

	vars := map[string]any{"input": input}

	var data shopifyCheckoutCreateData
	if err := c.http.StorefrontGQL(ctx, gqlCheckoutCreate, vars, &data); err != nil {
		return nil, fmt.Errorf("shopify: checkout create: %w", err)
	}

	if len(data.CheckoutCreate.UserErrors) > 0 {
		msgs := c.mapper.checkoutUserErrorsToMessages(data.CheckoutCreate.UserErrors)
		// Return an error response whenever user errors are present with no checkout
		// resource, or when any error is unrecoverable.
		if hasUnrecoverable(msgs) || data.CheckoutCreate.Checkout == nil {
			return c.checkoutErrorResponse(msgs, c.cfg.StoreBaseURL), nil
		}
	}

	if data.CheckoutCreate.Checkout == nil {
		return nil, errors.New("shopify: checkout create returned nil checkout with no user errors")
	}

	resp := c.mapper.checkoutToUCPResponse(
		data.CheckoutCreate.Checkout,
		c.cfg,
	)

	if len(data.CheckoutCreate.UserErrors) > 0 {
		resp.Messages = append(resp.Messages,
			c.mapper.checkoutUserErrorsToMessages(data.CheckoutCreate.UserErrors)...,
		)
	}

	// Apply fulfillment and discount extensions.
	c.mapper.ApplyFulfillmentExtension(resp, data.CheckoutCreate.Checkout)
	c.mapper.ApplyDiscountExtension(resp, data.CheckoutCreate.Checkout)
	carryConsent(resp, req.Buyer)

	return resp, nil
}

func (c *Connector) CheckoutGet(ctx context.Context, storeID string, checkoutID string) (*base.CheckoutResponse, error) {
	vars := map[string]any{"checkoutId": checkoutID}

	var data shopifyCheckoutGetData
	if err := c.http.StorefrontGQL(ctx, gqlCheckoutGet, vars, &data); err != nil {
		return nil, fmt.Errorf("shopify: checkout get %s: %w", checkoutID, err)
	}

	if data.Node == nil {
		return nil, fmt.Errorf("%w: checkout %s", ErrNotFound, checkoutID)
	}

	resp := c.mapper.checkoutToUCPResponse(data.Node, c.cfg)
	c.mapper.ApplyFulfillmentExtension(resp, data.Node)
	c.mapper.ApplyDiscountExtension(resp, data.Node)
	return resp, nil
}

// CheckoutUpdate applies buyer info, address, and shipping method changes to
// an existing checkout session. Per the UCP spec, this is PUT-semantics —
// the full desired state is provided. The connector applies individual Shopify
// mutations for each component that has changed.
func (c *Connector) CheckoutUpdate(ctx context.Context, storeID string, checkoutID string, req *base.UpdateCheckoutRequest) (*base.CheckoutResponse, error) {
	// Apply email update if provided.
	if req.Buyer != nil && req.Buyer.Email != "" {
		var emailData shopifyCheckoutEmailUpdateData
		if err := c.http.StorefrontGQL(ctx, gqlCheckoutEmailUpdate, map[string]any{
			"checkoutId": checkoutID,
			"email":      req.Buyer.Email,
		}, &emailData); err != nil {
			return nil, fmt.Errorf("shopify: checkout email update: %w", err)
		}
		if len(emailData.CheckoutEmailUpdateV2.UserErrors) > 0 {
			msgs := c.mapper.checkoutUserErrorsToMessages(emailData.CheckoutEmailUpdateV2.UserErrors)
			if hasUnrecoverable(msgs) {
				return c.checkoutErrorResponse(msgs, c.cfg.StoreBaseURL+"/checkout"), nil
			}
		}
	}

	// Apply shipping address if provided.
	if req.Fulfillment != nil && req.Fulfillment.SelectedDestination != nil {
		var addrData shopifyCheckoutShippingAddressUpdateData
		if err := c.http.StorefrontGQL(ctx, gqlCheckoutShippingAddressUpdate, map[string]any{
			"checkoutId":      checkoutID,
			"shippingAddress": c.mapper.ucpAddressToShopifyMailingInput(req.Fulfillment.SelectedDestination),
		}, &addrData); err != nil {
			return nil, fmt.Errorf("shopify: checkout shipping address update: %w", err)
		}
		if len(addrData.CheckoutShippingAddressUpdateV2.UserErrors) > 0 {
			msgs := c.mapper.checkoutUserErrorsToMessages(addrData.CheckoutShippingAddressUpdateV2.UserErrors)
			if hasUnrecoverable(msgs) {
				return c.checkoutErrorResponse(msgs, c.cfg.StoreBaseURL+"/checkout"), nil
			}
		}
	}

	// Apply shipping rate selection if provided.
	if req.Fulfillment != nil && req.Fulfillment.SelectedMethodID != "" {
		var rateData shopifyCheckoutShippingLineUpdateData
		if err := c.http.StorefrontGQL(ctx, gqlCheckoutShippingLineUpdate, map[string]any{
			"checkoutId":         checkoutID,
			"shippingRateHandle": req.Fulfillment.SelectedMethodID,
		}, &rateData); err != nil {
			return nil, fmt.Errorf("shopify: checkout shipping line update: %w", err)
		}
		if len(rateData.CheckoutShippingLineUpdate.UserErrors) > 0 {
			msgs := c.mapper.checkoutUserErrorsToMessages(rateData.CheckoutShippingLineUpdate.UserErrors)
			if hasUnrecoverable(msgs) {
				return c.checkoutErrorResponse(msgs, c.cfg.StoreBaseURL+"/checkout"), nil
			}
		}
	}

	// Apply discount code changes if the platform sent them.
	if req.Discounts != nil {
		if err := c.applyDiscountCodes(ctx, checkoutID, req.Discounts.Codes); err != nil {
			c.logger.WarnContext(ctx, "shopify: failed to apply discount codes",
				"checkout_id", checkoutID, "error", err)
		}
	}

	// Return the latest checkout state (includes extensions via CheckoutGet).
	co, err := c.CheckoutGet(ctx, storeID, checkoutID)
	if err != nil {
		return nil, err
	}
	carryConsent(co, req.Buyer)
	return co, nil
}

// CheckoutComplete submits the tokenised payment credential to Shopify to
// place the order. The platform must have already acquired a payment token
// via the appropriate payment handler (Shop Pay or a custom tokeniser).
func (c *Connector) CheckoutComplete(ctx context.Context, storeID string, checkoutID string, req *base.CompleteCheckoutRequest) (*base.CheckoutResponse, error) {
	if req.Payment == nil || len(req.Payment.Instruments) == 0 {
		return c.checkoutErrorResponse([]model.Message{
			model.NewErrorMessage(model.MessageError{
				Code:     model.ErrorCode("missing"),
				Path:     "$.payment.instruments",
				Content:  "Payment instrument is required to complete checkout",
				Severity: model.MessageSeverityRecoverable,
			}),
		}, c.cfg.StoreBaseURL+"/checkout"), nil
	}

	instrument := req.Payment.Instruments[0]
	if instrument.Credential == nil {
		return c.checkoutErrorResponse([]model.Message{
			model.NewErrorMessage(model.MessageError{
				Code:     model.ErrorCode("missing"),
				Path:     "$.payment.instruments[0].credential",
				Content:  "Payment credential (token) is required",
				Severity: model.MessageSeverityRecoverable,
			}),
		}, c.cfg.StoreBaseURL+"/checkout"), nil
	}

	// Build the Shopify tokenised payment input from the UCP payment credential.
	paymentInput, err := c.buildTokenizedPaymentInput(instrument)
	if err != nil {
		return nil, fmt.Errorf("shopify: build payment input: %w", err)
	}

	var data shopifyCheckoutCompleteData
	if err := c.http.StorefrontGQL(ctx, gqlCheckoutCompleteWithTokenizedPayment, map[string]any{
		"checkoutId": checkoutID,
		"payment":    paymentInput,
	}, &data); err != nil {
		return nil, fmt.Errorf("shopify: checkout complete: %w", err)
	}

	if len(data.CheckoutCompleteWithTokenizedPaymentV3.UserErrors) > 0 {
		msgs := c.mapper.checkoutUserErrorsToMessages(data.CheckoutCompleteWithTokenizedPaymentV3.UserErrors)
		return c.checkoutErrorResponse(msgs, c.cfg.StoreBaseURL+"/checkout"), nil
	}

	co := data.CheckoutCompleteWithTokenizedPaymentV3.Checkout
	if co == nil {
		return nil, errors.New("shopify: checkout complete returned nil checkout")
	}

	// Check payment result for async/failed states.
	if pr := data.CheckoutCompleteWithTokenizedPaymentV3.Payment; pr != nil {
		if !pr.Ready {
			// Payment is processing asynchronously.
			resp := c.mapper.checkoutToUCPResponse(co, c.cfg)
			resp.Status = base.CheckoutStatusCompleteInProgress
			return resp, nil
		}
		if pr.ErrorMessage != "" {
			severity := model.MessageSeverityRecoverable
			if isPaymentStepUpRequired(pr.ErrorMessage) {
				severity = model.MessageSeverity("requires_buyer_input")
			}
			return c.checkoutErrorResponse([]model.Message{
				model.NewErrorMessage(model.MessageError{
					Code:     model.ErrorCode("payment_failed"),
					Content:  pr.ErrorMessage,
					Severity: severity,
				}),
			}, c.cfg.StoreBaseURL+"/checkout"), nil
		}
	}

	return c.mapper.checkoutToUCPResponse(co, c.cfg), nil
}

func (c *Connector) CheckoutCancel(ctx context.Context, storeID string, checkoutID string) (*base.CheckoutResponse, error) {
	// Shopify Storefront API doesn't have an explicit checkout cancel mutation.
	// A checkout expires automatically. We return a synthetic canceled response.
	return &base.CheckoutResponse{
		Status: base.CheckoutStatusCanceled,
	}, nil
}

// isPaymentStepUpRequired returns true when the PSP error message indicates that
// additional buyer authentication (3DS/SCA, identity verification, or a payment
// review hold) is required — conditions that UCP cannot express inline and that
// require the buyer to complete the step-up on the merchant website.
func isPaymentStepUpRequired(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "3d secure") ||
		strings.Contains(lower, "3ds") ||
		strings.Contains(lower, "sca") ||
		strings.Contains(lower, "authentication required") ||
		strings.Contains(lower, "identity verification") ||
		strings.Contains(lower, "payment review")
}

// CheckoutURL returns the web checkout URL for this Shopify checkout session.
// The checkoutID is a Shopify GID (gid://shopify/Checkout/{token}); the token
// is extracted and appended to the store's base URL.
func (c *Connector) CheckoutURL(_ context.Context, _ string, checkoutID string) (string, error) {
	token := gidToNumericID(checkoutID)
	if token == "" || token == checkoutID {
		return "", fmt.Errorf("shopify: cannot derive checkout URL from ID %q", checkoutID)
	}
	return c.cfg.StoreBaseURL + "/checkouts/" + token, nil
}

// ─── Orders ───────────────────────────────────────────────────────────────────

func (c *Connector) OrderGet(ctx context.Context, storeID string, orderID string) (*base.OrderEvent, error) {
	var resp shopifyAdminOrderResponse
	if err := c.http.AdminGET(ctx, fmt.Sprintf("/orders/%s.json", orderID), &resp); err != nil {
		return nil, fmt.Errorf("shopify: order get %s: %w", orderID, err)
	}
	return c.mapper.adminOrderToUCPEvent(resp.Order), nil
}

// UpdateProduct writes corrected product fields back to the merchant's store.
// TODO: implement per-connector (G2 write capability)
func (c *Connector) UpdateProduct(ctx context.Context, storeID string, itemID string, updates map[string]interface{}) error {
	return fmt.Errorf("UpdateProduct: not yet implemented")
}

// ─── Store-API pull methods ────────────────────────────────────────────────
// Shopify is frozen for CI-testing only (sarvex-v1-architecture.md) — these are minimal
// compile-compliance stubs, not a feature addition. No pull-sync support is
// implemented or planned for Shopify.

// FetchProduct implements base.Connector.
func (c *Connector) FetchProduct(ctx context.Context, storeID string, sku string) (*base.CatalogEntity, error) {
	return nil, fmt.Errorf("shopify: FetchProduct not supported")
}

// FetchInventory implements base.Connector.
func (c *Connector) FetchInventory(ctx context.Context, storeID string, sku string) (*base.InventoryEntity, error) {
	return nil, fmt.Errorf("shopify: FetchInventory not supported")
}

// FetchPricing implements base.Connector.
func (c *Connector) FetchPricing(ctx context.Context, storeID string, sku string) (*base.PricingEntity, error) {
	return nil, fmt.Errorf("shopify: FetchPricing not supported")
}

// FetchCatalog implements base.Connector.
func (c *Connector) FetchCatalog(ctx context.Context, storeID string, pageSize int) ([]base.CatalogEntity, error) {
	return nil, fmt.Errorf("shopify: FetchCatalog not supported")
}

// Ping implements base.Connector.
func (c *Connector) Ping(ctx context.Context, storeID string) (base.HealthStatus, error) {
	return base.HealthFailed, fmt.Errorf("shopify: Ping not supported")
}

// SimulateShipping triggers a fulfilled webhook via the backend simulator.
// orderID is the platform-native order ID as returned in the completed checkout
// response (e.g. "gid://shopify/Order/order-1"). Only available when the
// gateway is running with TestingEnvironment set.
func (c *Connector) SimulateShipping(ctx context.Context, _ string, orderID string) error {
	// Strip the GID prefix to get the raw order ID used by the simulator store.
	rawID := orderID
	if idx := strings.LastIndex(orderID, "/"); idx >= 0 {
		rawID = orderID[idx+1:]
	}

	url := fmt.Sprintf("%s://%s/simulate/shipping/%s", c.http.scheme, c.cfg.ShopDomain, rawID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("shopify: simulate shipping build request: %w", err)
	}

	resp, err := c.http.underlying.Do(req)
	if err != nil {
		return fmt.Errorf("shopify: simulate shipping request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: order %s", ErrNotFound, orderID)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("shopify: simulate shipping returned %d for order %s", resp.StatusCode, orderID)
	}
	return nil
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

func (c *Connector) buildCartLineInputs(items []base.LineItemCreateRequest) []map[string]any {
	lines := make([]map[string]any, 0, len(items))
	for _, li := range items {
		lines = append(lines, map[string]any{
			"merchandiseId": li.Item.ID,
			"quantity":      li.Quantity,
		})
	}
	return lines
}

func (c *Connector) buildCheckoutLineInputs(items []base.LineItemCreateRequest) []map[string]any {
	lines := make([]map[string]any, 0, len(items))
	for _, li := range items {
		lines = append(lines, map[string]any{
			"variantId": li.Item.ID,
			"quantity":  li.Quantity,
		})
	}
	return lines
}

// buildTokenizedPaymentInput converts a UCP SelectedPaymentInstrument into the shape
// Shopify expects for checkoutCompleteWithTokenizedPaymentV3.
func (c *Connector) buildTokenizedPaymentInput(instrument model.SelectedPaymentInstrument) (map[string]any, error) {
	if instrument.Credential == nil {
		return nil, errors.New("instrument has no credential")
	}
	if instrument.Credential.Token == nil {
		return nil, errors.New("instrument credential has no token")
	}

	billingAddr := map[string]any{
		"firstName": "",
		"lastName":  "",
		"address1":  "",
		"city":      "",
		"country":   "",
		"zip":       "",
	}
	if instrument.BillingAddress != nil {
		billingAddr = c.mapper.ucpAddressToShopifyMailingInput(instrument.BillingAddress)
	}

	return map[string]any{
		"paymentData":    instrument.Credential.Token.Token,
		"type":           instrument.Credential.Type,
		"billingAddress": billingAddr,
		"idempotencyKey": instrument.ID,
	}, nil
}

// applyDiscountCodes applies or removes discount codes on a Shopify checkout.
// Shopify supports one active discount code at a time via the Storefront API;
// to replace a code we remove the current one first then apply the new one.
// Sending an empty slice clears all codes.
func (c *Connector) applyDiscountCodes(ctx context.Context, checkoutID string, codes []string) error {
	// Always remove the current discount code first (idempotent if none applied).
	query, vars := discountCodeRemoveArgs(checkoutID)
	var removeData shopifyCheckoutDiscountCodeRemoveData
	if err := c.http.StorefrontGQL(ctx, query, vars, &removeData); err != nil {
		return fmt.Errorf("shopify: discount code remove: %w", err)
	}

	if len(codes) == 0 {
		return nil
	}

	// Apply the first code (Shopify Storefront API only supports one at a time).
	query, vars = discountCodeApplyArgs(checkoutID, codes[0])
	var applyData shopifyCheckoutDiscountCodeApplyData
	if err := c.http.StorefrontGQL(ctx, query, vars, &applyData); err != nil {
		return fmt.Errorf("shopify: discount code apply %q: %w", codes[0], err)
	}
	if len(applyData.CheckoutDiscountCodeApplyV2.UserErrors) > 0 {
		e := applyData.CheckoutDiscountCodeApplyV2.UserErrors[0]
		return fmt.Errorf("shopify: discount code rejected [%s]: %s", e.Code, e.Message)
	}
	return nil
}

// cartErrorResponse builds a CartResponse that carries error messages but
// still has a UCP envelope (status success — the request succeeded, the cart
// just has problems).
// ─── Buyer Consent extension (dev.ucp.shopping.buyer_consent) ────────────────

// carryConsent copies buyer consent from the request onto the response buyer
// object. Consent is a platform-supplied signal — the business (connector)
// stores it as needed for compliance and echoes it back unchanged so the
// platform knows it was received.
//
// This implements the echo-back requirement of the buyer consent extension:
// https://base.dev/2026-04-08/specification/buyer-consent/
func carryConsent(resp *base.CheckoutResponse, reqBuyer *model.BuyerWithConsent) {
	if reqBuyer == nil || reqBuyer.Consent == nil {
		return
	}
	if resp.Buyer == nil {
		resp.Buyer = &model.BuyerWithConsent{}
	}
	resp.Buyer.Consent = reqBuyer.Consent
}

func (c *Connector) cartErrorResponse(msgs []model.Message) *base.CartResponse {
	return &base.CartResponse{
		Messages: msgs,
	}
}

// checkoutErrorResponse builds a CheckoutResponse for unrecoverable errors
// where no checkout resource can be returned.
func (c *Connector) checkoutErrorResponse(msgs []model.Message, continueURL string) *base.CheckoutResponse {
	if hasUnrecoverable(msgs) {
		return &base.CheckoutResponse{
			Messages:    msgs,
			ContinueURL: continueURL,
		}
	}
	return &base.CheckoutResponse{
		Messages:    msgs,
		ContinueURL: continueURL,
	}
}

func hasUnrecoverable(msgs []model.Message) bool {
	for _, m := range msgs {
		if err, ok := m.AsError(); ok && err.Severity == model.MessageSeverityUnrecoverable {
			return true
		}
	}
	return false
}
