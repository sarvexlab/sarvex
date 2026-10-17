// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package httpx provides transport-layer plumbing for UCP negotiation
// over HTTP: header parsing, profile fetching with bounded caching, and
// standard error responses.
package httpx

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// UCPAgentHeader is the parsed form of the UCP-Agent header. Only the
// `profile` member is defined by the spec today; additional members are
// preserved to allow forward-compatible extensions.
//
// See https://ucp.dev/latest/specification/overview/#platform-advertisement-on-request
type UCPAgentHeader struct {
	ProfileURL string
	Extras     map[string]string
}

// ParseUCPAgent parses an RFC 8941 Dictionary Structured Field value of
// the form:
//
//	profile="https://platform.example/.well-known/ucp"
//
// We implement just the subset of RFC 8941 actually used by UCP — a
// dictionary of quoted-string members. Anything more exotic gets a clear
// error rather than silent acceptance.
func ParseUCPAgent(header string) (UCPAgentHeader, error) {
	if header == "" {
		return UCPAgentHeader{}, errors.New("httpx: empty UCP-Agent header")
	}
	out := UCPAgentHeader{Extras: map[string]string{}}

	for _, raw := range splitTopLevel(header, ',') {
		kv := strings.SplitN(strings.TrimSpace(raw), "=", 2)
		if len(kv) != 2 {
			return UCPAgentHeader{}, fmt.Errorf("httpx: malformed UCP-Agent member %q", raw)
		}
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		val := strings.TrimSpace(kv[1])
		unquoted, err := unquote(val)
		if err != nil {
			return UCPAgentHeader{}, fmt.Errorf("httpx: UCP-Agent[%s]: %w", key, err)
		}
		if key == "profile" {
			out.ProfileURL = unquoted
		} else {
			out.Extras[key] = unquoted
		}
	}

	if out.ProfileURL == "" {
		return UCPAgentHeader{}, errors.New("httpx: UCP-Agent missing required profile member")
	}
	if err := validateProfileURL(out.ProfileURL); err != nil {
		return UCPAgentHeader{}, err
	}
	return out, nil
}

// FormatUCPAgent renders a UCP-Agent header value for outbound requests.
// The profile URL is quoted per RFC 8941.
func FormatUCPAgent(profileURL string) (string, error) {
	if err := validateProfileURL(profileURL); err != nil {
		return "", err
	}
	return fmt.Sprintf(`profile=%q`, profileURL), nil
}

func validateProfileURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("httpx: invalid profile URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("httpx: profile URL %q must use https", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("httpx: profile URL %q missing host", raw)
	}
	return nil
}

// splitTopLevel splits s on sep but ignores sep inside double-quoted spans.
// Sufficient for the dictionaries UCP uses.
func splitTopLevel(s string, sep byte) []string {
	var out []string
	var buf strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' && (i == 0 || s[i-1] != '\\'):
			inQuote = !inQuote
			buf.WriteByte(c)
		case c == sep && !inQuote:
			out = append(out, buf.String())
			buf.Reset()
		default:
			buf.WriteByte(c)
		}
	}
	if buf.Len() > 0 {
		out = append(out, buf.String())
	}
	return out
}

func unquote(s string) (string, error) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", fmt.Errorf("expected quoted string, got %q", s)
	}
	return s[1 : len(s)-1], nil
}
