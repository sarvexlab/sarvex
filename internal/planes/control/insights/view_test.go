// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package insights_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/sarvexlab/sarvex/internal/planes/control/insights"
)

func productRow(storeID, date, productID string, count int64, surface string) insights.ProductDemandDaily {
	return insights.ProductDemandDaily{
		StoreID: storeID, Date: date, ProductID: productID, RequestCount: count,
		SurfaceBreakdown: map[string]int64{surface: count},
	}
}

// TestComputeProductDemand_TopNCorrectness is A2: the top-N product demand
// view is sorted by summed request_count descending and truncated to N.
func TestComputeProductDemand_TopNCorrectness(t *testing.T) {
	rows := []insights.ProductDemandDaily{
		productRow("store-1", "2026_08_01", "sku-a", 10, "google_gemini"),
		productRow("store-1", "2026_08_02", "sku-a", 5, "google_gemini"), // sums to 15 across two days
		productRow("store-1", "2026_08_01", "sku-b", 50, "google_ai_mode"),
		productRow("store-1", "2026_08_01", "sku-c", 8, "google_gemini"),
	}

	views := insights.ComputeProductDemand(rows, "", 0, 2)
	if assert.Len(t, views, 2) {
		assert.Equal(t, "sku-b", views[0].ProductID)
		assert.Equal(t, int64(50), views[0].RequestCount)
		assert.Equal(t, "sku-a", views[1].ProductID)
		assert.Equal(t, int64(15), views[1].RequestCount)
	}
}

// TestComputeProductDemand_SmallCountSuppression is A2: a product below
// DefaultSuppressionThreshold is dropped from the result.
func TestComputeProductDemand_SmallCountSuppression(t *testing.T) {
	rows := []insights.ProductDemandDaily{
		productRow("store-1", "2026_08_01", "sku-a", 10, "google_gemini"),
		productRow("store-1", "2026_08_01", "sku-b", 3, "google_gemini"), // below default threshold (5)
	}
	views := insights.ComputeProductDemand(rows, "", 0, insights.DefaultTopN)
	if assert.Len(t, views, 1) {
		assert.Equal(t, "sku-a", views[0].ProductID)
	}
}

// TestComputeProductDemand_SurfaceFilter verifies the surface query
// parameter both filters and re-ranks by that surface's contribution only.
func TestComputeProductDemand_SurfaceFilter(t *testing.T) {
	rows := []insights.ProductDemandDaily{
		{StoreID: "store-1", Date: "2026_08_01", ProductID: "sku-a", RequestCount: 20, SurfaceBreakdown: map[string]int64{"google_gemini": 15, "google_ai_mode": 5}},
	}
	views := insights.ComputeProductDemand(rows, "google_ai_mode", 0, insights.DefaultTopN)
	if assert.Len(t, views, 1) {
		assert.Equal(t, int64(5), views[0].RequestCount)
	}
}

// TestComputeCapabilityGaps_SortInvariant is A4: default gap ordering is
// estimated_impact_minor descending, request_count descending as tiebreak
// — with no sort parameter available at all (R11's invariant is
// unconditional in this build, since impact is always 0/unmeasurable —
// see the test below asserting request_count-desc governs the whole
// order when every impact figure ties at zero).
func TestComputeCapabilityGaps_SortInvariant(t *testing.T) {
	now := time.Now()
	rows := []insights.CapabilityGapMonthly{
		{CapabilityID: "dev.ucp.shopping.loyalty", RequestCount: 20, ImpactConfidence: insights.ImpactUnmeasurable, FirstSeenAt: now, LastSeenAt: now},
		{CapabilityID: "dev.ucp.shopping.gift_wrap", RequestCount: 340, ImpactConfidence: insights.ImpactUnmeasurable, FirstSeenAt: now, LastSeenAt: now},
		{CapabilityID: "dev.ucp.shopping.subscriptions", RequestCount: 100, ImpactConfidence: insights.ImpactUnmeasurable, FirstSeenAt: now, LastSeenAt: now},
	}
	views := insights.ComputeCapabilityGaps(rows, 0)
	if assert.Len(t, views, 3) {
		assert.Equal(t, "dev.ucp.shopping.gift_wrap", views[0].CapabilityID)
		assert.Equal(t, "dev.ucp.shopping.subscriptions", views[1].CapabilityID)
		assert.Equal(t, "dev.ucp.shopping.loyalty", views[2].CapabilityID)
	}
}

// TestComputeCapabilityGaps_ImpactBeforeRequestCount proves impact wins
// over request_count when they disagree — the literal R11 tiebreak rule,
// not just a request_count sort that happens to look right when every
// impact figure is zero.
func TestComputeCapabilityGaps_ImpactBeforeRequestCount(t *testing.T) {
	rows := []insights.CapabilityGapMonthly{
		{CapabilityID: "low-impact-high-count", RequestCount: 1000, EstimatedImpactMinor: 100, ImpactConfidence: insights.ImpactMeasured},
		{CapabilityID: "high-impact-low-count", RequestCount: 10, EstimatedImpactMinor: 500000, ImpactConfidence: insights.ImpactMeasured},
	}
	views := insights.ComputeCapabilityGaps(rows, 0)
	if assert.Len(t, views, 2) {
		assert.Equal(t, "high-impact-low-count", views[0].CapabilityID, "higher estimated_impact_minor must sort first regardless of request_count")
	}
}

// TestComputeCapabilityGaps_SmallCountSuppression is A2/R14.
func TestComputeCapabilityGaps_SmallCountSuppression(t *testing.T) {
	rows := []insights.CapabilityGapMonthly{
		{CapabilityID: "common", RequestCount: 50, ImpactConfidence: insights.ImpactUnmeasurable},
		{CapabilityID: "noise", RequestCount: 1, ImpactConfidence: insights.ImpactUnmeasurable},
	}
	views := insights.ComputeCapabilityGaps(rows, 0)
	if assert.Len(t, views, 1) {
		assert.Equal(t, "common", views[0].CapabilityID)
	}
}

// TestComputeCapabilityGaps_UnmeasurableNeverBecomesAModelOutput is A3's
// hard-rule check: every row built by this build's fold logic carries
// EstimatedImpactMinor == 0 and ImpactConfidence == unmeasurable, and
// ComputeCapabilityGaps must pass those values through verbatim — never
// synthesize a nonzero figure.
func TestComputeCapabilityGaps_UnmeasurableNeverBecomesAModelOutput(t *testing.T) {
	rows := []insights.CapabilityGapMonthly{
		{CapabilityID: "dev.ucp.shopping.loyalty", RequestCount: 340, EstimatedImpactMinor: 0, ImpactConfidence: insights.ImpactUnmeasurable},
	}
	views := insights.ComputeCapabilityGaps(rows, 0)
	if assert.Len(t, views, 1) {
		assert.Equal(t, int64(0), views[0].EstimatedImpactMinor)
		assert.Equal(t, string(insights.ImpactUnmeasurable), views[0].ImpactConfidence)
	}
}

// TestComputeNegotiationView_Aggregation folds multiple days and checks
// failure/handler-mismatch summation plus suppression.
func TestComputeNegotiationView_Aggregation(t *testing.T) {
	daily := []insights.NegotiationDaily{
		{Date: "2026_08_01", Attempts: 10, Successes: 7, Failures: []insights.NegotiationFailureBreakdown{
			{Reason: insights.NegotiationFailureCapabilitiesIncompatible, Count: 3, MissingCapabilityCounts: map[string]int64{"dev.ucp.shopping.loyalty": 3}},
		}},
		{Date: "2026_08_02", Attempts: 5, Successes: 5, Failures: nil},
	}
	mismatch := []insights.PaymentHandlerMismatchDaily{
		{Date: "2026_08_01", Occurrences: 2, OfferedHandlerCounts: map[string]int64{"dev.ucp.payments.paypal": 2}, MerchantDeclaredHandlers: []string{"dev.ucp.payments.card"}},
	}

	view := insights.ComputeNegotiationView(daily, mismatch, 1) // threshold 1 so the small counts survive for this assertion
	assert.Equal(t, int64(15), view.Attempts)
	assert.Equal(t, int64(12), view.Successes)
	if assert.Len(t, view.Failures, 1) {
		assert.Equal(t, int64(3), view.Failures[0].Count)
		assert.Equal(t, int64(3), view.Failures[0].MissingCapabilityCounts["dev.ucp.shopping.loyalty"])
	}
	assert.Equal(t, int64(2), view.PaymentHandlerMismatch.Occurrences)
	assert.Equal(t, int64(2), view.PaymentHandlerMismatch.OfferedHandlerCounts["dev.ucp.payments.paypal"])
	assert.Equal(t, []string{"dev.ucp.payments.card"}, view.PaymentHandlerMismatch.MerchantDeclaredHandlers)
}

// TestComputeNegotiationView_SmallCountSuppression is R14 applied to the
// failure-reason and offered-handler breakdowns.
func TestComputeNegotiationView_SmallCountSuppression(t *testing.T) {
	daily := []insights.NegotiationDaily{
		{Date: "2026_08_01", Attempts: 10, Successes: 8, Failures: []insights.NegotiationFailureBreakdown{
			{Reason: insights.NegotiationFailureVersionUnsupported, Count: 2}, // below default threshold (5)
		}},
	}
	view := insights.ComputeNegotiationView(daily, nil, 0)
	assert.Empty(t, view.Failures, "a failure-reason bucket below the suppression threshold must not appear")
	// Totals are never suppressed even when itemized breakdowns are.
	assert.Equal(t, int64(10), view.Attempts)
}
