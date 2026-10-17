// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package negotiation

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// stubStoreProvider is a minimal ucp.StoreProvider for middleware tests —
// the store-config middleware only cares that a non-nil provider reaches
// SetStoreProfile, not what it returns.
type stubStoreProvider struct{ storeID string }

func (s *stubStoreProvider) StoreID() string                            { return s.storeID }
func (s *stubStoreProvider) Version() string                            { return "2026-04-08" }
func (s *stubStoreProvider) Services() map[string][]wire.Service        { return nil }
func (s *stubStoreProvider) Capabilities() map[string][]wire.Capability { return nil }
func (s *stubStoreProvider) PaymentHandlers() map[string][]wire.PaymentHandler {
	return nil
}
func (s *stubStoreProvider) SigningKeys() []wire.JWK { return nil }
func (s *stubStoreProvider) StorefrontURL() string   { return "" }
func (s *stubStoreProvider) Configuration() ucp.Configuration {
	return ucp.Configuration{StoreID: s.storeID}
}

func withStoreConfigTestContext(storeID string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("storeID")
	c.SetParamValues(storeID)
	return c, rec
}

func TestWithStoreConfigProvider_Success(t *testing.T) {
	provider := func(ctx context.Context, storeID string) (ucp.StoreProvider, error) {
		return &stubStoreProvider{storeID: storeID}, nil
	}
	c, rec := withStoreConfigTestContext("store-1")

	var nextCalled bool
	mw := WithStoreConfigProvider(provider)
	err := mw(func(c echo.Context) error {
		nextCalled = true
		inst, ok := GetStoreProfile(c)
		require.True(t, ok, "expected store profile to be set in context")
		require.Equal(t, "store-1", inst.StoreID())
		return c.NoContent(http.StatusOK)
	})(c)

	require.NoError(t, err)
	require.True(t, nextCalled)
	require.Equal(t, http.StatusOK, rec.Code)
}

// A missing profile (ucp.ErrStoreProfileNotFound) must surface as 404, not
// the 500 a generic provider error gets — see profile_source.go's doc
// comment on why a cache miss is a well-defined state, not a failure.
func TestWithStoreConfigProvider_NotFoundIs404(t *testing.T) {
	provider := func(ctx context.Context, storeID string) (ucp.StoreProvider, error) {
		return nil, ucp.ErrStoreProfileNotFound
	}
	c, _ := withStoreConfigTestContext("missing-store")

	var nextCalled bool
	mw := WithStoreConfigProvider(provider)
	err := mw(func(c echo.Context) error {
		nextCalled = true
		return nil
	})(c)

	require.False(t, nextCalled, "next must not run when the profile is missing")
	var httpErr *echo.HTTPError
	require.True(t, errors.As(err, &httpErr))
	require.Equal(t, http.StatusNotFound, httpErr.Code)
}

func TestWithStoreConfigProvider_OtherErrorIs500(t *testing.T) {
	provider := func(ctx context.Context, storeID string) (ucp.StoreProvider, error) {
		return nil, errors.New("redis is down")
	}
	c, _ := withStoreConfigTestContext("store-1")

	var nextCalled bool
	mw := WithStoreConfigProvider(provider)
	err := mw(func(c echo.Context) error {
		nextCalled = true
		return nil
	})(c)

	require.False(t, nextCalled)
	var httpErr *echo.HTTPError
	require.True(t, errors.As(err, &httpErr))
	require.Equal(t, http.StatusInternalServerError, httpErr.Code)
}
