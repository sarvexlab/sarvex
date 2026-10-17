// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package channel is the Channel Dashboard: it consumes checkout lifecycle
// events and per-request telemetry to build agent sessions and a per-
// store/per-surface/per-day funnel rollup, and serves both (via
// pre-computed aggregates only, never a raw scan — R14) as a read-only
// control-plane API. Extends the checkout-plane's existing per-store
// revenue/telemetry event stream — it introduces no new event channels.
//
// Like the Lost Revenue Ledger (internal/planes/control/ledger) this
// package deliberately does not import internal/planes/serving: every
// event field it needs arrives as JSON on the bus, and Surface here is an
// independent closed-string-enum copy of serving.Surface's values, not an
// import of that type — importing the serving plane from a control-plane
// package would violate the plane-isolation invariant (sarvex-v1-architecture.md A10.1,
// enforced the same way checkoutescalation's isolation_test.go enforces it
// for that package). See consumer_test.go's own import-boundary test for
// the same check applied here.
package channel

import "time"

// Surface is the closed multi-surface enum (R8): which agent platform
// drove a session or day's traffic. Values mirror
// internal/planes/serving.Surface exactly (string-for-string) since both
// are populated from the same UCP-Agent-header-derived classification —
// see that package's deriveSurface for the classification rule and its
// doc comment for why SurfaceOpenAIChatGPT is defined but never actually
// produced today (no signal in this codebase distinguishes ChatGPT/ACP
// traffic from any other non-Google agent yet).
//
// R9: every aggregate is keyed by Surface, and adding a future member
// requires no schema migration — DailyAggregate/Session store Surface as
// a plain string field with no enum constraint at the Mongo layer;
// TestSurfaceEnumExtension_NoMigrationRequired proves a new member folds
// into existing rollups without a migration.
type Surface string

const (
	SurfaceGoogleAIMode  Surface = "google_ai_mode"
	SurfaceGoogleGemini  Surface = "google_gemini"
	SurfaceOpenAIChatGPT Surface = "openai_chatgpt"
	SurfaceOtherAgent    Surface = "other_agent"
	SurfaceUnknown       Surface = "unknown"
)

// SurfaceAll is the sentinel query value meaning "every surface, combined"
// — used by the read API's summary/comparison endpoints (R13) to
// distinguish an explicit per-surface filter from the all-surfaces
// rollup. Never stored: DailyAggregate.Surface is always one real Surface
// value.
const SurfaceAll = "all"

// Attribution distinguishes a session whose correlation to its request
// stream is protocol-guaranteed (Confirmed) from one this worker had to
// partially reconstruct (Inferred) — R2. The dashboard must never present
// an Inferred session as exact.
//
// This package's correlation key is exclusively the explicit
// checkout_session_id the versioned checkout lifecycle contract already
// carries on every event (sarvex-v1-syncing-plane-async.yaml,
// event_version "1.0") — precedence tier (a) in R1's ordering. Tiers (b)
// and (c) were evaluated and rejected: see consumer.go's package doc
// comment for the evidence (the negotiation-context identity available on
// the wire — the calling platform's profile URL host — identifies the
// *platform*, e.g. "all of Gemini", not a buyer or session, so keying on
// it would conflate unrelated buyers into one fabricated session; and no
// derived key survives that same conflation risk, since nothing on the
// wire distinguishes two different buyers calling the same store over the
// same surface). Consequently every Session this package ever builds is
// checkout-anchored and Confirmed by construction *except* for the one
// case Attribution actually exists to flag: this worker's own first
// observation of a given checkout_session_id was not the session's
// initiating event (out-of-order delivery, or the initiating event
// predates this worker's deployment/subscription) — in that case the
// session's started_at and early request_count are reconstructed rather
// than observed, and it is marked Inferred.
//
// Pure pre-checkout discovery (catalog search/lookup with no checkout
// session ever created) has no correlation identifier on the wire at
// all — see consumer.go's doc comment — so per R2 this package does not
// invent sessions for it; DailyAggregate.DiscoveryRequests is the
// request-level metric R2 calls for instead.
type Attribution string

const (
	AttributionConfirmed Attribution = "confirmed"
	AttributionInferred  Attribution = "inferred"
)

// Session is one agent checkout session (R3), stored at
// merchants/{merchant_id}/control_agent_sessions/{session_id} (a Mongo document
// carrying merchant_id as a field, same path-convention-not-literal-
// Firestore note as ledger.Entry). session_id is the checkout session's
// own id (R1's explicit correlation identifier) — never a synthetic id.
//
// No PII (R4): no buyer identifier, no IP address, no raw user agent —
// only the Surface/AgentPlatform classification already derived
// server-side for the lifecycle event contract.
type Session struct {
	SessionID  string `bson:"_id"`
	MerchantID string `bson:"merchant_id"`
	StoreID    string `bson:"store_id"`

	Surface       Surface `bson:"surface"`
	AgentPlatform string  `bson:"agent_platform,omitempty"`

	StartedAt time.Time  `bson:"started_at"`
	EndedAt   *time.Time `bson:"ended_at,omitempty"`

	RequestCount int64 `bson:"request_count"`

	// ProductsViewedCount is always 0 today — deliberately. No telemetry
	// correlates a pre-checkout catalog search/lookup request to a
	// checkout_session_id (discovery is a stateless, cache-only read path
	// per architecture invariant A10.2; it carries no session
	// identifier), so there is no honest signal for "products viewed
	// during this session" once a session is checkout-anchored (R1's
	// finding). Populating this from the session's cart contents instead
	// would silently relabel "added to cart" as "viewed", which is a
	// different, narrower signal — exactly the kind of invented precision
	// R2/the hard rules forbid. The field is kept for schema forward-
	// compatibility (R3's document shape) and reported as a deliberately-
	// unsupported metric rather than removed or guessed.
	ProductsViewedCount int64 `bson:"products_viewed_count"`

	// ReachedCart is set once the session has at least one successful
	// cart/buyer/fulfillment update (serving.checkout.updated) beyond its
	// initial creation.
	ReachedCart bool `bson:"reached_cart"`
	// ReachedCheckout is set once the session has submitted payment
	// (serving.checkout.payment_submitted) — the genuine, non-trivial
	// funnel gate distinct from merely creating a checkout session object
	// (every Session here is checkout-anchored, so "created" alone would
	// be tautological — see Attribution's doc comment).
	ReachedCheckout bool `bson:"reached_checkout"`
	Completed       bool `bson:"completed"`

	RevenueMinor int64  `bson:"revenue_minor"`
	Currency     string `bson:"currency,omitempty"`

	Attribution Attribution `bson:"attribution"`

	// Date is StartedAt truncated to yyyy_mm_dd UTC — the DailyAggregate
	// partition this session folds its funnel contribution into. Stored
	// (not recomputed) so a session late-arriving events fold into the
	// same daily bucket its started_at determined, even if those events
	// are processed on a later wall-clock day.
	Date string `bson:"date"`

	// ProcessedEventIDs dedups lifecycle events already folded into this
	// session (A1: duplicate/out-of-order delivery must be idempotent).
	// Bounded: a single checkout session emits at most a handful of
	// lifecycle events, unlike the high-volume telemetry stream (see
	// mongo_store.go's separate dedup collection for that one).
	ProcessedEventIDs []string `bson:"processed_event_ids"`

	CreatedAt time.Time `bson:"created_at"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// DailyAggregate is the funnel/revenue rollup at
// merchants/{merchant_id}/stores/{store_id}/control_channel_daily/{date}__{surface}
// (R5), keyed by {store_id, surface, date} per R5's literal key — the
// document also carries merchant_id/currency as fields, same path-
// convention note as Session. All reads (R13/R14) serve from this
// document and WebBaselineDay only — never a raw scan of Session or
// events.
//
// Every count is a plain integer; every rate the read API returns is
// derived at read time from these counts (R5/R7) — nothing here is ever a
// stored percentage or a float.
type DailyAggregate struct {
	ID         string  `bson:"_id"` // storeID + "__" + surface + "__" + date
	MerchantID string  `bson:"merchant_id"`
	StoreID    string  `bson:"store_id"`
	Surface    Surface `bson:"surface"`
	Date       string  `bson:"date"` // yyyy_mm_dd UTC
	Currency   string  `bson:"currency,omitempty"`

	// ─── Funnel stage counts (R5) — monotonically non-increasing through
	// the funnel for any generated stream (A3): Sessions >=
	// SessionsReachedCart >= SessionsReachedCheckout >= CheckoutsCompleted.
	// CheckoutsInitiated always equals Sessions by construction — every
	// Session here is checkout-anchored (see Attribution's doc comment) —
	// kept as its own counter because R5 names it explicitly and to keep
	// the funnel's stage names self-documenting for the read API.
	Sessions                int64 `bson:"sessions"`
	SessionsReachedCart     int64 `bson:"sessions_reached_cart"`
	SessionsReachedCheckout int64 `bson:"sessions_reached_checkout"`
	CheckoutsInitiated      int64 `bson:"checkouts_initiated"`
	CheckoutsCompleted      int64 `bson:"checkouts_completed"`

	// SessionsInferred is the subset of Sessions marked
	// Attribution=inferred (R2) — the read API must surface this
	// alongside Sessions so a merchant never mistakes a reconstructed
	// count for an exact one.
	SessionsInferred int64 `bson:"sessions_inferred"`

	RevenueMinor int64 `bson:"revenue_minor"`

	// DiscoveryRequests is R2's fallback request-level metric for traffic
	// this package cannot sessionize (pre-checkout catalog search/lookup
	// — no correlation identifier exists for it, see Session's doc
	// comment). It is a request count, never presented as a session
	// count.
	DiscoveryRequests int64 `bson:"discovery_requests"`

	UpdatedAt time.Time `bson:"updated_at"`
}

// DailyAggregateID builds DailyAggregate's document id.
func DailyAggregateID(storeID string, surface Surface, date string) string {
	return storeID + "__" + string(surface) + "__" + date
}

// DateKey truncates t to the yyyy_mm_dd UTC bucket DailyAggregate/Session
// partition on.
func DateKey(t time.Time) string {
	return t.UTC().Format("2006_01_02")
}

// FunnelRates are R5/R7's derived-at-read-time conversion rates and AOV —
// never stored. Zero-safe: a zero denominator yields a zero rate rather
// than dividing by zero (R7).
type FunnelRates struct {
	CartRate       float64 `json:"cart_rate"`       // sessions_reached_cart / sessions
	CheckoutRate   float64 `json:"checkout_rate"`   // sessions_reached_checkout / sessions_reached_cart
	CompletionRate float64 `json:"completion_rate"` // checkouts_completed / checkouts_initiated
	AOVMinor       int64   `json:"aov_minor"`       // revenue_minor / checkouts_completed
}

func safeRate(numerator, denominator int64) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

// ComputeFunnelRates derives FunnelRates from a's stored counts.
func ComputeFunnelRates(a DailyAggregate) FunnelRates {
	aov := int64(0)
	if a.CheckoutsCompleted > 0 {
		aov = a.RevenueMinor / a.CheckoutsCompleted
	}
	return FunnelRates{
		CartRate:       safeRate(a.SessionsReachedCart, a.Sessions),
		CheckoutRate:   safeRate(a.SessionsReachedCheckout, a.SessionsReachedCart),
		CompletionRate: safeRate(a.CheckoutsCompleted, a.CheckoutsInitiated),
		AOVMinor:       aov,
	}
}
