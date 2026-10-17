// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

func notImplemented(c echo.Context) error {
	return c.JSON(http.StatusNotImplemented, domain.ErrorEnvelope{
		Error: domain.ErrorDetail{
			Code:    "not_implemented",
			Message: "endpoint not yet implemented",
		},
	})
}

func errUnauthorized(c echo.Context) error {
	return c.JSON(http.StatusUnauthorized, domain.ErrorEnvelope{
		Error: domain.ErrorDetail{
			Code:    "unauthorized",
			Message: "authentication required",
		},
	})
}

func errForbidden(c echo.Context) error {
	return c.JSON(http.StatusForbidden, domain.ErrorEnvelope{
		Error: domain.ErrorDetail{
			Code:    "forbidden",
			Message: "operator role required",
		},
	})
}

func errNotFound(c echo.Context) error {
	return c.JSON(http.StatusNotFound, domain.ErrorEnvelope{
		Error: domain.ErrorDetail{
			Code:    "not_found",
			Message: "resource not found",
		},
	})
}

func errConflict(c echo.Context, msg string) error {
	return c.JSON(http.StatusConflict, domain.ErrorEnvelope{
		Error: domain.ErrorDetail{
			Code:    "conflict",
			Message: msg,
		},
	})
}

func errValidation(c echo.Context, details any) error {
	return c.JSON(http.StatusUnprocessableEntity, domain.ErrorEnvelope{
		Error: domain.ErrorDetail{
			Code:    "validation_error",
			Message: "one or more fields are invalid",
			Details: details,
		},
	})
}

func errAnalyticsTierRequired(c echo.Context) error {
	return c.JSON(http.StatusForbidden, domain.ErrorEnvelope{
		Error: domain.ErrorDetail{
			Code:    "analytics_tier_required",
			Message: "the Lost Revenue Ledger is available on the Concierge and Scale plans — upgrade to view it",
		},
	})
}

func errBillingReadOnly(c echo.Context) error {
	return c.JSON(http.StatusForbidden, domain.ErrorEnvelope{
		Error: domain.ErrorDetail{
			Code:    "billing_read_only",
			Message: "subscription is not active — upgrade or update payment to regain access",
		},
	})
}

func errInternal(c echo.Context) error {
	return c.JSON(http.StatusInternalServerError, domain.ErrorEnvelope{
		Error: domain.ErrorDetail{
			Code:    "internal_error",
			Message: "an internal error occurred",
		},
	})
}
