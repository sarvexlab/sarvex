// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue"
	"github.com/sarvexlab/sarvex/internal/planes/control/api"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/store"
)

// ── Stubs ───────────────────────────────────────────────────────────────────

type stubProfileRepo struct {
	bp         *domain.StoreBusinessProfile
	cfg        *domain.StoreConfiguration
	getBPErr   error
	getCfgErr  error
	saveBPErr  error
	saveCfgErr error

	savedBP  *domain.StoreBusinessProfile
	savedCfg *domain.StoreConfiguration
}

func (s *stubProfileRepo) GetBusinessProfile(_ context.Context, _ domain.StoreID) (*domain.StoreBusinessProfile, error) {
	if s.getBPErr != nil {
		return nil, s.getBPErr
	}
	return s.bp, nil
}

func (s *stubProfileRepo) SaveBusinessProfile(_ context.Context, p *domain.StoreBusinessProfile) error {
	if s.saveBPErr != nil {
		return s.saveBPErr
	}
	s.savedBP = p
	return nil
}

func (s *stubProfileRepo) GetConfiguration(_ context.Context, _ domain.StoreID) (*domain.StoreConfiguration, error) {
	if s.getCfgErr != nil {
		return nil, s.getCfgErr
	}
	return s.cfg, nil
}

func (s *stubProfileRepo) SaveConfiguration(_ context.Context, c *domain.StoreConfiguration) error {
	if s.saveCfgErr != nil {
		return s.saveCfgErr
	}
	s.savedCfg = c
	return nil
}

func (s *stubProfileRepo) GetCapabilities(_ context.Context) (catalogue.Catalogue, error) {
	return nil, nil
}

// captureBus records published events; satisfies eventbus.EventBus.
type captureBus struct {
	mu     sync.Mutex
	events []eventbus.DomainEvent
}

func (b *captureBus) Publish(_ context.Context, e eventbus.DomainEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
	return nil
}
func (b *captureBus) Subscribe(context.Context, string, eventbus.EventHandler) (eventbus.Subscription, error) {
	return nil, nil
}
func (b *captureBus) Unsubscribe(context.Context, eventbus.Subscription) error { return nil }
func (b *captureBus) Close() error                                             { return nil }

// stubStoreServiceForProfile is a store.Service test double satisfying only
// Get (the lazy-profile-creation ownership check); every other method
// falls back to store.StubService's ErrNotImplemented.
type stubStoreServiceForProfile struct {
	store.StubService
	st     domain.Store
	getErr error
}

func (s *stubStoreServiceForProfile) Get(_ context.Context, _ domain.MerchantID, _ domain.StoreID) (domain.Store, error) {
	if s.getErr != nil {
		return domain.Store{}, s.getErr
	}
	return s.st, nil
}

func newProfileTestEcho(repo store.ProfileRepository, bus eventbus.EventBus) *echo.Echo {
	// Defaults to a store owned by merch-1 (merchantToken's principal) so
	// existing tests that don't care about the lazy-create ownership check
	// are unaffected; tests exercising that check build their own
	// api.Dependencies directly.
	return newProfileTestEchoWithStore(repo, bus, &stubStoreServiceForProfile{st: domain.Store{StoreID: "store-1", MerchantID: "merch-1"}})
}

func newProfileTestEchoWithStore(repo store.ProfileRepository, bus eventbus.EventBus, storeSvc store.Service) *echo.Echo {
	deps := api.Dependencies{
		Profile:        store.NewProfileService(repo),
		Store:          storeSvc,
		EventPublisher: bus,
		Billing:        activeBillingStub{},
		ResolveBearer:  testResolver,
		ResolveHMAC:    func(c echo.Context, sig string) (domain.Principal, error) { return domain.Principal{}, nil },
	}
	h := api.New(deps)
	e := echo.New()
	_ = h.SetupRoutes("/v1", e.Group("/v1"))
	return e
}

// ── GET /stores/:store_id/profile ────────────────────────────────────────────

func TestGetStoreProfile_OK(t *testing.T) {
	repo := &stubProfileRepo{
		bp: &domain.StoreBusinessProfile{
			StoreID: "store-1", MerchantID: "merch-1",
			LegalName: "Acme Pty Ltd", SupportEmail: "support@acme.com", StorefrontURL: "https://acme.example.com",
			ReturnPolicyURL: "https://acme.example.com/returns",
		},
		cfg: &domain.StoreConfiguration{
			StoreID: "store-1", MerchantID: "merch-1",
			FulfillmentRegions: []string{"AU", "NZ"},
			ShippingZones: map[string]domain.ShippingZone{
				"dom": {ID: "dom", Name: "Domestic", Rates: []domain.Rate{{Method: "ground", Cost: domain.Money{AmountMinor: 1200, Currency: "AUD"}}}},
			},
		},
	}
	e := newProfileTestEcho(repo, &captureBus{})
	rec := do(e, http.MethodGet, "/v1/stores/store-1/profile", bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)

	var got map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "Acme Pty Ltd", got["legal_name"])
	assert.Equal(t, "https://acme.example.com", got["domain"])
	assert.Equal(t, "https://acme.example.com/returns", got["return_policy_url"])
	assert.Equal(t, []any{"AU", "NZ"}, got["fulfillment_regions"])
	zones := got["shipping_zones"].(map[string]any)
	assert.Contains(t, zones, "dom")
}

// TestGetStoreProfile_LazyCreatesWhenMissing covers stores that existed
// before profile-seeding-on-create was added (or any other historical gap):
// the store is real and owned by this merchant, but has no profile row yet.
// GET must provision one and return 200, not 404.
func TestGetStoreProfile_LazyCreatesWhenMissing(t *testing.T) {
	repo := &stubProfileRepo{getBPErr: store.ErrProfileNotFound}
	e := newProfileTestEcho(repo, &captureBus{})
	rec := do(e, http.MethodGet, "/v1/stores/store-1/profile", bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)

	require.NotNil(t, repo.savedBP)
	assert.Equal(t, "store-1", repo.savedBP.StoreID)
	assert.Equal(t, "merch-1", repo.savedBP.MerchantID)

	var got map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "store-1", got["store_id"])
}

// TestGetStoreProfile_NotFound_UnknownStore verifies the lazy-create path
// does not fire for a store that doesn't belong to (or doesn't exist for)
// this merchant — a guessed/foreign store_id must still 404, not plant a
// profile claiming the wrong ownership.
func TestGetStoreProfile_NotFound_UnknownStore(t *testing.T) {
	repo := &stubProfileRepo{getBPErr: store.ErrProfileNotFound}
	e := newProfileTestEchoWithStore(repo, &captureBus{}, &stubStoreServiceForProfile{getErr: store.ErrNotFound})
	rec := do(e, http.MethodGet, "/v1/stores/store-1/profile", bearer(merchantToken))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assertErrorEnvelope(t, rec)
	assert.Nil(t, repo.savedBP, "must not create a profile for a store this merchant doesn't own")
}

// TestGetStoreProfile_LazyCreateSaveError verifies a persistence failure
// during lazy-create surfaces as 500, not a silent 404.
func TestGetStoreProfile_LazyCreateSaveError(t *testing.T) {
	repo := &stubProfileRepo{getBPErr: store.ErrProfileNotFound, saveBPErr: errSentinel}
	e := newProfileTestEcho(repo, &captureBus{})
	rec := do(e, http.MethodGet, "/v1/stores/store-1/profile", bearer(merchantToken))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGetStoreProfile_CrossMerchantHidden(t *testing.T) {
	repo := &stubProfileRepo{bp: &domain.StoreBusinessProfile{StoreID: "store-1", MerchantID: "someone-else"}}
	e := newProfileTestEcho(repo, &captureBus{})
	rec := do(e, http.MethodGet, "/v1/stores/store-1/profile", bearer(merchantToken))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetStoreProfile_Unauthorized(t *testing.T) {
	e := newProfileTestEcho(&stubProfileRepo{}, &captureBus{})
	rec := do(e, http.MethodGet, "/v1/stores/store-1/profile", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetStoreProfile_InternalError(t *testing.T) {
	repo := &stubProfileRepo{getBPErr: errSentinel}
	e := newProfileTestEcho(repo, &captureBus{})
	rec := do(e, http.MethodGet, "/v1/stores/store-1/profile", bearer(merchantToken))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ── PUT /stores/:store_id/profile ────────────────────────────────────────────

func TestUpdateStoreProfile_OK(t *testing.T) {
	repo := &stubProfileRepo{}
	bus := &captureBus{}
	e := newProfileTestEcho(repo, bus)

	body := map[string]any{
		"legal_name":          "New Co",
		"support_email":       "help@newco.com",
		"domain":              "https://newco.example.com",
		"return_policy_url":   "https://newco.example.com/returns",
		"fulfillment_regions": []string{"AU"},
		"shipping_zones": map[string]any{
			"dom": map[string]any{"id": "dom", "name": "Domestic", "rates": []any{
				map[string]any{"method": "ground", "cost": map[string]any{"amount_minor": 1000, "currency": "AUD"}},
			}},
		},
	}
	rec := doJSON(e, http.MethodPut, "/v1/stores/store-1/profile", body, bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)

	// Persistence side effects scoped to the caller's merchant.
	require.NotNil(t, repo.savedBP)
	assert.Equal(t, "merch-1", repo.savedBP.MerchantID)
	assert.Equal(t, "https://newco.example.com", repo.savedBP.StorefrontURL)
	assert.Equal(t, "https://newco.example.com/returns", repo.savedBP.ReturnPolicyURL)
	require.NotNil(t, repo.savedCfg)
	assert.Equal(t, []string{"AU"}, repo.savedCfg.FulfillmentRegions)
	assert.Contains(t, repo.savedCfg.ShippingZones, "dom")

	// Event emitted for the serving plane. Asserted against the constant in
	// internal/pkg/ucp, the single source of truth for bus topic names
	// (sarvex-v1-architecture.md) — the literal this replaced ("store.profile_updated")
	// was a stale second copy of a name that had since changed.
	require.Len(t, bus.events, 1)
	assert.Equal(t, ucp.EventStoreProfileUpdated, bus.events[0].EventType())
	assert.Equal(t, "store-1", bus.events[0].StoreID())
}

func TestUpdateStoreProfile_InvalidEmail(t *testing.T) {
	e := newProfileTestEcho(&stubProfileRepo{}, &captureBus{})
	body := map[string]any{"support_email": "not-an-email"}
	rec := doJSON(e, http.MethodPut, "/v1/stores/store-1/profile", body, bearer(merchantToken))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestUpdateStoreProfile_InvalidDomain(t *testing.T) {
	e := newProfileTestEcho(&stubProfileRepo{}, &captureBus{})
	body := map[string]any{"domain": "ftp://nope"}
	rec := doJSON(e, http.MethodPut, "/v1/stores/store-1/profile", body, bearer(merchantToken))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestUpdateStoreProfile_InvalidReturnPolicyURL(t *testing.T) {
	e := newProfileTestEcho(&stubProfileRepo{}, &captureBus{})
	body := map[string]any{"return_policy_url": "not-a-url"}
	rec := doJSON(e, http.MethodPut, "/v1/stores/store-1/profile", body, bearer(merchantToken))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestUpdateStoreProfile_ShippingZoneMissingCurrency(t *testing.T) {
	e := newProfileTestEcho(&stubProfileRepo{}, &captureBus{})
	body := map[string]any{
		"shipping_zones": map[string]any{
			"dom": map[string]any{"id": "dom", "name": "Domestic", "rates": []any{
				map[string]any{"method": "ground", "cost": map[string]any{"amount_minor": 1000}},
			}},
		},
	}
	rec := doJSON(e, http.MethodPut, "/v1/stores/store-1/profile", body, bearer(merchantToken))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestUpdateStoreProfile_CrossTenantDenied(t *testing.T) {
	repo := &stubProfileRepo{saveBPErr: store.ErrProfileNotFound}
	e := newProfileTestEcho(repo, &captureBus{})
	body := map[string]any{"legal_name": "Hijack Co"}
	rec := doJSON(e, http.MethodPut, "/v1/stores/store-1/profile", body, bearer(merchantToken))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUpdateStoreProfile_Unauthorized(t *testing.T) {
	e := newProfileTestEcho(&stubProfileRepo{}, &captureBus{})
	rec := doJSON(e, http.MethodPut, "/v1/stores/store-1/profile", map[string]any{}, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
