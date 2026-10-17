// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/pkg/middlewares"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/negotiation"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wireadapt"
	"github.com/sarvexlab/sarvex/internal/pkg/utils"
	"github.com/sarvexlab/sarvex/internal/planes/serving"
)

type ServingHandler interface {
	utils.HandlerSetup
}

type servingHandler struct {
	logger      *slog.Logger
	config      *utils.Configurations
	deps        ServingDependencies
	rateLimiter serving.RateLimiter
	// checkout and catalog own their respective domain logic (session
	// persistence + connector orchestration; cache reads + pagination) —
	// this handler only binds requests to them and translates the
	// result/error to an HTTP response.
	checkout *serving.CheckoutService
	catalog  *serving.CatalogService
	order    *serving.OrderService
	cart     *serving.CartService
}

func NewServingHandler(
	config *utils.Configurations,
	deps ServingDependencies,
	logger *slog.Logger,
	sessionStore serving.SessionStore,
) ServingHandler {
	webhooks := serving.NewWebhookDeliverer(logger)
	cartStore := deps.CartStore
	if cartStore == nil {
		cartStore = serving.NewMemoryCartStore(24 * time.Hour)
	}
	return &servingHandler{
		logger:      logger,
		config:      config,
		deps:        deps,
		rateLimiter: deps.RateLimiter,
		checkout:    serving.NewCheckoutService(sessionStore, deps.ServingCache, deps.Registry, deps.EventPublisher, logger, webhooks),
		catalog:     serving.NewCatalogService(deps.ServingCache),
		order:       serving.NewOrderService(sessionStore, webhooks),
		cart:        serving.NewCartService(cartStore, deps.Registry, logger),
	}
}

func (h *servingHandler) SetupRoutes(groupName string, group *echo.Group) error {
	group.Use(middlewares.WithCorrelationID())
	group.Use(middlewares.WithRequestValidation(true))
	group.Use(middlewares.WithBodyLimit())
	group.Use(middlewares.WithTimeout())
	group.Use(middlewares.WithServerHeader())

	// WithTelemetry and WithIdentity are outermost/first: telemetry must
	// observe signature/negotiation failures that short-circuit the rest of
	// the chain, and it needs tenant identity (store/merchant id) resolved
	// to attribute the event — identity resolution only needs the storeID
	// path param, not a verified signature, so running it first doesn't
	// weaken signature verification for anything downstream.
	appliedMiddlewares := NewRequestPipeline().
		WithTelemetry(h.deps.EventPublisher, h.logger).
		WithIdentity(h.deps.StoreProvider).
		WithSignatureVerification(h.deps.SignatureConfig).
		WithNegotiation(h.deps.NegotiationService, h.deps.EventPublisher).
		WithRateLimiting(h.rateLimiter).
		WithRequestLogging(h.deps.EventPublisher, h.logger).
		WithResponseSigning(h.deps.SignatureConfig).
		Build()
	restrictedMiddlewares := NewRequestPipeline().
		WithTelemetry(h.deps.EventPublisher, h.logger).
		WithIdentity(h.deps.StoreProvider).
		WithRateLimiting(h.rateLimiter).
		WithRequestLogging(h.deps.EventPublisher, h.logger).
		Build()

	// Profile
	group.GET("/.well-known/ucp", h.GetProfile, restrictedMiddlewares...)

	// Catalog — read-only from cache. POST /catalog/search matches the
	// reconciled ucp.dev REST transport (dev.ucp.shopping.catalog.search,
	// source/services/shopping/rest.openapi.json in the pinned spec repo):
	// search_catalog is POST-only, taking a JSON body, not GET+query
	// params — each generation's codec (internal/spec/vYYYYMMDD/codec/
	// catalog.go) maps its request and response.
	group.POST("/catalog/search", h.CatalogSearch, appliedMiddlewares...)
	group.GET("/catalog/:itemId", h.CatalogLookup, appliedMiddlewares...)

	// Cart — the dev.ucp.shopping.cart capability: a pre-purchase basket with
	// merchant-estimated pricing.
	group.POST("/carts", h.CartCreate, appliedMiddlewares...)
	group.GET("/carts/:cartId", h.CartGet, appliedMiddlewares...)
	group.PUT("/carts/:cartId", h.CartUpdate, appliedMiddlewares...)

	// Checkout
	group.POST("/checkout-sessions", h.CheckoutCreate, appliedMiddlewares...)
	group.GET("/checkout-sessions/:checkoutId", h.CheckoutGet, appliedMiddlewares...)
	group.PUT("/checkout-sessions/:checkoutId", h.CheckoutUpdate, appliedMiddlewares...)
	group.POST("/checkout-sessions/:checkoutId/complete", h.CheckoutComplete, appliedMiddlewares...)
	group.POST("/checkout-sessions/:checkoutId/cancel", h.CheckoutCancel, appliedMiddlewares...)

	// Orders — the dev.ucp.shopping.order capability's REST surface. A
	// completed checkout session doubles as Sarvex's order record
	// (order.go's OrderService); there is no separate order store.
	group.GET("/orders/:orderId", h.OrderGet, appliedMiddlewares...)
	group.PUT("/orders/:orderId", h.OrderUpdate, appliedMiddlewares...)

	// Conformance/testing affordance (see SimulateShipping): a
	// secret-gated hook the official UCP conformance suite uses to drive a
	// shipment. Disabled entirely (404) unless SIMULATION_SECRET is set, which
	// only `sarvex conform serve` does — never production. Restricted
	// middlewares (identity + rate limit, no signature/negotiation): the shared
	// secret is the gate.
	group.POST("/testing/simulate-shipping/:orderId", h.SimulateShipping, restrictedMiddlewares...)

	return nil
}

// ─── Profile ─────────────────────────────────────────────────────────────────

func (h *servingHandler) GetProfile(c echo.Context) error {
	storeID := c.Param("storeID")
	instance, ok := negotiation.GetStoreProfile(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "instance not loaded")
	}

	profile, err := h.deps.ServingCache.GetProfile(c.Request().Context(), instance.StoreID())
	if err != nil {
		h.logger.Error("failed to get profile", "error", err, "store_id", storeID)
		return echo.NewHTTPError(http.StatusNotFound, "profile not found")
	}

	// Return spec-compliant UCP profile per https://ucp.dev/2026-04-08/specification/overview/#business-profile
	// Profile already contains all required fields (ucp, signing_keys) from cache
	return c.JSON(http.StatusOK, profile)
}

// ─── Catalog ─────────────────────────────────────────────────────────────────

func (h *servingHandler) CatalogSearch(c echo.Context) error {
	instance, ok := negotiation.GetStoreProfile(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "instance not loaded")
	}

	// The spec search_request body (dev.ucp.shopping.catalog.search) — POST
	// with a JSON body, not GET+query params; see the route registration's
	// doc comment in SetupRoutes — read in the negotiated generation's shape.
	v := requestVersion(c)
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	var query model.CatalogQuery
	if len(bytes.TrimSpace(body)) > 0 {
		if query, err = wireCodecs.DecodeCatalogSearch(v, body); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
		}
	}

	// limit/cursor are validated/defaulted by CatalogService.Search; 0 and ""
	// are "unset".
	result, err := h.catalog.Search(c.Request().Context(), instance.StoreID(), query.Query, query.Limit, query.Cursor)
	if err != nil {
		return h.mapCatalogError(err)
	}

	h.emitCatalogQuery(c, instance, serving.CatalogQuerySearch, matchedProductIDs(result))

	// Catalog reads are cache-only and side-effect-free (sarvex-v1-architecture.md A10.2), so
	// the page is rendered by whichever generation negotiation selected.
	out, err := wireCodecs.EncodeCatalogSearch(v, catalogPage(result, query))
	if err != nil {
		return h.renderFailure(c, v, "catalog_search", err)
	}
	return c.JSONBlob(http.StatusOK, out)
}

func (h *servingHandler) CatalogLookup(c echo.Context) error {
	instance, ok := negotiation.GetStoreProfile(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "instance not loaded")
	}

	itemID := c.Param("itemId")
	product, err := h.catalog.Lookup(c.Request().Context(), instance.StoreID(), itemID)

	// Recorded regardless of hit/miss (itemID, not product.SKU) — see
	// CatalogQueryRecordedEvent's doc comment on RequestedProductIDs.
	h.emitCatalogQuery(c, instance, serving.CatalogQueryLookup, []string{itemID})

	if err != nil {
		return h.mapCatalogError(err)
	}

	// Rendered as the spec's Product, the same shape search returns.
	// Returning the CacheProduct straight through served Sarvex's internal
	// projection to agents — flat sku/stock fields where the spec has
	// price_range and variants[].availability, plus last_score_time, which
	// is bookkeeping no agent should ever see.
	v := requestVersion(c)
	out, err := wireCodecs.EncodeCatalogProduct(v, catalogProduct(product))
	if err != nil {
		return h.renderFailure(c, v, "catalog_product", err)
	}
	return c.JSONBlob(http.StatusOK, out)
}

// matchedProductIDs extracts the SKUs of a search result's returned page —
// the "requested" signal for a search is what the agent was actually shown,
// not the free-text query that produced it (never captured, see
// CatalogQueryRecordedEvent).
func matchedProductIDs(result *serving.CatalogSearchResult) []string {
	ids := make([]string, len(result.Items))
	for i, p := range result.Items {
		ids[i] = p.SKU
	}
	return ids
}

// emitCatalogQuery publishes a CatalogQueryRecordedEvent for a catalog
// search/lookup request, fire-and-forget (EmitCatalogQueryRecorded never
// blocks the response).
func (h *servingHandler) emitCatalogQuery(c echo.Context, instance ucp.StoreProvider, kind serving.CatalogQueryKind, requestedProductIDs []string) {
	ev := serving.NewCatalogQueryRecordedEvent(
		instance.StoreID(),
		instance.Configuration().MerchantID,
		kind,
		agentPlatformFromRequest(c.Request()),
		requestedProductIDs,
	)
	serving.EmitCatalogQueryRecorded(h.deps.EventPublisher, h.logger, ev)
}

// ─── Cart ────────────────────────────────────────────────────────────────────
// Request wire types live in cart_contract.go; orchestration in
// serving.CartService. Errors share mapCheckoutError's mapping.

func (h *servingHandler) CartCreate(c echo.Context) error {
	instance, ok := negotiation.GetStoreProfile(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "instance not loaded")
	}

	var req CartCreateRequest
	if _, err := h.bind(c, &req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	resp, err := h.cart.Create(c.Request().Context(), instance, instance.StoreID(), serving.CreateCartInput{
		LineItems:      req.LineItems,
		Buyer:          req.Buyer,
		Context:        req.Context,
		Discounts:      req.Discounts,
		IdempotencyKey: c.Request().Header.Get("Idempotency-Key"),
		BodyHash:       computeCartCreateBodyHash(&req),
	})
	if err != nil {
		return h.mapCheckoutError(c, err)
	}
	return h.respond(c, http.StatusCreated, wireadapt.Cart, resp)
}

func (h *servingHandler) CartGet(c echo.Context) error {
	instance, ok := negotiation.GetStoreProfile(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "instance not loaded")
	}

	resp, err := h.cart.Get(c.Request().Context(), instance.StoreID(), c.Param("cartId"))
	if err != nil {
		return h.mapCheckoutError(c, err)
	}
	return h.respond(c, http.StatusOK, wireadapt.Cart, resp)
}

func (h *servingHandler) CartUpdate(c echo.Context) error {
	instance, ok := negotiation.GetStoreProfile(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "instance not loaded")
	}

	var req CartUpdateRequest
	if _, err := h.bind(c, &req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	resp, err := h.cart.Update(c.Request().Context(), instance, instance.StoreID(), c.Param("cartId"), serving.UpdateCartInput{
		LineItems: req.LineItems,
		Buyer:     req.Buyer,
		Discounts: req.Discounts,
	})
	if err != nil {
		return h.mapCheckoutError(c, err)
	}
	return h.respond(c, http.StatusOK, wireadapt.Cart, resp)
}

// ─── Checkout ────────────────────────────────────────────────────────────────
// Request/response wire types and error-status mapping live in
// checkout_contract.go — these methods only bind, call the service, and
// write the result.

func (h *servingHandler) CheckoutCreate(c echo.Context) error {
	instance, ok := negotiation.GetStoreProfile(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "instance not loaded")
	}
	storeID := instance.StoreID()

	var req CheckoutCreateRequest
	extras, err := h.bind(c, &req)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	// Compute hash of normalized request body for idempotency conflict detection.
	// Hash captures the semantic payload (line items, buyer, fulfillment, payment)
	// and enables efficient, normalized comparison when the same idempotency key
	// is reused with a different request body.
	bodyHash := computeCheckoutCreateBodyHash(&req)

	var webhookURL string
	if profile, ok := negotiation.GetPlatformProfile(c); ok {
		webhookURL = extractOrderWebhookURL(profile)
	}

	// A checkout created from a cart starts from the cart's state; anything the
	// request states itself wins. The cart is resolved fenced by this store, so
	// another store's cart id is simply not found.
	lineItems, buyer, discounts := req.LineItems, req.Buyer, req.Discounts
	if req.CartID != "" {
		seed, err := h.cart.Seed(c.Request().Context(), storeID, req.CartID)
		if err != nil {
			return h.mapCheckoutError(c, err)
		}
		if len(lineItems) == 0 {
			lineItems = seed.LineItems
		}
		if buyer == nil && seed.Buyer != nil {
			buyer = &model.BuyerWithConsent{Buyer: *seed.Buyer}
		}
		if discounts == nil {
			discounts = seed.Discounts
		}
	}

	resp, err := h.checkout.Create(c.Request().Context(), instance, storeID, serving.CreateCheckoutInput{
		LineItems:      lineItems,
		Buyer:          buyer,
		Fulfillment:    req.Fulfillment.toBase(),
		Payment:        req.Payment,
		Discounts:      discounts,
		WebhookURL:     webhookURL,
		IdempotencyKey: c.Request().Header.Get("Idempotency-Key"),
		BodyHash:       bodyHash,
		Surface:        checkoutSurface,
		AgentPlatform:  agentPlatformFromRequest(c.Request()),

		SpecVersion:      string(requestVersion(c)),
		BuyerConsentWire: extras.BuyerConsent,
	})
	if err != nil {
		return h.mapCheckoutError(c, err)
	}

	return h.respond(c, http.StatusCreated, wireadapt.Checkout, resp)
}

func (h *servingHandler) CheckoutGet(c echo.Context) error {
	instance, ok := negotiation.GetStoreProfile(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "instance not loaded")
	}

	resp, err := h.checkout.GetResponse(c.Request().Context(), instance.StoreID(), c.Param("checkoutId"))
	if err != nil {
		return h.mapCheckoutError(c, err)
	}

	return h.respond(c, http.StatusOK, wireadapt.Checkout, resp)
}

func (h *servingHandler) CheckoutCancel(c echo.Context) error {
	instance, ok := negotiation.GetStoreProfile(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "instance not loaded")
	}

	resp, err := h.checkout.Cancel(c.Request().Context(), instance.StoreID(), c.Param("checkoutId"))
	if err != nil {
		return h.mapCheckoutError(c, err)
	}

	return h.respond(c, http.StatusOK, wireadapt.Checkout, resp)
}

func (h *servingHandler) CheckoutUpdate(c echo.Context) error {
	instance, ok := negotiation.GetStoreProfile(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "instance not loaded")
	}
	storeID := instance.StoreID()

	var req CheckoutUpdateRequest
	extras, err := h.bind(c, &req)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	var webhookURL string
	if profile, ok := negotiation.GetPlatformProfile(c); ok {
		webhookURL = extractOrderWebhookURL(profile)
	}

	resp, err := h.checkout.Update(c.Request().Context(), instance, storeID, c.Param("checkoutId"), serving.UpdateCheckoutInput{
		LineItems:      req.LineItems,
		Buyer:          req.Buyer,
		Fulfillment:    req.Fulfillment.toBase(),
		Payment:        req.Payment,
		Discounts:      req.Discounts,
		WebhookURL:     webhookURL,
		IdempotencyKey: c.Request().Header.Get("Idempotency-Key"),
		BodyHash:       computeCheckoutUpdateBodyHash(&req),

		SpecVersion:      string(requestVersion(c)),
		BuyerConsentWire: extras.BuyerConsent,
	})
	if err != nil {
		return h.mapCheckoutError(c, err)
	}

	return h.respond(c, http.StatusOK, wireadapt.Checkout, resp)
}

func (h *servingHandler) CheckoutComplete(c echo.Context) error {
	instance, ok := negotiation.GetStoreProfile(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "instance not loaded")
	}
	storeID := instance.StoreID()

	var req CheckoutCompleteRequest
	if _, err := h.bind(c, &req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	// Convert the opaque credential to a safe Payment structure (no raw credentials).
	// The OpaquePaymentCredential ensures that raw credential data (card numbers, CVCs)
	// never reaches CheckoutService.Complete, enforcing the architectural invariant.
	var payment *model.Payment
	if req.Credential != nil {
		payment = req.Credential.ToPayment()
	}

	// Idempotency-Key travels in the header, the same transport Create uses
	// (see CheckoutCreateRequest's doc comment) — req.IdempotencyKey is a
	// body-carried fallback for callers that put it there instead.
	idempotencyKey := c.Request().Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		idempotencyKey = req.IdempotencyKey
	}

	result, err := h.checkout.Complete(c.Request().Context(), instance, storeID, c.Param("checkoutId"), payment, idempotencyKey)
	if err != nil {
		return h.mapCheckoutError(c, err)
	}

	// The checkout capability's complete endpoint responds with the full
	// Checkout resource (status completed/requires_escalation, totals, links,
	// order confirmation), which the connector already produced — return it
	// verbatim. The bespoke envelope is a fallback only for the rare path that
	// reached a result without a spec-shaped response to carry.
	// Response is signed per spec by response middleware.
	if result.Response != nil {
		return h.respond(c, http.StatusOK, wireadapt.Checkout, result.Response)
	}
	return c.JSON(http.StatusOK, checkoutCompleteBody(result))
}

// ─── Orders ──────────────────────────────────────────────────────────────────
// Request/response wire types and error mapping live in order_contract.go.

func (h *servingHandler) OrderGet(c echo.Context) error {
	instance, ok := negotiation.GetStoreProfile(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "instance not loaded")
	}

	order, err := h.order.Get(c.Request().Context(), instance.StoreID(), c.Param("orderId"))
	if err != nil {
		return h.mapOrderError(c, err)
	}

	return h.respond(c, http.StatusOK, wireadapt.Order, order)
}

func (h *servingHandler) OrderUpdate(c echo.Context) error {
	instance, ok := negotiation.GetStoreProfile(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "instance not loaded")
	}

	// Both a structurally malformed body (e.g. "adjustments" sent as an
	// object instead of an array) and a semantically invalid one (an
	// adjustment naming a status/type the spec doesn't define) are
	// UnprocessableEntity here — order_test.py's invalid-input checks
	// don't distinguish the two, and neither is a case where retrying the
	// same bytes would ever succeed.
	var req OrderUpdateRequest
	if _, err := h.bind(c, &req); err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "malformed request body")
	}
	in, err := req.toInput()
	if err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}

	order, err := h.order.Update(c.Request().Context(), instance.StoreID(), c.Param("orderId"), in)
	if err != nil {
		return h.mapOrderError(c, err)
	}

	return h.respond(c, http.StatusOK, wireadapt.Order, order)
}

// computeCheckoutCreateBodyHash computes a SHA256 hash of the normalized request body
// for idempotency conflict detection. The hash enables efficient comparison when an
// idempotency key is reused: if the hash differs, the request body has changed and
// should be rejected as a conflict.
func computeCheckoutCreateBodyHash(req *CheckoutCreateRequest) string {
	if req == nil {
		return ""
	}

	// Marshal the request to canonical JSON (preserves field order, no extra whitespace).
	// This ensures the same semantic request always produces the same hash.
	data, err := json.Marshal(req)
	if err != nil {
		// If marshaling fails, return empty hash (idempotency falls back to line item comparison).
		return ""
	}

	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash)
}

// computeCheckoutUpdateBodyHash hashes an update request body the same way
// computeCheckoutCreateBodyHash hashes a create body, so a replayed PUT with an
// unchanged body compares equal and a changed one conflicts.
func computeCheckoutUpdateBodyHash(req *CheckoutUpdateRequest) string {
	if req == nil {
		return ""
	}
	data, err := json.Marshal(req)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash)
}
