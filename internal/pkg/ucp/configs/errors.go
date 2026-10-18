// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package configs

import "fmt"

// NewMissingFieldError constructs a validation error for a missing required field
// in a specific connector.
func NewMissingFieldError(connector, field string) error {
	return fmt.Errorf("%s connector: required field %q is empty", connector, field)
}
