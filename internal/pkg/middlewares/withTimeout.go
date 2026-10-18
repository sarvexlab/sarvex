// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package middlewares

import (
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func WithTimeout() echo.MiddlewareFunc {
	return middleware.TimeoutWithConfig(middleware.TimeoutConfig{
		Timeout: 20 * time.Second,
	})
}
