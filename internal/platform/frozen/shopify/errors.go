// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package shopify

import (
	"errors"
	"fmt"
	"net/http"
)

// Sentinel errors — use errors.Is for matching.
var (
	ErrNotFound           = errors.New("shopify: resource not found")
	ErrUnauthorized       = errors.New("shopify: unauthorized — check access token")
	ErrForbidden          = errors.New("shopify: forbidden — insufficient token scope")
	ErrRateLimited        = errors.New("shopify: rate limited")
	ErrInventoryExhausted = errors.New("shopify: item out of stock or unavailable")
	ErrCheckoutExpired    = errors.New("shopify: checkout session expired or completed")
)

// APIError wraps a Shopify API error response with HTTP context.
type APIError struct {
	StatusCode int
	Body       string
	Errors     map[string][]string // Shopify REST error map
	GQLErrors  []GQLError          // GraphQL errors array
}

func (e *APIError) Error() string {
	return fmt.Sprintf("shopify api error %d: %s", e.StatusCode, e.Body)
}

// IsNotFound returns true if the error is a 404 or equivalent GQL NOT_FOUND.
func (e *APIError) IsNotFound() bool {
	return e.StatusCode == http.StatusNotFound
}

// IsRetryable returns true for transient conditions that warrant a retry.
func (e *APIError) IsRetryable() bool {
	return e.StatusCode == http.StatusTooManyRequests ||
		e.StatusCode == http.StatusServiceUnavailable ||
		e.StatusCode == http.StatusGatewayTimeout
}

// GQLError is a single error entry from a Shopify GraphQL error array.
type GQLError struct {
	Message    string             `json:"message"`
	Locations  []GQLErrorLocation `json:"locations,omitempty"`
	Path       []string           `json:"path,omitempty"`
	Extensions map[string]any     `json:"extensions,omitempty"`
}

type GQLErrorLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// classifyHTTPError maps a Shopify HTTP status code to a typed sentinel error.
// Returns nil for 2xx status codes.
func classifyHTTPError(status int, body string) error {
	switch status {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent:
		return nil
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ErrUnauthorized, body)
	case http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrForbidden, body)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, body)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w: %s", ErrRateLimited, body)
	default:
		return &APIError{StatusCode: status, Body: body}
	}
}
