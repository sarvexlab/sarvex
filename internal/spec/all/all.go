// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package all assembles the spec registry of every UCP generation Sarvex
// knows. Adding a generation is one import and one line here; no established
// generation's module changes.
package all

import (
	"sync"

	"github.com/sarvexlab/sarvex/internal/spec"
	v20260408 "github.com/sarvexlab/sarvex/internal/spec/v20260408/release"
	v20260825 "github.com/sarvexlab/sarvex/internal/spec/v20260825/release"
)

var (
	once     sync.Once
	registry *spec.Registry
)

// New returns a fresh registry of every known generation.
func New() *spec.Registry {
	return spec.NewRegistry(
		v20260408.New(),
		v20260825.New(),
	)
}

// Default is the process-wide registry.
func Default() *spec.Registry {
	once.Do(func() { registry = New() })
	return registry
}
