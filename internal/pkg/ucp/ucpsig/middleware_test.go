// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package ucpsig_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/ucpsig"
)

func makeKeyPair(t *testing.T, kid string) (*ecdsa.PrivateKey, ucpsig.JWK) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	jwk, err := ucpsig.JWKFromPublicKey(kid, &priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return priv, jwk
}

// TestEchoVerifyAndSign exercises the full echo middleware chain:
// platform-side signing client → verify middleware → sign-response middleware
// → handler that does c.JSON → platform-side response verification.
func TestEchoVerifyAndSign(t *testing.T) {
	// Platform key (request signer).
	pPriv, pJWK := makeKeyPair(t, "plat-2026")
	pSigner, _ := ucpsig.NewSigner(pPriv, pJWK)

	// Business key (response signer).
	bPriv, bJWK := makeKeyPair(t, "biz-2026")
	bSigner, _ := ucpsig.NewSigner(bPriv, bJWK)

	platformProfileURL := "https://platform.example/.well-known/ucp"
	businessProfileURL := "https://business.example/.well-known/ucp"

	// Static resolver knows both.
	resolver := &fakeResolverByURL{m: map[string]*ucpsig.Profile{
		platformProfileURL: {SigningKeys: []ucpsig.JWK{pJWK}},
		businessProfileURL: {SigningKeys: []ucpsig.JWK{bJWK}},
	}}
	verifier := ucpsig.NewVerifier(resolver, false)

	// Build the Echo app.
	e := echo.New()
	e.HideBanner = true
	cfg := ucpsig.SignatureConfig{
		Verifier: verifier,
		SignerProvider: func(c echo.Context) (*ucpsig.Signer, error) {
			return bSigner, nil
		},
	}
	g := e.Group("/ucp",
		ucpsig.WithRequestSignatureVerifier(cfg),
		ucpsig.WithResponseSignatureGenerator(cfg),
	)
	g.POST("/checkout-sessions", func(c echo.Context) error {
		vs, ok := ucpsig.VerifiedFrom(c)
		if !ok {
			t.Errorf("VerifiedFrom returned !ok")
		}
		if vs.Params.KeyID != "plat-2026" {
			t.Errorf("unexpected platform keyid: %s", vs.Params.KeyID)
		}
		var payload struct {
			Checkout struct {
				LineItems []map[string]any `json:"line_items"`
			} `json:"checkout"`
		}
		if err := c.Bind(&payload); err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, map[string]any{
			"checkout": map[string]any{
				"id":     "chk_123",
				"status": "ready_for_complete",
				"echo":   payload.Checkout.LineItems,
			},
		})
	})

	srv := httptest.NewServer(e)
	defer srv.Close()

	// Build platform-side client that signs.
	client := &http.Client{Transport: &ucpsig.SigningTransport{
		Signer:     pSigner,
		ProfileURL: platformProfileURL,
		Base:       http.DefaultTransport,
	}}

	body := []byte(`{"checkout":{"line_items":[{"id":"prod_123","quantity":2}]}}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/ucp/checkout-sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "abc-def-ghi-22-chars-XYZ")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d: %s", resp.StatusCode, respBody)
	}

	// Verify response signature.
	for _, h := range []string{"Content-Digest", "Signature-Input", "Signature"} {
		if resp.Header.Get(h) == "" {
			t.Errorf("missing %s on response", h)
		}
	}
	if !strings.Contains(resp.Header.Get("Signature-Input"), `keyid="biz-2026"`) {
		t.Errorf("Signature-Input missing biz keyid")
	}
	if _, err := verifier.VerifyResponse(context.Background(), resp, respBody, businessProfileURL); err != nil {
		t.Fatalf("verify response: %v", err)
	}

	// Sanity-check payload made it through.
	var parsed struct {
		Checkout struct {
			Echo []map[string]any `json:"echo"`
		} `json:"checkout"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed.Checkout.Echo) != 1 || parsed.Checkout.Echo[0]["id"] != "prod_123" {
		t.Errorf("handler didn't see the request body: %v", parsed)
	}
}

func TestEchoVerifyRejectsUnsigned(t *testing.T) {
	_, bJWK := makeKeyPair(t, "biz-2026")
	resolver := &fakeResolverByURL{m: map[string]*ucpsig.Profile{
		"https://business.example/.well-known/ucp": {SigningKeys: []ucpsig.JWK{bJWK}},
	}}
	verifier := ucpsig.NewVerifier(resolver, false)

	e := echo.New()
	e.HideBanner = true
	cfg := ucpsig.SignatureConfig{
		Verifier: verifier,
	}
	e.POST("/ucp/x", func(c echo.Context) error { return c.NoContent(http.StatusOK) }, ucpsig.WithRequestSignatureVerifier(cfg))

	srv := httptest.NewServer(e)
	defer srv.Close()

	// No signature headers at all.
	resp, err := http.Post(srv.URL+"/ucp/x", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "signature_missing") {
		t.Errorf("expected signature_missing in body, got: %s", body)
	}
}

func TestEchoSignResponsePreservesStatus(t *testing.T) {
	_, jwk := makeKeyPair(t, "biz-2026")
	priv2, _ := makeKeyPairRaw(t)
	signer, _ := ucpsig.NewSigner(priv2, jwk)

	e := echo.New()
	e.HideBanner = true
	cfg := ucpsig.SignatureConfig{
		SignerProvider: func(c echo.Context) (*ucpsig.Signer, error) {
			return signer, nil
		},
	}
	e.GET("/x", func(c echo.Context) error {
		return c.JSON(http.StatusAccepted, map[string]string{"hi": "there"})
	}, ucpsig.WithResponseSignatureGenerator(cfg))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rec.Code)
	}
	if rec.Header().Get("Signature") == "" {
		t.Error("Signature header not set")
	}
	if !strings.Contains(rec.Body.String(), `"hi":"there"`) {
		t.Errorf("body mangled: %s", rec.Body.String())
	}
}

// fakeResolverByURL is a multi-URL resolver for tests.
type fakeResolverByURL struct {
	m map[string]*ucpsig.Profile
}

func (f *fakeResolverByURL) Resolve(_ context.Context, url string) (*ucpsig.Profile, error) {
	if p, ok := f.m[url]; ok {
		return p, nil
	}
	return nil, ucpsig.ErrProfileUnreachable
}

func makeKeyPairRaw(t *testing.T) (*ecdsa.PrivateKey, ucpsig.JWK) {
	return makeKeyPair(t, "k")
}
