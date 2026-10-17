// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/email"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/api"
)

const contactTestToken = "contact-shared-secret"

func newContactTestHandler(t *testing.T) *echo.Echo {
	t.Helper()
	client, err := email.NewClient(email.Config{}) // ModeMock: logs, never sends over the network.
	if err != nil {
		t.Fatalf("email.NewClient: %v", err)
	}
	sender := email.NewSender(client, "hello@sarvex.example.com", "Sarvex")

	deps := api.Dependencies{
		ContactSender:         sender,
		ContactNotifyEmail:    "hello@sarvex.example.com",
		ContactApiToken:       contactTestToken,
		ContactAllowedOrigins: []string{"https://sarvex.example.com", "https://www.sarvex.example.com"},
	}
	h := api.New(deps)
	e := echo.New()
	g := e.Group("/v1")
	_ = h.SetupRoutes("/v1", g)
	return e
}

func postContact(e *echo.Echo, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/contact", bytes.NewBufferString(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if token != "" {
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestPostContact_MissingToken_Unauthorized(t *testing.T) {
	e := newContactTestHandler(t)
	rec := postContact(e, "", `{"name":"A","email":"a@b.com","message":"hi"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestPostContact_WrongToken_Unauthorized(t *testing.T) {
	e := newContactTestHandler(t)
	rec := postContact(e, "wrong-token", `{"name":"A","email":"a@b.com","message":"hi"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestPostContact_NoTokenConfigured_NotFound(t *testing.T) {
	client, err := email.NewClient(email.Config{})
	if err != nil {
		t.Fatalf("email.NewClient: %v", err)
	}
	deps := api.Dependencies{
		ContactSender:         email.NewSender(client, "hello@sarvex.example.com", "Sarvex"),
		ContactNotifyEmail:    "hello@sarvex.example.com",
		ContactAllowedOrigins: []string{"https://sarvex.example.com", "https://www.sarvex.example.com"},
		// ContactApiToken intentionally empty.
	}
	h := api.New(deps)
	e := echo.New()
	g := e.Group("/v1")
	_ = h.SetupRoutes("/v1", g)

	rec := postContact(e, "anything", `{"name":"A","email":"a@b.com"}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPostContact_MissingFields_ValidationError(t *testing.T) {
	e := newContactTestHandler(t)
	rec := postContact(e, contactTestToken, `{"name":"","email":"not-an-email"}`)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestPostContact_Valid_Accepted(t *testing.T) {
	e := newContactTestHandler(t)
	rec := postContact(e, contactTestToken, `{"name":"Jane Doe","email":"jane@example.com","message":"Hello there","source":"sarvex.example.com/contact"}`)
	assert.Equal(t, http.StatusAccepted, rec.Code)
}

func TestPostContact_CORS_AllowsWebsiteOrigin(t *testing.T) {
	e := newContactTestHandler(t)
	req := httptest.NewRequest(http.MethodOptions, "/v1/contact", nil)
	req.Header.Set(echo.HeaderOrigin, "https://sarvex.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, "https://sarvex.example.com", rec.Header().Get(echo.HeaderAccessControlAllowOrigin))
}

func TestPostContact_CORS_RejectsOtherOrigin(t *testing.T) {
	e := newContactTestHandler(t)
	req := httptest.NewRequest(http.MethodOptions, "/v1/contact", nil)
	req.Header.Set(echo.HeaderOrigin, "https://evil.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Empty(t, rec.Header().Get(echo.HeaderAccessControlAllowOrigin))
}
