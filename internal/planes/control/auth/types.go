// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package auth handles authentication and authorization for the control plane.
package auth

import (
	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// PrincipalResolver resolves authentication credentials to a Principal.
// Can be bearer token (JWT), HMAC signature, or other schemes.
// The token parameter contains the auth credential.
type PrincipalResolver func(token string) (domain.Principal, error)

// HMACResolver resolves HMAC-signed requests to a Principal.
// Unlike PrincipalResolver, it has access to the full request context for
// body and header verification.
type HMACResolver func(c echo.Context, signature string) (domain.Principal, error)
