// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package httpx

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// ErrorCode is a UCP-defined error code string. We keep these as
// constants rather than an enum so they round-trip through JSON cleanly.
//
// See https://ucp.dev/latest/specification/overview/#error-codes
type ErrorCode string

const (
	CodeInvalidProfileURL        ErrorCode = "invalid_profile_url"
	CodeProfileUnreachable       ErrorCode = "profile_unreachable"
	CodeProfileMalformed         ErrorCode = "profile_malformed"
	CodeVersionUnsupported       ErrorCode = "version_unsupported"
	CodeCapabilitiesIncompatible ErrorCode = "capabilities_incompatible"
)

// httpStatusFor returns the REST status code for an error code per the
// spec's error table.
func httpStatusFor(code ErrorCode) int {
	switch code {
	case CodeInvalidProfileURL:
		return http.StatusBadRequest
	case CodeProfileUnreachable:
		return http.StatusFailedDependency
	case CodeProfileMalformed, CodeVersionUnsupported:
		return http.StatusUnprocessableEntity
	case CodeCapabilitiesIncompatible:
		// Special case: spec returns 200 OK with ucp.status = "error".
		return http.StatusOK
	default:
		return http.StatusInternalServerError
	}
}

// MapError translates a fetch-layer error into a UCP error code. Errors
// not matching a known sentinel return ("", false).
func MapError(err error) (ErrorCode, bool) {
	switch {
	case errors.Is(err, ErrInvalidProfileURL):
		return CodeInvalidProfileURL, true
	case errors.Is(err, ErrProfileUnreachable):
		return CodeProfileUnreachable, true
	case errors.Is(err, ErrProfileMalformed):
		return CodeProfileMalformed, true
	}
	return "", false
}

// DiscoveryError is the body shape for discovery/version failures —
// returned at the HTTP level with a 4xx status.
type DiscoveryError struct {
	Code        ErrorCode `json:"code"`
	Content     string    `json:"content,omitempty"`
	ContinueURL string    `json:"continue_url,omitempty"`
}

// WriteDiscoveryError writes a UCP discovery/version error response.
// continueURL is optional but RECOMMENDED — it lets agents fall back
// gracefully to a web flow.
func WriteDiscoveryError(w http.ResponseWriter, code ErrorCode, content, continueURL string) {
	status := httpStatusFor(code)
	body := DiscoveryError{Code: code, Content: content, ContinueURL: continueURL}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// Message is a UCP message entry inside a response.
type Message struct {
	Type     string `json:"type"` // "error" | "info" | "warning"
	Code     string `json:"code"`
	Content  string `json:"content,omitempty"`
	Severity string `json:"severity,omitempty"` // "unrecoverable" | "requires_buyer_input" | ...
	Path     string `json:"path,omitempty"`
}

// NegotiationFailure is the body shape for capabilities_incompatible —
// HTTP 200 with ucp.status = "error".
type NegotiationFailure struct {
	UCP         FailureUCP `json:"ucp"`
	Messages    []Message  `json:"messages,omitempty"`
	ContinueURL string     `json:"continue_url,omitempty"`
}

// FailureUCP is the inner ucp object for a negotiation failure response.
type FailureUCP struct {
	Version      wire.Version                 `json:"version"`
	Status       string                       `json:"status"` // always "error"
	Capabilities map[string][]wire.Capability `json:"capabilities"`
}

// WriteNegotiationFailure writes a capabilities_incompatible response.
// HTTP 200, with ucp.status = "error" in the body.
func WriteNegotiationFailure(w http.ResponseWriter, protocolVersion wire.Version, content, continueURL string) {
	body := NegotiationFailure{
		UCP: FailureUCP{
			Version:      protocolVersion,
			Status:       "error",
			Capabilities: map[string][]wire.Capability{},
		},
		Messages: []Message{{
			Type:     "error",
			Code:     string(CodeCapabilitiesIncompatible),
			Content:  content,
			Severity: "unrecoverable",
		}},
		ContinueURL: continueURL,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}
