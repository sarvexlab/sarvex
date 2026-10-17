// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package woocommerce

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// Sentinel errors — use errors.Is for matching.
var (
	ErrNotFound           = errors.New("woocommerce: resource not found")
	ErrUnauthorized       = errors.New("woocommerce: unauthorized — check consumer key/secret")
	ErrForbidden          = errors.New("woocommerce: forbidden — insufficient permissions")
	ErrRateLimited        = errors.New("woocommerce: rate limited")
	ErrInventoryExhausted = errors.New("woocommerce: item out of stock or unavailable")
	ErrCheckoutExpired    = errors.New("woocommerce: checkout session expired or completed")
)

// APIError wraps a WooCommerce API error response with HTTP context.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("woocommerce api error %d: %s", e.StatusCode, e.Body)
}

// IsRetryable returns true for transient conditions that warrant a retry.
func (e *APIError) IsRetryable() bool {
	return e.StatusCode == http.StatusTooManyRequests ||
		e.StatusCode == http.StatusServiceUnavailable ||
		e.StatusCode == http.StatusGatewayTimeout
}

// checkoutRejection converts a store error into a business-level rejection
// when the store answered with a client (4xx) status — out of stock, unknown
// product, a declined payment, a missing selection — rather than a
// transport/5xx failure that should surface as "store unavailable". Returns
// nil when err is not such a rejection, so the caller falls through to its
// unavailable path. The store's own message is carried through verbatim so the
// 4xx body stays diagnostic (the conformance suite matches on its substring).
func checkoutRejection(err error) *base.CheckoutRejectedError {
	var apiErr *APIError
	switch {
	case errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500:
		msg := storeMessage(apiErr.Body)
		return &base.CheckoutRejectedError{Code: rejectionCode(msg), Detail: msg}
	case errors.Is(err, ErrNotFound):
		return &base.CheckoutRejectedError{Code: "not_found", Detail: err.Error()}
	case errors.Is(err, ErrInventoryExhausted):
		return &base.CheckoutRejectedError{Code: "out_of_stock", Detail: err.Error()}
	default:
		return nil
	}
}

// storeMessage pulls the human-readable "message" out of a WooCommerce error
// body ({"code","message","data"}), falling back to the raw body when it is
// not that shape.
func storeMessage(body string) string {
	var parsed struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err == nil && parsed.Message != "" {
		return parsed.Message
	}
	return body
}

// rejectionCode maps a store message to a UCP standard error code where one
// clearly applies; empty otherwise.
func rejectionCode(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "stock"):
		return "out_of_stock"
	case strings.Contains(lower, "not found"):
		return "not_found"
	case strings.Contains(lower, "payment"):
		return "payment_failed"
	case strings.Contains(lower, "fulfillment"), strings.Contains(lower, "required"):
		return "field_required"
	default:
		return ""
	}
}

// classifyHTTPError maps a WooCommerce HTTP status code to a typed sentinel error.
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
