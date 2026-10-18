// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package middlewares

import (
	"github.com/labstack/echo/v4"
)

func WithRequestValidation(disable bool) echo.MiddlewareFunc {
	return func(hf echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// _, context := common.GetSarvexContext(c)
			// c.Set(common.SarvexContext, context)
			return hf(c)
		}
	}
}
