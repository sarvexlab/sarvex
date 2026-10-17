// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package middlewares

import (
	"github.com/labstack/echo/v4"
	"github.com/sarvexlab/sarvex/internal/pkg/common"
)

func WithServerHeader() echo.MiddlewareFunc {
	return func(hf echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Response().Header().Set(echo.HeaderServer, common.ServerType)
			return hf(c)
		}
	}
}
