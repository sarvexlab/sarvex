// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"sort"
	"time"
)

// DefaultTopN is R2's default top-N for product demand.
const DefaultTopN = 100

// DefaultSuppressionThreshold is R14's default small-count suppression
// floor: an aggregate row with a count below this is dropped from read
// results rather than shown, to avoid presenting noise as signal.
const DefaultSuppressionThreshold = 5

// ProductDemandView is one product's request_count/surface_breakdown
// summed across a read API date range (R2's request_count/surface_breakdown
// only — see package doc comment for why reached_cart_count/
// completed_count are not part of this build).
type ProductDemandView struct {
	ProductID        string           `json:"product_id"`
	RequestCount     int64            `json:"request_count"`
	SurfaceBreakdown map[string]int64 `json:"surface_breakdown"`
}

// ComputeProductDemand folds rows (one or more days of ProductDemandDaily,
// as returned by Store.ListProductDemand) into a top-N, suppressed,
// request_count-descending product demand view (R2/R11/R14). surface, if
// non-empty, filters to that surface's contribution only — both for the
// count used to rank/suppress and for which requests are summed at all.
// minCount <= 0 uses DefaultSuppressionThreshold; topN <= 0 uses DefaultTopN.
func ComputeProductDemand(rows []ProductDemandDaily, surface string, minCount, topN int) []ProductDemandView {
	if minCount <= 0 {
		minCount = DefaultSuppressionThreshold
	}
	if topN <= 0 {
		topN = DefaultTopN
	}

	byProduct := map[string]*ProductDemandView{}
	order := []string{}
	for _, row := range rows {
		v, ok := byProduct[row.ProductID]
		if !ok {
			v = &ProductDemandView{ProductID: row.ProductID, SurfaceBreakdown: map[string]int64{}}
			byProduct[row.ProductID] = v
			order = append(order, row.ProductID)
		}
		for s, count := range row.SurfaceBreakdown {
			if surface != "" && s != surface {
				continue
			}
			v.SurfaceBreakdown[s] += count
			v.RequestCount += count
		}
	}

	views := make([]ProductDemandView, 0, len(order))
	for _, id := range order {
		v := byProduct[id]
		if v.RequestCount < int64(minCount) {
			continue
		}
		views = append(views, *v)
	}

	sort.Slice(views, func(i, j int) bool {
		if views[i].RequestCount != views[j].RequestCount {
			return views[i].RequestCount > views[j].RequestCount
		}
		return views[i].ProductID < views[j].ProductID
	})
	if len(views) > topN {
		views = views[:topN]
	}
	return views
}

// NegotiationFailureView is one failure_reason's summed breakdown across a
// date range (R5), suppressed per R14.
type NegotiationFailureView struct {
	Reason                  string           `json:"reason"`
	Count                   int64            `json:"count"`
	MissingCapabilityCounts map[string]int64 `json:"missing_capability_counts,omitempty"`
}

// PaymentHandlerMismatchView is R6's summed no_common_payment_handler
// breakdown across a date range.
type PaymentHandlerMismatchView struct {
	Occurrences              int64            `json:"occurrences"`
	OfferedHandlerCounts     map[string]int64 `json:"offered_handler_counts"`
	MerchantDeclaredHandlers []string         `json:"merchant_declared_handlers"`
}

// NegotiationView is the summed negotiation outcome view for a date range
// (R5/R6).
type NegotiationView struct {
	Attempts               int64                      `json:"attempts"`
	Successes              int64                      `json:"successes"`
	Failures               []NegotiationFailureView   `json:"failures"`
	PaymentHandlerMismatch PaymentHandlerMismatchView `json:"payment_handler_mismatch"`
}

// ComputeNegotiationView folds a date range's NegotiationDaily and
// PaymentHandlerMismatchDaily rows into one summed view. minCount <= 0
// uses DefaultSuppressionThreshold, applied to each failure reason's count
// and each offered-handler's count individually (R14) — never to the
// top-level attempts/successes totals, which are never itemized noise.
func ComputeNegotiationView(daily []NegotiationDaily, mismatch []PaymentHandlerMismatchDaily, minCount int) NegotiationView {
	if minCount <= 0 {
		minCount = DefaultSuppressionThreshold
	}

	view := NegotiationView{PaymentHandlerMismatch: PaymentHandlerMismatchView{
		OfferedHandlerCounts:     map[string]int64{},
		MerchantDeclaredHandlers: []string{},
	}}

	failureCounts := map[string]int64{}
	failureMissing := map[string]map[string]int64{}
	failureOrder := []string{}
	for _, d := range daily {
		view.Attempts += d.Attempts
		view.Successes += d.Successes
		for _, f := range d.Failures {
			reason := string(f.Reason)
			if _, ok := failureCounts[reason]; !ok {
				failureOrder = append(failureOrder, reason)
				failureMissing[reason] = map[string]int64{}
			}
			failureCounts[reason] += f.Count
			for cap, c := range f.MissingCapabilityCounts {
				failureMissing[reason][cap] += c
			}
		}
	}
	for _, reason := range failureOrder {
		count := failureCounts[reason]
		if count < int64(minCount) {
			continue
		}
		fv := NegotiationFailureView{Reason: reason, Count: count}
		if missing := failureMissing[reason]; len(missing) > 0 {
			fv.MissingCapabilityCounts = suppressCounts(missing, minCount)
		}
		view.Failures = append(view.Failures, fv)
	}
	sort.Slice(view.Failures, func(i, j int) bool { return view.Failures[i].Count > view.Failures[j].Count })

	declaredSeen := map[string]struct{}{}
	for _, m := range mismatch {
		view.PaymentHandlerMismatch.Occurrences += m.Occurrences
		for h, c := range m.OfferedHandlerCounts {
			view.PaymentHandlerMismatch.OfferedHandlerCounts[h] += c
		}
		for _, h := range m.MerchantDeclaredHandlers {
			if _, ok := declaredSeen[h]; !ok {
				declaredSeen[h] = struct{}{}
				view.PaymentHandlerMismatch.MerchantDeclaredHandlers = append(view.PaymentHandlerMismatch.MerchantDeclaredHandlers, h)
			}
		}
	}
	view.PaymentHandlerMismatch.OfferedHandlerCounts = suppressCounts(view.PaymentHandlerMismatch.OfferedHandlerCounts, minCount)
	sort.Strings(view.PaymentHandlerMismatch.MerchantDeclaredHandlers)

	return view
}

func suppressCounts(counts map[string]int64, minCount int) map[string]int64 {
	out := make(map[string]int64, len(counts))
	for k, v := range counts {
		if v >= int64(minCount) {
			out[k] = v
		}
	}
	return out
}

// CapabilityGapView is R8's read-time view of one CapabilityGapMonthly
// document — DistinctAgentCount is derived (len of the stored set) rather
// than stored redundantly.
type CapabilityGapView struct {
	CapabilityID         string           `json:"capability_id"`
	RequestCount         int64            `json:"request_count"`
	DistinctAgentCount   int              `json:"distinct_agent_count"`
	SurfaceBreakdown     map[string]int64 `json:"surface_breakdown"`
	FirstSeenAt          time.Time        `json:"first_seen_at"`
	LastSeenAt           time.Time        `json:"last_seen_at"`
	EstimatedImpactMinor int64            `json:"estimated_impact_minor"`
	ImpactConfidence     string           `json:"impact_confidence"`
	MerchantSupported    bool             `json:"merchant_supported"`
	RemediationAvailable bool             `json:"remediation_available"`
	RecommendedAction    string           `json:"recommended_action"`
}

// ComputeCapabilityGaps applies R14's suppression and R11's default sort
// invariant (estimated_impact_minor descending, request_count descending
// as tiebreak) to a month's CapabilityGapMonthly rows. minCount <= 0 uses
// DefaultSuppressionThreshold.
func ComputeCapabilityGaps(rows []CapabilityGapMonthly, minCount int) []CapabilityGapView {
	if minCount <= 0 {
		minCount = DefaultSuppressionThreshold
	}

	views := make([]CapabilityGapView, 0, len(rows))
	for _, r := range rows {
		if r.RequestCount < int64(minCount) {
			continue
		}
		views = append(views, CapabilityGapView{
			CapabilityID:         r.CapabilityID,
			RequestCount:         r.RequestCount,
			DistinctAgentCount:   len(r.DistinctPlatformURIs),
			SurfaceBreakdown:     r.SurfaceBreakdown,
			FirstSeenAt:          r.FirstSeenAt,
			LastSeenAt:           r.LastSeenAt,
			EstimatedImpactMinor: r.EstimatedImpactMinor,
			ImpactConfidence:     string(r.ImpactConfidence),
			MerchantSupported:    r.MerchantSupported,
			RemediationAvailable: r.RemediationAvailable,
			RecommendedAction:    r.RecommendedAction,
		})
	}

	sort.Slice(views, func(i, j int) bool {
		if views[i].EstimatedImpactMinor != views[j].EstimatedImpactMinor {
			return views[i].EstimatedImpactMinor > views[j].EstimatedImpactMinor
		}
		if views[i].RequestCount != views[j].RequestCount {
			return views[i].RequestCount > views[j].RequestCount
		}
		return views[i].CapabilityID < views[j].CapabilityID
	})
	return views
}
