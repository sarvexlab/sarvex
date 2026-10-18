// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package base defines the Connector interface that all commerce backend adapters
// must implement to participate in the Sarvex UCP gateway. The interface maps
// directly to UCP capability operations; each method corresponds to one UCP
// operation as defined in the spec.
//
// Implementations live under internal/pkg/connectors/{platform}/ and are registered
// in the gateway router by merchant configuration.
package base

import "context"

// Connector is the full contract a backend adapter must satisfy to be a valid
// UCP gateway target. All methods are scoped to a single merchantID — the
// implementation is responsible for routing to the correct merchant account.
//
// Implementations MUST be safe for concurrent use.
type Connector interface {
	// ─── Catalog ──────────────────────────────────────────────────────────────

	// CatalogSearch executes a product search against the backend catalog and
	// returns UCP-normalised catalog items.
	CatalogSearch(ctx context.Context, storeID string, req *CatalogSearchRequest) (*CatalogSearchResponse, error)

	// CatalogLookup fetches a single product by its platform-native item ID.
	CatalogLookup(ctx context.Context, storeID string, itemID string) (*CatalogLookupResponse, error)

	// UpdateProduct writes corrected product fields back to the merchant's store.
	// Called by the sync plane when applying rule-based remediations.
	// The connector MUST handle the reversibility requirement: the sync plane provides
	// the old value for audit trail purposes. Returns an error if the write fails.
	// TODO: implement per-connector (WooCommerce, Magento, etc.)
	UpdateProduct(ctx context.Context, storeID string, itemID string, updates map[string]any) error

	// ─── Cart ─────────────────────────────────────────────────────────────────

	// CartCreate creates a new cart session and returns a CartResponse.
	CartCreate(ctx context.Context, storeID string, req *CreateCartRequest) (*CartResponse, error)

	// CartGet retrieves an existing cart by its gateway-assigned cart ID.
	CartGet(ctx context.Context, storeID string, cartID string) (*CartResponse, error)

	// CartUpdate replaces the full cart state (PUT semantics per spec).
	CartUpdate(ctx context.Context, storeID string, cartID string, req *UpdateCartRequest) (*CartResponse, error)

	// ─── Checkout ─────────────────────────────────────────────────────────────

	// CheckoutCreate creates a new checkout session. The response will always
	// include the negotiated payment_handlers for the merchant.
	CheckoutCreate(ctx context.Context, storeID string, req *CreateCheckoutRequest) (*CheckoutResponse, error)

	// CheckoutGet retrieves an existing checkout session by ID.
	CheckoutGet(ctx context.Context, storeID string, checkoutID string) (*CheckoutResponse, error)

	// CheckoutUpdate replaces the full checkout state (PUT semantics per spec).
	// Called repeatedly as the platform resolves recoverable errors.
	CheckoutUpdate(ctx context.Context, storeID string, checkoutID string, req *UpdateCheckoutRequest) (*CheckoutResponse, error)

	// CheckoutComplete attempts to finalise the checkout and place the order.
	// A successful response will have status = "completed" or
	// status = "complete_in_progress" (async processing).
	CheckoutComplete(ctx context.Context, storeID string, checkoutID string, req *CompleteCheckoutRequest) (*CheckoutResponse, error)

	// CheckoutCancel cancels the checkout session.
	CheckoutCancel(ctx context.Context, storeID string, checkoutID string) (*CheckoutResponse, error)

	// CheckoutURL returns the web checkout URL for the given session or cart ID.
	// Called by the gateway engine when emitting requires_escalation to populate
	// continue_url. The returned URL MUST resolve to the merchant's live checkout
	// page for this specific session so a buyer can resume their purchase.
	// Returns an error if the URL cannot be determined; the caller must fail loudly.
	CheckoutURL(ctx context.Context, storeID string, checkoutID string) (string, error)

	// ─── Order events ─────────────────────────────────────────────────────────

	// OrderGet retrieves a placed order by its platform-native order ID.
	// Used for post-completion status polling when webhooks are unavailable.
	OrderGet(ctx context.Context, storeID string, orderID string) (*OrderEvent, error)

	// ─── Store-API pull (sync-plane only — never call from the serving plane) ──
	// These back the sync plane's catalog ingestion, reconciliation sweep, and
	// connector health reporting. Per architecture invariant §4.5.2, the serving
	// plane reads only the serving cache and must never call these directly.

	// FetchProduct pulls the current full product record for storeID/sku.
	FetchProduct(ctx context.Context, storeID string, sku string) (*CatalogEntity, error)

	// FetchInventory pulls just the current stock state for storeID/sku.
	FetchInventory(ctx context.Context, storeID string, sku string) (*InventoryEntity, error)

	// FetchPricing pulls just the current price for storeID/sku.
	FetchPricing(ctx context.Context, storeID string, sku string) (*PricingEntity, error)

	// FetchCatalog pages through the store's full catalog and returns the
	// complete normalised set. pageSize <= 0 uses the connector's default.
	FetchCatalog(ctx context.Context, storeID string, pageSize int) ([]CatalogEntity, error)

	// Ping checks reachability of the store's backend for connector status
	// reporting (control plane connector health).
	Ping(ctx context.Context, storeID string) (HealthStatus, error)
}

// SimulationConnector is an optional interface for testing-only simulation.
// The gateway exposes POST /testing/simulate-shipping/:checkoutId when
// TestingEnvironment is non-empty, and delegates to this method.
// The orderID corresponds to the platform-native order ID returned by the
// completed checkout (e.g. "gid://shopify/Order/order-1" for Shopify).
type SimulationConnector interface {
	SimulateShipping(ctx context.Context, storeID string, orderID string) error
}
