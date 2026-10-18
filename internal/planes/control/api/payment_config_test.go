// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/planes/control/api"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/store"
)

// stubPaymentConfigRepo is a test stub for PaymentConfigRepository.
type stubPaymentConfigRepo struct {
	getErr    error
	getResult *store.PaymentConfigModel
	upsertErr error
	deleteErr error
}

func (s stubPaymentConfigRepo) Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (*store.PaymentConfigModel, error) {
	return s.getResult, s.getErr
}

func (s stubPaymentConfigRepo) Upsert(ctx context.Context, m *store.PaymentConfigModel) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	return nil
}

func (s stubPaymentConfigRepo) Delete(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) error {
	return s.deleteErr
}

var googlePayParams = map[string]string{
	"merchant_name": "Acme", "merchant_id": "SECRETISH-1234", "merchant_origin": "https://acme.example",
	"gateway": "stripe", "gateway_merchant_id": "acct_123",
}

// paymentEcho builds the control API with a payment-config repo and a store
// service whose Get returns st (or getErr).
func paymentEcho(repo stubPaymentConfigRepo, st domain.Store, getErr error, bus eventbus.EventBus) *echo.Echo {
	deps := api.Dependencies{
		PaymentConfigRepo: repo,
		Store:             &stubStoreServiceForProfile{st: st, getErr: getErr},
		EventPublisher:    bus,
		Billing:           activeBillingStub{},
		PaymentValidator:  store.NewPaymentValidator(nil, slog.New(slog.DiscardHandler)),
		ResolveBearer:     testResolver,
		ResolveHMAC:       func(c echo.Context, sig string) (domain.Principal, error) { return domain.Principal{}, nil },
	}
	e := echo.New()
	_ = api.New(deps).SetupRoutes("/v1", e.Group("/v1"))
	return e
}

func putPaymentConfig(e *echo.Echo, payload any) *httptest.ResponseRecorder {
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/v1/stores/store-1/payment-config", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+merchantToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

var wooStore = domain.Store{StoreID: "store-1", MerchantID: "merch-1", Platform: "woocommerce"}

// TestGetPaymentConfig_Success returns handler status in the contract shape and
// never the stored parameters.
func TestGetPaymentConfig_Success(t *testing.T) {
	repo := stubPaymentConfigRepo{
		getResult: &store.PaymentConfigModel{
			ID:         "store-1",
			MerchantID: "merch-1",
			Handlers: map[string]store.PaymentHandlerConfigModel{
				"com.google.pay": {Enabled: true, Config: googlePayParams},
			},
			UpdatedBy: "admin@example.com",
		},
	}
	e := paymentEcho(repo, wooStore, nil, nil)

	rec := do(e, http.MethodGet, "/v1/stores/store-1/payment-config", bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)
	raw := rec.Body.String()
	assert.NotContains(t, raw, "SECRETISH-1234", "stored parameters must never be returned")
	assert.NotContains(t, raw, "acct_123")

	var view struct {
		StoreID  string `json:"store_id"`
		Handlers []struct {
			HandlerID    string `json:"handler_id"`
			Name         string `json:"name"`
			Enabled      bool   `json:"enabled"`
			Configured   bool   `json:"configured"`
			ConfigStatus string `json:"config_status"`
		} `json:"handlers"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &view))
	assert.Equal(t, "store-1", view.StoreID)
	require.Len(t, view.Handlers, 1)
	h := view.Handlers[0]
	assert.Equal(t, "google_pay", h.HandlerID)
	assert.Equal(t, "com.google.pay", h.Name)
	assert.True(t, h.Enabled)
	assert.True(t, h.Configured)
	assert.Equal(t, "ready", h.ConfigStatus)
}

// TestGetPaymentConfig_NotFound returns an empty config when not configured.
func TestGetPaymentConfig_NotFound(t *testing.T) {
	repo := stubPaymentConfigRepo{
		getErr: store.ErrPaymentConfigNotFound,
	}

	deps := api.Dependencies{
		PaymentConfigRepo: repo,
		Billing:           activeBillingStub{},
		ResolveBearer:     testResolver,
		ResolveHMAC:       func(c echo.Context, sig string) (domain.Principal, error) { return domain.Principal{}, nil },
	}
	h := api.New(deps)
	e := echo.New()
	g := e.Group("/v1")
	_ = h.SetupRoutes("/v1", g)

	rec := do(e, http.MethodGet, "/v1/stores/store-1/payment-config", bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)

	var config struct {
		StoreID  string `json:"store_id"`
		Handlers []any  `json:"handlers"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&config))
	assert.Equal(t, "store-1", config.StoreID)
	assert.NotNil(t, config.Handlers, "handlers must serialise as [] not null")
	assert.Empty(t, config.Handlers)
}

// TestGetPaymentConfig_Unauthorized rejects requests without bearer token.
func TestGetPaymentConfig_Unauthorized(t *testing.T) {
	repo := stubPaymentConfigRepo{}
	deps := api.Dependencies{
		PaymentConfigRepo: repo,
		Billing:           activeBillingStub{},
		ResolveBearer:     testResolver,
		ResolveHMAC:       func(c echo.Context, sig string) (domain.Principal, error) { return domain.Principal{}, nil },
	}
	h := api.New(deps)
	e := echo.New()
	g := e.Group("/v1")
	_ = h.SetupRoutes("/v1", g)

	rec := do(e, http.MethodGet, "/v1/stores/store-1/payment-config", map[string]string{})
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestGetPaymentConfig_MissingStoreID returns 422 when store_id is empty (validation error).
func TestGetPaymentConfig_MissingStoreID(t *testing.T) {
	repo := stubPaymentConfigRepo{}
	deps := api.Dependencies{
		PaymentConfigRepo: repo,
		Billing:           activeBillingStub{},
		ResolveBearer:     testResolver,
		ResolveHMAC:       func(c echo.Context, sig string) (domain.Principal, error) { return domain.Principal{}, nil },
	}
	h := api.New(deps)
	e := echo.New()
	g := e.Group("/v1")
	_ = h.SetupRoutes("/v1", g)

	// Empty store_id parameter results in a 422 validation error from the handler
	rec := do(e, http.MethodGet, "/v1/stores//payment-config", bearer(merchantToken))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assertErrorEnvelope(t, rec)
}

// TestUpdatePaymentConfig_Success stores public parameters, returns status only
// and tells the serving plane to rebuild.
func TestUpdatePaymentConfig_Success(t *testing.T) {
	bus := &captureBus{}
	e := paymentEcho(stubPaymentConfigRepo{}, wooStore, nil, bus)

	rec := putPaymentConfig(e, map[string]any{"handlers": []map[string]any{
		{"handler_id": "google_pay", "enabled": true, "parameters": googlePayParams},
	}})

	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "SECRETISH-1234", "PUT must not echo parameters")
	assert.Contains(t, rec.Body.String(), `"config_status":"ready"`)

	require.Len(t, bus.events, 1)
	assert.Equal(t, ucp.EventPaymentConfigUpdated, bus.events[0].EventType())
	assert.NotContains(t, string(bus.events[0].Payload()), "SECRETISH-1234", "event payload must not carry parameters")
}

func TestUpdatePaymentConfig_Rejects(t *testing.T) {
	cases := []struct {
		name    string
		st      domain.Store
		getErr  error
		handler map[string]any
		want    int
	}{
		{"unknown handler", wooStore, nil, map[string]any{"handler_id": "stripe", "enabled": true}, http.StatusUnprocessableEntity},
		{"undeclared (possibly secret) parameter", wooStore, nil, map[string]any{
			"handler_id": "google_pay", "enabled": false, "parameters": map[string]any{"api_secret": "sk_live_x"},
		}, http.StatusUnprocessableEntity},
		{"enabled without required parameters", wooStore, nil, map[string]any{
			"handler_id": "google_pay", "enabled": true, "parameters": map[string]any{"merchant_id": "1"},
		}, http.StatusUnprocessableEntity},
		{"non-string parameter", wooStore, nil, map[string]any{
			"handler_id": "google_pay", "enabled": true, "parameters": map[string]any{"merchant_id": 1},
		}, http.StatusUnprocessableEntity},
		{"shop_pay on a WooCommerce store", wooStore, nil, map[string]any{
			"handler_id": "shop_pay", "enabled": true, "parameters": map[string]any{"shop_id": "1"},
		}, http.StatusUnprocessableEntity},
		{"store not owned by the caller", domain.Store{}, store.ErrNotFound, map[string]any{
			"handler_id": "google_pay", "enabled": false,
		}, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := paymentEcho(stubPaymentConfigRepo{}, tc.st, tc.getErr, nil)
			rec := putPaymentConfig(e, map[string]any{"handlers": []map[string]any{tc.handler}})
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
			assertErrorEnvelope(t, rec)
		})
	}
}

// TestUpdatePaymentConfig_InvalidJSON returns 422 for malformed request (validation error).
func TestUpdatePaymentConfig_InvalidJSON(t *testing.T) {
	repo := stubPaymentConfigRepo{}
	deps := api.Dependencies{
		PaymentConfigRepo: repo,
		Billing:           activeBillingStub{},
		PaymentValidator:  store.NewPaymentValidator(nil, slog.Default()),
		ResolveBearer:     testResolver,
		ResolveHMAC:       func(c echo.Context, sig string) (domain.Principal, error) { return domain.Principal{}, nil },
	}
	h := api.New(deps)
	e := echo.New()
	g := e.Group("/v1")
	_ = h.SetupRoutes("/v1", g)

	req := httptest.NewRequest(http.MethodPut, "/v1/stores/store-1/payment-config", bytes.NewReader([]byte(`invalid json`)))
	req.Header.Set("Authorization", "Bearer "+merchantToken)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assertErrorEnvelope(t, rec)
}

// TestUpdatePaymentConfig_Unauthorized rejects requests without bearer token.
func TestUpdatePaymentConfig_Unauthorized(t *testing.T) {
	repo := stubPaymentConfigRepo{}
	deps := api.Dependencies{
		PaymentConfigRepo: repo,
		Billing:           activeBillingStub{},
		PaymentValidator:  store.NewPaymentValidator(nil, slog.Default()),
		ResolveBearer:     testResolver,
		ResolveHMAC:       func(c echo.Context, sig string) (domain.Principal, error) { return domain.Principal{}, nil },
	}
	h := api.New(deps)
	e := echo.New()
	g := e.Group("/v1")
	_ = h.SetupRoutes("/v1", g)

	payload := map[string]interface{}{"handlers": []interface{}{}}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPut, "/v1/stores/store-1/payment-config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestUpdatePaymentConfig_RepositoryError returns 500 on repo failure.
func TestUpdatePaymentConfig_RepositoryError(t *testing.T) {
	e := paymentEcho(stubPaymentConfigRepo{upsertErr: errSentinel}, wooStore, nil, nil)

	rec := putPaymentConfig(e, map[string]any{"handlers": []map[string]any{
		{"handler_id": "google_pay", "enabled": false},
	}})

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assertErrorEnvelope(t, rec)
}

// TestUpdatePaymentConfig_EmptyHandlers allows clearing all handlers.
func TestUpdatePaymentConfig_EmptyHandlers(t *testing.T) {
	e := paymentEcho(stubPaymentConfigRepo{}, wooStore, nil, nil)

	rec := putPaymentConfig(e, map[string]any{"handlers": []any{}})

	require.Equal(t, http.StatusOK, rec.Code)
	var config struct {
		Handlers []any `json:"handlers"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&config))
	assert.Empty(t, config.Handlers)
}
