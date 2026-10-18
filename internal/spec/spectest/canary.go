// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package spectest provides a test-only spec generation: a canary registered
// beside the real ones to prove that adding a generation changes nothing an
// existing one produces (docs/plans/version-platform-modularity.md §4.5).
package spectest

import (
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/generation"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/types"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wireadapt"
	"github.com/sarvexlab/sarvex/internal/spec"
	specall "github.com/sarvexlab/sarvex/internal/spec/all"
)

// CanaryVersion is a generation no UCP release has; it sorts after every real
// one, so registering it also moves Latest.
const CanaryVersion types.Version = "9999-12-31"

// Canary is a generation that serves what the baseline serves, with the
// baseline's codec and profile document under its own version.
func Canary() spec.Version {
	reg := specall.New()
	base, _ := reg.Get(reg.Baseline())
	return canary{base: base}
}

// WithCanary is a registry of every known generation plus the canary.
func WithCanary() *spec.Registry {
	reg := specall.New()
	vs := make([]spec.Version, 0, len(reg.All())+1)
	for _, id := range reg.All() {
		v, _ := reg.Get(id)
		vs = append(vs, v)
	}
	return spec.NewRegistry(append(vs, Canary())...)
}

type canary struct{ base spec.Version }

func (canary) ID() types.Version  { return CanaryVersion }
func (c canary) Serves() []string { return c.base.Serves() }

func (c canary) Codec() wireadapt.Codec { return canaryCodec{c.base.Codec()} }

func (c canary) Generation() generation.Generation {
	return canaryGeneration{c.base.Generation()}
}

type canaryCodec struct{ wireadapt.Codec }

func (canaryCodec) Version() types.Version { return CanaryVersion }

type canaryGeneration struct{ generation.Generation }

func (canaryGeneration) Version() types.Version { return CanaryVersion }
