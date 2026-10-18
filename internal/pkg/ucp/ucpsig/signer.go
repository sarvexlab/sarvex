// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package ucpsig

import (
	"bytes"
	"crypto/ecdsa"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// SignatureLabel is the dictionary label used in Signature-Input and Signature
// headers. UCP examples use "sig1" consistently and that's what we emit.
const SignatureLabel = "sig1"

// Signer signs outbound HTTP messages with a UCP key. A single Signer should
// be created at startup and reused.
//
// Concurrency: Signer is safe for concurrent use.
type Signer struct {
	priv *ecdsa.PrivateKey
	jwk  JWK // includes Kid and curve metadata
}

// NewSigner constructs a Signer from a private key and its public-JWK
// description. The JWK provides Kid and Crv; the D field is not required and
// will be ignored if present.
func NewSigner(priv *ecdsa.PrivateKey, jwk JWK) (*Signer, error) {
	if priv == nil {
		return nil, fmt.Errorf("ucpsig: nil private key")
	}
	if jwk.Kid == "" {
		return nil, fmt.Errorf("ucpsig: JWK must have a kid")
	}
	if _, err := jwk.Curve(); err != nil {
		return nil, err
	}
	return &Signer{priv: priv, jwk: jwk}, nil
}

// KeyID returns the key id this signer signs with. Useful for logging and
// for callers that build their own profile documents.
func (s *Signer) KeyID() string { return s.jwk.Kid }

// SignRequest signs an outbound HTTP request in place. It computes the
// Content-Digest (if body present), populates Signature-Input and Signature,
// and rewinds the body so the request remains sendable.
//
// Applicability:
//   - Platform side: sign every request to a business endpoint
//     (POST /checkout-sessions, GET /checkout-sessions/{id}, etc).
//   - Business side: sign webhook deliveries to platforms.
//
// Required headers the caller is responsible for setting BEFORE calling
// SignRequest, since they are part of the signature base:
//
//   - UCP-Agent (when present): set this to your own profile URL using
//     the form `profile="https://..."`. SignRequest reads it; it does not
//     synthesise it.
//   - Idempotency-Key: required for state-changing methods (POST/PUT/PATCH/
//     DELETE) per the spec. The signer will refuse to sign such methods
//     without one.
//   - Content-Type: required if a body is present.
func (s *Signer) SignRequest(req *http.Request) error {
	body, err := drainBody(req)
	if err != nil {
		return err
	}
	hasBody := len(body) > 0

	// Compute and set Content-Digest if there's a body.
	if hasBody {
		req.Header.Set("Content-Digest", DigestHeaderValue(body))
		if req.Header.Get("Content-Type") == "" {
			return fmt.Errorf("ucpsig: Content-Type required when body present")
		}
	}

	// Idempotency-Key required for state-changing methods.
	method := strings.ToUpper(req.Method)
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		if req.Header.Get("Idempotency-Key") == "" {
			return fmt.Errorf("ucpsig: Idempotency-Key required for %s requests", method)
		}
	}

	components := []string{"@method", "@authority", "@path"}
	if req.URL != nil && req.URL.RawQuery != "" {
		components = append(components, "@query")
	}
	if req.Header.Get("UCP-Agent") != "" {
		components = append(components, "ucp-agent")
	}
	if req.Header.Get("Idempotency-Key") != "" {
		components = append(components, "idempotency-key")
	}
	if hasBody {
		components = append(components, "content-digest", "content-type")
	}

	params := SignatureParams{
		Label:      SignatureLabel,
		Components: components,
		KeyID:      s.jwk.Kid,
	}

	base, err := BuildSignatureBase(DirectionRequest, req, 0, nil, params)
	if err != nil {
		return err
	}
	sig, err := SignBase(base, s.priv, s.jwk)
	if err != nil {
		return err
	}

	req.Header.Set("Signature-Input", SignatureLabel+"="+serializeSignatureParams(params))
	req.Header.Set("Signature", EncodeSignatureHeader(SignatureLabel, sig))
	return nil
}

// SignResponseHeaders computes the Signature-Input and Signature header values
// for a response with the given status code, headers, and body. Callers should
// set the Content-Digest header on the response themselves; the verify
// middleware's buffering response writer does this transparently.
//
// Applicability:
//   - Business side: sign responses to platform requests (RECOMMENDED for
//     payment authorization and checkout completion; OPTIONAL elsewhere).
//   - Platform side: sign responses to business-initiated requests if any.
func (s *Signer) SignResponseHeaders(status int, headers http.Header, body []byte, created int64) (sigInput, sig string, err error) {
	hasBody := len(body) > 0
	if hasBody {
		if headers.Get("Content-Digest") == "" {
			headers.Set("Content-Digest", DigestHeaderValue(body))
		}
		if headers.Get("Content-Type") == "" {
			return "", "", fmt.Errorf("ucpsig: Content-Type required when body present")
		}
	}

	components := []string{"@status"}
	if hasBody {
		components = append(components, "content-digest", "content-type")
	}

	params := SignatureParams{
		Label:      SignatureLabel,
		Components: components,
		KeyID:      s.jwk.Kid,
		Created:    created,
	}

	base, err := BuildSignatureBase(DirectionResponse, nil, status, headers, params)
	if err != nil {
		return "", "", err
	}
	raw, err := SignBase(base, s.priv, s.jwk)
	if err != nil {
		return "", "", err
	}
	return SignatureLabel + "=" + serializeSignatureParams(params),
		EncodeSignatureHeader(SignatureLabel, raw),
		nil
}

// drainBody reads req.Body into memory and resets it. Returns nil if there
// was no body.
func drainBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("ucpsig: read request body: %w", err)
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	// Preserve net/http's ability to retry by setting GetBody.
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	if req.ContentLength <= 0 {
		req.ContentLength = int64(len(body))
	}
	return body, nil
}
