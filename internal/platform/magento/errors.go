// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package magento

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// Sentinel errors for common Magento API failures.
var (
	ErrNotFound           = errors.New("magento: not found")
	ErrUnauthorized       = errors.New("magento: unauthorized")
	ErrForbidden          = errors.New("magento: forbidden")
	ErrRateLimited        = errors.New("magento: rate limited")
	ErrInventoryExhausted = errors.New("magento: inventory exhausted")
	ErrCheckoutExpired    = errors.New("magento: checkout expired")
	ErrTokenRefresh       = errors.New("magento: failed to refresh admin token")
)

// errTransport marks a network/transport-level failure (as opposed to a
// classified HTTP error response) from a store-API pull request, so the
// retry classifier can treat it as transient. See connector.go's
// isRetryablePullError.
var errTransport = errors.New("magento: transport error")

// APIError represents an error response from the Magento API.
type APIError struct {
	StatusCode int
	Message    string
	Err        error
}

func (e *APIError) Error() string {
	return fmt.Sprintf("magento: http %d: %s", e.StatusCode, e.Message)
}

// IsRetryable returns true if the error is transient and safe to retry.
func (e *APIError) IsRetryable() bool {
	switch e.StatusCode {
	case 408, 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

// classifyHTTPError maps HTTP status codes to sentinel errors.
func classifyHTTPError(statusCode int, body string) error {
	switch statusCode {
	case 200, 201:
		return nil
	case 401:
		return ErrUnauthorized
	case 403:
		return ErrForbidden
	case 404:
		return ErrNotFound
	case 429:
		return ErrRateLimited
	case 500, 502, 503, 504:
		return &APIError{StatusCode: statusCode, Message: "server error"}
	default:
		// 400 and other client errors keep the store's own message: it is what
		// tells a buyer why a checkout was refused (out of stock, invalid
		// address, ...).
		return &APIError{
			StatusCode: statusCode,
			Message:    extractErrorMessage(body),
		}
	}
}

// extractErrorMessage extracts a meaningful error message from a Magento error
// response: {"message": "...%1...", "parameters": [...]} where %1, %2 (or
// %name for a parameters object) are substituted into the message.
func extractErrorMessage(body string) string {
	var parsed struct {
		Message    string          `json:"message"`
		Parameters json.RawMessage `json:"parameters"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err == nil && parsed.Message != "" {
		msg := parsed.Message
		var list []any
		if json.Unmarshal(parsed.Parameters, &list) == nil {
			for i, p := range list {
				msg = strings.ReplaceAll(msg, fmt.Sprintf("%%%d", i+1), fmt.Sprint(p))
			}
		}
		var named map[string]any
		if json.Unmarshal(parsed.Parameters, &named) == nil {
			for k, v := range named {
				msg = strings.ReplaceAll(msg, "%"+k, fmt.Sprint(v))
			}
		}
		return msg
	}
	if len(body) > 200 {
		return body[:200]
	}
	return body
}

// checkoutRejection converts a store error into a business-level rejection
// when the store answered with a client (4xx) status — out of stock, unknown
// product, an invalid address, a declined payment — rather than a
// transport/5xx failure that should surface as "store unavailable". Returns
// nil when err is not such a rejection, so the caller falls through to its
// unavailable path. The store's own message is carried through verbatim.
func checkoutRejection(err error) *base.CheckoutRejectedError {
	var apiErr *APIError
	switch {
	case errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500:
		return &base.CheckoutRejectedError{Code: rejectionCode(apiErr.Message), Detail: apiErr.Message}
	case errors.Is(err, ErrNotFound):
		return &base.CheckoutRejectedError{Code: "not_found", Detail: err.Error()}
	case errors.Is(err, ErrInventoryExhausted):
		return &base.CheckoutRejectedError{Code: "out_of_stock", Detail: err.Error()}
	default:
		return nil
	}
}

// rejectionCode maps a store message to a UCP standard error code where one
// clearly applies; empty otherwise.
func rejectionCode(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "stock"), strings.Contains(lower, "not available"), strings.Contains(lower, "requested qty"):
		return "out_of_stock"
	case strings.Contains(lower, "could not find a product"), strings.Contains(lower, "doesn't exist"), strings.Contains(lower, "not found"):
		return "not_found"
	case strings.Contains(lower, "payment"):
		return "payment_failed"
	case strings.Contains(lower, "shipping method"), strings.Contains(lower, "address"), strings.Contains(lower, "email"), strings.Contains(lower, "required"):
		return "field_required"
	default:
		return ""
	}
}
