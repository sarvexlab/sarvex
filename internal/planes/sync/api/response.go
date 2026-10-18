// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/pkg/contracts/apierror"
)

func notImplemented(c echo.Context) error {
	return c.JSON(http.StatusNotImplemented, apierror.Envelope{
		Error: apierror.Detail{
			Code:    "not_implemented",
			Message: "endpoint not yet implemented",
		},
	})
}

func errUnauthorized(c echo.Context) error {
	return c.JSON(http.StatusUnauthorized, apierror.Envelope{
		Error: apierror.Detail{
			Code:    "unauthorized",
			Message: "authentication required",
		},
	})
}

func errValidation(c echo.Context, details any) error {
	return c.JSON(http.StatusUnprocessableEntity, apierror.Envelope{
		Error: apierror.Detail{
			Code:    "validation_error",
			Message: "one or more fields are invalid",
			Details: details,
		},
	})
}

func errInternal(c echo.Context) error {
	return c.JSON(http.StatusInternalServerError, apierror.Envelope{
		Error: apierror.Detail{
			Code:    "internal_error",
			Message: "an internal error occurred",
		},
	})
}

func errProfileNotReady(c echo.Context) error {
	return c.JSON(http.StatusConflict, apierror.Envelope{
		Error: apierror.Detail{
			Code:    "profile_not_ready",
			Message: "no servable profile yet",
		},
	})
}
