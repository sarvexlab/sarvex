// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package webbaseline syncs the Channel Dashboard's web-channel baseline
// (R10-R12): the merchant's non-agentic commerce performance, sourced from
// the connector's own order/analytics data — never from Sarvex's own
// traffic, since Sarvex never sees a merchant's web sessions. Runs in the
// sync plane on a schedule, off the checkout hot path (R11).
//
// ── R10's per-connector capability audit ──
//
// internal/pkg/ucp/connectors/base.Connector (the interface every store
// connector — woocommerce, magento — implements) exposes no bulk order or
// analytics-reporting method: OrderGet fetches exactly one order by its
// platform-native id (used for post-completion status polling), and
// nothing else on that interface can answer "how many orders / how much
// revenue did this store's *own* platform report for date D." Neither the
// WooCommerce nor the Magento connector implements anything beyond
// base.Connector today (confirmed by reading
// internal/platform/woocommerce and .../magento in full — no
// reports/orders-list client method exists in either).
//
// Building that capability for real (e.g. WooCommerce's wc/v3/reports/*
// or orders-list REST surface) would mean guessing at scopes and response
// shapes this codebase has never exercised or tested against a live
// store — a correctness risk R12's "never substitute an estimate" rule
// exists specifically to guard against. Source below is the seam that
// capability plugs into once a connector actually implements it (verified
// by TestSurfaceEnumExtension-style additive-capability tests); until
// then, Syncer honestly writes confidence=unavailable for every
// store/day rather than fabricating a number — see syncer.go.
package webbaseline

import "time"

// Confidence is R11's closed tri-state for how much a BaselineDay's
// numbers can be trusted.
type Confidence string

const (
	// ConfidenceConnectorReported: the connector supplied a real,
	// platform-reported order count/revenue for this day.
	ConfidenceConnectorReported Confidence = "connector_reported"
	// ConfidencePartial: the connector supplied data for only part of the
	// day, or with a caveat the source flagged.
	ConfidencePartial Confidence = "partial"
	// ConfidenceUnavailable: no connector-reported data exists for this
	// store/day — R12 requires the comparison API to report this
	// honestly, never estimate a substitute.
	ConfidenceUnavailable Confidence = "unavailable"
)

// BaselineDay is R11's document, stored at
// merchants/{merchant_id}/stores/{store_id}/sync_web_baseline_daily/{yyyy_mm_dd}.
// Same path-convention-not-literal-Firestore note as
// internal/planes/control/ledger.Entry — every document carries
// merchant_id/store_id as fields.
type BaselineDay struct {
	ID         string `bson:"_id"` // storeID + "__" + date
	MerchantID string `bson:"merchant_id"`
	StoreID    string `bson:"store_id"`
	Date       string `bson:"date"` // yyyy_mm_dd UTC

	Orders       int64  `bson:"orders"`
	RevenueMinor int64  `bson:"revenue_minor"`
	AOVMinor     int64  `bson:"aov_minor"` // zero-safe, stored (not derived) per R11's literal schema
	Currency     string `bson:"currency,omitempty"`

	// Source is the connector id (platform name, e.g. "woocommerce") this
	// day's data came from — or was attempted from, when Confidence is
	// unavailable (R10/R12's audit: knowing *which* connector can't
	// supply the data yet is itself useful signal, not noise).
	Source     string     `bson:"source"`
	Confidence Confidence `bson:"confidence"`
	SyncedAt   time.Time  `bson:"synced_at"`
}

// computeAOV is R11/R7's zero-safe AOV, minor units only.
func computeAOV(revenueMinor, orders int64) int64 {
	if orders == 0 {
		return 0
	}
	return revenueMinor / orders
}
