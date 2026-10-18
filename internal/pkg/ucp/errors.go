// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package ucp

import "fmt"

// NegotiationErrorResponse is the flat body for 4xx negotiation errors.
// Spec: https://ucp.dev/2026-04-08/specification/overview/#negotiation-protocol
type NegotiationErrorResponse struct {
	Code        string `json:"code"`
	Content     string `json:"content"`
	ContinueURL string `json:"continue_url,omitempty"`
}

// UCPErrorResponse is used for internal/infrastructure errors (e.g. missing UCP instance).
type UCPErrorResponse struct {
	Status   string   `json:"status"`
	Messages []UCPMsg `json:"messages"`
}

type UCPMsg struct {
	Type     string `json:"type"`
	Code     string `json:"code"`
	Content  string `json:"content"`
	Severity string `json:"severity"`
}

// CapabilitiesIncompatibleResponse is the 200 body when capability intersection is empty.
type CapabilitiesIncompatibleResponse struct {
	UCP         CapabilitiesErrorUCPBlock `json:"ucp"`
	Messages    []UCPMsg                  `json:"messages"`
	ContinueURL string                    `json:"continue_url,omitempty"`
}

// CapabilitiesErrorUCPBlock is the ucp field in a capabilities_incompatible response.
type CapabilitiesErrorUCPBlock struct {
	Version      string         `json:"version"`
	Status       string         `json:"status"`
	Capabilities map[string]any `json:"capabilities"`
}

// VersionUnsupportedError is returned when the platform's UCP version is newer than the business version.
type VersionUnsupportedError struct {
	PlatformVersion string
	BusinessVersion string
}

func (e *VersionUnsupportedError) Error() string {
	return fmt.Sprintf("version_unsupported: platform %s > business %s", e.PlatformVersion, e.BusinessVersion)
}

// NamespaceValidationError is returned when spec origins don't match capability namespaces.
type NamespaceValidationError struct {
	Mismatches []string
}

func (e *NamespaceValidationError) Error() string {
	return fmt.Sprintf("namespace validation failed: %v", e.Mismatches)
}

// ParseError is returned when the UCP-Agent header is malformed.
type ParseError struct {
	Header string
	Reason string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("malformed UCP-Agent header %q: %s", e.Header, e.Reason)
}

// ProfileUnreachableError is returned when the platform profile URL resolves but the fetch fails
// (network error, timeout, or non-2xx response). Maps to HTTP 424.
type ProfileUnreachableError struct {
	Cause string
}

func (e *ProfileUnreachableError) Error() string {
	return "profile_unreachable: " + e.Cause
}

// ProfileMalformedError is returned when the fetched profile is not valid JSON or violates schema.
// Also used for namespace validation failures. Maps to HTTP 422.
type ProfileMalformedError struct {
	Cause string
}

func (e *ProfileMalformedError) Error() string {
	return "profile_malformed: " + e.Cause
}

// CapabilitiesIncompatibleError is returned when the capability intersection is empty after pruning.
// Maps to HTTP 200 with ucp.status="error". BusinessVersion is included in the response ucp block.
type CapabilitiesIncompatibleError struct {
	BusinessVersion string
}

func (e *CapabilitiesIncompatibleError) Error() string {
	return "capabilities_incompatible: no compatible capabilities in intersection"
}

// SignatureMissingError is returned when the Signature-Input header is absent. Maps to HTTP 401.
type SignatureMissingError struct {
	Cause string
}

// SignatureInvalidError is returned when JWS signature verification fails. Maps to HTTP 401.
type SignatureInvalidError struct {
	Cause string
}

// KeyNotFoundError is returned when the keyid from Signature-Input is not in signing_keys. Maps to HTTP 401.
type KeyNotFoundError struct {
	KeyID string
}

// DigestMismatchError is returned when Content-Digest validation fails. Maps to HTTP 400.
type DigestMismatchError struct {
	Cause string
}

// AlgorithmUnsupportedError is returned when the signature algorithm is not supported. Maps to HTTP 400.
type AlgorithmUnsupportedError struct {
	Algorithm string
}
