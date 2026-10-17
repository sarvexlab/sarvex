// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/billing"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// captureHandler records the principal injected by middleware into captured.
func captureHandler(captured *domain.Principal) echo.HandlerFunc {
	return func(c echo.Context) error {
		p := principalFrom(c)
		*captured = p
		return c.JSON(http.StatusOK, p)
	}
}

func stubResolver(token string) (domain.Principal, error) {
	switch token {
	case "operator":
		return domain.Principal{UserID: "op-1", Role: domain.RoleOperator, MerchantID: ""}, nil
	case "merchant":
		return domain.Principal{UserID: "m-1", Role: domain.RoleOwner, MerchantID: "merch-abc"}, nil
	}
	return domain.Principal{}, errors.New("unknown token")
}

func stubHMACResolver(c echo.Context, signature string) (domain.Principal, error) {
	if signature == "" {
		return domain.Principal{}, errors.New("empty signature")
	}
	return domain.Principal{MerchantID: "test-merchant"}, nil
}

// stubAccountService is a controllable account.Service for handler component tests.
type stubAccountService struct {
	acc       domain.Account
	members   []domain.Member
	getErr    error
	updateErr error
}

func (s *stubAccountService) Get(_ context.Context, _ domain.MerchantID) (domain.Account, error) {
	return s.acc, s.getErr
}

func (s *stubAccountService) Update(_ context.Context, _ domain.MerchantID, _ domain.AccountUpdate) (domain.Account, error) {
	return s.acc, s.updateErr
}

func (s *stubAccountService) ListMembers(_ context.Context, _ domain.MerchantID, _ string, _ int) (domain.ListOf[domain.Member], error) {
	return domain.ListOf[domain.Member]{Data: s.members, Page: domain.Page{HasMore: false}}, nil
}

func (s *stubAccountService) Create(_ context.Context, _ string, _ *domain.BusinessFacts, _ string) (domain.Account, error) {
	return s.acc, s.getErr
}

func (s *stubAccountService) List(_ context.Context, _ string, _ int) (domain.ListOf[domain.Account], error) {
	return domain.ListOf[domain.Account]{Data: []domain.Account{s.acc}, Page: domain.Page{HasMore: false}}, s.getErr
}

func (s *stubAccountService) InviteMember(_ context.Context, _ domain.MerchantID, _ string, _ domain.MemberRole) (domain.Member, error) {
	if len(s.members) > 0 {
		return s.members[0], nil
	}
	return domain.Member{}, s.getErr
}
func (s *stubAccountService) BindInvitedMember(ctx context.Context, email string, idpSub string) (domain.Member, domain.MerchantID, error) {
	if len(s.members) > 0 {
		return s.members[0], s.acc.MerchantID, nil
	}
	return domain.Member{}, s.acc.MerchantID, s.getErr
}

func (s *stubAccountService) InviteTeamMember(_ context.Context, _ domain.MerchantID, _ string) (domain.Member, error) {
	if len(s.members) > 0 {
		return s.members[0], nil
	}
	return domain.Member{}, s.getErr
}

var testAccount = domain.Account{
	MerchantID: "merch-1",
	Name:       "Acme Store",
	Timezone:   "Australia/Sydney",
	BusinessFacts: domain.BusinessFacts{
		LegalEntityName: "Acme Pty Ltd",
		SupportEmail:    "support@acme.com",
	},
	CreatedAt: time.Now().UTC(),
}

func TestRequireAuth_RejectsNoToken(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	mw := requireAuth(stubResolver, &stubAccountService{acc: testAccount})
	err := mw(captureHandler(new(domain.Principal)))(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAuth_RejectsBadToken(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer bad-token")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	mw := requireAuth(stubResolver, &stubAccountService{acc: testAccount})
	err := mw(captureHandler(new(domain.Principal)))(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAuth_MerchantPrincipalPassedThrough(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer merchant")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	var captured domain.Principal
	mw := requireAuth(stubResolver, &stubAccountService{acc: testAccount})
	err := mw(captureHandler(&captured))(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, domain.MerchantID("merch-abc"), captured.MerchantID)
	assert.Equal(t, domain.RoleOwner, captured.Role)
}

// TestRequireAuth_OperateAs_InjectsMerchantID verifies that when an operator
// sends X-Operate-As, that value becomes the effective MerchantID in the
// principal. This is the only path through which merchant_id reaches the
// context for operator actions.
func TestRequireAuth_OperateAs_InjectsMerchantID(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer operator")
	req.Header.Set("X-Operate-As", "target-merchant-456")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	var captured domain.Principal
	mw := requireAuth(stubResolver, &stubAccountService{acc: testAccount})
	err := mw(captureHandler(&captured))(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, domain.RoleOperator, captured.Role)
	assert.Equal(t, domain.MerchantID("target-merchant-456"), captured.MerchantID,
		"operator X-Operate-As must become the effective MerchantID")
}

// TestRequireAuth_OperatorWithoutOperateAs_HasEmptyMerchantID verifies that
// an operator without X-Operate-As does not accidentally inherit any
// merchant context.
func TestRequireAuth_OperatorWithoutOperateAs_HasEmptyMerchantID(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer operator")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	var captured domain.Principal
	mw := requireAuth(stubResolver, &stubAccountService{acc: testAccount})
	err := mw(captureHandler(&captured))(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, domain.MerchantID(""), captured.MerchantID,
		"operator without X-Operate-As must have no merchant context")
}

// TestRequireAuth_OperateAs_IgnoredForNonOperator verifies that a merchant
// bearer token cannot use X-Operate-As to escape their own tenant.
func TestRequireAuth_OperateAs_IgnoredForNonOperator(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer merchant")
	req.Header.Set("X-Operate-As", "someone-elses-merchant")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	var captured domain.Principal
	mw := requireAuth(stubResolver, &stubAccountService{acc: testAccount})
	err := mw(captureHandler(&captured))(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, domain.MerchantID("merch-abc"), captured.MerchantID,
		"non-operator must not be able to override tenant via X-Operate-As")
}

func TestRequireOperator_RejectsNonOperator(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(ctxPrincipal), domain.Principal{Role: domain.RoleOwner})

	err := requireOperator(captureHandler(new(domain.Principal)))(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRequireOperator_AllowsOperator(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(ctxPrincipal), domain.Principal{Role: domain.RoleOperator})

	var captured domain.Principal
	err := requireOperator(captureHandler(&captured))(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, domain.RoleOperator, captured.Role)
}

func TestRequireHMAC_RejectsMissingSignature(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	mw := requireHMAC(stubHMACResolver)
	err := mw(captureHandler(new(domain.Principal)))(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// stubBillingStatusService is a controllable billing.Service for
// requireActiveBilling tests — only Get matters here.
type stubBillingStatusService struct {
	billing.StubService
	status domain.BillingStatus
	err    error
}

func (s stubBillingStatusService) Get(_ context.Context, _ domain.MerchantID) (domain.Billing, error) {
	return domain.Billing{Status: s.status}, s.err
}

func requireActiveBillingTestContext(role domain.Role, merchantID domain.MerchantID) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(ctxPrincipal), domain.Principal{Role: role, MerchantID: merchantID})
	return c, rec
}

func TestRequireActiveBilling_AllowsActive(t *testing.T) {
	c, rec := requireActiveBillingTestContext(domain.RoleOwner, "merch-1")
	mw := requireActiveBilling(stubBillingStatusService{status: domain.BillingActive})
	err := mw(captureHandler(new(domain.Principal)))(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRequireActiveBilling_AllowsActiveViaInvoice(t *testing.T) {
	c, rec := requireActiveBillingTestContext(domain.RoleOwner, "merch-1")
	mw := requireActiveBilling(stubBillingStatusService{status: domain.BillingActiveViaInvoice})
	err := mw(captureHandler(new(domain.Principal)))(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRequireActiveBilling_BlocksPastDue(t *testing.T) {
	c, rec := requireActiveBillingTestContext(domain.RoleOwner, "merch-1")
	mw := requireActiveBilling(stubBillingStatusService{status: domain.BillingPastDue})
	err := mw(captureHandler(new(domain.Principal)))(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRequireActiveBilling_BlocksCanceled(t *testing.T) {
	c, rec := requireActiveBillingTestContext(domain.RoleOwner, "merch-1")
	mw := requireActiveBilling(stubBillingStatusService{status: domain.BillingCanceled})
	err := mw(captureHandler(new(domain.Principal)))(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRequireActiveBilling_AllowsOperatorRegardlessOfStatus(t *testing.T) {
	c, rec := requireActiveBillingTestContext(domain.RoleOperator, "merch-1")
	mw := requireActiveBilling(stubBillingStatusService{status: domain.BillingCanceled})
	err := mw(captureHandler(new(domain.Principal)))(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// countingBillingStatusReader is a billing.PlanReader that counts calls, so
// TestRequireActiveBilling_CachedReaderAvoidsRepeatedSourceCalls can assert
// the cache — not the source — served the repeat requests.
type countingBillingStatusReader struct {
	calls  int
	status domain.BillingStatus
}

func (s *countingBillingStatusReader) Get(_ context.Context, _ domain.MerchantID) (domain.Billing, error) {
	s.calls++
	return domain.Billing{Status: s.status}, nil
}

// TestRequireActiveBilling_CachedReaderAvoidsRepeatedSourceCalls documents
// the reason SetupRoutes wraps h.deps.Billing in a billing.CachedPlanReader
// before constructing this middleware: repeated gated requests for the same
// merchant within the TTL must not each round-trip the billing source.
func TestRequireActiveBilling_CachedReaderAvoidsRepeatedSourceCalls(t *testing.T) {
	source := &countingBillingStatusReader{status: domain.BillingActive}
	cached := billing.NewCachedPlanReader(source, time.Minute)
	mw := requireActiveBilling(cached)

	for range 3 {
		c, rec := requireActiveBillingTestContext(domain.RoleOwner, "merch-1")
		err := mw(captureHandler(new(domain.Principal)))(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
	}

	assert.Equal(t, 1, source.calls, "expected the cached reader to serve repeat requests for the same merchant without re-hitting the source")
}

func TestRequireActiveBilling_GetErrorReturnsInternalError(t *testing.T) {
	c, rec := requireActiveBillingTestContext(domain.RoleOwner, "merch-1")
	mw := requireActiveBilling(stubBillingStatusService{err: assert.AnError})
	err := mw(captureHandler(new(domain.Principal)))(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
