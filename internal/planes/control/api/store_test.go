// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/sarvexlab/sarvex/internal/platform"
	"github.com/sarvexlab/sarvex/internal/platform/woocommerce"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/api"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/journey"
	"github.com/sarvexlab/sarvex/internal/planes/control/store"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
)

// createOnlyStoreService returns a fixed store from Create; every other
// method falls back to store.StubService's ErrNotImplemented.
type createOnlyStoreService struct {
	store.StubService
	created domain.Store
}

func (s createOnlyStoreService) Create(_ context.Context, _ domain.MerchantID, _ string, _ domain.Platform) (domain.Store, error) {
	return s.created, nil
}

func newCreateStoreTestEcho(profileRepo store.ProfileRepository) *echo.Echo {
	deps := api.Dependencies{
		Store: createOnlyStoreService{created: domain.Store{
			StoreID:  "store-1",
			Platform: woocommerce.PlatformName,
		}},
		Journey:       journey.StubService{},
		Profile:       store.NewProfileService(profileRepo),
		Billing:       activeBillingStub{},
		ResolveBearer: testResolver,
		ResolveHMAC:   func(c echo.Context, sig string) (domain.Principal, error) { return domain.Principal{}, nil },
	}
	h := api.New(deps)
	e := echo.New()
	_ = h.SetupRoutes("/v1", e.Group("/v1"))
	return e
}

func TestCreateStore_SeedsEmptyProfile(t *testing.T) {
	repo := &stubProfileRepo{}
	e := newCreateStoreTestEcho(repo)

	body := map[string]any{"display_name": "Acme", "platform": "woocommerce"}
	rec := doJSON(e, http.MethodPost, "/v1/stores", body, bearer(merchantToken))
	require.Equal(t, http.StatusCreated, rec.Code)

	// A profile row now exists for this store, so GET /profile won't 404
	// until the merchant's first real save — verified against the repo the
	// GET handler reads from.
	require.NotNil(t, repo.savedBP)
	assert.Equal(t, "store-1", repo.savedBP.StoreID)
	assert.Equal(t, "merch-1", repo.savedBP.MerchantID)
}

func TestCreateStore_ProfileSeedFailureDoesNotFailRequest(t *testing.T) {
	repo := &stubProfileRepo{saveBPErr: errSentinel}
	e := newCreateStoreTestEcho(repo)

	body := map[string]any{"display_name": "Acme", "platform": "woocommerce"}
	rec := doJSON(e, http.MethodPost, "/v1/stores", body, bearer(merchantToken))
	assert.Equal(t, http.StatusCreated, rec.Code)
}

// getOnlyStoreService returns a fixed store from Get/List; every other
// method falls back to store.StubService's ErrNotImplemented.
type getOnlyStoreService struct {
	store.StubService
	st domain.Store
}

func (s getOnlyStoreService) Get(_ context.Context, _ domain.MerchantID, _ domain.StoreID) (domain.Store, error) {
	return s.st, nil
}

func (s getOnlyStoreService) List(_ context.Context, _ domain.MerchantID, _ string, _ int) (domain.ListOf[domain.Store], error) {
	return domain.ListOf[domain.Store]{Data: []domain.Store{s.st}}, nil
}

func newGetStoreTestEcho(storeSvc store.Service, profileRepo store.ProfileRepository) *echo.Echo {
	return newGetStoreTestEchoWithTasks(storeSvc, profileRepo, nil)
}

// newGetStoreTestEchoWithTasks additionally wires a task.Service so tests
// can assert on the store switcher's open_task_count/blocked badge. Pass
// nil to exercise the "Task dependency not configured" default (0, false).
func newGetStoreTestEchoWithTasks(storeSvc store.Service, profileRepo store.ProfileRepository, taskSvc task.Service) *echo.Echo {
	return newGetStoreTestEchoWithTasksAndJourney(storeSvc, profileRepo, taskSvc, nil)
}

// getOnlyJourneyService returns a fixed journey from Get; every other method
// falls back to journey.StubService's ErrNotImplemented.
type getOnlyJourneyService struct {
	journey.StubService
	j domain.Journey
}

func (s getOnlyJourneyService) Get(_ context.Context, _ domain.MerchantID, _ domain.StoreID) (domain.Journey, error) {
	return s.j, nil
}

// newGetStoreTestEchoWithTasksAndJourney additionally wires a
// journey.Service so tests can assert on the store response's journey_state
// field. Pass nil to exercise the "Journey dependency not configured"
// default (empty string).
func newGetStoreTestEchoWithTasksAndJourney(storeSvc store.Service, profileRepo store.ProfileRepository, taskSvc task.Service, journeySvc journey.Service) *echo.Echo {
	deps := api.Dependencies{
		Store:         storeSvc,
		Profile:       store.NewProfileService(profileRepo),
		Task:          taskSvc,
		Journey:       journeySvc,
		Billing:       activeBillingStub{},
		ResolveBearer: testResolver,
		ResolveHMAC:   func(c echo.Context, sig string) (domain.Principal, error) { return domain.Principal{}, nil },
	}
	h := api.New(deps)
	e := echo.New()
	_ = h.SetupRoutes("/v1", e.Group("/v1"))
	return e
}

// TestGetStore_PopulatesDomainFromProfile verifies GET /stores/:store_id
// fills the contract's store.domain field (sarvex-v1-control-plane.yaml)
// from the store's profile-declared storefront URL — the store record
// itself carries no domain field, so this join is the only source for it.
func TestGetStore_PopulatesDomainFromProfile(t *testing.T) {
	storeSvc := getOnlyStoreService{st: domain.Store{StoreID: "store-1", MerchantID: "merch-1", DisplayName: "Acme"}}
	profileRepo := &stubProfileRepo{bp: &domain.StoreBusinessProfile{
		StoreID: "store-1", MerchantID: "merch-1", StorefrontURL: "https://store-acme.com.au",
	}}
	e := newGetStoreTestEcho(storeSvc, profileRepo)

	rec := do(e, http.MethodGet, "/v1/stores/store-1", bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)

	var got map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "https://store-acme.com.au", got["domain"])
}

// TestGetStore_DomainEmptyWhenNoProfileYet verifies a store with no profile
// row yet gets an empty domain, not an error.
func TestGetStore_DomainEmptyWhenNoProfileYet(t *testing.T) {
	storeSvc := getOnlyStoreService{st: domain.Store{StoreID: "store-1", MerchantID: "merch-1"}}
	profileRepo := &stubProfileRepo{getBPErr: store.ErrProfileNotFound}
	e := newGetStoreTestEcho(storeSvc, profileRepo)

	rec := do(e, http.MethodGet, "/v1/stores/store-1", bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)

	var got map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "", got["domain"])
}

// TestListStores_PopulatesDomainFromProfile verifies GET /stores also joins
// the domain field per store, not just the single-store GET.
func TestListStores_PopulatesDomainFromProfile(t *testing.T) {
	storeSvc := getOnlyStoreService{st: domain.Store{StoreID: "store-1", MerchantID: "merch-1"}}
	profileRepo := &stubProfileRepo{bp: &domain.StoreBusinessProfile{
		StoreID: "store-1", MerchantID: "merch-1", StorefrontURL: "https://store-acme.com.au",
	}}
	e := newGetStoreTestEcho(storeSvc, profileRepo)

	rec := do(e, http.MethodGet, "/v1/stores", bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)

	var got map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	data := got["data"].([]any)
	require.Len(t, data, 1)
	assert.Equal(t, "https://store-acme.com.au", data[0].(map[string]any)["domain"])
}

// ── open_task_count / blocked (header store switcher badge) ─────────────────

// TestGetStore_IncludesTaskSummary verifies GET /stores/:store_id counts
// only merchant-owned open tasks toward open_task_count (matching the
// switcher's "N for you" badge), while blocked reflects ANY open task with
// blocks_checkout, regardless of owner.
func TestGetStore_IncludesTaskSummary(t *testing.T) {
	storeSvc := getOnlyStoreService{st: domain.Store{StoreID: "store-1", MerchantID: "merch-1"}}
	profileRepo := &stubProfileRepo{getBPErr: store.ErrProfileNotFound}
	taskSvc := &stubTaskService{tasks: []domain.Task{
		{TaskID: "t1", Owner: "merchant", State: "open", BlocksCheckout: false},
		{TaskID: "t2", Owner: "merchant", State: "open", BlocksCheckout: false},
		{TaskID: "t3", Owner: "sarvex", State: "open", BlocksCheckout: true},
	}}
	e := newGetStoreTestEchoWithTasks(storeSvc, profileRepo, taskSvc)

	rec := do(e, http.MethodGet, "/v1/stores/store-1", bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)

	require.NotNil(t, taskSvc.lastStoreID)
	assert.Equal(t, domain.StoreID("store-1"), *taskSvc.lastStoreID)

	var got map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, float64(2), got["open_task_count"])
	assert.Equal(t, true, got["blocked"])
}

// TestGetStore_TaskSummaryDefaultsWhenTaskServiceUnconfigured verifies the
// badge degrades to (0, false) rather than panicking or failing the request
// when the Task dependency isn't wired.
func TestGetStore_TaskSummaryDefaultsWhenTaskServiceUnconfigured(t *testing.T) {
	storeSvc := getOnlyStoreService{st: domain.Store{StoreID: "store-1", MerchantID: "merch-1"}}
	profileRepo := &stubProfileRepo{getBPErr: store.ErrProfileNotFound}
	e := newGetStoreTestEcho(storeSvc, profileRepo)

	rec := do(e, http.MethodGet, "/v1/stores/store-1", bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)

	var got map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, float64(0), got["open_task_count"])
	assert.Equal(t, false, got["blocked"])
}

// ── journey_state (sarvex-v1-control-plane.yaml's store schema; drives the
// console's Presence tab "live" vs "draft" treatment) ───────────────────────

// TestGetStore_IncludesJourneyState verifies GET /stores/:store_id joins the
// store's current journey state — a store that has actually reached live
// must report journey_state: "live", not a value frozen at store creation.
func TestGetStore_IncludesJourneyState(t *testing.T) {
	storeSvc := getOnlyStoreService{st: domain.Store{StoreID: "store-1", MerchantID: "merch-1"}}
	profileRepo := &stubProfileRepo{getBPErr: store.ErrProfileNotFound}
	journeySvc := getOnlyJourneyService{j: domain.Journey{State: domain.JourneyLive}}
	e := newGetStoreTestEchoWithTasksAndJourney(storeSvc, profileRepo, nil, journeySvc)

	rec := do(e, http.MethodGet, "/v1/stores/store-1", bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)

	var got map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "live", got["journey_state"])
}

// TestGetStore_JourneyStateEmptyWhenJourneyServiceUnconfigured verifies the
// field degrades to empty rather than panicking or failing the request when
// the Journey dependency isn't wired.
func TestGetStore_JourneyStateEmptyWhenJourneyServiceUnconfigured(t *testing.T) {
	storeSvc := getOnlyStoreService{st: domain.Store{StoreID: "store-1", MerchantID: "merch-1"}}
	profileRepo := &stubProfileRepo{getBPErr: store.ErrProfileNotFound}
	e := newGetStoreTestEcho(storeSvc, profileRepo)

	rec := do(e, http.MethodGet, "/v1/stores/store-1", bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)

	var got map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	_, present := got["journey_state"]
	assert.False(t, present, "journey_state should be omitted (omitempty), not present as an empty string")
}

// TestListStores_IncludesJourneyState verifies GET /stores also joins
// journey_state per store, not just the single-store GET.
func TestListStores_IncludesJourneyState(t *testing.T) {
	storeSvc := getOnlyStoreService{st: domain.Store{StoreID: "store-1", MerchantID: "merch-1"}}
	profileRepo := &stubProfileRepo{getBPErr: store.ErrProfileNotFound}
	journeySvc := getOnlyJourneyService{j: domain.Journey{State: domain.JourneyLive}}
	e := newGetStoreTestEchoWithTasksAndJourney(storeSvc, profileRepo, nil, journeySvc)

	rec := do(e, http.MethodGet, "/v1/stores", bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)

	var got map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	data := got["data"].([]any)
	require.Len(t, data, 1)
	assert.Equal(t, "live", data[0].(map[string]any)["journey_state"])
}

// TestListStores_IncludesTaskSummary verifies GET /stores also joins the
// task summary per store, not just the single-store GET.
func TestListStores_IncludesTaskSummary(t *testing.T) {
	storeSvc := getOnlyStoreService{st: domain.Store{StoreID: "store-1", MerchantID: "merch-1"}}
	profileRepo := &stubProfileRepo{getBPErr: store.ErrProfileNotFound}
	taskSvc := &stubTaskService{tasks: []domain.Task{
		{TaskID: "t1", Owner: "merchant", State: "open", BlocksCheckout: false},
	}}
	e := newGetStoreTestEchoWithTasks(storeSvc, profileRepo, taskSvc)

	rec := do(e, http.MethodGet, "/v1/stores", bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)

	var got map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	data := got["data"].([]any)
	require.Len(t, data, 1)
	row := data[0].(map[string]any)
	assert.Equal(t, float64(1), row["open_task_count"])
	assert.Equal(t, false, row["blocked"])
}

// The platforms a store can be created on are the platform registry's, not a
// list kept in the control plane: every registered platform is accepted, and
// anything else — the frozen Shopify connector included — is refused with a
// message naming the registered ones.
func TestCreateStore_AcceptsExactlyTheRegisteredPlatforms(t *testing.T) {
	for _, name := range platform.Names() {
		t.Run(string(name), func(t *testing.T) {
			e := newCreateStoreTestEcho(&stubProfileRepo{})
			body := map[string]any{"display_name": "Acme", "platform": string(name)}
			rec := doJSON(e, http.MethodPost, "/v1/stores", body, bearer(merchantToken))
			assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		})
	}

	e := newCreateStoreTestEcho(&stubProfileRepo{})
	body := map[string]any{"display_name": "Acme", "platform": "shopify"}
	rec := doJSON(e, http.MethodPost, "/v1/stores", body, bearer(merchantToken))
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	for _, name := range platform.Names() {
		assert.Contains(t, rec.Body.String(), string(name))
	}
}
