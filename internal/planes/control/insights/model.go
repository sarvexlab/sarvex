// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package insights is Agent Behavior Insights: it consumes
// serving.catalog.query_recorded and the ucp.negotiation.succeeded/failed
// events (internal/pkg/ucp/negotiation) and maintains per-store daily/
// monthly aggregates — what agents asked for, where negotiation failed,
// and which capabilities agents requested that the merchant doesn't
// declare — served read-only via the control-plane API.
//
// Like the Lost Revenue Ledger (internal/planes/control/ledger) and the
// Channel Dashboard (internal/planes/control/channel), this package
// deliberately does not import internal/planes/serving: every field it
// needs arrives as JSON on the bus, and Surface here is an independent
// closed-string-enum copy of serving.Surface's values, not an import of
// that type (see isolation_test.go).
//
// Two requirements from the original task spec are NOT implemented here,
// by design, because the telemetry to support them honestly does not
// exist — see this package's audit findings (reported alongside the PR
// that introduced it):
//
//   - Attribute-demand (R3): the UCP request types have no protocol-level
//     way to name a requested attribute, so there is nothing honest to
//     emit or aggregate. No attribute_gaps collection, no read endpoint.
//   - Comparison/drop analysis (R4) and per-product cart/completion
//     funnel counts (part of R2): CatalogQueryRecordedEvent carries no
//     session/visit correlation id (only store_id, surface,
//     agent_platform) — exactly the same correlation gap the Channel
//     Dashboard already documented and worked around for pre-checkout
//     discovery traffic (see channel.Session's ProductsViewedCount doc
//     comment). No co_queried_pairs, no drop_after_query_count, no
//     reached_cart_count/completed_count on product demand.
package insights

import (
	"strings"
	"time"
)

// Surface is the closed agent-surface enum, string-for-string identical to
// internal/planes/serving.Surface and internal/planes/control/channel.Surface
// — an independent copy per the plane-isolation invariant, not an import.
type Surface string

const (
	SurfaceGoogleAIMode Surface = "google_ai_mode"
	SurfaceGoogleGemini Surface = "google_gemini"
	SurfaceOtherAgent   Surface = "other_agent"
	SurfaceUnknown      Surface = "unknown"
)

// deriveSurface classifies a raw agent-platform host string. Mirrors
// internal/planes/serving.deriveSurface's rule exactly (case-insensitive
// substring match on "gemini" then "google"), duplicated rather than
// imported for the same reason internal/planes/control/channel duplicates
// it — this is a control-plane package and must never import a serving-
// plane package (CLAUDE.md §2.1).
func deriveSurface(agentPlatformHost string) Surface {
	host := strings.ToLower(agentPlatformHost)
	switch {
	case host == "":
		return SurfaceUnknown
	case strings.Contains(host, "gemini"):
		return SurfaceGoogleGemini
	case strings.Contains(host, "google"):
		return SurfaceGoogleAIMode
	default:
		return SurfaceOtherAgent
	}
}

// DateKey truncates t to the yyyy_mm_dd UTC bucket daily aggregates
// partition on.
func DateKey(t time.Time) string {
	return t.UTC().Format("2006_01_02")
}

// MonthKey truncates t to the yyyy_mm UTC bucket capability_gaps
// partitions on.
func MonthKey(t time.Time) string {
	return t.UTC().Format("2006_01")
}

// ── R2: product demand ──────────────────────────────────────────────────

// ProductDemandDaily is one product's daily agent-request count at
// merchants/{merchant_id}/stores/{store_id}/control_product_demand_daily/{date}/{product_id}
// (a Mongo document carrying merchant_id/store_id as fields — path
// convention only, not literal Firestore, same note as ledger.Entry).
//
// Deliberately has no reached_cart_count/completed_count field: those
// require correlating a specific catalog-query request to a specific
// later checkout, which CatalogQueryRecordedEvent's payload cannot
// support (see package doc comment). request_count and SurfaceBreakdown
// are the only claims this aggregate makes.
type ProductDemandDaily struct {
	ID               string           `bson:"_id"` // storeID__date__productID
	MerchantID       string           `bson:"merchant_id"`
	StoreID          string           `bson:"store_id"`
	Date             string           `bson:"date"` // yyyy_mm_dd UTC
	ProductID        string           `bson:"product_id"`
	RequestCount     int64            `bson:"request_count"`
	SurfaceBreakdown map[string]int64 `bson:"surface_breakdown"`
	UpdatedAt        time.Time        `bson:"updated_at"`
}

func productDemandID(storeID, date, productID string) string {
	return storeID + "__" + date + "__" + productID
}

// ── R5/R6: negotiation ──────────────────────────────────────────────────

// NegotiationFailureReason is the closed enum negotiation.Negotiate
// actually produces (internal/pkg/ucp/negotiation/service.go's Negotiate —
// see its step 6 and step 8). Derived from code, not invented: these are
// the only two strings NewNegotiationFailedEvent is ever called with.
type NegotiationFailureReason string

const (
	NegotiationFailureVersionUnsupported       NegotiationFailureReason = "version_unsupported"
	NegotiationFailureCapabilitiesIncompatible NegotiationFailureReason = "capabilities_incompatible"
)

// NegotiationFailureBreakdown is one failure_reason's contribution to a
// day's control_negotiation_daily rollup (R5).
//
// MissingCapabilityCounts is only ever populated for
// NegotiationFailureCapabilitiesIncompatible: that is the one failure
// reason where the cause literally is a capability-set delta (Negotiate's
// step 8: the intersection of every declared/requested capability is
// empty). version_unsupported fails on the UCP version field itself, not
// a capability delta — populating this map there would misrepresent an
// incidental set difference as "the delta that caused it", so it is left
// nil for that reason (R5: "per-failure the requested-vs-supported delta
// that caused it").
type NegotiationFailureBreakdown struct {
	Reason                  NegotiationFailureReason `bson:"reason"`
	Count                   int64                    `bson:"count"`
	MissingCapabilityCounts map[string]int64         `bson:"missing_capability_counts,omitempty"`
}

// NegotiationDaily is the negotiation outcome rollup at
// merchants/{merchant_id}/stores/{store_id}/control_negotiation_daily/{date} (R5).
type NegotiationDaily struct {
	ID         string                        `bson:"_id"` // storeID__date
	MerchantID string                        `bson:"merchant_id"`
	StoreID    string                        `bson:"store_id"`
	Date       string                        `bson:"date"`
	Attempts   int64                         `bson:"attempts"`
	Successes  int64                         `bson:"successes"`
	Failures   []NegotiationFailureBreakdown `bson:"failures"`
	UpdatedAt  time.Time                     `bson:"updated_at"`
}

func negotiationDailyID(storeID, date string) string {
	return storeID + "__" + date
}

// PaymentHandlerMismatchDaily is R6's dedicated aggregate: how often the
// agent's offered payment-handler set and the merchant's declared set had
// no overlap ("no_common_payment_handler"), and which handlers were
// offered. Computed observationally from CapabilitySets on EVERY
// negotiation event (succeeded or failed) — per
// internal/pkg/ucp/negotiation/events.go's CapabilitySets doc comment, an
// empty payment-handler intersection does NOT fail negotiation today, so
// this condition is invisible in NegotiationDaily.Failures and needs its
// own rollup (R6: "should be its own aggregate, not buried in a generic
// failure count").
type PaymentHandlerMismatchDaily struct {
	ID         string `bson:"_id"` // storeID__date
	MerchantID string `bson:"merchant_id"`
	StoreID    string `bson:"store_id"`
	Date       string `bson:"date"`

	// Occurrences is the count of negotiation events where the platform
	// offered at least one payment handler and none of them were in the
	// merchant's declared set.
	Occurrences int64 `bson:"occurrences"`
	// OfferedHandlerCounts counts, per handler name, how many of those
	// occurrences offered it — e.g. "agents offered PayPal 210 times".
	OfferedHandlerCounts map[string]int64 `bson:"offered_handler_counts"`
	// MerchantDeclaredHandlers is the merchant's declared payment-handler
	// set as observed on the most recent occurrence folded into this
	// document (a snapshot, not a count — the declared set doesn't vary
	// per request the way offered handlers do).
	MerchantDeclaredHandlers []string  `bson:"merchant_declared_handlers"`
	UpdatedAt                time.Time `bson:"updated_at"`
}

func paymentHandlerMismatchID(storeID, date string) string {
	return storeID + "__" + date
}

// ── R8-R11: capability gaps ─────────────────────────────────────────────

// ImpactConfidence is R9's closed confidence enum for a capability gap's
// estimated_impact_minor.
type ImpactConfidence string

const (
	// ImpactMeasured: every session counted toward estimated_impact_minor
	// had a cart snapshot demonstrably preceded by the missing capability.
	ImpactMeasured ImpactConfidence = "measured"
	// ImpactPartial: some but not all counted sessions had a cart snapshot.
	ImpactPartial ImpactConfidence = "partial"
	// ImpactUnmeasurable: no session could be demonstrably attributed —
	// estimated_impact_minor is 0, never a model output (R9's hard rule).
	ImpactUnmeasurable ImpactConfidence = "unmeasurable"
)

// CapabilityGapMonthly is R8's headline aggregate at
// merchants/{merchant_id}/stores/{store_id}/capability_gaps/{yyyy_mm}/{capability_id}.
// capability_id is always a value taken verbatim from a negotiation
// event's RequestedCapabilities (itself sourced from the wire profile's
// capability map keys — internal/spec/v20260408/wire — the same
// reverse-domain names internal/pkg/ucp/types deals in), never a
// free-form string (R8).
//
// EstimatedImpactMinor/ImpactConfidence are always
// (0, ImpactUnmeasurable) in this implementation: computing anything else
// requires correlating a specific negotiation attempt to a specific later
// checkout's cart snapshot, and no identifier on the wire supports that
// correlation (see package doc comment and the PR's reported findings).
// This is the honest output R9 requires when correlation doesn't exist,
// not a shortcut — never overwrite it with a heuristic.
type CapabilityGapMonthly struct {
	ID           string `bson:"_id"` // storeID__month__capabilityID
	MerchantID   string `bson:"merchant_id"`
	StoreID      string `bson:"store_id"`
	Month        string `bson:"month"` // yyyy_mm
	CapabilityID string `bson:"capability_id"`

	RequestCount int64 `bson:"request_count"`
	// DistinctPlatformURIs is the set of distinct platform_profile_uri
	// values observed requesting this capability this month — the closest
	// honest proxy this telemetry supports for "distinct calling agent"
	// (there is no per-buyer or per-conversation identity on the wire, see
	// channel.Attribution's doc comment for the same finding elsewhere).
	// len() of this is DistinctAgentCount at read time.
	DistinctPlatformURIs []string         `bson:"distinct_platform_uris"`
	SurfaceBreakdown     map[string]int64 `bson:"surface_breakdown"`

	FirstSeenAt time.Time `bson:"first_seen_at"`
	LastSeenAt  time.Time `bson:"last_seen_at"`

	EstimatedImpactMinor int64            `bson:"estimated_impact_minor"`
	ImpactConfidence     ImpactConfidence `bson:"impact_confidence"`

	// MerchantSupported is computed from the same negotiation event's
	// DeclaredCapabilities that produced this gap increment. By
	// construction this is always false — a capability only becomes part
	// of a "gap" (requested minus declared) when it is NOT in
	// DeclaredCapabilities for that event — but the field is kept
	// (rather than omitted) because the read API and any future
	// remediation-tracking need a stored, queryable flag rather than an
	// implied invariant.
	MerchantSupported bool `bson:"merchant_supported"`
	// RemediationAvailable is always false in V1: nothing in this
	// codebase lets Sarvex serve a UCP capability on a merchant's behalf
	// (no capability-hosting mechanism exists — see the PR's audit
	// findings). Every gap in this build requires merchant-side work.
	RemediationAvailable bool   `bson:"remediation_available"`
	RecommendedAction    string `bson:"recommended_action"`

	UpdatedAt time.Time `bson:"updated_at"`
}

func capabilityGapID(storeID, month, capabilityID string) string {
	return storeID + "__" + month + "__" + capabilityID
}

// recommendedActionFor builds R10's merchant-readable, no-protocol-jargon
// action string for a capability gap. Every gap in this build requires
// merchant-side work (RemediationAvailable is always false), so this is
// the only branch implemented.
func recommendedActionFor(capabilityID string) string {
	return "Agents have been asking your store to support \"" + capabilityID + "\" — declare it in your UCP profile if your store can support it."
}
