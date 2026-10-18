// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"encoding/json"
	"time"
)

// Severity classifies the urgency of a finding.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// HealthStatementKind identifies what the statement covers.
type HealthStatementKind string

const (
	HealthConsistency      HealthStatementKind = "consistency"
	HealthSemanticEndpoint HealthStatementKind = "semantic_endpoint"
	HealthSyncFreshness    HealthStatementKind = "sync_freshness"
	HealthProfileEdge      HealthStatementKind = "profile_edge"
)

// HealthStatement is a single semantic health assertion about the integration.
//
// Message is server-rendered English, kept as the fallback for any client
// that has no copy of its own for a given kind. Params carries the same
// facts as data so a client can render its own sentence: the console needs
// them to pluralize properly, to format a duration as "about 2 hours"
// rather than Go's "2h0m58.304129489s", and to say what the merchant should
// do about it — none of which is expressible in a pre-rendered string.
type HealthStatement struct {
	Kind      HealthStatementKind `json:"kind"`
	OK        bool                `json:"ok"`
	Message   string              `json:"message"`
	Params    map[string]any      `json:"params,omitempty"`
	CheckedAt time.Time           `json:"checked_at"`
}

// Health is the collection of health statements for a merchant's integration.
type Health struct {
	Statements []HealthStatement `json:"statements"`
}

// ConsistencyFinding records a price/stock/data divergence across sources.
// Carries bson tags (in addition to json) because it's persisted directly
// as a sub-document on TaskModel.Finding (see task/model.go) — without
// them, the mongo driver falls back to its default lowercase-no-separator
// field names, which don't match the snake_case convention used everywhere
// else in this codebase's Mongo documents.
type ConsistencyFinding struct {
	FindingID  string            `json:"finding_id" bson:"finding_id"`
	SKU        string            `json:"sku" bson:"sku"`
	Field      string            `json:"field" bson:"field"`
	Sources    map[string]string `json:"sources" bson:"sources"`
	Severity   Severity          `json:"severity" bson:"severity"`
	DetectedAt time.Time         `json:"detected_at" bson:"detected_at"`
}

// Presence is the plain-language summary of a merchant's UCP hosting.
type Presence struct {
	Summary               string            `json:"summary"`
	ProductCount          int               `json:"product_count"`
	Capabilities          []string          `json:"capabilities"`
	FreshnessDeclarations map[string]string `json:"freshness_declarations"`
	Serving               ServingState      `json:"serving"`
	ProfileURL            string            `json:"profile_url"`
	ProfileReachable      bool              `json:"profile_reachable"`
	// PublishedVersion is the single UCP spec version currently rendered
	// into this store's /.well-known/ucp document (profile.UCP.Version) —
	// the ucp.dev spec models exactly one declared version per profile
	// document, so this is never a list.
	PublishedVersion string `json:"published_version,omitempty"`
	// SupportedVersions are the UCP spec generations Sarvex's negotiation
	// engine can actually select as the response version for this store
	// today (the spec registry's servable generations — see
	// sarvex-v1-architecture.md §D R3). Oldest first. A platform agent may
	// negotiate this store into any of these, not just PublishedVersion —
	// PublishedVersion is only what the static discovery document itself
	// declares.
	SupportedVersions []string `json:"supported_versions,omitempty"`
}

// ServingState describes the operational mode of the serving plane for a merchant.
type ServingState string

const (
	ServingStateActive      ServingState = "serving"
	ServingStateDegraded    ServingState = "degraded"
	ServingStateProfileOnly ServingState = "profile_only"
	ServingStateNotServing  ServingState = "not_serving"
)

// ManifestCache describes the state of the cached serving snapshot.
type ManifestCache struct {
	BuiltAt         time.Time `json:"built_at"`
	TTLSeconds      int       `json:"ttl_seconds"`
	Stale           bool      `json:"stale"`
	SourceWatermark time.Time `json:"source_watermark"`
}

// ManifestView is the raw serving manifest plus its cache state. Manifest is
// json.RawMessage, not map[string]any: the producer (GetManifest) already
// has the manifest as marshalled JSON bytes from the serving cache, and this
// type exists only to be marshalled again into the HTTP response — decoding
// to a map and re-encoding it would pay two extra JSON passes to reproduce
// the exact same bytes. json.RawMessage is copied verbatim into the response
// instead of being reflected over.
type ManifestView struct {
	Manifest json.RawMessage `json:"manifest"`
	Cache    ManifestCache   `json:"cache"`
}
