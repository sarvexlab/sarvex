// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"errors"
	"fmt"
	"testing"

	servingpkg "github.com/sarvexlab/sarvex/internal/pkg/serving"
)

// The serving cache has one not-found sentinel. It used to have two — one in
// this plane (returned by RedisCache) and one in internal/pkg/serving (checked
// by the control plane's operator service) — so a store with no snapshot yet
// never matched the operator's errors.Is(err, serving.ErrNotFound) and was
// reported as a failure instead of "nothing yet".
func TestErrNotFoundIsTheSharedSentinel(t *testing.T) {
	wrapped := fmt.Errorf("get profile: %w", ErrNotFound)
	if !errors.Is(wrapped, servingpkg.ErrNotFound) {
		t.Fatal("the serving plane's ErrNotFound must be internal/pkg/serving's")
	}
}
