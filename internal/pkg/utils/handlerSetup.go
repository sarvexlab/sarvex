// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"github.com/labstack/echo/v4"
)

type HandlerSetup interface {
	SetupRoutes(groupName string, group *echo.Group) error
}

type BindingHandlerSetup interface {
	SetupBinding() error
}
