// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package channel

import (
	"context"
	"sync"
	"time"
)

// inMemoryStore is a Store implementation over Go maps, driving the exact
// same pure fold logic (foldSessionEvent, fold.go) that mongoStore wraps
// in a real transaction. Exported as NewInMemoryStoreForTest so the
// external channel_test package (consumer_test.go) can exercise Consumer
// against realistic Store semantics without MongoDB — production
// correctness (the real transaction/index behavior) is covered separately
// by mongo_store_integration_test.go (tag integration).
type inMemoryStore struct {
	mu               sync.Mutex
	sessions         map[string]Session
	daily            map[string]DailyAggregate
	discoveryDeduped map[string]bool
}

// NewInMemoryStoreForTest constructs a test-only in-memory Store.
func NewInMemoryStoreForTest() Store {
	return &inMemoryStore{
		sessions:         map[string]Session{},
		daily:            map[string]DailyAggregate{},
		discoveryDeduped: map[string]bool{},
	}
}

func (s *inMemoryStore) FoldSessionEvent(_ context.Context, ev SessionEvent) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var existingPtr *Session
	if existing, ok := s.sessions[ev.SessionID]; ok {
		existingPtr = &existing
	}
	updated, delta, duplicate := foldSessionEvent(existingPtr, ev, time.Now().UTC())
	if duplicate {
		return false, nil
	}
	s.sessions[updated.SessionID] = updated

	if !delta.isZero() {
		id := DailyAggregateID(updated.StoreID, updated.Surface, updated.Date)
		agg, ok := s.daily[id]
		if !ok {
			agg = DailyAggregate{ID: id, MerchantID: updated.MerchantID, StoreID: updated.StoreID, Surface: updated.Surface, Date: updated.Date}
		}
		agg.Sessions += delta.sessions
		agg.CheckoutsInitiated += delta.checkoutsInitiated
		agg.SessionsInferred += delta.sessionsInferred
		agg.SessionsReachedCart += delta.reachedCart
		agg.SessionsReachedCheckout += delta.reachedCheckout
		agg.CheckoutsCompleted += delta.completed
		agg.RevenueMinor += delta.revenueMinor
		if updated.Currency != "" {
			agg.Currency = updated.Currency
		}
		agg.UpdatedAt = time.Now().UTC()
		s.daily[id] = agg
	}
	return true, nil
}

func (s *inMemoryStore) RecordDiscoveryRequest(_ context.Context, eventID, merchantID, storeID string, surface Surface, occurredAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.discoveryDeduped[eventID] {
		return nil
	}
	s.discoveryDeduped[eventID] = true

	date := DateKey(occurredAt)
	id := DailyAggregateID(storeID, surface, date)
	agg, ok := s.daily[id]
	if !ok {
		agg = DailyAggregate{ID: id, MerchantID: merchantID, StoreID: storeID, Surface: surface, Date: date}
	}
	agg.DiscoveryRequests++
	agg.UpdatedAt = time.Now().UTC()
	s.daily[id] = agg
	return nil
}

func (s *inMemoryStore) GetDailyAggregate(_ context.Context, merchantID, storeID string, surface Surface, date string) (*DailyAggregate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	agg, ok := s.daily[DailyAggregateID(storeID, surface, date)]
	if !ok || agg.MerchantID != merchantID {
		return nil, ErrNotFound
	}
	cp := agg
	return &cp, nil
}

func (s *inMemoryStore) ListDailyAggregates(_ context.Context, merchantID, storeID string, surface Surface, from, to string) ([]DailyAggregate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []DailyAggregate
	for _, agg := range s.daily {
		if agg.MerchantID != merchantID {
			continue
		}
		if storeID != "" && agg.StoreID != storeID {
			continue
		}
		if surface != "" && agg.Surface != surface {
			continue
		}
		if agg.Date < from || agg.Date > to {
			continue
		}
		out = append(out, agg)
	}
	return out, nil
}

func (s *inMemoryStore) GetSession(_ context.Context, sessionID string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		return nil, ErrNotFound
	}
	cp := sess
	return &cp, nil
}
