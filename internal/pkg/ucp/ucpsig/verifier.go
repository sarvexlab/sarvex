// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package ucpsig

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

// Verifier verifies inbound HTTP messages against UCP profiles.
//
// Concurrency: Verifier is safe for concurrent use; the underlying resolver
// is responsible for its own caching/locking.
type Verifier struct {
	resolver          ProfileResolver
	AllowInsecureHTTP bool // Allow http:// profile URLs (local testing only)

	// MaxBodyBytes caps how much of a request body will be buffered for digest
	// verification. Requests larger than this are rejected. Set to 0 for
	// "unlimited" (not recommended).
	MaxBodyBytes int64
}

// NewVerifier constructs a verifier. The resolver supplies signing profiles
// for the UCP-Agent values encountered in incoming requests. allowInsecureHTTP,
// when true, allows profile URLs to use http:// (for local testing only).
func NewVerifier(resolver ProfileResolver, allowInsecureHTTP bool) *Verifier {
	return &Verifier{resolver: resolver, AllowInsecureHTTP: allowInsecureHTTP, MaxBodyBytes: 5 << 20} // 5 MiB default
}

// VerifyRequest verifies a signed HTTP request. The request body is buffered
// and req.Body is replaced with a reader that yields the same bytes, so
// downstream handlers can read it normally.
//
// Applicability:
//   - Business side: every request from a platform.
//   - Platform side: every webhook delivery from a business.
//
// On success, returns the SignatureParams describing what was verified (in
// particular Params.KeyID and the signer's profile URL) which the caller can
// log for audit.
func (v *Verifier) VerifyRequest(ctx context.Context, req *http.Request) (SignatureParams, string, error) {
	sigInput := req.Header.Get("Signature-Input")
	sigHdr := req.Header.Get("Signature")
	if sigInput == "" || sigHdr == "" {
		return SignatureParams{}, "", ErrSignatureMissing
	}
	params, err := ParseSignatureInput(sigInput, SignatureLabel)
	if err != nil {
		return SignatureParams{}, "", ErrSignatureInvalid.withCause(err)
	}
	if params.KeyID == "" {
		return params, "", ErrSignatureInvalid.withCause(fmt.Errorf("missing keyid"))
	}

	// Resolve signer profile from UCP-Agent header.
	ucpAgent := req.Header.Get("UCP-Agent")
	if ucpAgent == "" {
		return params, "", ErrSignatureInvalid.withCause(fmt.Errorf("UCP-Agent header required to identify signer"))
	}
	profileURL, err := ParseUCPAgent(ucpAgent, v.AllowInsecureHTTP)
	if err != nil {
		return params, "", err
	}
	prof, err := v.resolver.Resolve(ctx, profileURL)
	if err != nil {
		return params, profileURL, err
	}
	jwk := prof.FindKey(params.KeyID)
	if jwk == nil {
		return params, profileURL, ErrKeyNotFound
	}

	// Buffer body and verify digest if signed.
	body, err := v.bufferBody(req)
	if err != nil {
		return params, profileURL, err
	}
	if containsComponent(params.Components, "content-digest") {
		cd := req.Header.Get("Content-Digest")
		if cd == "" {
			return params, profileURL, ErrDigestMismatch.withCause(fmt.Errorf("Content-Digest header missing"))
		}
		if err := VerifyDigest(cd, body); err != nil {
			return params, profileURL, err
		}
	}

	// Reconstruct base and verify.
	base, err := BuildSignatureBase(DirectionRequest, req, 0, nil, params)
	if err != nil {
		return params, profileURL, ErrSignatureInvalid.withCause(err)
	}
	rawSig, err := ParseSignature(sigHdr, SignatureLabel)
	if err != nil {
		return params, profileURL, ErrSignatureInvalid.withCause(err)
	}
	if err := VerifyBase(base, rawSig, *jwk); err != nil {
		return params, profileURL, err
	}
	return params, profileURL, nil
}

// VerifyResponse verifies a signed HTTP response. The body must be passed
// explicitly because http.Response bodies are streams and the caller usually
// wants to use them afterwards. The signerProfileURL is the profile of the
// party that produced the response (typically the business, when called from
// a platform client).
//
// Applicability:
//   - Platform side: verify responses to outbound requests when business
//     responses are signed (RECOMMENDED for payment authorization /
//     checkout completion).
func (v *Verifier) VerifyResponse(ctx context.Context, resp *http.Response, body []byte, signerProfileURL string) (SignatureParams, error) {
	sigInput := resp.Header.Get("Signature-Input")
	sigHdr := resp.Header.Get("Signature")
	if sigInput == "" || sigHdr == "" {
		return SignatureParams{}, ErrSignatureMissing
	}
	params, err := ParseSignatureInput(sigInput, SignatureLabel)
	if err != nil {
		return SignatureParams{}, ErrSignatureInvalid.withCause(err)
	}
	if params.KeyID == "" {
		return params, ErrSignatureInvalid.withCause(fmt.Errorf("missing keyid"))
	}
	prof, err := v.resolver.Resolve(ctx, signerProfileURL)
	if err != nil {
		return params, err
	}
	jwk := prof.FindKey(params.KeyID)
	if jwk == nil {
		return params, ErrKeyNotFound
	}
	if containsComponent(params.Components, "content-digest") {
		cd := resp.Header.Get("Content-Digest")
		if cd == "" {
			return params, ErrDigestMismatch.withCause(fmt.Errorf("Content-Digest header missing"))
		}
		if err := VerifyDigest(cd, body); err != nil {
			return params, err
		}
	}
	base, err := BuildSignatureBase(DirectionResponse, nil, resp.StatusCode, resp.Header, params)
	if err != nil {
		return params, ErrSignatureInvalid.withCause(err)
	}
	rawSig, err := ParseSignature(sigHdr, SignatureLabel)
	if err != nil {
		return params, ErrSignatureInvalid.withCause(err)
	}
	if err := VerifyBase(base, rawSig, *jwk); err != nil {
		return params, err
	}
	return params, nil
}

func (v *Verifier) bufferBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	var reader io.Reader = req.Body
	if v.MaxBodyBytes > 0 {
		reader = io.LimitReader(req.Body, v.MaxBodyBytes+1)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, ErrSignatureInvalid.withCause(fmt.Errorf("read body: %w", err))
	}
	if v.MaxBodyBytes > 0 && int64(len(body)) > v.MaxBodyBytes {
		return nil, ErrSignatureInvalid.withCause(fmt.Errorf("body exceeds %d bytes", v.MaxBodyBytes))
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func containsComponent(components []string, name string) bool {
	for _, c := range components {
		if c == name {
			return true
		}
	}
	return false
}
