// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/account"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/api"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/catalog"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/connector"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/journey"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/operator"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/task"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/validation"
)

// stubAccountService is a controllable account.Service for handler component tests.
type stubAccountService struct {
	acc       domain.Account
	members   []domain.Member
	getErr    error
	updateErr error
	inviteErr error
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
	if s.inviteErr != nil {
		return domain.Member{}, s.inviteErr
	}
	if len(s.members) > 0 {
		return s.members[0], nil
	}
	return domain.Member{Email: "invited@example.com", Status: domain.MemberStatusInvited, Roles: []domain.MemberRole{domain.MemberRoleStaff}}, nil
}

func newAccountTestEcho(svc account.Service) *echo.Echo {
	return newAccountTestEchoWithLocales(svc, nil)
}

// stubLocaleProvider is a controllable account.LocaleProvider for handler
// component tests.
type stubLocaleProvider struct {
	countries []account.CountryLocale
	err       error
}

func (s *stubLocaleProvider) List(context.Context) ([]account.CountryLocale, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.countries, nil
}

func newAccountTestEchoWithLocales(svc account.Service, locales account.LocaleProvider) *echo.Echo {
	deps := api.Dependencies{
		Account:       svc,
		Locales:       locales,
		Connector:     connector.StubService{},
		Catalog:       catalog.StubService{},
		Task:          task.StubService{},
		Journey:       journey.StubService{},
		Billing:       activeBillingStub{},
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

func doJSON(e *echo.Echo, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
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

// ── GET /account ──────────────────────────────────────────────────────────────

func TestGetAccount_OK(t *testing.T) {
	e := newAccountTestEcho(&stubAccountService{acc: testAccount})
	rec := do(e, http.MethodGet, "/v1/account", bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got domain.Account
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, domain.MerchantID("merch-1"), got.MerchantID)
	assert.Equal(t, "Acme Store", got.Name)
}

// TestGetAccount_IncludesLocaleOptions verifies GET /account attaches the
// country/timezone reference list so the console's Locale dropdowns can
// rely entirely on the server response.
func TestGetAccount_IncludesLocaleOptions(t *testing.T) {
	locales := &stubLocaleProvider{countries: []account.CountryLocale{
		{Code: "AU", Name: "Australia", TimeZones: []string{"Australia/Sydney", "Australia/Perth"}},
		{Code: "NZ", Name: "New Zealand", TimeZones: []string{"Pacific/Auckland"}},
	}}
	e := newAccountTestEchoWithLocales(&stubAccountService{acc: testAccount}, locales)
	rec := do(e, http.MethodGet, "/v1/account", bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)

	var got map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	localeOpts := got["locale_options"].(map[string]any)
	countries := localeOpts["countries"].([]any)
	require.Len(t, countries, 2)
	au := countries[0].(map[string]any)
	assert.Equal(t, "AU", au["code"])
	assert.Equal(t, "Australia", au["name"])
	assert.Equal(t, []any{"Australia/Sydney", "Australia/Perth"}, au["time_zones"])
}

// TestGetAccount_LocaleOptionsEmptyWhenUnconfigured verifies a nil Locales
// dependency degrades to an empty list rather than a 500 or a missing field
// the client would have to special-case.
func TestGetAccount_LocaleOptionsEmptyWhenUnconfigured(t *testing.T) {
	e := newAccountTestEcho(&stubAccountService{acc: testAccount})
	rec := do(e, http.MethodGet, "/v1/account", bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)

	var got map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	localeOpts := got["locale_options"].(map[string]any)
	assert.Equal(t, []any{}, localeOpts["countries"])
}

// TestGetAccount_LocaleOptionsProviderError verifies a provider failure
// doesn't fail the whole account fetch — locale_options just comes back
// empty and the rest of the account response is still usable.
func TestGetAccount_LocaleOptionsProviderError(t *testing.T) {
	locales := &stubLocaleProvider{err: errSentinel}
	e := newAccountTestEchoWithLocales(&stubAccountService{acc: testAccount}, locales)
	rec := do(e, http.MethodGet, "/v1/account", bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)

	var got map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "Acme Store", got["name"])
	localeOpts := got["locale_options"].(map[string]any)
	assert.Equal(t, []any{}, localeOpts["countries"])
}

func TestGetAccount_Unauthorized(t *testing.T) {
	e := newAccountTestEcho(&stubAccountService{acc: testAccount})
	rec := do(e, http.MethodGet, "/v1/account", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestGetAccount_NotFound(t *testing.T) {
	e := newAccountTestEcho(&stubAccountService{getErr: account.ErrNotFound})
	rec := do(e, http.MethodGet, "/v1/account", bearer(merchantToken))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestGetAccount_InternalError(t *testing.T) {
	e := newAccountTestEcho(&stubAccountService{getErr: errSentinel})
	rec := do(e, http.MethodGet, "/v1/account", bearer(merchantToken))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assertErrorEnvelope(t, rec)
}

// ── PATCH /account ────────────────────────────────────────────────────────────

func TestUpdateAccount_OK(t *testing.T) {
	e := newAccountTestEcho(&stubAccountService{acc: testAccount})
	body := map[string]any{"name": "Updated Store"}
	rec := doJSON(e, http.MethodPatch, "/v1/account", body, bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got domain.Account
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "Acme Store", got.Name) // stub always returns testAccount
}

func TestUpdateAccount_Unauthorized(t *testing.T) {
	e := newAccountTestEcho(&stubAccountService{acc: testAccount})
	rec := doJSON(e, http.MethodPatch, "/v1/account", map[string]any{"name": "x"}, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestUpdateAccount_InvalidJSON(t *testing.T) {
	e := newAccountTestEcho(&stubAccountService{acc: testAccount})
	req := httptest.NewRequest(http.MethodPatch, "/v1/account", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+merchantToken)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestUpdateAccount_InternalError(t *testing.T) {
	e := newAccountTestEcho(&stubAccountService{updateErr: errSentinel})
	rec := doJSON(e, http.MethodPatch, "/v1/account", map[string]any{"name": "x"}, bearer(merchantToken))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assertErrorEnvelope(t, rec)
}

// ── GET /account/members ──────────────────────────────────────────────────────

func TestListMembers_OK(t *testing.T) {
	members := []domain.Member{
		{UserID: "u-1", Email: "a@example.com", Name: "Alice", Roles: []domain.MemberRole{"owner"}, Status: domain.MemberStatusActive, CountryCode: "AU"},
		{UserID: "u-2", Email: "b@example.com", Name: "Bob", Roles: []domain.MemberRole{"staff"}, Status: domain.MemberStatusActive, CountryCode: "AU"},
	}
	e := newAccountTestEcho(&stubAccountService{members: members})
	rec := do(e, http.MethodGet, "/v1/account/members", bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got domain.ListOf[domain.Member]
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Len(t, got.Data, 2)
	assert.Equal(t, "u-1", got.Data[0].UserID)
	assert.Equal(t, "Alice", got.Data[0].Name)
	assert.Equal(t, []domain.MemberRole{"owner"}, got.Data[0].Roles)
	assert.Equal(t, "AU", got.Data[0].CountryCode)
}

func TestListMembers_Empty(t *testing.T) {
	e := newAccountTestEcho(&stubAccountService{members: nil})
	rec := do(e, http.MethodGet, "/v1/account/members", bearer(merchantToken))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got domain.ListOf[domain.Member]
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Empty(t, got.Data)
}

func TestListMembers_Unauthorized(t *testing.T) {
	e := newAccountTestEcho(&stubAccountService{})
	rec := do(e, http.MethodGet, "/v1/account/members", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assertErrorEnvelope(t, rec)
}

// ── POST /account/members (owner self-serve invite) ─────────────────────────────

func TestInviteTeamMember_OK(t *testing.T) {
	invited := domain.Member{Email: "new@example.com", Status: domain.MemberStatusInvited, Roles: []domain.MemberRole{domain.MemberRoleStaff}}
	e := newAccountTestEcho(&stubAccountService{members: []domain.Member{invited}})
	rec := doJSON(e, http.MethodPost, "/v1/account/members", map[string]any{"email": "new@example.com"}, bearer(merchantToken))
	assert.Equal(t, http.StatusCreated, rec.Code)

	var got domain.Member
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "new@example.com", got.Email)
	assert.Equal(t, domain.MemberStatusInvited, got.Status)
	assert.Equal(t, []domain.MemberRole{domain.MemberRoleStaff}, got.Roles)
}

func TestInviteTeamMember_Unauthorized(t *testing.T) {
	e := newAccountTestEcho(&stubAccountService{})
	rec := doJSON(e, http.MethodPost, "/v1/account/members", map[string]any{"email": "new@example.com"}, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assertErrorEnvelope(t, rec)
}

// TestInviteTeamMember_StaffForbidden verifies only the account owner may
// self-serve invite — a staff principal is rejected by requireOwner.
func TestInviteTeamMember_StaffForbidden(t *testing.T) {
	e := newAccountTestEcho(&stubAccountService{})
	rec := doJSON(e, http.MethodPost, "/v1/account/members", map[string]any{"email": "new@example.com"}, bearer(staffToken))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestInviteTeamMember_MissingEmail(t *testing.T) {
	e := newAccountTestEcho(&stubAccountService{})
	rec := doJSON(e, http.MethodPost, "/v1/account/members", map[string]any{}, bearer(merchantToken))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestInviteTeamMember_InvalidJSON(t *testing.T) {
	e := newAccountTestEcho(&stubAccountService{})
	req := httptest.NewRequest(http.MethodPost, "/v1/account/members", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+merchantToken)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestInviteTeamMember_AlreadyActive(t *testing.T) {
	e := newAccountTestEcho(&stubAccountService{inviteErr: account.ErrMemberAlreadyActive})
	rec := doJSON(e, http.MethodPost, "/v1/account/members", map[string]any{"email": "active@example.com"}, bearer(merchantToken))
	assert.Equal(t, http.StatusConflict, rec.Code)

	var env domain.ErrorEnvelope
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&env))
	assert.Equal(t, "member_already_active", env.Error.Code)
}

func TestInviteTeamMember_LimitReached(t *testing.T) {
	e := newAccountTestEcho(&stubAccountService{inviteErr: account.ErrMemberLimitReached})
	rec := doJSON(e, http.MethodPost, "/v1/account/members", map[string]any{"email": "new@example.com"}, bearer(merchantToken))
	assert.Equal(t, http.StatusConflict, rec.Code)

	var env domain.ErrorEnvelope
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&env))
	assert.Equal(t, "member_limit_reached", env.Error.Code)
}

func TestInviteTeamMember_InternalError(t *testing.T) {
	e := newAccountTestEcho(&stubAccountService{inviteErr: errSentinel})
	rec := doJSON(e, http.MethodPost, "/v1/account/members", map[string]any{"email": "new@example.com"}, bearer(merchantToken))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assertErrorEnvelope(t, rec)
}
