// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"context"
	"sync"
	"time"
)

// inMemoryStore is a Store implementation over Go maps, driving the exact
// same dedup-then-fold semantics mongoStore's Mongo collections implement
// (claim a dedup key, then apply increments) — mirrors
// channel.inMemoryStore's identical role for that package. Exported as
// NewInMemoryStoreForTest so consumer_test.go (package insights_test) and
// view_test.go/capability_gap_test.go can exercise realistic idempotency
// semantics without MongoDB; mongo_store_integration_test.go (tag
// integration) covers the real transactional/index behavior.
type inMemoryStore struct {
	mu              sync.Mutex
	dedup           map[string]bool
	productDemand   map[string]ProductDemandDaily
	negotiation     map[string]NegotiationDaily
	handlerMismatch map[string]PaymentHandlerMismatchDaily
	capabilityGaps  map[string]CapabilityGapMonthly
}

// NewInMemoryStoreForTest constructs a test-only in-memory Store.
func NewInMemoryStoreForTest() Store {
	return &inMemoryStore{
		dedup:           map[string]bool{},
		productDemand:   map[string]ProductDemandDaily{},
		negotiation:     map[string]NegotiationDaily{},
		handlerMismatch: map[string]PaymentHandlerMismatchDaily{},
		capabilityGaps:  map[string]CapabilityGapMonthly{},
	}
}

func (s *inMemoryStore) claim(key string) bool {
	if s.dedup[key] {
		return false
	}
	s.dedup[key] = true
	return true
}

func (s *inMemoryStore) FoldCatalogQuery(_ context.Context, q CatalogQuery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if q.EventID == "" || len(q.ProductIDs) == 0 {
		return nil
	}
	if !s.claim("catalog_query:" + q.EventID) {
		return nil
	}

	date := DateKey(q.OccurredAt)
	surface := string(deriveSurface(q.AgentPlatformHost))
	now := time.Now().UTC()
	for _, productID := range q.ProductIDs {
		id := productDemandID(q.StoreID, date, productID)
		row, ok := s.productDemand[id]
		if !ok {
			row = ProductDemandDaily{
				ID: id, MerchantID: q.MerchantID, StoreID: q.StoreID, Date: date, ProductID: productID,
				SurfaceBreakdown: map[string]int64{},
			}
		}
		row.RequestCount++
		row.SurfaceBreakdown[surface]++
		row.UpdatedAt = now
		s.productDemand[id] = row
	}
	return nil
}

func (s *inMemoryStore) FoldNegotiationOutcome(_ context.Context, out NegotiationOutcome) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if out.DedupKey == "" {
		return nil
	}
	if !s.claim("negotiation:" + out.DedupKey) {
		return nil
	}

	date := DateKey(out.OccurredAt)
	now := time.Now().UTC()

	// negotiation_daily
	id := negotiationDailyID(out.StoreID, date)
	nd, ok := s.negotiation[id]
	if !ok {
		nd = NegotiationDaily{ID: id, MerchantID: out.MerchantID, StoreID: out.StoreID, Date: date}
	}
	nd.Attempts++
	if out.Succeeded {
		nd.Successes++
	} else {
		// Mirrors mongo_store.go's foldNegotiationDaily: missing_capability_counts
		// only means something for capabilities_incompatible.
		var missing []string
		if out.FailureReason == NegotiationFailureCapabilitiesIncompatible {
			missing = missingCapabilities(out.RequestedCapabilities, out.DeclaredCapabilities)
		}
		found := false
		for i, f := range nd.Failures {
			if f.Reason == out.FailureReason {
				nd.Failures[i].Count++
				if len(missing) > 0 {
					if nd.Failures[i].MissingCapabilityCounts == nil {
						nd.Failures[i].MissingCapabilityCounts = map[string]int64{}
					}
					for _, cap := range missing {
						nd.Failures[i].MissingCapabilityCounts[cap]++
					}
				}
				found = true
				break
			}
		}
		if !found {
			breakdown := NegotiationFailureBreakdown{Reason: out.FailureReason, Count: 1}
			if len(missing) > 0 {
				breakdown.MissingCapabilityCounts = map[string]int64{}
				for _, cap := range missing {
					breakdown.MissingCapabilityCounts[cap] = 1
				}
			}
			nd.Failures = append(nd.Failures, breakdown)
		}
	}
	nd.UpdatedAt = now
	s.negotiation[id] = nd

	// payment_handler_mismatch_daily
	if len(out.RequestedPaymentHandlers) > 0 {
		offered := missingCapabilities(out.RequestedPaymentHandlers, out.DeclaredPaymentHandlers)
		if len(offered) == len(out.RequestedPaymentHandlers) {
			hid := paymentHandlerMismatchID(out.StoreID, date)
			hm, ok := s.handlerMismatch[hid]
			if !ok {
				hm = PaymentHandlerMismatchDaily{ID: hid, MerchantID: out.MerchantID, StoreID: out.StoreID, Date: date, OfferedHandlerCounts: map[string]int64{}}
			}
			hm.Occurrences++
			for _, h := range offered {
				hm.OfferedHandlerCounts[h]++
			}
			hm.MerchantDeclaredHandlers = out.DeclaredPaymentHandlers
			hm.UpdatedAt = now
			s.handlerMismatch[hid] = hm
		}
	}

	// capability_gaps_monthly
	if !out.Succeeded && out.FailureReason == NegotiationFailureCapabilitiesIncompatible {
		missing := missingCapabilities(out.RequestedCapabilities, out.DeclaredCapabilities)
		month := MonthKey(out.OccurredAt)
		surface := string(deriveSurface(profileHost(out.PlatformProfileURI)))
		for _, capabilityID := range missing {
			cid := capabilityGapID(out.StoreID, month, capabilityID)
			cg, ok := s.capabilityGaps[cid]
			if !ok {
				cg = CapabilityGapMonthly{
					ID: cid, MerchantID: out.MerchantID, StoreID: out.StoreID, Month: month, CapabilityID: capabilityID,
					SurfaceBreakdown: map[string]int64{}, FirstSeenAt: out.OccurredAt,
					ImpactConfidence: ImpactUnmeasurable, RecommendedAction: recommendedActionFor(capabilityID),
				}
			}
			cg.RequestCount++
			cg.SurfaceBreakdown[surface]++
			cg.LastSeenAt = out.OccurredAt
			cg.UpdatedAt = now
			if out.PlatformProfileURI != "" && !containsStr(cg.DistinctPlatformURIs, out.PlatformProfileURI) {
				cg.DistinctPlatformURIs = append(cg.DistinctPlatformURIs, out.PlatformProfileURI)
			}
			s.capabilityGaps[cid] = cg
		}
	}

	return nil
}

func containsStr(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func (s *inMemoryStore) ListProductDemand(_ context.Context, merchantID, storeID, from, to string) ([]ProductDemandDaily, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ProductDemandDaily
	for _, row := range s.productDemand {
		if row.MerchantID != merchantID {
			continue
		}
		if storeID != "" && row.StoreID != storeID {
			continue
		}
		if row.Date < from || row.Date > to {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func (s *inMemoryStore) ListNegotiationDaily(_ context.Context, merchantID, storeID, from, to string) ([]NegotiationDaily, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []NegotiationDaily
	for _, row := range s.negotiation {
		if row.MerchantID != merchantID {
			continue
		}
		if storeID != "" && row.StoreID != storeID {
			continue
		}
		if row.Date < from || row.Date > to {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func (s *inMemoryStore) ListPaymentHandlerMismatchDaily(_ context.Context, merchantID, storeID, from, to string) ([]PaymentHandlerMismatchDaily, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []PaymentHandlerMismatchDaily
	for _, row := range s.handlerMismatch {
		if row.MerchantID != merchantID {
			continue
		}
		if storeID != "" && row.StoreID != storeID {
			continue
		}
		if row.Date < from || row.Date > to {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func (s *inMemoryStore) ListCapabilityGaps(_ context.Context, merchantID, storeID, month string) ([]CapabilityGapMonthly, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []CapabilityGapMonthly
	for _, row := range s.capabilityGaps {
		if row.MerchantID != merchantID {
			continue
		}
		if storeID != "" && row.StoreID != storeID {
			continue
		}
		if row.Month != month {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}
