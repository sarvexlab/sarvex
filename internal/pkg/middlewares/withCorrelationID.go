// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package middlewares

import (
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	nanoId "github.com/matoous/go-nanoid/v2"
)

func WithCorrelationID() echo.MiddlewareFunc {
	config := middleware.RequestIDConfig{
		Skipper: func(c echo.Context) bool {
			return strings.Contains(c.Request().URL.Path, "healthz")
		},
		Generator: func() string {
			cid, err := nanoId.New()
			if cid != "" && err == nil {
				return cid
			}

			return uuid.New().String()
		},
		TargetHeader: echo.HeaderXRequestID,
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if config.Skipper(c) {
				return next(c)
			}

			req := c.Request()
			res := c.Response()

			cid := req.Header.Get(config.TargetHeader)
			if cid == "" {
				cid = config.Generator()
				req.Header.Set(config.TargetHeader, cid)
			}

			res.Header().Set(config.TargetHeader, cid)

			if config.RequestIDHandler != nil {
				config.RequestIDHandler(c, cid)
			}

			return next(c)
		}
	}
}
