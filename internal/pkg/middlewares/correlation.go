// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package middlewares

import (
	"github.com/labstack/echo/v4"
)

// GetCorrelationID extracts the correlation ID (X-Request-ID) from the Echo context.
// Returns the ID if present, or empty string if not found.
func GetCorrelationID(c echo.Context) string {
	// Try request header first (for client-provided IDs)
	if cid := c.Request().Header.Get(echo.HeaderXRequestID); cid != "" {
		return cid
	}
	// Fall back to response header (set by WithCorrelationID middleware)
	return c.Response().Header().Get(echo.HeaderXRequestID)
}
