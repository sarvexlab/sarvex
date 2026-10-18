// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sarvexlab/sarvex/internal/platform/woocommerce"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/account"
	"github.com/sarvexlab/sarvex/internal/planes/control/activation"
	"github.com/sarvexlab/sarvex/internal/planes/control/api"
	"github.com/sarvexlab/sarvex/internal/planes/control/catalog"
	"github.com/sarvexlab/sarvex/internal/planes/control/connector"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/journey"
	"github.com/sarvexlab/sarvex/internal/planes/control/operator"
	"github.com/sarvexlab/sarvex/internal/planes/control/surface"
	"github.com/sarvexlab/sarvex/internal/planes/control/surface/directagent"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
	"github.com/sarvexlab/sarvex/internal/planes/control/validation"
)

// These tests run the real activation and surface services behind the HTTP
// layer rather than stubs: the properties under test (tenancy isolation,
// the active-store gate, withdrawal semantics) are exactly the ones a stub
// would assume away.

const (
	tenantAToken = "tenant-a-token"
	tenantBToken = "tenant-b-token"

	tenantA = domain.MerchantID("merch-a")
	tenantB = domain.MerchantID("merch-b")
)

// twoTenantResolver issues principals for two distinct merchants, so a
// cross-tenant read is actually expressible.
func twoTenantResolver(token string) (domain.Principal, error) {
	switch token {
	case tenantAToken:
		return domain.Principal{UserID: "u-a", Role: domain.RoleOwner, MerchantID: tenantA}, nil
	case tenantBToken:
		return domain.Principal{UserID: "u-b", Role: domain.RoleOwner, MerchantID: tenantB}, nil
	default:
		return domain.Principal{}, errors.New("no token")
	}
}

// ── in-memory repositories ───────────────────────────────────────────────

type memActivationRepo struct {
	docs map[string]*activation.ActivationModel
}

func newMemActivationRepo() *memActivationRepo {
	return &memActivationRepo{docs: map[string]*activation.ActivationModel{}}
}

func (r *memActivationRepo) key(m domain.MerchantID, s domain.StoreID) string {
	return string(m) + ":" + string(s)
}

func (r *memActivationRepo) Get(_ context.Context, m domain.MerchantID, s domain.StoreID) (*activation.ActivationModel, error) {
	doc, ok := r.docs[r.key(m, s)]
	if !ok {
		return nil, activation.ErrNotFound
	}
	clone := *doc
	return &clone, nil
}

func (r *memActivationRepo) Upsert(_ context.Context, m *activation.ActivationModel) error {
	clone := *m
	r.docs[r.key(domain.MerchantID(m.MerchantID), domain.StoreID(m.ID))] = &clone
	return nil
}

func (r *memActivationRepo) ListByState(context.Context, domain.UCPState) ([]*activation.ActivationModel, error) {
	return nil, nil
}

func (r *memActivationRepo) Delete(_ context.Context, m domain.MerchantID, s domain.StoreID) error {
	delete(r.docs, r.key(m, s))
	return nil
}

type memSurfaceRepo struct {
	docs map[string]*surface.EnrollmentModel
}

func newMemSurfaceRepo() *memSurfaceRepo {
	return &memSurfaceRepo{docs: map[string]*surface.EnrollmentModel{}}
}

func (r *memSurfaceRepo) Get(_ context.Context, m domain.MerchantID, s domain.StoreID, surfaceID string) (*surface.EnrollmentModel, error) {
	doc, ok := r.docs[surface.DocumentID(m, s, surfaceID)]
	if !ok {
		return nil, surface.ErrNotFound
	}
	clone := *doc
	return &clone, nil
}

func (r *memSurfaceRepo) ListByStore(_ context.Context, m domain.MerchantID, s domain.StoreID) ([]*surface.EnrollmentModel, error) {
	var out []*surface.EnrollmentModel
	for _, doc := range r.docs {
		if doc.MerchantID == string(m) && doc.StoreID == string(s) {
			clone := *doc
			out = append(out, &clone)
		}
	}
	return out, nil
}

func (r *memSurfaceRepo) Upsert(_ context.Context, m *surface.EnrollmentModel) error {
	clone := *m
	r.docs[surface.DocumentID(domain.MerchantID(m.MerchantID), domain.StoreID(m.StoreID), m.SurfaceID)] = &clone
	return nil
}

func (r *memSurfaceRepo) Delete(_ context.Context, m domain.MerchantID, s domain.StoreID, surfaceID string) error {
	delete(r.docs, surface.DocumentID(m, s, surfaceID))
	return nil
}

func (r *memSurfaceRepo) ListStoresBySurfaceState(context.Context, string, domain.SurfaceState) ([]string, error) {
	return nil, nil
}

type apiStubStores struct{}

func (apiStubStores) Get(_ context.Context, m domain.MerchantID, s domain.StoreID) (domain.Store, error) {
	return domain.Store{StoreID: s, MerchantID: m, Platform: woocommerce.PlatformName}, nil
}

type apiStubConnectors struct{}

func (apiStubConnectors) ListByStore(context.Context, domain.MerchantID, domain.StoreID) ([]domain.Connector, error) {
	return nil, nil
}

// ── harness ──────────────────────────────────────────────────────────────

type surfaceTestEnv struct {
	echo       *echo.Echo
	activation *activation.ActivationService
	surfaces   surface.Service
}

// newSurfaceTestEnv wires the real services behind the HTTP layer. Two
// surfaces are registered: direct_agent (auto-enrolling, no requirements)
// and a plain opt-in one, which is enough to exercise every route.
func newSurfaceTestEnv(t *testing.T) *surfaceTestEnv {
	t.Helper()

	actSvc := activation.NewActivationService(newMemActivationRepo(), apiStubStores{}, nil, nil, nil)
	registry, err := surface.NewRegistry(
		directagent.Registration(),
		surface.Registration{Descriptor: domain.SurfaceDescriptor{
			SurfaceID:      "test_surface",
			DisplayNameKey: "test.name",
			Protocol:       domain.SurfaceProtocolUCP,
		}},
	)
	require.NoError(t, err)

	surfSvc := surface.NewSurfaceService(registry, newMemSurfaceRepo(), actSvc, apiStubConnectors{}, nil, nil)

	deps := api.Dependencies{
		Account:       account.StubService{},
		Connector:     connector.StubService{},
		Catalog:       catalog.StubService{},
		Task:          task.StubService{},
		Journey:       journey.StubService{},
		Billing:       activeBillingStub{},
		Operator:      operator.StubService{},
		Validation:    validation.StubService{},
		Activation:    actSvc,
		Surfaces:      surfSvc,
		ResolveBearer: twoTenantResolver,
		ResolveHMAC: func(echo.Context, string) (domain.Principal, error) {
			return domain.Principal{MerchantID: "test"}, nil
		},
	}

	e := echo.New()
	require.NoError(t, api.New(deps).SetupRoutes("/v1", e.Group("/v1")))

	return &surfaceTestEnv{echo: e, activation: actSvc, surfaces: surfSvc}
}

// activateStore drives a store all the way to active.
func (env *surfaceTestEnv) activateStore(t *testing.T, m domain.MerchantID, s domain.StoreID) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, env.activation.Initialize(ctx, m, s))
	for _, sig := range []activation.Signal{
		activation.SignalConnectorLinked,
		activation.SignalProfilePublished,
		activation.SignalVerificationPassed,
		activation.SignalActivated,
	} {
		require.NoError(t, env.activation.HandleSignal(ctx, m, s, sig))
	}
}

// call issues an authenticated request. Named separately from the package's
// existing doJSON because it takes a bearer token rather than a header map
// — every route under test is bearer-authenticated.
func call(e *echo.Echo, method, path, token string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// ── Activation routes ────────────────────────────────────────────────────

func TestGetActivation_OK(t *testing.T) {
	env := newSurfaceTestEnv(t)
	env.activateStore(t, tenantA, "store-1")

	rec := call(env.echo, http.MethodGet, "/v1/stores/store-1/activation", tenantAToken, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var got domain.Activation
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, domain.UCPStateActive, got.UCPState)
	assert.NotEmpty(t, got.Steps, "the console renders progress from per-step detail")
}

// TestGetActivation_CrossTenantIs404 is the tenancy assertion in A7:
// merchant_id comes from the bearer claim, so tenant B asking for tenant
// A's store must get not-found — never a 403, which would confirm the store
// exists.
func TestGetActivation_CrossTenantIs404(t *testing.T) {
	env := newSurfaceTestEnv(t)
	env.activateStore(t, tenantA, "store-1")

	rec := call(env.echo, http.MethodGet, "/v1/stores/store-1/activation", tenantBToken, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetActivation_Unauthorized(t *testing.T) {
	env := newSurfaceTestEnv(t)
	rec := call(env.echo, http.MethodGet, "/v1/stores/store-1/activation", "", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ── Registry route ───────────────────────────────────────────────────────

func TestListSurfaceRegistry_ReturnsDescriptors(t *testing.T) {
	env := newSurfaceTestEnv(t)

	rec := call(env.echo, http.MethodGet, "/v1/surfaces", tenantAToken, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var got domain.ListOf[domain.SurfaceDescriptor]
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	require.Len(t, got.Data, 2)

	ids := []string{got.Data[0].SurfaceID, got.Data[1].SurfaceID}
	assert.Contains(t, ids, directagent.SurfaceID)
	assert.Contains(t, ids, "test_surface")
}

// ── Enrollment gating (A7) ───────────────────────────────────────────────

// TestEnrollSurface_RequiresActiveStore asserts the API enforces the one
// cross-lifecycle rule, and does it as a 409: the request is well-formed
// and the merchant is entitled to it — the store just isn't there yet, and
// the remedy lives on a different page.
func TestEnrollSurface_RequiresActiveStore(t *testing.T) {
	env := newSurfaceTestEnv(t)
	ctx := context.Background()
	require.NoError(t, env.activation.Initialize(ctx, tenantA, "store-1"))
	require.NoError(t, env.activation.HandleSignal(ctx, tenantA, "store-1", activation.SignalConnectorLinked))

	rec := call(env.echo, http.MethodPost, "/v1/stores/store-1/surfaces/test_surface/enroll", tenantAToken, nil)
	assert.Equal(t, http.StatusConflict, rec.Code)

	var env2 domain.ErrorEnvelope
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&env2))
	assert.Contains(t, env2.Error.Message, "active")
}

func TestEnrollSurface_OKWhenActive(t *testing.T) {
	env := newSurfaceTestEnv(t)
	env.activateStore(t, tenantA, "store-1")

	rec := call(env.echo, http.MethodPost, "/v1/stores/store-1/surfaces/test_surface/enroll", tenantAToken, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var got domain.SurfaceEnrollment
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, domain.SurfaceEnrolled, got.State)
	assert.Equal(t, "test_surface", got.Descriptor.SurfaceID,
		"the descriptor rides along so the console renders the flow generically")
}

func TestEnrollSurface_UnknownSurfaceIs404(t *testing.T) {
	env := newSurfaceTestEnv(t)
	env.activateStore(t, tenantA, "store-1")

	rec := call(env.echo, http.MethodPost, "/v1/stores/store-1/surfaces/no_such_surface/enroll", tenantAToken, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestSurfaceRoutes_CrossTenantCannotSeeEnrollment asserts enrollment is
// per-tenant: tenant B looking at the same store id sees the surface as
// available, never tenant A's enrollment.
func TestSurfaceRoutes_CrossTenantCannotSeeEnrollment(t *testing.T) {
	env := newSurfaceTestEnv(t)
	env.activateStore(t, tenantA, "store-1")

	rec := call(env.echo, http.MethodPost, "/v1/stores/store-1/surfaces/test_surface/enroll", tenantAToken, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = call(env.echo, http.MethodGet, "/v1/stores/store-1/surfaces", tenantBToken, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var got domain.ListOf[domain.SurfaceEnrollment]
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	for _, e := range got.Data {
		assert.Equal(t, domain.SurfaceAvailable, e.State,
			"surface %s leaked another tenant's enrollment", e.SurfaceID)
	}
}

// ── Config and prerequisites ─────────────────────────────────────────────

func TestSetSurfaceConfig_OK(t *testing.T) {
	env := newSurfaceTestEnv(t)
	env.activateStore(t, tenantA, "store-1")

	rec := call(env.echo, http.MethodPut, "/v1/stores/store-1/surfaces/test_surface/config", tenantAToken,
		map[string]any{"config": map[string]string{"key": "value"}})
	require.Equal(t, http.StatusOK, rec.Code)

	var got domain.SurfaceEnrollment
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "value", got.Config["key"])
}

func TestEvaluatePrerequisites_OK(t *testing.T) {
	env := newSurfaceTestEnv(t)
	env.activateStore(t, tenantA, "store-1")

	rec := call(env.echo, http.MethodPost, "/v1/stores/store-1/surfaces/test_surface/enroll", tenantAToken, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	// Re-evaluating is safe to repeat and never restarts enrollment (R10).
	for range 3 {
		rec = call(env.echo, http.MethodPost,
			"/v1/stores/store-1/surfaces/test_surface/prerequisites/evaluate", tenantAToken, nil)
		require.Equal(t, http.StatusOK, rec.Code)

		var got domain.SurfaceEnrollment
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
		assert.Equal(t, domain.SurfaceEnrolled, got.State)
	}
}

// ── Withdrawal (A7) ──────────────────────────────────────────────────────

// TestWithdrawSurface_LeavesActivationIntact is the withdrawal semantics
// R16 spells out: DELETE returns the surface to available and must not
// affect the store's activation state or any other surface.
func TestWithdrawSurface_LeavesActivationIntact(t *testing.T) {
	env := newSurfaceTestEnv(t)
	env.activateStore(t, tenantA, "store-1")

	// Enroll in both surfaces.
	require.NoError(t, env.surfaces.OnStoreActivated(context.Background(), tenantA, "store-1"))
	rec := call(env.echo, http.MethodPost, "/v1/stores/store-1/surfaces/test_surface/enroll", tenantAToken, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = call(env.echo, http.MethodDelete, "/v1/stores/store-1/surfaces/test_surface", tenantAToken, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// The withdrawn surface is back to available.
	rec = call(env.echo, http.MethodGet, "/v1/stores/store-1/surfaces", tenantAToken, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var list domain.ListOf[domain.SurfaceEnrollment]
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&list))

	byID := map[string]domain.SurfaceEnrollment{}
	for _, e := range list.Data {
		byID[e.SurfaceID] = e
	}
	assert.Equal(t, domain.SurfaceAvailable, byID["test_surface"].State)
	assert.Equal(t, domain.SurfaceEnrolled, byID[directagent.SurfaceID].State,
		"withdrawing one surface must not touch another")

	// And activation is exactly where it was.
	rec = call(env.echo, http.MethodGet, "/v1/stores/store-1/activation", tenantAToken, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var act domain.Activation
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&act))
	assert.Equal(t, domain.UCPStateActive, act.UCPState,
		"withdrawing from a surface never de-activates a store")
}

func TestWithdrawSurface_IsIdempotent(t *testing.T) {
	env := newSurfaceTestEnv(t)
	env.activateStore(t, tenantA, "store-1")

	for range 2 {
		rec := call(env.echo, http.MethodDelete, "/v1/stores/store-1/surfaces/test_surface", tenantAToken, nil)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	}
}

// TestSurfaceRoutes_RequireAuth sweeps every route for the bearer
// requirement in one place, so a newly added surface route cannot be
// shipped unauthenticated.
func TestSurfaceRoutes_RequireAuth(t *testing.T) {
	env := newSurfaceTestEnv(t)

	routes := []struct{ method, path string }{
		{http.MethodGet, "/v1/surfaces"},
		{http.MethodGet, "/v1/stores/store-1/surfaces"},
		{http.MethodPost, "/v1/stores/store-1/surfaces/test_surface/enroll"},
		{http.MethodPost, "/v1/stores/store-1/surfaces/test_surface/prerequisites/evaluate"},
		{http.MethodPost, "/v1/stores/store-1/surfaces/test_surface/submit"},
		{http.MethodPut, "/v1/stores/store-1/surfaces/test_surface/config"},
		{http.MethodDelete, "/v1/stores/store-1/surfaces/test_surface"},
		{http.MethodGet, "/v1/stores/store-1/activation"},
		{http.MethodPost, "/v1/stores/store-1/activation/verify"},
	}

	for _, r := range routes {
		rec := call(env.echo, r.method, r.path, "", nil)
		assert.Equalf(t, http.StatusUnauthorized, rec.Code, "%s %s", r.method, r.path)
	}
}
