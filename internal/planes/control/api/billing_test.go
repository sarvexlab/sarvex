// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/api"
	"github.com/sarvexlab/sarvex/internal/planes/control/billing"
	"github.com/sarvexlab/sarvex/internal/planes/control/catalog"
	"github.com/sarvexlab/sarvex/internal/planes/control/connector"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/journey"
	"github.com/sarvexlab/sarvex/internal/planes/control/operator"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
	"github.com/sarvexlab/sarvex/internal/planes/control/validation"
)

// stubBillingService is a controllable billing.Service for handler tests.
type stubBillingService struct {
	billingInfo     domain.Billing
	hostedURL       domain.HostedURL
	prefs           domain.NotificationPreferences
	getErr          error
	portalErr       error
	getPrefsErr     error
	setPrefsErr     error
	webhookErr      error
	provisionErr    error
	ensurePortalID  string
	ensurePortalErr error
}

func (s *stubBillingService) Get(_ context.Context, _ domain.MerchantID) (domain.Billing, error) {
	return s.billingInfo, s.getErr
}

func (s *stubBillingService) CreatePortalLink(_ context.Context, _ domain.MerchantID) (domain.HostedURL, error) {
	return s.hostedURL, s.portalErr
}

func (s *stubBillingService) GetNotificationPreferences(_ context.Context, _ domain.MerchantID) (domain.NotificationPreferences, error) {
	return s.prefs, s.getPrefsErr
}

func (s *stubBillingService) SetNotificationPreferences(_ context.Context, _ domain.MerchantID, _ domain.NotificationPreferences) (domain.NotificationPreferences, error) {
	return s.prefs, s.setPrefsErr
}

func (s *stubBillingService) HandleStripeWebhook(_ context.Context, _ []byte, _ string) error {
	return s.webhookErr
}

func (s *stubBillingService) ProvisionTrial(_ context.Context, _ domain.MerchantID, _ string) error {
	return s.provisionErr
}

func (s *stubBillingService) EnsurePortalConfiguration(_ context.Context) (string, error) {
	return s.ensurePortalID, s.ensurePortalErr
}

func newBillingTestEcho(billingSvc billing.Service) *echo.Echo {
	deps := api.Dependencies{
		Account:       &stubAccountService{acc: testAccount},
		Connector:     connector.StubService{},
		Catalog:       catalog.StubService{},
		Task:          task.StubService{},
		Journey:       journey.StubService{},
		Billing:       billingSvc,
		Operator:      operator.StubService{},
		Validation:    validation.StubService{},
		ResolveBearer: testResolver,
		ResolveHMAC: func(c echo.Context, sig string) (domain.Principal, error) {
			return domain.Principal{MerchantID: "test"}, nil
		},
	}
	h := api.New(deps)
	e := echo.New()
	_ = h.SetupRoutes("/v1", e.Group("/v1"))
	return e
}

var testBilling = domain.Billing{
	Plan:    domain.PlanSelfServe,
	Status:  domain.BillingActive,
	SKUBand: "500",
	Usage:   domain.BillingUsage{SKUCount: 42, SKUCap: 500},
}

// ── GET /billing ──────────────────────────────────────────────────────────────

func TestGetBilling_OK(t *testing.T) {
	e := newBillingTestEcho(&stubBillingService{billingInfo: testBilling})
	rec := do(e, http.MethodGet, "/v1/billing", bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got domain.Billing
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, domain.PlanSelfServe, got.Plan)
	assert.Equal(t, domain.BillingActive, got.Status)
}

func TestGetBilling_Unauthorized(t *testing.T) {
	e := newBillingTestEcho(&stubBillingService{})
	rec := do(e, http.MethodGet, "/v1/billing", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestGetBilling_InternalError(t *testing.T) {
	e := newBillingTestEcho(&stubBillingService{getErr: errSentinel})
	rec := do(e, http.MethodGet, "/v1/billing", bearer(merchantToken))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assertErrorEnvelope(t, rec)
}

// ── POST /billing/portal-link ─────────────────────────────────────────────────

func TestBillingPortalLink_OK(t *testing.T) {
	url := "https://billing.stripe.com/portal/abc"
	e := newBillingTestEcho(&stubBillingService{hostedURL: domain.HostedURL{URL: url}})
	rec := do(e, http.MethodPost, "/v1/billing/portal-link", bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got domain.HostedURL
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, url, got.URL)
}

func TestBillingPortalLink_NoStripeCustomer_NotFound(t *testing.T) {
	e := newBillingTestEcho(&stubBillingService{portalErr: billing.ErrNoStripeCustomer})
	rec := do(e, http.MethodPost, "/v1/billing/portal-link", bearer(merchantToken))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestBillingPortalLink_Unauthorized(t *testing.T) {
	e := newBillingTestEcho(&stubBillingService{})
	rec := do(e, http.MethodPost, "/v1/billing/portal-link", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestBillingPortalLink_InternalError(t *testing.T) {
	e := newBillingTestEcho(&stubBillingService{portalErr: errSentinel})
	rec := do(e, http.MethodPost, "/v1/billing/portal-link", bearer(merchantToken))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assertErrorEnvelope(t, rec)
}

// ── POST /billing/webhooks/stripe ───────────────────────────────────────────────

func TestStripeWebhook_OK(t *testing.T) {
	e := newBillingTestEcho(&stubBillingService{})
	rec := do(e, http.MethodPost, "/v1/billing/webhooks/stripe", map[string]string{"Stripe-Signature": "t=1,v1=abc"})
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestStripeWebhook_InvalidSignature(t *testing.T) {
	e := newBillingTestEcho(&stubBillingService{webhookErr: billing.ErrInvalidWebhookSignature})
	rec := do(e, http.MethodPost, "/v1/billing/webhooks/stripe", map[string]string{"Stripe-Signature": "bad"})
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestStripeWebhook_InternalError(t *testing.T) {
	e := newBillingTestEcho(&stubBillingService{webhookErr: errSentinel})
	rec := do(e, http.MethodPost, "/v1/billing/webhooks/stripe", map[string]string{"Stripe-Signature": "t=1,v1=abc"})
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestStripeWebhook_NoAuthRequired(t *testing.T) {
	// Unlike every other billing route, this one carries no bearer/HMAC
	// requirement — verification happens inside the service via the
	// Stripe-Signature header.
	e := newBillingTestEcho(&stubBillingService{})
	rec := do(e, http.MethodPost, "/v1/billing/webhooks/stripe", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// ── GET /notifications/preferences ───────────────────────────────────────────

func TestGetNotificationPreferences_OK(t *testing.T) {
	email := "alerts@acme.com"
	prefs := domain.NotificationPreferences{
		EmailEnabled: true,
		WeeklyDigest: false,
		AlertEmail:   &email,
	}
	e := newBillingTestEcho(&stubBillingService{prefs: prefs})
	rec := do(e, http.MethodGet, "/v1/notifications/preferences", bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got domain.NotificationPreferences
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.True(t, got.EmailEnabled)
	require.NotNil(t, got.AlertEmail)
	assert.Equal(t, "alerts@acme.com", *got.AlertEmail)
}

func TestGetNotificationPreferences_Unauthorized(t *testing.T) {
	e := newBillingTestEcho(&stubBillingService{})
	rec := do(e, http.MethodGet, "/v1/notifications/preferences", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestGetNotificationPreferences_InternalError(t *testing.T) {
	e := newBillingTestEcho(&stubBillingService{getPrefsErr: errSentinel})
	rec := do(e, http.MethodGet, "/v1/notifications/preferences", bearer(merchantToken))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assertErrorEnvelope(t, rec)
}

// ── PUT /notifications/preferences ───────────────────────────────────────────

func TestSetNotificationPreferences_OK(t *testing.T) {
	email := "alerts@acme.com"
	prefs := domain.NotificationPreferences{
		EmailEnabled: true,
		WeeklyDigest: true,
		AlertEmail:   &email,
	}
	e := newBillingTestEcho(&stubBillingService{prefs: prefs, billingInfo: domain.Billing{Status: domain.BillingActive}})
	body := map[string]any{
		"email_enabled": true,
		"weekly_digest": true,
		"alert_email":   "alerts@acme.com",
	}
	rec := doJSON(e, http.MethodPut, "/v1/notifications/preferences", body, bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got domain.NotificationPreferences
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.True(t, got.EmailEnabled)
	assert.True(t, got.WeeklyDigest)
}

func TestSetNotificationPreferences_Unauthorized(t *testing.T) {
	e := newBillingTestEcho(&stubBillingService{})
	rec := doJSON(e, http.MethodPut, "/v1/notifications/preferences", map[string]any{}, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestSetNotificationPreferences_InternalError(t *testing.T) {
	e := newBillingTestEcho(&stubBillingService{setPrefsErr: errSentinel, billingInfo: domain.Billing{Status: domain.BillingActive}})
	rec := doJSON(e, http.MethodPut, "/v1/notifications/preferences", map[string]any{}, bearer(merchantToken))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assertErrorEnvelope(t, rec)
}
