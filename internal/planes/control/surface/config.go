// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package surface

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// ErrInvalidConfig is returned when merchant-supplied config does not match
// the surface's declared schema. It is a client error.
var ErrInvalidConfig = errors.New("surface: invalid config")

// validateSchema rejects a descriptor whose config schema could never render
// or validate: an empty or repeated key, or an unknown type.
func validateSchema(surfaceID string, schema []domain.SurfaceConfigField) error {
	seen := make(map[string]bool, len(schema))
	for _, f := range schema {
		if f.Key == "" {
			return fmt.Errorf("surface %q: config field has no key", surfaceID)
		}
		if seen[f.Key] {
			return fmt.Errorf("surface %q: duplicate config field %q", surfaceID, f.Key)
		}
		seen[f.Key] = true
		switch f.Type {
		case domain.SurfaceConfigText, domain.SurfaceConfigBool, domain.SurfaceConfigList, domain.SurfaceConfigJSON:
		default:
			return fmt.Errorf("surface %q: config field %q has unknown type %q", surfaceID, f.Key, f.Type)
		}
	}
	return nil
}

// validateConfig checks config against a declared schema. A surface with no
// schema accepts anything, exactly as before schemas existed. Required is not
// enforced here: a missing required value is a failing prerequisite the
// merchant is told about, not a rejected request.
func validateConfig(schema []domain.SurfaceConfigField, config map[string]string) error {
	if len(schema) == 0 {
		return nil
	}
	byKey := make(map[string]domain.SurfaceConfigField, len(schema))
	for _, f := range schema {
		byKey[f.Key] = f
	}
	for k, v := range config {
		f, ok := byKey[k]
		if !ok {
			return fmt.Errorf("%w: unknown field %q", ErrInvalidConfig, k)
		}
		switch f.Type {
		case domain.SurfaceConfigBool:
			if v != "" && v != "true" && v != "false" {
				return fmt.Errorf("%w: %q must be true or false", ErrInvalidConfig, k)
			}
		case domain.SurfaceConfigJSON:
			if strings.TrimSpace(v) != "" && !json.Valid([]byte(v)) {
				return fmt.Errorf("%w: %q is not valid JSON", ErrInvalidConfig, k)
			}
		}
	}
	return nil
}
