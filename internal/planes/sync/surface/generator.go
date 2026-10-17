// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package surface is the sync plane's half of an agent surface: it generates
// the artifacts a surface needs and records what it observes about them.
//
// The control plane decides who is enrolled on a surface; this package never
// does. It reads control_store_surfaces (read-only, by name — CLAUDE.md §2.1)
// to learn which stores are on which surface, generates only for those, and
// writes the result to sync_surface_artifacts, which it alone writes. The
// control plane reads that collection back to show the artifact and to
// recognise the surface's verification signal.
//
// Adding a surface's artifact is a Generator and one line in the sync
// plane's surface registration. Nothing here names a surface.
package surface

import "context"

// Product is the subset of a store's catalog a generator may need.
type Product struct {
	SKU string
}

// Input is everything a generator may look at, scoped to one store.
type Input struct {
	MerchantID string
	StoreID    string
	// Config is the merchant-supplied per-surface configuration, as the
	// control plane stored it on the enrollment.
	Config   map[string]string
	Products []Product
	// URL is the address the artifact will be served at, for generators whose
	// document refers to itself.
	URL string
}

// Output is a generated artifact.
type Output struct {
	ContentType string
	Body        []byte
	// Detail is read-only display facts the console shows as label/value
	// rows (counts, for instance). Values are strings.
	Detail map[string]string
}

// Generator produces one surface artifact for a store.
type Generator interface {
	// SurfaceID is the surface the artifact belongs to; a store is only
	// generated for while enrolled on it.
	SurfaceID() string
	// ArtifactID names the artifact within the surface; it matches the
	// control-plane descriptor's artifact id.
	ArtifactID() string
	// Extension is appended to the artifact's public URL (".xml"). May be
	// empty.
	Extension() string
	// Generate renders the artifact. It must be deterministic for a given
	// Input and free of side effects: it is re-run on every catalog change.
	Generate(ctx context.Context, in Input) (Output, error)
}
