// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package ucpsig implements HTTP Message Signatures (RFC 9421) and Content
// Digest (RFC 9530) for the Universal Commerce Protocol.
//
// The package is organised in three layers:
//
//   - Core primitives: JWK keys, signature base construction, ECDSA signing
//     and verification with raw r||s encoding, and SHA-256 content digests.
//   - Profile resolution: fetching and caching /.well-known/ucp documents
//     with their signing_keys array, supporting key rotation.
//   - HTTP middleware: a signing http.RoundTripper for outbound requests,
//     a verifying http.Handler middleware for inbound requests, and a
//     response-signing ResponseWriter for outbound responses.
//
// Applicability matrix:
//
//	Direction                          | Outbound side    | Inbound side
//	-----------------------------------|------------------|-----------------
//	Platform -> Business (REST)        | SigningTransport | VerifyMiddleware
//	Business -> Platform (webhook)     | SigningTransport | VerifyMiddleware
//	Business -> Platform (response)    | SignResponse     | VerifyResponse
//	Platform -> Business (MCP/HTTP)    | SigningTransport | VerifyMiddleware
//
// The package is transport-agnostic with respect to MCP vs REST: because MCP
// uses streamable HTTP with JSON-RPC bodies, the same middleware works for
// both. Bodies are digested as raw bytes, so intermediaries MUST NOT
// re-serialise JSON payloads.
package ucpsig

import "errors"

// UCP signature error codes (mapped to HTTP status per the spec).
var (
	ErrSignatureMissing     = &Error{Code: "signature_missing", HTTPStatus: 401, Message: "required signature header not present"}
	ErrSignatureInvalid     = &Error{Code: "signature_invalid", HTTPStatus: 401, Message: "signature verification failed"}
	ErrKeyNotFound          = &Error{Code: "key_not_found", HTTPStatus: 401, Message: "key id not found in signer's signing_keys"}
	ErrDigestMismatch       = &Error{Code: "digest_mismatch", HTTPStatus: 400, Message: "body digest does not match Content-Digest"}
	ErrAlgorithmUnsupported = &Error{Code: "algorithm_unsupported", HTTPStatus: 400, Message: "signature algorithm not supported"}
	ErrInvalidProfileURL    = &Error{Code: "invalid_profile_url", HTTPStatus: 400, Message: "profile URL malformed or invalid scheme"}
	ErrProfileUnreachable   = &Error{Code: "profile_unreachable", HTTPStatus: 424, Message: "unable to fetch signer's profile"}
	ErrProfileNotTrusted    = &Error{Code: "profile_not_trusted", HTTPStatus: 403, Message: "profile URL not in registry of pre-approved platforms"}
)

// Error is a UCP-coded error. It implements the error interface and carries
// the wire code, suggested HTTP status, and a human-readable message.
type Error struct {
	Code       string
	HTTPStatus int
	Message    string
	Cause      error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return e.Code + ": " + e.Message + ": " + e.Cause.Error()
	}
	return e.Code + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.Cause }

// withCause returns a copy of e with the given cause attached.
func (e *Error) withCause(err error) *Error {
	return &Error{Code: e.Code, HTTPStatus: e.HTTPStatus, Message: e.Message, Cause: err}
}

// AsError unwraps to *Error if possible.
func AsError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
