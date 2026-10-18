// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVerifyRoutes_ParameterNamesAreNotRouteIdentity(t *testing.T) {
	code := map[string]bool{"/checkout-sessions/{checkout_id}/timeline": true}
	spec := map[string]bool{"/checkout-sessions/{checkout_session_id}/timeline": true}
	assert.Empty(t, verifyRoutes(code, spec))
}

func TestVerifyRoutes_StillReportsRealDrift(t *testing.T) {
	code := map[string]bool{"/a/{id}": true, "/only-code": true}
	spec := map[string]bool{"/a/{other}": true, "/only-spec": true}
	got := verifyRoutes(code, spec)
	assert.Equal(t, []string{
		"code-only: /only-code (not in OpenAPI spec)",
		"spec-only: /only-spec (not in code)",
	}, got)
}

func TestVerifyRoutes_DifferentDepthIsDifferentRoute(t *testing.T) {
	code := map[string]bool{"/a/{id}": true}
	spec := map[string]bool{"/a/{id}/b": true}
	assert.Len(t, verifyRoutes(code, spec), 2)
}
