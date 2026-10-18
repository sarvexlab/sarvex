// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package ucpsig

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
	"time"
)

// generateKey is shared by the round-trip tests.
func generateKey(t *testing.T, kid string) (*ecdsa.PrivateKey, JWK) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	jwk, err := JWKFromPublicKey(kid, &priv.PublicKey)
	if err != nil {
		t.Fatalf("jwk: %v", err)
	}
	return priv, jwk
}

func TestRequestSignAndVerify(t *testing.T) {
	priv, jwk := generateKey(t, "platform-2026")
	signer, err := NewSigner(priv, jwk)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	verifier := NewVerifier(StaticProfileResolver{
		Profile: &Profile{SigningKeys: []JWK{jwk}},
	}, false)

	body := []byte(`{"checkout":{"line_items":[{"id":"prod_123","quantity":2}]}}`)
	req := httptest.NewRequest(http.MethodPost,
		"https://merchant.example.com/checkout-sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "550e8400-e29b-41d4-a716-446655440000")
	req.Header.Set("UCP-Agent", `profile="https://platform.example/.well-known/ucp"`)

	if err := signer.SignRequest(req); err != nil {
		t.Fatalf("sign: %v", err)
	}

	for _, h := range []string{"Signature-Input", "Signature", "Content-Digest"} {
		if req.Header.Get(h) == "" {
			t.Errorf("missing %s after signing", h)
		}
	}
	if !strings.Contains(req.Header.Get("Signature-Input"), `keyid="platform-2026"`) {
		t.Errorf("Signature-Input should include keyid: %s", req.Header.Get("Signature-Input"))
	}

	params, profURL, err := verifier.VerifyRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if params.KeyID != "platform-2026" {
		t.Errorf("unexpected keyid: %s", params.KeyID)
	}
	if profURL != "https://platform.example/.well-known/ucp" {
		t.Errorf("unexpected profile url: %s", profURL)
	}

	got, _ := io.ReadAll(req.Body)
	if !bytes.Equal(got, body) {
		t.Errorf("body not restored after verification")
	}
}

func TestRequestTamperDetection(t *testing.T) {
	priv, jwk := generateKey(t, "platform-2026")
	signer, _ := NewSigner(priv, jwk)
	verifier := NewVerifier(StaticProfileResolver{
		Profile: &Profile{SigningKeys: []JWK{jwk}},
	}, false)

	body := []byte(`{"amount":100}`)
	req := httptest.NewRequest(http.MethodPost, "https://m.example/x", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "abc-123")
	req.Header.Set("UCP-Agent", `profile="https://p.example/.well-known/ucp"`)
	if err := signer.SignRequest(req); err != nil {
		t.Fatalf("sign: %v", err)
	}

	// Tamper with body.
	tampered := []byte(`{"amount":999}`)
	req.Body = io.NopCloser(bytes.NewReader(tampered))

	_, _, err := verifier.VerifyRequest(context.Background(), req)
	if err == nil {
		t.Fatalf("expected verification failure")
	}
	if e, ok := AsError(err); !ok || e.Code != "digest_mismatch" {
		t.Errorf("expected digest_mismatch, got %v", err)
	}
}

func TestRequestWrongKey(t *testing.T) {
	priv, jwk := generateKey(t, "platform-2026")
	signer, _ := NewSigner(priv, jwk)

	// Verifier has a different key under the same kid.
	_, wrongJWK := generateKey(t, "platform-2026")
	verifier := NewVerifier(StaticProfileResolver{
		Profile: &Profile{SigningKeys: []JWK{wrongJWK}},
	}, false)

	req := httptest.NewRequest(http.MethodGet, "https://m.example/x", nil)
	req.Header.Set("UCP-Agent", `profile="https://p.example/.well-known/ucp"`)
	if err := signer.SignRequest(req); err != nil {
		t.Fatalf("sign: %v", err)
	}

	_, _, err := verifier.VerifyRequest(context.Background(), req)
	if e, ok := AsError(err); !ok || e.Code != "signature_invalid" {
		t.Fatalf("expected signature_invalid, got %v", err)
	}
}

func TestMissingIdempotencyKey(t *testing.T) {
	priv, jwk := generateKey(t, "k1")
	signer, _ := NewSigner(priv, jwk)

	req := httptest.NewRequest(http.MethodPost, "https://m.example/x", strings.NewReader(`{"x":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("UCP-Agent", `profile="https://p.example/.well-known/ucp"`)

	err := signer.SignRequest(req)
	if err == nil || !strings.Contains(err.Error(), "Idempotency-Key") {
		t.Fatalf("expected Idempotency-Key error, got %v", err)
	}
}

// A counterparty's profile publishes its keys under its own generation's
// name: "signing_keys" (2026-04-08) or "keys" (2026-08-25). The resolver
// finds them under either. (It used to read only "signing_keys", so every
// request signed by a 2026-08-25 agent failed with "profile has no
// signing_keys".)
func TestHTTPProfileResolver_ReadsEitherGenerationsKeys(t *testing.T) {
	_, jwk := generateKey(t, "k1")
	for _, member := range []string{"signing_keys", "keys"} {
		t.Run(member, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"ucp": map[string]any{"version": "2026-08-25"}, member: []JWK{jwk}})
			}))
			defer srv.Close()

			r := NewHTTPProfileResolver(srv.Client(), time.Minute)
			r.AllowInsecureHTTP = true
			p, err := r.Resolve(context.Background(), srv.URL+"/.well-known/ucp")
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if p.FindKey("k1") == nil {
				t.Errorf("key k1 not found under %q", member)
			}
		})
	}
}

// JWK alg is case-sensitive (RFC 7517 §4.4); the registered name is "ES256".
func TestECPublicKeyToJWK_UsesRegisteredAlgName(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if got := ECPublicKeyToJWK(&priv.PublicKey, "k1").Alg; got != "ES256" {
		t.Errorf("alg = %q, want ES256", got)
	}
}
