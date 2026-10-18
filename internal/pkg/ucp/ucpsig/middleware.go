// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package ucpsig

import (
	"bytes"
	"context"
	"maps"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

type SignerProvider func(c echo.Context) (*Signer, error)

// SignatureConfig configures the signature validation middleware.
type SignatureConfig struct {
	Disabled       bool // skip validation (useful for testing non-UCP requests)
	Verifier       *Verifier
	SignerProvider SignerProvider
}

// WithSignatureValidation returns an Echo middleware that validates HTTP Message Signatures (RFC 9421)
// on inbound requests from platforms.
//
// Prerequisites:
//   - WithNegotiation middleware must run before this middleware to fetch and store the platform profile
//     (which contains the signing_keys) in the Echo context.
//
// Error responses use the standard NegotiationErrorResponse format with appropriate status codes:
//   - 401: signature_missing, signature_invalid, key_not_found
//   - 400: digest_mismatch, algorithm_unsupported
// func WithSignatureValidation(cfg SignatureConfig) echo.MiddlewareFunc {
// 	return func(next echo.HandlerFunc) echo.HandlerFunc {
// 		return func(c echo.Context) error {
// 			// Skip if disabled (for testing or non-UCP requests)
// 			if cfg.Disabled {
// 				return next(c)
// 			}

// 			// Get the platform profile (stored by WithNegotiation)
// 			platformProfile, ok := GetPlatformProfile(c)
// 			if !ok {
// 				// No platform profile = no UCP request or negotiation didn't run
// 				// Pass through without signature validation
// 				return next(c)
// 			}

// 			// Read and buffer the request body so we can verify the digest
// 			bodyBytes, err := io.ReadAll(c.Request().Body)
// 			if err != nil {
// 				return c.JSON(http.StatusBadRequest, ucp.NegotiationErrorResponse{
// 					Code:    "digest_mismatch",
// 					Content: "failed to read request body",
// 				})
// 			}
// 			c.Request().Body = io.NopCloser(bytes.NewReader(bodyBytes))

// 			// Extract and validate signature headers
// 			signatureInput := c.Request().Header.Get("Signature-Input")
// 			if signatureInput == "" {
// 				return c.JSON(http.StatusUnauthorized, ucp.NegotiationErrorResponse{
// 					Code:    "signature_missing",
// 					Content: "Signature-Input header required",
// 				})
// 			}

// 			signatureHeader := c.Request().Header.Get("Signature")
// 			if signatureHeader == "" {
// 				return c.JSON(http.StatusUnauthorized, ucp.NegotiationErrorResponse{
// 					Code:    "signature_missing",
// 					Content: "Signature header required",
// 				})
// 			}

// 			contentDigest := c.Request().Header.Get("Content-Digest")
// 			if contentDigest == "" {
// 				return c.JSON(http.StatusBadRequest, ucp.NegotiationErrorResponse{
// 					Code:    "digest_mismatch",
// 					Content: "Content-Digest header required",
// 				})
// 			}

// 			// Parse Signature-Input to extract keyid and alg
// 			keyid, alg, components, err := ParseSignatureInput(signatureInput)
// 			if err != nil {
// 				if sigErr, ok := err.(*ucp.SignatureMissingError); ok {
// 					return c.JSON(http.StatusUnauthorized, ucp.NegotiationErrorResponse{
// 						Code:    "signature_missing",
// 						Content: sigErr.Error(),
// 					})
// 				}
// 				return c.JSON(http.StatusUnauthorized, ucp.NegotiationErrorResponse{
// 					Code:    "signature_missing",
// 					Content: err.Error(),
// 				})
// 			}

// 			// Validate algorithm (only ES256 supported)
// 			if !strings.EqualFold(alg, "es256") {
// 				return c.JSON(http.StatusBadRequest, ucp.NegotiationErrorResponse{
// 					Code:    "algorithm_unsupported",
// 					Content: "only es256 algorithm is supported",
// 				})
// 			}

// 			// Look up the public key
// 			var signingKey *ucp.JWK
// 			for _, key := range platformProfile.SigningKeys {
// 				if key.Kid == keyid {
// 					signingKey = &key
// 					break
// 				}
// 			}
// 			if signingKey == nil {
// 				return c.JSON(http.StatusUnauthorized, ucp.NegotiationErrorResponse{
// 					Code:    "key_not_found",
// 					Content: "signing key not found",
// 				})
// 			}

// 			// Validate body digest
// 			digestAlgo, digestB64, err := ParseContentDigest(contentDigest)
// 			if err != nil {
// 				return c.JSON(http.StatusBadRequest, ucp.NegotiationErrorResponse{
// 					Code:    "digest_mismatch",
// 					Content: err.Error(),
// 				})
// 			}

// 			if !strings.EqualFold(digestAlgo, "sha-256") {
// 				return c.JSON(http.StatusBadRequest, ucp.NegotiationErrorResponse{
// 					Code:    "algorithm_unsupported",
// 					Content: "only sha-256 digest algorithm is supported",
// 				})
// 			}

// 			if err := VerifyBodyDigest(bodyBytes, digestB64); err != nil {
// 				return c.JSON(http.StatusBadRequest, ucp.NegotiationErrorResponse{
// 					Code:    "digest_mismatch",
// 					Content: err.Error(),
// 				})
// 			}

// 			// Parse and verify the signature
// 			sigBytes, err := ParseSignatureHeader(signatureHeader)
// 			if err != nil {
// 				return c.JSON(http.StatusUnauthorized, ucp.NegotiationErrorResponse{
// 					Code:    "signature_invalid",
// 					Content: err.Error(),
// 				})
// 			}

// 			// Convert JWK to ecdsa.PublicKey
// 			pubKey, err := JWKToECPublicKey(signingKey)
// 			if err != nil {
// 				return c.JSON(http.StatusUnauthorized, ucp.NegotiationErrorResponse{
// 					Code:    "signature_invalid",
// 					Content: err.Error(),
// 				})
// 			}

// 			// Build signature base and verify
// 			sigBase, err := BuildSignatureBase(c.Request(), components, keyid, alg)
// 			if err != nil {
// 				return c.JSON(http.StatusUnauthorized, ucp.NegotiationErrorResponse{
// 					Code:    "signature_invalid",
// 					Content: err.Error(),
// 				})
// 			}

// 			if err := VerifySignature(pubKey, sigBase, sigBytes); err != nil {
// 				return c.JSON(http.StatusUnauthorized, ucp.NegotiationErrorResponse{
// 					Code:    "signature_invalid",
// 					Content: err.Error(),
// 				})
// 			}

// 			// Signature is valid; pass through to next handler
// 			return next(c)
// 		}
// 	}
// }

const ContextKey = "ucpsig.verified"

// ResultContextKey stores the coarse outcome of signature verification
// ("valid", "missing", "invalid") on every request that passes through
// WithRequestSignatureVerifier, success or failure — unlike ContextKey
// (only set on success), this lets an outer telemetry middleware observe
// signature failures even though the verifier itself short-circuits the
// chain before the handler (and any later middleware) runs.
const ResultContextKey = "ucpsig.result"

// ResultFrom returns the coarse signature-verification outcome stashed by
// WithRequestSignatureVerifier. ok is false if the request never reached
// that middleware (e.g. verification disabled).
func ResultFrom(c echo.Context) (string, bool) {
	v, ok := c.Get(ResultContextKey).(string)
	return v, ok
}

// resultForError classifies a VerifyRequest error into the coarse outcome
// recorded on ResultContextKey.
func resultForError(err error) string {
	if AsErrorCode(err, "signature_missing") {
		return "missing"
	}
	return "invalid"
}

// AsErrorCode reports whether err is a *ucpsig.Error with the given Code.
func AsErrorCode(err error, code string) bool {
	e, ok := AsError(err)
	return ok && e.Code == code
}

// SigningTransport is an http.RoundTripper that signs every outbound request
// before passing it to a wrapped transport. Use this on the platform side for
// all platform->business calls and on the business side for webhook deliveries.
//
// Example:
//
//	signer, _ := ucpsig.NewSigner(priv, jwk)
//	client := &http.Client{
//	    Transport: &ucpsig.SigningTransport{
//	        Signer:     signer,
//	        ProfileURL: "https://platform.example/.well-known/ucp",
//	        Base:       http.DefaultTransport,
//	    },
//	}
type SigningTransport struct {
	Signer *Signer

	// ProfileURL is the URL of the signer's own /.well-known/ucp document.
	// It is placed in the UCP-Agent header so the recipient can discover the
	// signing key. Required.
	ProfileURL string

	// Base is the underlying transport. If nil, http.DefaultTransport is used.
	Base http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t *SigningTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	// Clone the request to keep RoundTrip side-effect free for the caller.
	req2 := req.Clone(req.Context())
	if t.ProfileURL != "" && req2.Header.Get("UCP-Agent") == "" {
		req2.Header.Set("UCP-Agent", `profile="`+t.ProfileURL+`"`)
	}
	if err := t.Signer.SignRequest(req2); err != nil {
		return nil, err
	}
	return base.RoundTrip(req2)
}

func WithRequestSignatureVerifier(cfg SignatureConfig) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if cfg.Disabled {
				return next(c)
			}
			req := c.Request()
			ctx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
			defer cancel()

			params, profileURL, err := cfg.Verifier.VerifyRequest(ctx, req)
			if err != nil {
				c.Set(ResultContextKey, resultForError(err))
				return toHTTPError(err)
			}
			c.Set(ContextKey, VerifiedSignature{
				Params:     params,
				ProfileURL: profileURL,
			})
			c.Set(ResultContextKey, "valid")
			return next(c)
		}
	}
}

// VerifiedFrom returns the verified signature info stashed by Verify. Returns
// ok=false if the request did not pass through the Verify middleware.
func VerifiedFrom(c echo.Context) (VerifiedSignature, bool) {
	v, ok := c.Get(ContextKey).(VerifiedSignature)
	return v, ok
}

func WithResponseSignatureGenerator(cfg SignatureConfig) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if cfg.Disabled {
				return next(c)
			}
			resp := c.Response()
			original := resp.Writer

			buf := &bufferingWriter{header: make(http.Header)}
			resp.Writer = buf
			// Echo's Response tracks Status/Size/Committed on itself, not on
			// the underlying writer. Reset them so we can detect what the
			// handler set during this call.
			resp.Status = http.StatusOK
			resp.Size = 0
			resp.Committed = false

			err := next(c)

			// Restore the original writer regardless of outcome.
			resp.Writer = original

			if err != nil {
				// Don't sign errors. Let Echo's error handler render them
				// against the un-swapped writer.
				resp.Committed = false
				return err
			}

			// Echo's c.JSON / c.String / c.Blob all flow through
			// Response.WriteHeader -> buf.WriteHeader, which means resp.Status
			// is now the handler's chosen status. Use it.
			status := resp.Status
			if status == 0 {
				status = http.StatusOK
			}

			body := buf.body.Bytes()
			signer, err := cfg.SignerProvider(c)
			if err != nil {
				// Don't sign errors. Let Echo's error handler render them
				// against the un-swapped writer.
				resp.Committed = false
				return err
			}
			sigInput, sigHdr, signErr := signer.SignResponseHeaders(status, buf.header, body, time.Now().Unix())
			if signErr != nil {
				// Reset so Echo's error handler can produce a clean response.
				resp.Committed = false
				return echo.NewHTTPError(http.StatusInternalServerError, "ucpsig: sign response: "+signErr.Error())
			}

			// Copy the handler's headers (plus Content-Digest set by the
			// signer) and the signature headers onto the real writer.
			dst := original.Header()
			maps.Copy(dst, buf.header)
			dst.Set("Signature-Input", sigInput)
			dst.Set("Signature", sigHdr)

			original.WriteHeader(status)
			n, werr := original.Write(body)
			// Update Echo's response bookkeeping so logging/access middleware
			// and after-response hooks see accurate numbers.
			resp.Size = int64(n)
			resp.Committed = true
			return werr
		}
	}
}

// bufferingWriter implements http.ResponseWriter by capturing status, headers
// and body. It does not implement Flusher/Hijacker because the wrapped
// response is buffered until the handler returns.
type bufferingWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferingWriter) Header() http.Header         { return b.header }
func (b *bufferingWriter) WriteHeader(status int)      { b.status = status }
func (b *bufferingWriter) Write(p []byte) (int, error) { return b.body.Write(p) }

// toHTTPError converts a ucpsig.Error to an *echo.HTTPError that renders the
// UCP-coded JSON envelope (`{"code":..., "content":...}`).
func toHTTPError(err error) error {
	e, ok := AsError(err)
	if !ok {
		return echo.NewHTTPError(http.StatusInternalServerError, "internal error")
	}
	body := map[string]string{"code": e.Code, "content": e.Message}
	if e.Cause != nil {
		body["content"] = e.Message + ": " + e.Cause.Error()
	}
	httpErr := echo.NewHTTPError(e.HTTPStatus)
	httpErr.Message = body
	return httpErr
}
