// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package middlewares

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func WithCORS(dashboardUri string) echo.MiddlewareFunc {
	return middleware.CORSWithConfig(middleware.CORSConfig{
		AllowMethods: []string{http.MethodGet, http.MethodOptions, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodPost, http.MethodDelete},
		AllowOriginFunc: func(origin string) (bool, error) {
			originTrimmed := strings.TrimRight(strings.TrimSpace(origin), "/")
			dashboardUriTrimmed := strings.TrimRight(strings.TrimSpace(dashboardUri), "/")
			return originTrimmed == dashboardUriTrimmed, nil
		},
	})
}
