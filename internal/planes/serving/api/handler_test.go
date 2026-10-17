// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/commerce/model"

	"github.com/sarvexlabs/sarvex.cp/internal/platform/woocommerce"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/negotiation"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wire"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/utils"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/serving"
)

// ──── Stubs ──────────────────────────────────────────────────────────────────

type stubServingCache struct {
	profile   *serving.CacheProfile
	products  map[string]*serving.CacheProduct
	watermark *serving.CacheWatermark
}

func (s *stubServingCache) GetProduct(ctx context.Context, merchantID, sku string) (*serving.CacheProduct, error) {
	if p, ok := s.products[sku]; ok {
		return p, nil
	}
	return nil, serving.ErrNotFound
}

func (s *stubServingCache) Search(ctx context.Context, merchantID, query string, limit int, cursor string) (*serving.CacheSearchResult, error) {
	items := make([]*serving.CacheProduct, 0)
	for _, p := range s.products {
		items = append(items, p)
	}
	return &serving.CacheSearchResult{Items: items, HasMore: false}, nil
}

func (s *stubServingCache) GetProfile(ctx context.Context, merchantID string) (*serving.CacheProfile, error) {
	if s.profile == nil {
		return nil, serving.ErrNotFound
	}
	return s.profile, nil
}

func (s *stubServingCache) Watermark(ctx context.Context, merchantID string) (*serving.CacheWatermark, error) {
	if s.watermark != nil {
		return s.watermark, nil
	}
	return &serving.CacheWatermark{
		BuiltAt:    time.Now().Add(-2 * time.Minute),
		SourceSync: time.Now().Add(-1 * time.Minute),
		IsStale:    false,
		Age:        1 * time.Minute,
	}, nil
}

// stubEventPublisher is shared across concurrent goroutines in tests that
// exercise the full HTTP pipeline: WithTelemetry and the versioned
// checkout lifecycle contract (internal/planes/serving/lifecycle_contract.go)
// both publish fire-and-forget off the request goroutine (R6), so mu
// guards every access.
type stubEventPublisher struct {
	mu        sync.Mutex
	published []eventbus.DomainEvent
}

func (s *stubEventPublisher) Publish(ctx context.Context, event eventbus.DomainEvent) error {
	s.mu.Lock()
	s.published = append(s.published, event)
	s.mu.Unlock()
	return nil
}

func (s *stubEventPublisher) Subscribe(ctx context.Context, topic string, handler eventbus.EventHandler) (eventbus.Subscription, error) {
	return nil, nil
}

func (s *stubEventPublisher) Unsubscribe(ctx context.Context, sub eventbus.Subscription) error {
	return nil
}

func (s *stubEventPublisher) Close() error { return nil }

// stubStoreProvider satisfies ucp.StoreProvider for handler tests.
type stubStoreProvider struct {
	storeID string
}

func (s *stubStoreProvider) StoreID() string                            { return s.storeID }
func (s *stubStoreProvider) Version() string                            { return "2026-04-08" }
func (s *stubStoreProvider) Services() map[string][]wire.Service        { return nil }
func (s *stubStoreProvider) Capabilities() map[string][]wire.Capability { return nil }

// PaymentHandlers advertises "googlepay" — the handler every CheckoutComplete
// test in this package submits — matching how CheckoutService.Complete now
// validates the submitted handler_id against what the store advertises.
func (s *stubStoreProvider) PaymentHandlers() map[string][]wire.PaymentHandler {
	return map[string][]wire.PaymentHandler{
		"com.google.pay": {{ID: "googlepay"}},
	}
}
func (s *stubStoreProvider) SigningKeys() []wire.JWK { return nil }
func (s *stubStoreProvider) StorefrontURL() string   { return "" }
func (s *stubStoreProvider) Configuration() ucp.Configuration {
	return ucp.Configuration{
		MerchantID: s.storeID,
		StoreID:    s.storeID,
		Platform:   woocommerce.PlatformName,
	}
}

// stubConnector records which checkout methods were called and with what args.
type stubConnector struct {
	createCalled bool
	createReq    *base.CreateCheckoutRequest
	createResp   *base.CheckoutResponse
	createErr    error

	getCalled bool
	getID     string
	getResp   *base.CheckoutResponse
	getErr    error

	updateCalled bool
	updateID     string
	updateReq    *base.UpdateCheckoutRequest
	updateResp   *base.CheckoutResponse
	updateErr    error

	cartCreateCalls int
	cartCreateReq   *base.CreateCartRequest
	cartCreateResp  *base.CartResponse
	cartCreateErr   error
	cartUpdateID    string
	cartUpdateReq   *base.UpdateCartRequest
	cartUpdateResp  *base.CartResponse
	cartUpdateErr   error

	completeCalled bool
	completeID     string
	completeReq    *base.CompleteCheckoutRequest
	completeResp   *base.CheckoutResponse
	completeErr    error
}

func (s *stubConnector) CheckoutCreate(ctx context.Context, storeID string, req *base.CreateCheckoutRequest) (*base.CheckoutResponse, error) {
	s.createCalled = true
	s.createReq = req
	return s.createResp, s.createErr
}

func (s *stubConnector) CheckoutUpdate(ctx context.Context, storeID, checkoutID string, req *base.UpdateCheckoutRequest) (*base.CheckoutResponse, error) {
	s.updateCalled = true
	s.updateID = checkoutID
	s.updateReq = req
	return s.updateResp, s.updateErr
}

func (s *stubConnector) CheckoutComplete(ctx context.Context, storeID, checkoutID string, req *base.CompleteCheckoutRequest) (*base.CheckoutResponse, error) {
	s.completeCalled = true
	s.completeID = checkoutID
	s.completeReq = req
	return s.completeResp, s.completeErr
}

// Unused connector methods — satisfy the interface.
func (s *stubConnector) CatalogSearch(ctx context.Context, storeID string, req *base.CatalogSearchRequest) (*base.CatalogSearchResponse, error) {
	return nil, nil
}
func (s *stubConnector) CatalogLookup(ctx context.Context, storeID string, itemID string) (*base.CatalogLookupResponse, error) {
	return nil, nil
}
func (s *stubConnector) UpdateProduct(ctx context.Context, storeID string, itemID string, updates map[string]any) error {
	return nil
}
func (s *stubConnector) CartCreate(ctx context.Context, storeID string, req *base.CreateCartRequest) (*base.CartResponse, error) {
	s.cartCreateCalls++
	s.cartCreateReq = req
	return s.cartCreateResp, s.cartCreateErr
}
func (s *stubConnector) CartGet(ctx context.Context, storeID string, cartID string) (*base.CartResponse, error) {
	return nil, nil
}
func (s *stubConnector) CartUpdate(ctx context.Context, storeID string, cartID string, req *base.UpdateCartRequest) (*base.CartResponse, error) {
	s.cartUpdateID = cartID
	s.cartUpdateReq = req
	return s.cartUpdateResp, s.cartUpdateErr
}
func (s *stubConnector) CheckoutGet(ctx context.Context, storeID string, checkoutID string) (*base.CheckoutResponse, error) {
	s.getCalled = true
	s.getID = checkoutID
	return s.getResp, s.getErr
}
func (s *stubConnector) CheckoutCancel(ctx context.Context, storeID string, checkoutID string) (*base.CheckoutResponse, error) {
	return nil, nil
}
func (s *stubConnector) CheckoutURL(ctx context.Context, storeID string, checkoutID string) (string, error) {
	return "", nil
}
func (s *stubConnector) OrderGet(ctx context.Context, storeID string, orderID string) (*base.OrderEvent, error) {
	return nil, nil
}

// Store-API pull methods — unused by these HTTP tests, satisfy the interface.
func (s *stubConnector) FetchProduct(ctx context.Context, storeID, sku string) (*base.CatalogEntity, error) {
	return nil, nil
}
func (s *stubConnector) FetchInventory(ctx context.Context, storeID, sku string) (*base.InventoryEntity, error) {
	return nil, nil
}
func (s *stubConnector) FetchPricing(ctx context.Context, storeID, sku string) (*base.PricingEntity, error) {
	return nil, nil
}
func (s *stubConnector) FetchCatalog(ctx context.Context, storeID string, pageSize int) ([]base.CatalogEntity, error) {
	return nil, nil
}
func (s *stubConnector) Ping(ctx context.Context, storeID string) (base.HealthStatus, error) {
	return base.HealthHealthy, nil
}

// noTestCredentials is a CredentialsProvider for tests whose registered
// connectors never invoke the store-API pull methods.
var noTestCredentials = base.CredentialsFunc(func(_ context.Context, _ string) (map[string]any, error) {
	return nil, fmt.Errorf("no credentials provider configured for this test")
})

// newTestHandler wires a handler with in-memory stores and the given connector stub.
func newTestHandler(sessionStore serving.SessionStore, conn *stubConnector, cache *stubServingCache, storeID string) *servingHandler {
	reg := base.New(noTestCredentials, slog.Default())
	reg.Set(woocommerce.PlatformName, storeID, conn)

	if cache == nil {
		cache = &stubServingCache{}
	}

	events := &stubEventPublisher{}
	return &servingHandler{
		logger: slog.Default(),
		config: &utils.Configurations{},
		deps: ServingDependencies{
			Registry:       reg,
			ServingCache:   cache,
			EventPublisher: events,
		},
		checkout: serving.NewCheckoutService(sessionStore, cache, reg, events, slog.Default()),
		catalog:  serving.NewCatalogService(cache),
	}
}

// newEchoContext creates a minimal Echo context with the store profile injected.
func newEchoContext(e *echo.Echo, method, path string, body []byte, storeID string) (echo.Context, *httptest.ResponseRecorder) {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	negotiation.SetStoreProfile(c, &stubStoreProvider{storeID: storeID})
	return c, rec
}

// ──── CheckoutCreate tests ────────────────────────────────────────────────────

func TestCheckoutCreate_CallsConnectorAndStoresBackendID(t *testing.T) {
	const storeID = "store-abc"
	conn := &stubConnector{
		createResp: &base.CheckoutResponse{
			ID:     "backend-123",
			Status: base.CheckoutStatusIncomplete,
		},
	}
	sessionStore := serving.NewMemorySessionStore(5 * time.Minute)
	defer sessionStore.Close()
	h := newTestHandler(sessionStore, conn, nil, storeID)

	body, _ := json.Marshal(CheckoutCreateRequest{
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 2}},
	})

	e := echo.New()
	c, rec := newEchoContext(e, http.MethodPost, "/checkout-sessions", body, storeID)

	if err := h.CheckoutCreate(c); err != nil {
		t.Fatalf("CheckoutCreate returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rec.Code)
	}
	if !conn.createCalled {
		t.Error("expected connector.CheckoutCreate to be called")
	}
	if len(conn.createReq.LineItems) != 1 || conn.createReq.LineItems[0].Item.ID != "SKU-1" {
		t.Errorf("unexpected line items passed to connector: %+v", conn.createReq.LineItems)
	}

	// The response id must be Sarvex's session id, not the backend's raw
	// checkout id — every subsequent /checkout-sessions/{id} call is keyed
	// on this value, so leaking the backend id here would make follow-up
	// calls unroutable.
	var resp base.CheckoutResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.ID == "" || resp.ID == "backend-123" {
		t.Fatalf("expected response id to be the session id, not the backend checkout id, got %q", resp.ID)
	}

	// BackendCheckoutID must be persisted on the session, addressable by
	// the id the agent was just given.
	session, err := sessionStore.Get(context.Background(), storeID, resp.ID)
	if err != nil {
		t.Fatalf("expected response id %q to resolve to a session: %v", resp.ID, err)
	}
	if session.BackendCheckoutID != "backend-123" {
		t.Errorf("expected session's backend_checkout_id to be backend-123, got %q", session.BackendCheckoutID)
	}
}

func TestCheckoutCreate_EmptyCart_ReturnsBadRequest(t *testing.T) {
	const storeID = "store-abc"
	sessionStore := serving.NewMemorySessionStore(5 * time.Minute)
	defer sessionStore.Close()
	h := newTestHandler(sessionStore, &stubConnector{}, nil, storeID)

	body, _ := json.Marshal(CheckoutCreateRequest{LineItems: []base.LineItemCreateRequest{}})
	e := echo.New()
	c, rec := newEchoContext(e, http.MethodPost, "/checkout-sessions", body, storeID)

	err := h.CheckoutCreate(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok || httpErr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 bad request, got err=%v rec=%d", err, rec.Code)
	}
}

func TestCheckoutCreate_ConnectorError_Returns503(t *testing.T) {
	const storeID = "store-abc"
	conn := &stubConnector{createErr: context.DeadlineExceeded}
	sessionStore := serving.NewMemorySessionStore(5 * time.Minute)
	defer sessionStore.Close()
	h := newTestHandler(sessionStore, conn, nil, storeID)

	body, _ := json.Marshal(CheckoutCreateRequest{
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
	})
	e := echo.New()
	c, _ := newEchoContext(e, http.MethodPost, "/checkout-sessions", body, storeID)

	err := h.CheckoutCreate(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok || httpErr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %v", err)
	}
}

func TestCheckoutCreate_IdempotencyReplay_ReturnsExistingSession(t *testing.T) {
	const storeID = "store-abc"
	conn := &stubConnector{
		getResp: &base.CheckoutResponse{
			ID:     "backend-123",
			Status: base.CheckoutStatusIncomplete,
		},
	}
	sessionStore := serving.NewMemorySessionStore(5 * time.Minute)
	defer sessionStore.Close()

	// Pre-seed a session that already completed Create successfully.
	existing := &serving.CheckoutSession{
		SessionID:         "sess-idem",
		StoreID:           storeID,
		MerchantID:        storeID,
		BackendCheckoutID: "backend-123",
		State:             serving.SessionStateIncomplete,
		IdempotencyKey:    "idem-key-1",
		LineItems:         []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}
	sessionStore.Create(context.Background(), existing)

	h := newTestHandler(sessionStore, conn, nil, storeID)

	body, _ := json.Marshal(CheckoutCreateRequest{
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
	})
	e := echo.New()
	c, rec := newEchoContext(e, http.MethodPost, "/checkout-sessions", body, storeID)
	c.Request().Header.Set("Idempotency-Key", "idem-key-1")

	if err := h.CheckoutCreate(c); err != nil {
		t.Fatalf("CheckoutCreate returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rec.Code)
	}
	// Must replay via CheckoutGet, not create a new backend session.
	if conn.createCalled {
		t.Error("connector.CheckoutCreate must NOT be called on idempotent replay")
	}
	if !conn.getCalled {
		t.Error("expected connector.CheckoutGet to be called for idempotent replay")
	}
	if conn.getID != "backend-123" {
		t.Errorf("expected CheckoutGet called with backend-123, got %q", conn.getID)
	}
}

func TestCheckoutCreate_IdempotencyReplay_PreviousFailure_ReturnsConflict(t *testing.T) {
	const storeID = "store-abc"
	sessionStore := serving.NewMemorySessionStore(5 * time.Minute)
	defer sessionStore.Close()

	// Session exists but connector Create failed (no BackendCheckoutID).
	failed := &serving.CheckoutSession{
		SessionID:      "sess-failed",
		StoreID:        storeID,
		MerchantID:     storeID,
		State:          serving.SessionStateFailed,
		IdempotencyKey: "idem-key-fail",
		LineItems:      []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	sessionStore.Create(context.Background(), failed)

	h := newTestHandler(sessionStore, &stubConnector{}, nil, storeID)

	body, _ := json.Marshal(CheckoutCreateRequest{
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
	})
	e := echo.New()
	c, _ := newEchoContext(e, http.MethodPost, "/checkout-sessions", body, storeID)
	c.Request().Header.Set("Idempotency-Key", "idem-key-fail")

	err := h.CheckoutCreate(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok || httpErr.Code != http.StatusConflict {
		t.Errorf("expected 409 conflict, got %v", err)
	}
}

// ──── CheckoutUpdate tests ────────────────────────────────────────────────────

func TestCheckoutUpdate_CallsConnectorAndTransitionsState(t *testing.T) {
	const storeID = "store-abc"
	conn := &stubConnector{
		updateResp: &base.CheckoutResponse{
			ID:     "backend-123",
			Status: base.CheckoutStatusReadyForComplete,
		},
	}
	sessionStore := serving.NewMemorySessionStore(5 * time.Minute)
	defer sessionStore.Close()

	// Pre-seed a session with a BackendCheckoutID.
	seed := &serving.CheckoutSession{
		SessionID:         "sess-1",
		StoreID:           storeID,
		MerchantID:        storeID,
		BackendCheckoutID: "backend-123",
		State:             serving.SessionStateIncomplete,
		LineItems:         []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}
	if err := sessionStore.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	h := newTestHandler(sessionStore, conn, nil, storeID)

	body, _ := json.Marshal(CheckoutUpdateRequest{
		Fulfillment: &wireFulfillmentRequest{
			Methods: []wireFulfillmentMethod{{
				Type:                  "shipping",
				SelectedDestinationID: "dest_1",
				Destinations: []wireFulfillmentDestination{{
					ID: "dest_1",
					PostalAddress: model.PostalAddress{
						FirstName: "Jane", LastName: "Doe",
						StreetAddress: "1 Main St", AddressLocality: "Springfield",
						AddressRegion: "IL", PostalCode: "62701", AddressCountry: "US",
					},
				}},
			}},
		},
	})
	e := echo.New()
	c, rec := newEchoContext(e, http.MethodPut, "/checkout-sessions/sess-1", body, storeID)
	c.SetParamNames("checkoutId")
	c.SetParamValues("sess-1")

	if err := h.CheckoutUpdate(c); err != nil {
		t.Fatalf("CheckoutUpdate returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !conn.updateCalled {
		t.Error("expected connector.CheckoutUpdate to be called")
	}
	if conn.updateID != "backend-123" {
		t.Errorf("expected backend-123 passed to connector, got %q", conn.updateID)
	}

	// Session state must be persisted as ready_for_complete.
	updated, _ := sessionStore.Get(context.Background(), storeID, "sess-1")
	if updated.State != serving.SessionStateReadyForComplete {
		t.Errorf("expected state ready_for_complete, got %q", updated.State)
	}
}

func TestCheckoutUpdate_MissingBackendID_ReturnsConflict(t *testing.T) {
	const storeID = "store-abc"
	sessionStore := serving.NewMemorySessionStore(5 * time.Minute)
	defer sessionStore.Close()

	seed := &serving.CheckoutSession{
		SessionID:  "sess-2",
		StoreID:    storeID,
		MerchantID: storeID,
		State:      serving.SessionStateIncomplete, // no BackendCheckoutID
		LineItems:  []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	sessionStore.Create(context.Background(), seed)

	h := newTestHandler(sessionStore, &stubConnector{}, nil, storeID)

	body, _ := json.Marshal(CheckoutUpdateRequest{})
	e := echo.New()
	c, _ := newEchoContext(e, http.MethodPut, "/checkout-sessions/sess-2", body, storeID)
	c.SetParamNames("checkoutId")
	c.SetParamValues("sess-2")

	err := h.CheckoutUpdate(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok || httpErr.Code != http.StatusConflict {
		t.Errorf("expected 409 conflict, got %v", err)
	}
}

// ──── CheckoutComplete tests ──────────────────────────────────────────────────

func TestCheckoutComplete_CallsConnectorCompleteNotCreate(t *testing.T) {
	const storeID = "store-abc"
	conn := &stubConnector{
		completeResp: &base.CheckoutResponse{
			ID:     "backend-123",
			Status: base.CheckoutStatusCompleted,
			Order: &model.OrderConfirmation{
				ID: "order-999",
			},
		},
	}
	sessionStore := serving.NewMemorySessionStore(5 * time.Minute)
	defer sessionStore.Close()

	seed := &serving.CheckoutSession{
		SessionID:         "sess-3",
		StoreID:           storeID,
		MerchantID:        storeID,
		BackendCheckoutID: "backend-123",
		State:             serving.SessionStateReadyForComplete,
		LineItems:         []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}
	sessionStore.Create(context.Background(), seed)

	h := newTestHandler(sessionStore, conn, nil, storeID)

	body, _ := json.Marshal(CheckoutCompleteRequest{
		Credential: NewOpaquePaymentCredential("googlepay", "opaque_token_12345"),
	})
	e := echo.New()
	c, rec := newEchoContext(e, http.MethodPost, "/checkout-sessions/sess-3/complete", body, storeID)
	c.SetParamNames("checkoutId")
	c.SetParamValues("sess-3")

	if err := h.CheckoutComplete(c); err != nil {
		t.Fatalf("CheckoutComplete returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if conn.createCalled {
		t.Error("connector.CheckoutCreate must NOT be called during complete")
	}
	if !conn.completeCalled {
		t.Error("expected connector.CheckoutComplete to be called")
	}
	if conn.completeID != "backend-123" {
		t.Errorf("expected backend-123 passed to connector.CheckoutComplete, got %q", conn.completeID)
	}

	// Session must transition to committed_pending_handoff.
	committed, _ := sessionStore.Get(context.Background(), storeID, "sess-3")
	if committed.State != serving.SessionStateCommittedPendingHandoff {
		t.Errorf("expected committed_pending_handoff, got %q", committed.State)
	}
}

func TestCheckoutComplete_FreshnessGuard_StaleCache(t *testing.T) {
	const storeID = "store-abc"
	staleCache := &stubServingCache{
		watermark: &serving.CacheWatermark{
			IsStale: true,
			Age:     10 * time.Minute,
		},
	}
	sessionStore := serving.NewMemorySessionStore(5 * time.Minute)
	defer sessionStore.Close()

	seed := &serving.CheckoutSession{
		SessionID:         "sess-4",
		StoreID:           storeID,
		MerchantID:        storeID,
		BackendCheckoutID: "backend-456",
		State:             serving.SessionStateReadyForComplete,
		LineItems:         []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}
	sessionStore.Create(context.Background(), seed)

	h := newTestHandler(sessionStore, &stubConnector{}, staleCache, storeID)

	body, _ := json.Marshal(CheckoutCompleteRequest{})
	e := echo.New()
	c, _ := newEchoContext(e, http.MethodPost, "/checkout-sessions/sess-4/complete", body, storeID)
	c.SetParamNames("checkoutId")
	c.SetParamValues("sess-4")

	err := h.CheckoutComplete(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok || httpErr.Code != http.StatusConflict {
		t.Errorf("expected 409 from freshness guard, got %v", err)
	}
}

func TestCheckoutComplete_MissingBackendID_ReturnsConflict(t *testing.T) {
	const storeID = "store-abc"
	sessionStore := serving.NewMemorySessionStore(5 * time.Minute)
	defer sessionStore.Close()

	seed := &serving.CheckoutSession{
		SessionID:  "sess-5",
		StoreID:    storeID,
		MerchantID: storeID,
		State:      serving.SessionStateIncomplete, // no BackendCheckoutID
		LineItems:  []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	sessionStore.Create(context.Background(), seed)

	h := newTestHandler(sessionStore, &stubConnector{}, nil, storeID)

	body, _ := json.Marshal(CheckoutCompleteRequest{})
	e := echo.New()
	c, _ := newEchoContext(e, http.MethodPost, "/checkout-sessions/sess-5/complete", body, storeID)
	c.SetParamNames("checkoutId")
	c.SetParamValues("sess-5")

	err := h.CheckoutComplete(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok || httpErr.Code != http.StatusConflict {
		t.Errorf("expected 409 conflict, got %v", err)
	}
}

func TestCheckoutComplete_RequiresEscalation_Returns200WithContinueURL(t *testing.T) {
	const storeID = "store-abc"
	conn := &stubConnector{
		completeResp: &base.CheckoutResponse{
			ID:          "backend-123",
			Status:      base.CheckoutStatusRequiresEscalation,
			ContinueURL: "https://shop.example/checkout/3ds",
			Messages: []model.Message{
				model.NewInfoMessage(model.MessageInfo{
					Code:    model.InfoCode("3ds_required"),
					Content: "Complete 3DS on merchant site",
				}),
			},
		},
	}
	sessionStore := serving.NewMemorySessionStore(5 * time.Minute)
	defer sessionStore.Close()

	seed := &serving.CheckoutSession{
		SessionID:         "sess-3ds",
		StoreID:           storeID,
		MerchantID:        storeID,
		BackendCheckoutID: "backend-123",
		State:             serving.SessionStateReadyForComplete,
		LineItems:         []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}
	sessionStore.Create(context.Background(), seed)

	h := newTestHandler(sessionStore, conn, nil, storeID)

	body, _ := json.Marshal(CheckoutCompleteRequest{
		Credential: NewOpaquePaymentCredential("googlepay", "opaque_token_12345"),
	})
	e := echo.New()
	c, rec := newEchoContext(e, http.MethodPost, "/checkout-sessions/sess-3ds/complete", body, storeID)
	c.SetParamNames("checkoutId")
	c.SetParamValues("sess-3ds")

	if err := h.CheckoutComplete(c); err != nil {
		t.Fatalf("CheckoutComplete returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != string(base.CheckoutStatusRequiresEscalation) {
		t.Errorf("expected status requires_escalation, got %v", resp["status"])
	}
	if resp["continue_url"] != "https://shop.example/checkout/3ds" {
		t.Errorf("expected continue_url, got %v", resp["continue_url"])
	}

	// Session must remain at ready_for_complete so the agent can retry after 3DS.
	session, _ := sessionStore.Get(context.Background(), storeID, "sess-3ds")
	if session.State != serving.SessionStateReadyForComplete {
		t.Errorf("expected session to stay ready_for_complete for retry, got %q", session.State)
	}
}

// ──── Session Store unit tests ────────────────────────────────────────────────

func TestMemorySessionStoreCreate(t *testing.T) {
	store := serving.NewMemorySessionStore(1 * time.Minute)
	defer store.Close()

	session := &serving.CheckoutSession{
		SessionID:  "sess123",
		StoreID:    "store123",
		MerchantID: "merchant123",
		State:      serving.SessionStateIncomplete,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := store.Create(context.Background(), session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	retrieved, err := store.Get(context.Background(), "store123", "sess123")
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}
	if retrieved.SessionID != "sess123" {
		t.Errorf("expected session ID sess123, got %s", retrieved.SessionID)
	}
}

func TestMemorySessionStoreIdempotency(t *testing.T) {
	store := serving.NewMemorySessionStore(1 * time.Minute)
	defer store.Close()

	session := &serving.CheckoutSession{
		SessionID:      "sess123",
		MerchantID:     "merchant123",
		State:          serving.SessionStateIncomplete,
		IdempotencyKey: "key123",
		LineItems:      []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "sku1"}, Quantity: 2}},
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if err := store.Create(context.Background(), session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Same idempotency key, different cart — must conflict.
	session2 := &serving.CheckoutSession{
		SessionID:      "sess124",
		MerchantID:     "merchant123",
		State:          serving.SessionStateIncomplete,
		IdempotencyKey: "key123",
		LineItems:      []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "sku2"}, Quantity: 1}},
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if err := store.Create(context.Background(), session2); err != serving.ErrIdempotencyKeyConflict {
		t.Errorf("expected ErrIdempotencyKeyConflict, got %v", err)
	}
}

func TestMemorySessionStoreUpdate(t *testing.T) {
	store := serving.NewMemorySessionStore(1 * time.Minute)
	defer store.Close()

	session := &serving.CheckoutSession{
		SessionID:  "sess123",
		StoreID:    "store123",
		MerchantID: "merchant123",
		State:      serving.SessionStateIncomplete,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := store.Create(context.Background(), session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	session.State = serving.SessionStateCommittedPendingHandoff
	session.UpdatedAt = time.Now()
	if err := store.Update(context.Background(), session); err != nil {
		t.Fatalf("failed to update session: %v", err)
	}

	retrieved, err := store.Get(context.Background(), "store123", "sess123")
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}
	if retrieved.State != serving.SessionStateCommittedPendingHandoff {
		t.Errorf("expected state committed-pending-handoff, got %s", retrieved.State)
	}
}

func TestMemorySessionStoreExpiration(t *testing.T) {
	store := serving.NewMemorySessionStore(100 * time.Millisecond)
	defer store.Close()

	session := &serving.CheckoutSession{
		SessionID:  "sess123",
		StoreID:    "store123",
		MerchantID: "merchant123",
		State:      serving.SessionStateIncomplete,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := store.Create(context.Background(), session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	_, err := store.Get(context.Background(), "store123", "sess123")
	if err != serving.ErrSessionExpired {
		t.Errorf("expected ErrSessionExpired, got %v", err)
	}
}

// ──── Currency extraction test ────────────────────────────────────────────────

func TestCheckoutSessionCurrencyExtraction(t *testing.T) {
	tests := []struct {
		name          string
		sessionTotals *serving.SessionTotals
		expectedCur   string
	}{
		{
			name:          "extract currency from session totals",
			sessionTotals: &serving.SessionTotals{Total: serving.Money{AmountMinor: 1000, Currency: "EUR"}},
			expectedCur:   "EUR",
		},
		{
			name:          "extract different currency",
			sessionTotals: &serving.SessionTotals{Total: serving.Money{AmountMinor: 5000, Currency: "GBP"}},
			expectedCur:   "GBP",
		},
		{
			name:          "default to USD when totals nil",
			sessionTotals: nil,
			expectedCur:   "USD",
		},
		{
			name:          "default to USD when currency empty",
			sessionTotals: &serving.SessionTotals{Total: serving.Money{AmountMinor: 1000, Currency: ""}},
			expectedCur:   "USD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := &serving.CheckoutSession{Totals: tt.sessionTotals}
			currency := "USD"
			if session.Totals != nil && session.Totals.Total.Currency != "" {
				currency = session.Totals.Total.Currency
			}
			if currency != tt.expectedCur {
				t.Errorf("expected currency %s, got %s", tt.expectedCur, currency)
			}
		})
	}
}
