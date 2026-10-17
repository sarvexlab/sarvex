// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package surface owns agent-surface enrollment: which distribution
// surfaces a store is live on, and what it takes to get there.
//
// A surface is declared, not coded. The descriptor
// (domain.SurfaceDescriptor) says what a surface requires, what Sarvex
// generates for it, what the merchant must do outside Sarvex, and what
// proves enrollment succeeded. The three things that cannot be data —
// evaluating a prerequisite, producing an artifact, recognising a
// verification signal — are interfaces with per-surface implementations,
// registered alongside the descriptor.
//
// Adding a surface is therefore: write a descriptor, write its adapters,
// register both. Nothing in this package, in activation, in the API layer,
// or in the console's routing changes. registry_extensibility_test.go
// proves that by registering a fixture surface with different
// prerequisites, no artifacts, and a different verification signal, and
// driving it to enrolled without touching any of those.
//
// The one rule connecting this package to activation runs one way:
// enrollment cannot progress past `available` unless the store is
// UCPStateActive. Surfaces never gate each other, and nothing here ever
// writes activation state.
package surface

import (
	"context"
	"fmt"
	"sort"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/activation"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// CheckContext is everything a prerequisite check may look at. It is
// deliberately a fixed, store-scoped struct rather than an open bag: a
// check that needs something not here needs its own narrow reader injected
// at construction, which keeps the dependency visible.
type CheckContext struct {
	MerchantID domain.MerchantID
	StoreID    domain.StoreID
	// Connectors are every connector linked to the store, of any type.
	Connectors []domain.Connector
	// Capabilities are the capability identifiers the store declares.
	Capabilities []string
	// Config is the merchant-supplied per-surface configuration
	// (PUT .../surfaces/{surface_id}/config).
	Config map[string]string
}

// PrerequisiteCheck evaluates one named admission requirement for one
// store. Implementations are per-surface and are free to consult whatever
// they were constructed with; what they may not do is depend on another
// surface's state.
//
// A check reports pass/fail with a merchant-readable reason and, where one
// exists, a remediation the merchant can act on. Checks are re-evaluated on
// demand (R10) — a merchant who fixes a return policy re-runs them without
// restarting enrollment — so implementations must be side-effect free and
// safe to call repeatedly.
type PrerequisiteCheck interface {
	// ID is the name the descriptor references.
	ID() string
	Evaluate(ctx context.Context, cc CheckContext) domain.PrerequisiteResult
}

// ArtifactProvider produces one Sarvex-generated, surface-scoped artifact
// for a store. Artifacts exist only for stores enrolling in the owning
// surface: no store outside that enrollment gets one generated, and no
// console surface mentions one (R12).
type ArtifactProvider interface {
	ID() string
	// Artifact returns the artifact's current state. A nil return means
	// "not generated yet", which is a normal condition, not an error.
	Artifact(ctx context.Context, cc CheckContext) (*domain.SurfaceArtifact, error)
}

// VerificationSignal recognises the evidence that a store's enrollment on
// one surface actually succeeded — Google's first feed fetch, another
// surface's callback, a fixture's flag. Enrollment sits in `verifying`
// until this reports true.
type VerificationSignal interface {
	// ID is the name the descriptor references.
	ID() string
	// Verified reports whether the signal has been observed for this
	// store. Polled when the console asks and when enrollment is
	// re-evaluated; implementations must be cheap and side-effect free.
	Verified(ctx context.Context, cc CheckContext) (bool, error)
}

// Registration is a descriptor plus the adapters that make it work.
type Registration struct {
	Descriptor   domain.SurfaceDescriptor
	Checks       []PrerequisiteCheck
	Artifacts    []ArtifactProvider
	Verification VerificationSignal
}

// Registry holds every known surface. It is read-mostly: registration
// happens once at construction, lookups happen per request.
type Registry struct {
	byID  map[string]Registration
	order []string
}

// NewRegistry builds a registry from the given registrations, validating
// each. An invalid descriptor is a programming error caught at boot rather
// than a surface that silently never admits anyone.
func NewRegistry(regs ...Registration) (*Registry, error) {
	r := &Registry{byID: make(map[string]Registration, len(regs))}
	for _, reg := range regs {
		if err := r.Register(reg); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Register adds one surface. Validation covers the things that would
// otherwise fail silently at request time: a duplicate id, a capability
// identifier outside the canonical set, a descriptor naming a check,
// artifact or verification signal with no adapter behind it.
func (r *Registry) Register(reg Registration) error {
	d := reg.Descriptor
	if d.SurfaceID == "" {
		return fmt.Errorf("surface: descriptor has no surface_id")
	}
	if _, dup := r.byID[d.SurfaceID]; dup {
		return fmt.Errorf("surface: duplicate surface_id %q", d.SurfaceID)
	}
	if err := activation.ValidateCapabilities(d.RequiredCapabilities); err != nil {
		return fmt.Errorf("surface %q: %w", d.SurfaceID, err)
	}

	if err := validateSchema(d.SurfaceID, d.ConfigSchema); err != nil {
		return err
	}
	if d.ConfigSchema == nil {
		// Serialise as [] rather than null, like the console expects of
		// every list on the descriptor.
		reg.Descriptor.ConfigSchema = []domain.SurfaceConfigField{}
		d = reg.Descriptor
	}

	checks := make(map[string]bool, len(reg.Checks))
	for _, c := range reg.Checks {
		checks[c.ID()] = true
	}
	for _, name := range d.PrerequisiteChecks {
		if !checks[name] {
			return fmt.Errorf("surface %q: prerequisite check %q has no adapter", d.SurfaceID, name)
		}
	}

	artifacts := make(map[string]bool, len(reg.Artifacts))
	for _, a := range reg.Artifacts {
		artifacts[a.ID()] = true
	}
	for _, name := range d.Artifacts {
		if !artifacts[name] {
			return fmt.Errorf("surface %q: artifact %q has no provider", d.SurfaceID, name)
		}
	}

	if d.VerificationSignal != "" {
		if reg.Verification == nil {
			return fmt.Errorf("surface %q: verification signal %q has no adapter", d.SurfaceID, d.VerificationSignal)
		}
		if reg.Verification.ID() != d.VerificationSignal {
			return fmt.Errorf("surface %q: verification adapter is %q, descriptor names %q",
				d.SurfaceID, reg.Verification.ID(), d.VerificationSignal)
		}
	}

	r.byID[d.SurfaceID] = reg
	r.order = append(r.order, d.SurfaceID)
	sort.Strings(r.order)
	return nil
}

// Get returns one registration.
func (r *Registry) Get(surfaceID string) (Registration, bool) {
	reg, ok := r.byID[surfaceID]
	return reg, ok
}

// Descriptors returns every registered descriptor, in stable id order.
func (r *Registry) Descriptors() []domain.SurfaceDescriptor {
	out := make([]domain.SurfaceDescriptor, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.byID[id].Descriptor)
	}
	return out
}

// IDs returns every registered surface id, in stable order.
func (r *Registry) IDs() []string {
	return append([]string(nil), r.order...)
}

// AutoEnrollIDs returns the surfaces that enroll themselves when a store
// reaches active.
func (r *Registry) AutoEnrollIDs() []string {
	var out []string
	for _, id := range r.order {
		if r.byID[id].Descriptor.AutoEnroll {
			out = append(out, id)
		}
	}
	return out
}
