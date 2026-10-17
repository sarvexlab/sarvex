// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

// http_test.go: end-to-end HTTP tests for the serving plane checkout endpoints.
//
// These tests stand up the full Echo router with the real middleware pipeline
// (identity resolution, negotiation passthrough, rate limiting, request/response
// signing disabled in test mode) and drive it through actual HTTP requests using
// httptest. No Redis or Mongo is required — external dependencies are replaced
// with in-memory stores and stub connectors.
//
// Coverage focus: checkout lifecycle (Create → Update → Complete) including
// idempotency, escalation, freshness guard, error paths, and tenant isolation.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/sarvexlab/sarvex/internal/platform/woocommerce"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/negotiation"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/ucpsig"
	"github.com/sarvexlab/sarvex/internal/pkg/utils"
	"github.com/sarvexlab/sarvex/internal/planes/serving"
)

// ──── Test server ──────────────────────────────────────────────────────────────

const (
	testStoreID      = "store-test"
	testConnectorID  = "connector-test"
	testPlatform     = "woocommerce"
	checkoutBasePath = "/ucp/" + testPlatform + "/" + testConnectorID
)

// servingTestServer is the HTTP test harness for serving plane endpoint tests.
type servingTestServer struct {
	server       *httptest.Server
	connector    *stubConnector
	sessionStore serving.SessionStore
	cache        *stubServingCache
}

// newServingTestServer starts a real Echo HTTP server with the full serving
// middleware pipeline and in-memory/stub replacements for all external services.
func newServingTestServer(t *testing.T) *servingTestServer {
	t.Helper()

	conn := &stubConnector{}
	cache := &stubServingCache{}
	sessionStore := serving.NewMemorySessionStore(5 * time.Minute)

	reg := base.New(noTestCredentials, slog.Default())
	reg.Set(woocommerce.PlatformName, testStoreID, conn)

	provider := ucp.StoreProviderFunc(func(_ context.Context, _ string) (ucp.StoreProvider, error) {
		return &stubStoreProvider{storeID: testStoreID}, nil
	})

	negSvc := negotiation.NewNegotiationService(
		negotiation.WithStoreProvider(provider),
		negotiation.WithHTTPClient(&http.Client{Timeout: 3 * time.Second}),
	)

	deps := ServingDependencies{
		Registry:           reg,
		NegotiationService: negSvc,
		EventPublisher:     &stubEventPublisher{},
		StoreProvider:      provider,
		SignatureConfig:    ucpsig.SignatureConfig{Disabled: true},
		ServingCache:       cache,
		RateLimiter:        serving.NewRateLimiter(slog.Default()),
	}

	e := echo.New()
	group := e.Group(servingGroupName)

	handler := NewServingHandler(&utils.Configurations{}, deps, slog.Default(), sessionStore)
	if err := handler.SetupRoutes(servingGroupName, group); err != nil {
		t.Fatalf("SetupRoutes: %v", err)
	}

	ts := httptest.NewServer(e)
	t.Cleanup(func() {
		ts.Close()
		sessionStore.Close()
	})

	return &servingTestServer{
		server:       ts,
		connector:    conn,
		sessionStore: sessionStore,
		cache:        cache,
	}
}

// do executes an HTTP request against the test server. Optional headers
// (e.g. Idempotency-Key, which travels in the header per spec, not the
// body) may be passed as trailing map arguments.
func (s *servingTestServer) do(t *testing.T, method, path string, body any, headers ...map[string]string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, s.server.URL+path, r)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, hs := range headers {
		for k, v := range hs {
			req.Header.Set(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// decode reads and decodes the response body into dst.
func decode(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// seedSession creates a session directly in the session store (bypasses HTTP).
func (s *servingTestServer) seedSession(t *testing.T, sess *serving.CheckoutSession) {
	t.Helper()
	if err := s.sessionStore.Create(context.Background(), sess); err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

// ──── CheckoutCreate ──────────────────────────────────────────────────────────

func TestHTTP_CheckoutCreate_HappyPath(t *testing.T) {
	ts := newServingTestServer(t)
	ts.connector.createResp = &base.CheckoutResponse{
		ID:     "backend-001",
		Status: base.CheckoutStatusIncomplete,
		Totals: model.Totals{{Type: "subtotal", Amount: 2000}},
	}

	resp := ts.do(t, http.MethodPost, checkoutBasePath+"/checkout-sessions", CheckoutCreateRequest{
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 2}},
	})

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}
	var body base.CheckoutResponse
	decode(t, resp, &body)
	// The response id is Sarvex's session id, not the backend's raw
	// checkout id — that's what GET/PUT/complete are keyed on.
	if body.ID == "" || body.ID == "backend-001" {
		t.Fatalf("expected response id to be the session id, not the backend checkout id, got %q", body.ID)
	}
	if session, err := ts.sessionStore.Get(context.Background(), testStoreID, body.ID); err != nil || session.BackendCheckoutID != "backend-001" {
		t.Errorf("expected response id %q to resolve to a session with backend_checkout_id backend-001, got session=%+v err=%v", body.ID, session, err)
	}
	if body.Status != base.CheckoutStatusIncomplete {
		t.Errorf("expected status incomplete, got %q", body.Status)
	}
	if !ts.connector.createCalled {
		t.Error("connector.CheckoutCreate not called")
	}
}

func TestHTTP_CheckoutCreate_EmptyCart_400(t *testing.T) {
	ts := newServingTestServer(t)
	resp := ts.do(t, http.MethodPost, checkoutBasePath+"/checkout-sessions", CheckoutCreateRequest{
		LineItems: []base.LineItemCreateRequest{},
	})
	if resp.StatusCode != http.StatusBadRequest {
		resp.Body.Close()
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHTTP_CheckoutCreate_NoCart_400(t *testing.T) {
	ts := newServingTestServer(t)
	resp := ts.do(t, http.MethodPost, checkoutBasePath+"/checkout-sessions", CheckoutCreateRequest{})
	if resp.StatusCode != http.StatusBadRequest {
		resp.Body.Close()
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHTTP_CheckoutCreate_ConnectorError_503(t *testing.T) {
	ts := newServingTestServer(t)
	ts.connector.createErr = context.DeadlineExceeded

	resp := ts.do(t, http.MethodPost, checkoutBasePath+"/checkout-sessions", CheckoutCreateRequest{
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", resp.StatusCode)
	}
}

func TestHTTP_CheckoutCreate_IdempotencyReplay_201(t *testing.T) {
	ts := newServingTestServer(t)
	ts.connector.getResp = &base.CheckoutResponse{
		ID:     "backend-idem",
		Status: base.CheckoutStatusIncomplete,
	}

	// Pre-seed session with a BackendCheckoutID and idempotency key.
	ts.seedSession(t, &serving.CheckoutSession{
		SessionID:         "sess-idem",
		StoreID:           testStoreID,
		MerchantID:        testStoreID,
		BackendCheckoutID: "backend-idem",
		State:             serving.SessionStateIncomplete,
		IdempotencyKey:    "idem-key-abc",
		LineItems:         []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	})

	resp := ts.do(t, http.MethodPost, checkoutBasePath+"/checkout-sessions", CheckoutCreateRequest{
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
	}, map[string]string{"Idempotency-Key": "idem-key-abc"})
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201 on replay, got %d", resp.StatusCode)
	}
	var body base.CheckoutResponse
	decode(t, resp, &body)
	// Replay still returns the session id, not the backend id.
	if body.ID != "sess-idem" {
		t.Errorf("expected replayed session id sess-idem, got %q", body.ID)
	}
	if ts.connector.createCalled {
		t.Error("connector.CheckoutCreate must NOT be called on idempotent replay")
	}
	if !ts.connector.getCalled {
		t.Error("expected connector.CheckoutGet called on replay")
	}
}

func TestHTTP_CheckoutCreate_IdempotencyPreviousFailure_409(t *testing.T) {
	ts := newServingTestServer(t)

	// Previous attempt: session exists but no BackendCheckoutID (connector failed).
	ts.seedSession(t, &serving.CheckoutSession{
		SessionID:      "sess-fail",
		StoreID:        testStoreID,
		MerchantID:     testStoreID,
		State:          serving.SessionStateFailed,
		IdempotencyKey: "idem-key-fail",
		LineItems:      []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	})

	resp := ts.do(t, http.MethodPost, checkoutBasePath+"/checkout-sessions", CheckoutCreateRequest{
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
	}, map[string]string{"Idempotency-Key": "idem-key-fail"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409, got %d", resp.StatusCode)
	}
}

func TestHTTP_CheckoutCreate_IdempotencyKeyReusedWithDifferentBody_409(t *testing.T) {
	ts := newServingTestServer(t)
	ts.connector.getResp = &base.CheckoutResponse{
		ID:     "backend-conflict",
		Status: base.CheckoutStatusIncomplete,
	}

	// Pre-seed session with a specific body hash (computed from original request).
	originalBodyHash := computeCheckoutCreateBodyHash(&CheckoutCreateRequest{
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
	})

	ts.seedSession(t, &serving.CheckoutSession{
		SessionID:         "sess-conflict",
		StoreID:           testStoreID,
		MerchantID:        testStoreID,
		BackendCheckoutID: "backend-conflict",
		State:             serving.SessionStateIncomplete,
		IdempotencyKey:    "idem-key-conflict",
		BodyHash:          originalBodyHash,
		LineItems:         []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	})

	// Attempt to reuse the same idempotency key with a different request body
	// (different line item). This should be rejected as a conflict.
	resp := ts.do(t, http.MethodPost, checkoutBasePath+"/checkout-sessions", CheckoutCreateRequest{
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-2"}, Quantity: 2}},
	}, map[string]string{"Idempotency-Key": "idem-key-conflict"})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409 on idempotency key reuse with different body, got %d", resp.StatusCode)
	}
}

func TestHTTP_CheckoutCreate_CorrelationIDInErrorResponse_409(t *testing.T) {
	ts := newServingTestServer(t)
	ts.connector.getResp = &base.CheckoutResponse{
		ID:     "backend-cid-test",
		Status: base.CheckoutStatusIncomplete,
	}

	// Pre-seed session with a body hash for conflict detection
	originalBodyHash := computeCheckoutCreateBodyHash(&CheckoutCreateRequest{
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
	})

	ts.seedSession(t, &serving.CheckoutSession{
		SessionID:         "sess-cid-test",
		StoreID:           testStoreID,
		MerchantID:        testStoreID,
		BackendCheckoutID: "backend-cid-test",
		State:             serving.SessionStateIncomplete,
		IdempotencyKey:    "idem-cid-test",
		BodyHash:          originalBodyHash,
		LineItems:         []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	})

	// Request with same idem key but different body should return 409 with correlation ID
	resp := ts.do(t, http.MethodPost, checkoutBasePath+"/checkout-sessions", CheckoutCreateRequest{
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-2"}, Quantity: 2}},
	}, map[string]string{"Idempotency-Key": "idem-cid-test"})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}

	// Verify correlation ID is in the response header
	correlationID := resp.Header.Get("X-Request-ID")
	if correlationID == "" {
		t.Error("expected correlation ID in X-Request-ID header, got empty")
	}
}

// ──── CheckoutGet ─────────────────────────────────────────────────────────────

func TestHTTP_CheckoutGet_ExistingSession_200(t *testing.T) {
	ts := newServingTestServer(t)
	ts.seedSession(t, &serving.CheckoutSession{
		SessionID:         "sess-get-1",
		StoreID:           testStoreID,
		MerchantID:        testStoreID,
		BackendCheckoutID: "backend-get-1",
		State:             serving.SessionStateIncomplete,
		LineItems:         []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
		// GetResponse (CheckoutGet's wire contract) returns the connector's
		// last spec-shaped response verbatim, not the session's own
		// bookkeeping shape — see session.go's LastResponse doc comment.
		LastResponse: &base.CheckoutResponse{
			ID:       "sess-get-1",
			Status:   base.CheckoutStatusIncomplete,
			Currency: "USD",
		},
	})

	resp := ts.do(t, http.MethodGet, checkoutBasePath+"/checkout-sessions/sess-get-1", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	var checkout base.CheckoutResponse
	decode(t, resp, &checkout)
	if checkout.ID != "sess-get-1" {
		t.Errorf("expected id sess-get-1, got %q", checkout.ID)
	}
	if checkout.Currency != "USD" {
		t.Errorf("expected currency USD, got %q", checkout.Currency)
	}
}

func TestHTTP_CheckoutGet_NotFound_404(t *testing.T) {
	ts := newServingTestServer(t)
	resp := ts.do(t, http.MethodGet, checkoutBasePath+"/checkout-sessions/no-such-session", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHTTP_CheckoutGet_ExpiredSession_410(t *testing.T) {
	// Use a 1ms TTL store so sessions expire immediately.
	conn := &stubConnector{}
	sessionStore := serving.NewMemorySessionStore(1 * time.Millisecond)

	reg := base.New(noTestCredentials, slog.Default())
	reg.Set(woocommerce.PlatformName, testStoreID, conn)
	provider := ucp.StoreProviderFunc(func(_ context.Context, _ string) (ucp.StoreProvider, error) {
		return &stubStoreProvider{storeID: testStoreID}, nil
	})
	deps := ServingDependencies{
		Registry:           reg,
		NegotiationService: negotiation.NewNegotiationService(negotiation.WithStoreProvider(provider), negotiation.WithHTTPClient(&http.Client{Timeout: 3 * time.Second})),
		EventPublisher:     &stubEventPublisher{},
		StoreProvider:      provider,
		SignatureConfig:    ucpsig.SignatureConfig{Disabled: true},
		ServingCache:       &stubServingCache{},
		RateLimiter:        serving.NewRateLimiter(slog.Default()),
	}
	e := echo.New()
	group := e.Group(servingGroupName)
	handler := NewServingHandler(&utils.Configurations{}, deps, slog.Default(), sessionStore)
	if err := handler.SetupRoutes(servingGroupName, group); err != nil {
		t.Fatalf("SetupRoutes: %v", err)
	}
	ts := httptest.NewServer(e)
	t.Cleanup(func() { ts.Close(); sessionStore.Close() })

	sessionStore.Create(context.Background(), &serving.CheckoutSession{
		SessionID:  "sess-expired",
		StoreID:    testStoreID,
		MerchantID: testStoreID,
		State:      serving.SessionStateIncomplete,
		LineItems:  []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	})
	time.Sleep(5 * time.Millisecond)

	resp, _ := http.Get(ts.URL + checkoutBasePath + "/checkout-sessions/sess-expired")
	resp.Body.Close()
	if resp.StatusCode != http.StatusGone {
		t.Errorf("expected 410 for expired session, got %d", resp.StatusCode)
	}
}

// ──── CheckoutUpdate ──────────────────────────────────────────────────────────

func TestHTTP_CheckoutUpdate_HappyPath_200(t *testing.T) {
	ts := newServingTestServer(t)
	ts.connector.updateResp = &base.CheckoutResponse{
		ID:     "backend-upd",
		Status: base.CheckoutStatusReadyForComplete,
		Fulfillment: &model.Fulfillment{
			Methods: []model.FulfillmentMethod{{ID: "ship-1", Type: "shipping"}},
		},
	}

	ts.seedSession(t, &serving.CheckoutSession{
		SessionID:         "sess-upd",
		StoreID:           testStoreID,
		MerchantID:        testStoreID,
		BackendCheckoutID: "backend-upd",
		State:             serving.SessionStateIncomplete,
		LineItems:         []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	})

	resp := ts.do(t, http.MethodPut, checkoutBasePath+"/checkout-sessions/sess-upd", CheckoutUpdateRequest{
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
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 200, got %d: %s", resp.StatusCode, b)
		return
	}
	var body base.CheckoutResponse
	decode(t, resp, &body)
	if body.Status != base.CheckoutStatusReadyForComplete {
		t.Errorf("expected ready_for_complete, got %q", body.Status)
	}
	if !ts.connector.updateCalled {
		t.Error("connector.CheckoutUpdate not called")
	}
	if ts.connector.updateID != "backend-upd" {
		t.Errorf("wrong backend checkout ID passed to connector: %q", ts.connector.updateID)
	}

	// Verify session state persisted.
	session, _ := ts.sessionStore.Get(context.Background(), testStoreID, "sess-upd")
	if session.State != serving.SessionStateReadyForComplete {
		t.Errorf("expected session state ready_for_complete, got %q", session.State)
	}
}

func TestHTTP_CheckoutUpdate_SessionNotFound_404(t *testing.T) {
	ts := newServingTestServer(t)
	resp := ts.do(t, http.MethodPut, checkoutBasePath+"/checkout-sessions/no-such-session", CheckoutUpdateRequest{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHTTP_CheckoutUpdate_MissingBackendID_409(t *testing.T) {
	ts := newServingTestServer(t)
	ts.seedSession(t, &serving.CheckoutSession{
		SessionID:  "sess-no-backend",
		StoreID:    testStoreID,
		MerchantID: testStoreID,
		State:      serving.SessionStateIncomplete,
		LineItems:  []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	})

	resp := ts.do(t, http.MethodPut, checkoutBasePath+"/checkout-sessions/sess-no-backend", CheckoutUpdateRequest{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409, got %d", resp.StatusCode)
	}
}

func TestHTTP_CheckoutUpdate_ConnectorError_503(t *testing.T) {
	ts := newServingTestServer(t)
	ts.connector.updateErr = context.DeadlineExceeded
	ts.seedSession(t, &serving.CheckoutSession{
		SessionID:         "sess-upd-err",
		StoreID:           testStoreID,
		MerchantID:        testStoreID,
		BackendCheckoutID: "backend-upd-err",
		State:             serving.SessionStateIncomplete,
		LineItems:         []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	})

	resp := ts.do(t, http.MethodPut, checkoutBasePath+"/checkout-sessions/sess-upd-err", CheckoutUpdateRequest{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", resp.StatusCode)
	}
}

// ──── CheckoutComplete ────────────────────────────────────────────────────────

func TestHTTP_CheckoutComplete_HappyPath_200(t *testing.T) {
	ts := newServingTestServer(t)
	ts.connector.completeResp = &base.CheckoutResponse{
		ID:     "backend-cmp",
		Status: base.CheckoutStatusCompleted,
		Totals: model.Totals{{Type: "total", Amount: 5000}},
		Order: &model.OrderConfirmation{
			ID:           "order-9001",
			PermalinkURL: "https://shop.example/order/9001",
		},
	}

	ts.seedSession(t, &serving.CheckoutSession{
		SessionID:         "sess-cmp",
		StoreID:           testStoreID,
		MerchantID:        testStoreID,
		BackendCheckoutID: "backend-cmp",
		State:             serving.SessionStateReadyForComplete,
		LineItems: []base.LineItemCreateRequest{
			{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 2},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	resp := ts.do(t, http.MethodPost, checkoutBasePath+"/checkout-sessions/sess-cmp/complete", CheckoutCompleteRequest{
		Credential: NewOpaquePaymentCredential("googlepay", "opaque_token_test"),
	})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 200, got %d: %s", resp.StatusCode, b)
		return
	}
	var body map[string]any
	decode(t, resp, &body)
	order, _ := body["order"].(map[string]any)
	if order == nil {
		t.Fatal("expected order in response")
	}
	if order["id"] != "order-9001" {
		t.Errorf("expected order id order-9001, got %v", order["id"])
	}
	if order["permalink_url"] != "https://shop.example/order/9001" {
		t.Errorf("expected confirm url in permalink_url, got %v", order["permalink_url"])
	}
	// Complete returns the full Checkout resource, not a bespoke envelope.
	if body["status"] != string(base.CheckoutStatusCompleted) {
		t.Errorf("expected status completed, got %v", body["status"])
	}

	// Must use CheckoutComplete, never CheckoutCreate.
	if ts.connector.createCalled {
		t.Error("connector.CheckoutCreate must NOT be called during complete")
	}
	if !ts.connector.completeCalled {
		t.Error("connector.CheckoutComplete not called")
	}
	if ts.connector.completeID != "backend-cmp" {
		t.Errorf("wrong backend ID: %q", ts.connector.completeID)
	}

	// Session must be committed.
	session, _ := ts.sessionStore.Get(context.Background(), testStoreID, "sess-cmp")
	if session.State != serving.SessionStateCommittedPendingHandoff {
		t.Errorf("expected committed_pending_handoff, got %q", session.State)
	}
}

func TestHTTP_CheckoutComplete_FreshnessGuard_409(t *testing.T) {
	ts := newServingTestServer(t)
	ts.cache.watermark = &serving.CacheWatermark{IsStale: true, Age: 10 * time.Minute}
	ts.seedSession(t, &serving.CheckoutSession{
		SessionID:         "sess-fresh",
		StoreID:           testStoreID,
		MerchantID:        testStoreID,
		BackendCheckoutID: "backend-fresh",
		State:             serving.SessionStateReadyForComplete,
		LineItems:         []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	})

	resp := ts.do(t, http.MethodPost, checkoutBasePath+"/checkout-sessions/sess-fresh/complete",
		CheckoutCompleteRequest{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409 from freshness guard, got %d", resp.StatusCode)
	}
	if ts.connector.completeCalled {
		t.Error("connector.CheckoutComplete must not be called when freshness guard fails")
	}
}

func TestHTTP_CheckoutComplete_MissingBackendID_409(t *testing.T) {
	ts := newServingTestServer(t)
	ts.seedSession(t, &serving.CheckoutSession{
		SessionID:  "sess-no-bid",
		StoreID:    testStoreID,
		MerchantID: testStoreID,
		State:      serving.SessionStateIncomplete,
		LineItems:  []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	})

	resp := ts.do(t, http.MethodPost, checkoutBasePath+"/checkout-sessions/sess-no-bid/complete",
		CheckoutCompleteRequest{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409, got %d", resp.StatusCode)
	}
}

func TestHTTP_CheckoutComplete_ConnectorError_503(t *testing.T) {
	ts := newServingTestServer(t)
	ts.connector.completeErr = context.DeadlineExceeded
	ts.seedSession(t, &serving.CheckoutSession{
		SessionID:         "sess-cmp-err",
		StoreID:           testStoreID,
		MerchantID:        testStoreID,
		BackendCheckoutID: "backend-cmp-err",
		State:             serving.SessionStateReadyForComplete,
		LineItems:         []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	})

	resp := ts.do(t, http.MethodPost, checkoutBasePath+"/checkout-sessions/sess-cmp-err/complete",
		CheckoutCompleteRequest{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", resp.StatusCode)
	}
}

func TestHTTP_CheckoutComplete_SessionNotFound_404(t *testing.T) {
	ts := newServingTestServer(t)
	resp := ts.do(t, http.MethodPost, checkoutBasePath+"/checkout-sessions/no-such/complete",
		CheckoutCompleteRequest{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHTTP_CheckoutComplete_RequiresEscalation_200(t *testing.T) {
	ts := newServingTestServer(t)
	ts.connector.completeResp = &base.CheckoutResponse{
		Status:      base.CheckoutStatusRequiresEscalation,
		ContinueURL: "https://shop.example/checkout/3ds",
		Messages: []model.Message{
			model.NewInfoMessage(model.MessageInfo{
				Code:    model.InfoCode("3ds_required"),
				Content: "Complete 3DS",
			}),
		},
	}
	ts.seedSession(t, &serving.CheckoutSession{
		SessionID:         "sess-3ds",
		StoreID:           testStoreID,
		MerchantID:        testStoreID,
		BackendCheckoutID: "backend-3ds",
		State:             serving.SessionStateReadyForComplete,
		LineItems:         []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 1}},
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	})

	resp := ts.do(t, http.MethodPost, checkoutBasePath+"/checkout-sessions/sess-3ds/complete",
		CheckoutCompleteRequest{
			Credential: NewOpaquePaymentCredential("googlepay", "opaque_token_test"),
		})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 200 for escalation, got %d: %s", resp.StatusCode, b)
		return
	}
	var body map[string]any
	decode(t, resp, &body)
	if body["status"] != string(base.CheckoutStatusRequiresEscalation) {
		t.Errorf("expected requires_escalation, got %v", body["status"])
	}
	if body["continue_url"] != "https://shop.example/checkout/3ds" {
		t.Errorf("expected continue_url, got %v", body["continue_url"])
	}

	// Session must stay at ready_for_complete for agent retry after 3DS.
	session, _ := ts.sessionStore.Get(context.Background(), testStoreID, "sess-3ds")
	if session.State != serving.SessionStateReadyForComplete {
		t.Errorf("expected session to stay ready_for_complete, got %q", session.State)
	}
}

// ──── Full lifecycle ──────────────────────────────────────────────────────────

// TestHTTP_CheckoutFullLifecycle drives Create → Update → Complete through real
// HTTP requests and verifies state at each step.
func TestHTTP_CheckoutFullLifecycle(t *testing.T) {
	ts := newServingTestServer(t)

	// Step 1: Create — connector returns a backend session.
	ts.connector.createResp = &base.CheckoutResponse{
		ID:     "backend-flow",
		Status: base.CheckoutStatusIncomplete,
		Totals: model.Totals{{Type: "subtotal", Amount: 3000}},
	}
	createResp := ts.do(t, http.MethodPost, checkoutBasePath+"/checkout-sessions", CheckoutCreateRequest{
		LineItems: []base.LineItemCreateRequest{{Item: base.ItemRef{ID: "SKU-1"}, Quantity: 3}},
	}, map[string]string{"Idempotency-Key": "flow-key-1"})
	if createResp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(createResp.Body)
		t.Fatalf("Create: expected 201, got %d: %s", createResp.StatusCode, b)
	}
	var createBody base.CheckoutResponse
	decode(t, createResp, &createBody)
	if createBody.ID == "" || createBody.ID == "backend-flow" {
		t.Fatalf("Create: expected the session id (not the backend checkout id) in response, got %q", createBody.ID)
	}

	// A real agent has only the response's id to continue with — use it
	// directly, the same way `sim checkout run` does, rather than reaching
	// into the session store out of band.
	sessionID := createBody.ID

	existingSession, err := ts.sessionStore.Get(context.Background(), testStoreID, sessionID)
	if err != nil {
		t.Fatalf("expected response id %q to resolve to a session: %v", sessionID, err)
	}
	if existingSession.BackendCheckoutID != "backend-flow" {
		t.Fatalf("Create: BackendCheckoutID not persisted, got %q", existingSession.BackendCheckoutID)
	}
	if existingSession.State != serving.SessionStateIncomplete {
		t.Fatalf("Create: expected state incomplete, got %q", existingSession.State)
	}

	// Step 2: Update — add buyer address, connector returns shipping options.
	ts.connector.updateResp = &base.CheckoutResponse{
		ID:     "backend-flow",
		Status: base.CheckoutStatusReadyForComplete,
		Fulfillment: &model.Fulfillment{
			Methods: []model.FulfillmentMethod{
				{ID: "ship-1", Type: "shipping", Groups: []model.FulfillmentGroup{
					{ID: "grp-1", Options: []model.FulfillmentOption{
						{ID: "std", Title: "Standard Shipping", Totals: model.Totals{{Type: "total", Amount: 500}}},
					}},
				}},
			},
		},
		Totals: model.Totals{{Type: "total", Amount: 3500}},
	}
	updateResp := ts.do(t, http.MethodPut, checkoutBasePath+"/checkout-sessions/"+sessionID, CheckoutUpdateRequest{
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
	if updateResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(updateResp.Body)
		t.Fatalf("Update: expected 200, got %d: %s", updateResp.StatusCode, b)
	}
	var updateBody base.CheckoutResponse
	decode(t, updateResp, &updateBody)
	if updateBody.Status != base.CheckoutStatusReadyForComplete {
		t.Fatalf("Update: expected ready_for_complete, got %q", updateBody.Status)
	}
	if ts.connector.updateID != "backend-flow" {
		t.Fatalf("Update: connector called with wrong ID: %q", ts.connector.updateID)
	}

	updatedSession, _ := ts.sessionStore.Get(context.Background(), testStoreID, sessionID)
	if updatedSession.State != serving.SessionStateReadyForComplete {
		t.Fatalf("Update: session state not persisted, got %q", updatedSession.State)
	}

	// Step 3: Complete — connector captures and creates order.
	ts.connector.completeResp = &base.CheckoutResponse{
		ID:     "backend-flow",
		Status: base.CheckoutStatusCompleted,
		Totals: model.Totals{{Type: "total", Amount: 3500}},
		Order: &model.OrderConfirmation{
			ID:           "order-5678",
			PermalinkURL: "https://shop.example/order/5678",
		},
	}
	completeResp := ts.do(t, http.MethodPost, checkoutBasePath+"/checkout-sessions/"+sessionID+"/complete",
		CheckoutCompleteRequest{
			Credential: NewOpaquePaymentCredential("googlepay", "opaque_token_test"),
		},
	)
	if completeResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(completeResp.Body)
		t.Fatalf("Complete: expected 200, got %d: %s", completeResp.StatusCode, b)
	}
	var completeBody map[string]any
	decode(t, completeResp, &completeBody)
	order, _ := completeBody["order"].(map[string]any)
	if order["id"] != "order-5678" {
		t.Errorf("Complete: expected order-5678, got %v", order["id"])
	}
	// Complete returns the full Checkout resource: assert its spec status
	// rather than the old bespoke envelope's internal session state.
	if completeBody["status"] != string(base.CheckoutStatusCompleted) {
		t.Errorf("Complete: expected status completed, got %v", completeBody["status"])
	}

	// Verify final session state.
	finalSession, _ := ts.sessionStore.Get(context.Background(), testStoreID, sessionID)
	if finalSession.State != serving.SessionStateCommittedPendingHandoff {
		t.Errorf("Complete: session not committed, got %q", finalSession.State)
	}
}
