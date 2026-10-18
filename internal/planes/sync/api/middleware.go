// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/planes/sync/auth"
	"github.com/sarvexlab/sarvex/internal/planes/sync/domain"
)

type contextKey string

const (
	ctxPrincipal contextKey = "principal"
)

// Type aliases for resolver functions (defined in auth package)
type (
	HMACResolver = auth.HMACResolver
)

// requireHMAC enforces HMAC-signed connector ingest requests per X-Sarvex-Signature header.
// Buffers the request body so it can be verified as part of the HMAC validation.
// The resolver validates signature over body + timestamp + sequence headers.
func requireHMAC(resolve HMACResolver) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			sig := c.Request().Header.Get("X-Sarvex-Signature")
			if sig == "" {
				return errUnauthorized(c)
			}
			p, err := resolve(c, sig)
			if err != nil {
				return errUnauthorized(c)
			}
			c.Set(string(ctxPrincipal), p)
			return next(c)
		}
	}
}

// principalFrom extracts the resolved Principal from the context.
// Panics if called before requireAuth — that is a programming error.
func principalFrom(c echo.Context) domain.Principal {
	return c.Get(string(ctxPrincipal)).(domain.Principal)
}
