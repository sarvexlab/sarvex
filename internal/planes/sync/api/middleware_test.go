// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/sync/domain"
)

// captureHandler records the principal injected by middleware into captured.
func captureHandler(captured *domain.Principal) echo.HandlerFunc {
	return func(c echo.Context) error {
		p := principalFrom(c)
		*captured = p
		return c.JSON(http.StatusOK, p)
	}
}

func stubHMACResolver(c echo.Context, signature string) (domain.Principal, error) {
	if signature == "" {
		return domain.Principal{}, errors.New("empty signature")
	}
	return domain.Principal{MerchantID: "test-merchant"}, nil
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
