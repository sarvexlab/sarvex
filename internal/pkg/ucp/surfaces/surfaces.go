// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package surfaces holds the data contract shared by every plane that takes
// part in an agent surface: surface ids and the record a surface's
// Sarvex-generated artifact leaves behind.
//
// The planes never import one another (CLAUDE.md §2.1), so anything more than
// one of them has to agree on lives here, as data. A surface's behaviour does
// not: its control-plane half lives under internal/planes/control/surface/<id>
// and its sync-plane half under internal/planes/sync/surface/<id>.
package surfaces

import "time"

// Surface ids. A surface is named here once so that every plane's module for
// it, and the catalogue content scoped to it, agree on the string.
const (
	// DirectAgent is the zero-config surface every active store is on.
	DirectAgent = "direct_agent"
)

// All lists every surface id. internal/archtest uses it to confine each id to
// its own modules, so a new surface adds its const above and its line here.
var All = []string{DirectAgent}

// CollectionArtifacts is the sync plane's per-store, per-surface artifact
// collection. The sync plane is its one writer; the control plane reads it
// by name, read-only (CLAUDE.md §2.1), to show an artifact and to recognise
// a verification signal.
const CollectionArtifacts = "sync_surface_artifacts"

// ArtifactDocumentID is the composite _id of one artifact record. Tenant
// scope is carried by the merchant_id/store_id fields too; every read filters
// on both (ADR-2).
func ArtifactDocumentID(merchantID, storeID, surfaceID, artifactID string) string {
	return merchantID + ":" + storeID + ":" + surfaceID + ":" + artifactID
}

// Signal names a fact the sync plane observed about an artifact. A surface's
// verification adapter on the control plane asks whether one was seen.
type Signal string

// SignalFirstFetch is recorded the first time the surface's fetcher reads the
// artifact's public URL.
const SignalFirstFetch Signal = "first_fetch"

// ArtifactRecord is the stored form of one generated artifact.
type ArtifactRecord struct {
	ID         string `bson:"_id"`
	MerchantID string `bson:"merchant_id"`
	StoreID    string `bson:"store_id"`
	SurfaceID  string `bson:"surface_id"`
	ArtifactID string `bson:"artifact_id"`
	// Token is the unguessable public handle the artifact is served under.
	// It is not a tenant identifier: the serving plane resolves it to the
	// artifact's bytes only, and the tenant is read back from the record.
	Token string `bson:"token"`
	URL   string `bson:"url"`
	// Detail is read-only display facts (counts), rendered generically.
	Detail         map[string]string `bson:"detail,omitempty"`
	GeneratedAt    *time.Time        `bson:"generated_at,omitempty"`
	LastFetchedAt  *time.Time        `bson:"last_fetched_at,omitempty"`
	FirstFetchedAt *time.Time        `bson:"first_fetched_at,omitempty"`
	UpdatedAt      time.Time         `bson:"updated_at"`
}

// Observed reports whether the sync plane has seen the signal.
func (r ArtifactRecord) Observed(s Signal) bool {
	switch s {
	case SignalFirstFetch:
		return r.FirstFetchedAt != nil
	default:
		return false
	}
}
