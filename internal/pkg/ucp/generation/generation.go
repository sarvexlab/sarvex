// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package generation is the registry of each UCP spec generation's profile
// document: how a profile written in that generation reads into the
// generation-neutral form (internal/pkg/ucp/wire), and how Sarvex writes one
// in it. Generations differ there only in small ways (today, the name of the
// key set), and each difference lives in its own internal/spec/vYYYYMMDD
// module rather than in a branch here or in a caller.
//
// Each generation implements Generation once, in its
// internal/spec/vYYYYMMDD/profile package; the spec registry (internal/spec)
// assembles them. Nothing in this package or its consumers changes when one
// is added.
package generation

import (
	"encoding/json"
	"fmt"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/types"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// Generation is the per-UCP-spec-generation behavior Sarvex needs to
// participate in that generation. Each supported generation
// registers exactly one
// implementation via Registry.Register.
//
// It covers the profile document: reading a counterparty's profile written
// in this generation, and writing Sarvex's own in it. Everything else that
// differs per generation lives in that generation's wireadapt codec.
type Generation interface {
	// Version identifies which UCP spec generation this implementation
	// handles.
	Version() types.Version

	// DecodeProfile parses a raw UCP profile document (a business's
	// /.well-known/ucp bytes, or a platform's declared profile fetched
	// via the UCP-Agent header) written in this generation's form.
	DecodeProfile(raw json.RawMessage) (*wire.Profile, error)

	// EncodeProfile writes p in this generation's document form.
	EncodeProfile(p *wire.Profile) ([]byte, error)
}

// versionSniff is the minimal shape needed to read a raw UCP profile
// document's declared generation before deciding which Generation's
// DecodeProfile to hand it to.
type versionSniff struct {
	UCP struct {
		Version string `json:"version"`
	} `json:"ucp"`
}

// SniffVersion reads only the "ucp.version" field from a raw UCP profile
// document, without committing to any generation's full envelope shape.
// Callers resolve the result through a Registry to find the right
// Generation before fully decoding.
func SniffVersion(raw json.RawMessage) (types.Version, error) {
	var s versionSniff
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("generation: sniff ucp.version: %w", err)
	}
	if s.UCP.Version == "" {
		return "", fmt.Errorf("generation: profile has no ucp.version field")
	}
	return types.Version(s.UCP.Version), nil
}

// Registry holds the set of actively-supported Generations, keyed by
// spec version.
type Registry struct {
	generations map[types.Version]Generation
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{generations: make(map[types.Version]Generation)}
}

// Register adds g to the registry. Panics on a duplicate version
// registration — that is a startup-time programming error (two
// generations claiming the same version), not a runtime condition to
// handle gracefully.
func (r *Registry) Register(g Generation) {
	v := g.Version()
	if _, exists := r.generations[v]; exists {
		panic(fmt.Sprintf("generation: %s already registered", v))
	}
	r.generations[v] = g
}

// Get returns the Generation registered for v, if any.
func (r *Registry) Get(v types.Version) (Generation, bool) {
	g, ok := r.generations[v]
	return g, ok
}

// All returns every registered Generation. Order is unspecified.
func (r *Registry) All() []Generation {
	out := make([]Generation, 0, len(r.generations))
	for _, g := range r.generations {
		out = append(out, g)
	}
	return out
}

// DecodeProfile sniffs raw's declared generation, resolves the matching
// Generation from the registry, and fully decodes raw through it.
func (r *Registry) DecodeProfile(raw json.RawMessage) (*wire.Profile, error) {
	v, err := SniffVersion(raw)
	if err != nil {
		return nil, err
	}
	g, ok := r.Get(v)
	if !ok {
		return nil, fmt.Errorf("generation: no registered Generation for version %s", v)
	}
	return g.DecodeProfile(raw)
}
