// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package all is the sync plane's surface registration root: one line per
// surface that generates an artifact. Nothing else names a surface's sync
// half; the runner and the plane's composition root go through Generators.
package all

import (
	"github.com/sarvexlab/sarvex/internal/planes/sync/surface"
)

// Generators returns every registered surface artifact generator. No bundled
// surface generates an artifact today; a surface that does adds its
// Generator here.
func Generators() []surface.Generator {
	return []surface.Generator{}
}
