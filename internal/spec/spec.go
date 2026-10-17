// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package spec is the registry of the UCP spec generations Sarvex knows. Each
// generation is one module, internal/spec/vYYYYMMDD, which contributes one
// Version here (from its release package); internal/spec/all registers them,
// one line per generation. Everything that needs to know which generations
// exist — negotiation, rendering, the published profile, defaults — asks this
// registry, never a constant.
package spec

import (
	"fmt"
	"slices"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/generation"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/types"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wireadapt"
)

// Version is everything Sarvex needs from one spec generation.
type Version interface {
	// ID is the generation's date-based version.
	ID() types.Version

	// Serves names the capabilities and extensions Sarvex renders in this
	// generation (e.g. "dev.ucp.shopping.checkout"). A generation is only
	// negotiated, or advertised, for what it serves.
	Serves() []string

	// Codec translates the canonical model to and from this generation's
	// wire form.
	Codec() wireadapt.Codec

	// Generation reads and writes this generation's profile document.
	Generation() generation.Generation
}

// Registry holds the known generations.
type Registry struct {
	versions    map[types.Version]Version
	ids         []types.Version // oldest first
	codecs      *wireadapt.Registry
	generations *generation.Registry
}

// NewRegistry returns a registry of vs. It panics on a duplicate generation:
// two modules claiming one version is a programming error.
func NewRegistry(vs ...Version) *Registry {
	r := &Registry{
		versions:    make(map[types.Version]Version, len(vs)),
		codecs:      wireadapt.NewRegistry(),
		generations: generation.NewRegistry(),
	}
	for _, v := range vs {
		id := v.ID()
		if _, dup := r.versions[id]; dup {
			panic(fmt.Sprintf("spec: %s registered twice", id))
		}
		r.versions[id] = v
		r.ids = append(r.ids, id)
		r.codecs.Register(v.Codec())
		r.generations.Register(v.Generation())
	}
	slices.Sort(r.ids)
	return r
}

// Get returns the generation v, if known.
func (r *Registry) Get(v types.Version) (Version, bool) {
	ver, ok := r.versions[v]
	return ver, ok
}

// Known reports whether v is a known generation.
func (r *Registry) Known(v types.Version) bool {
	_, ok := r.versions[v]
	return ok
}

// Parse resolves a version string (as found in response metadata or a
// profile's "ucp.version") to a known generation.
func (r *Registry) Parse(s string) (types.Version, error) {
	v := types.Version(s)
	if !r.Known(v) {
		return "", fmt.Errorf("spec: unknown spec version %q", s)
	}
	return v, nil
}

// All lists every known generation, oldest first.
func (r *Registry) All() []types.Version { return slices.Clone(r.ids) }

// Latest is the newest known generation: the version Sarvex's own published
// profile declares as its envelope, and what newly-onboarded stores default
// to. It moves on its own when a newer generation is registered.
func (r *Registry) Latest() types.Version { return r.ids[len(r.ids)-1] }

// Baseline is the oldest known generation: what a request that was never
// negotiated is served in.
func (r *Registry) Baseline() types.Version { return r.ids[0] }

// Serving lists, oldest first, the generations that serve capability.
func (r *Registry) Serving(capability string) []types.Version {
	var out []types.Version
	for _, id := range r.ids {
		if slices.Contains(r.versions[id].Serves(), capability) {
			out = append(out, id)
		}
	}
	return out
}

// Servable lists, oldest first, the generations that serve anything.
func (r *Registry) Servable() []types.Version {
	var out []types.Version
	for _, id := range r.ids {
		if len(r.versions[id].Serves()) > 0 {
			out = append(out, id)
		}
	}
	return out
}

// Codecs is the wire codec of every known generation.
func (r *Registry) Codecs() *wireadapt.Registry { return r.codecs }

// Generations is the profile-document reader/writer of every known
// generation.
func (r *Registry) Generations() *generation.Registry { return r.generations }
