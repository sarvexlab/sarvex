// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// MemorySessionStore is an in-memory session store (single-process, no persistence).
type MemorySessionStore struct {
	mu          sync.RWMutex
	sessions    map[string]*CheckoutSession // sessionID → session
	idempotency map[string]string           // "storeID:idempotencyKey" → sessionID
	orders      map[string]string           // "storeID:orderID" → sessionID
	ttl         time.Duration
	cleanupTick *time.Ticker
	cleanupDone chan struct{}
}

// NewMemorySessionStore creates an in-memory session store with TTL cleanup.
func NewMemorySessionStore(ttl time.Duration) SessionStore {
	s := &MemorySessionStore{
		sessions:    make(map[string]*CheckoutSession),
		idempotency: make(map[string]string),
		orders:      make(map[string]string),
		ttl:         ttl,
		cleanupTick: time.NewTicker(1 * time.Minute),
		cleanupDone: make(chan struct{}),
	}
	go s.cleanupLoop()
	return s
}

// idempotencyIndexKey namespaces an idempotency key by store. Keys are chosen
// by callers, so two stores routinely use the same one; an index keyed by the
// bare key would hand one store the other's session.
func idempotencyIndexKey(storeID, key string) string {
	return fmt.Sprintf("%s:%s", storeID, key)
}

// orderKey is the orders index key for a store+order pair.
func orderKey(storeID, orderID string) string {
	return fmt.Sprintf("%s:%s", storeID, orderID)
}

// Close stops the cleanup loop.
func (s *MemorySessionStore) Close() {
	s.cleanupTick.Stop()
	s.cleanupDone <- struct{}{}
}

// Create stores a new session.
func (s *MemorySessionStore) Create(ctx context.Context, session *CheckoutSession) error {
	if session == nil || session.SessionID == "" || session.MerchantID == "" {
		return errors.New("session id and merchant id required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check idempotency key conflict.
	if session.IdempotencyKey != "" {
		if existingSessionID, exists := s.idempotency[idempotencyIndexKey(session.StoreID, session.IdempotencyKey)]; exists {
			if existingSession := s.sessions[existingSessionID]; existingSession != nil {
				// Return the existing session data to check for conflicts.
				if !sessionsEqual(existingSession, session) {
					return ErrIdempotencyKeyConflict
				}
				// Same data, safe to return success.
				*session = *existingSession
				return nil
			}
		}
	}

	// Store session.
	s.sessions[session.SessionID] = session
	if session.IdempotencyKey != "" {
		s.idempotency[idempotencyIndexKey(session.StoreID, session.IdempotencyKey)] = session.SessionID
	}
	if session.OrderID != "" {
		s.orders[orderKey(session.StoreID, session.OrderID)] = session.SessionID
	}
	return nil
}

// Get retrieves a session by ID.
func (s *MemorySessionStore) Get(ctx context.Context, storeID, sessionID string) (*CheckoutSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}

	if session.StoreID != storeID {
		return nil, ErrSessionNotFound
	}

	if s.isExpired(session) {
		return nil, ErrSessionExpired
	}

	return session, nil
}

// Update replaces a session.
func (s *MemorySessionStore) Update(ctx context.Context, session *CheckoutSession) error {
	if session == nil || session.SessionID == "" {
		return errors.New("session id required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.sessions[session.SessionID]
	if !ok {
		return ErrSessionNotFound
	}

	if session.IdempotencyKey != "" && session.IdempotencyKey != existing.IdempotencyKey {
		if _, exists := s.idempotency[idempotencyIndexKey(session.StoreID, session.IdempotencyKey)]; exists {
			return ErrIdempotencyKeyConflict
		}
		// Remove old idempotency key, add new one.
		if existing.IdempotencyKey != "" {
			delete(s.idempotency, idempotencyIndexKey(existing.StoreID, existing.IdempotencyKey))
		}
		s.idempotency[idempotencyIndexKey(session.StoreID, session.IdempotencyKey)] = session.SessionID
	}

	if session.OrderID != "" {
		s.orders[orderKey(session.StoreID, session.OrderID)] = session.SessionID
	}

	s.sessions[session.SessionID] = session
	return nil
}

// GetByOrderID retrieves the session that recorded the given order ID.
func (s *MemorySessionStore) GetByOrderID(ctx context.Context, storeID, orderID string) (*CheckoutSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessionID, ok := s.orders[orderKey(storeID, orderID)]
	if !ok {
		return nil, ErrSessionNotFound
	}

	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}

	if s.isExpired(session) {
		return nil, ErrSessionExpired
	}

	return session, nil
}

// GetByIdempotencyKey retrieves a session by idempotency key.
func (s *MemorySessionStore) GetByIdempotencyKey(ctx context.Context, storeID, idempotencyKey string) (*CheckoutSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessionID, ok := s.idempotency[idempotencyIndexKey(storeID, idempotencyKey)]
	if !ok {
		return nil, ErrSessionNotFound
	}

	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}

	if session.StoreID != storeID {
		return nil, ErrSessionNotFound
	}

	if s.isExpired(session) {
		return nil, ErrSessionExpired
	}

	return session, nil
}

// ListAbandonmentCandidates returns up to limit non-terminal,
// not-yet-notified sessions last updated at or before cutoff.
func (s *MemorySessionStore) ListAbandonmentCandidates(ctx context.Context, cutoff time.Time, limit int) ([]*CheckoutSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var candidates []*CheckoutSession
	for _, session := range s.sessions {
		if session.State.Terminal() {
			continue
		}
		if session.AbandonedNotifiedAt != nil {
			continue
		}
		if session.UpdatedAt.After(cutoff) {
			continue
		}
		candidates = append(candidates, session)
		if len(candidates) >= limit {
			break
		}
	}
	return candidates, nil
}

// cleanupLoop removes expired sessions periodically.
func (s *MemorySessionStore) cleanupLoop() {
	for {
		select {
		case <-s.cleanupTick.C:
			s.cleanup()
		case <-s.cleanupDone:
			return
		}
	}
}

// cleanup removes expired sessions.
func (s *MemorySessionStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, session := range s.sessions {
		if now.Sub(session.CreatedAt) > s.ttl {
			// Remove from idempotency map.
			if session.IdempotencyKey != "" {
				delete(s.idempotency, idempotencyIndexKey(session.StoreID, session.IdempotencyKey))
			}
			if session.OrderID != "" {
				delete(s.orders, orderKey(session.StoreID, session.OrderID))
			}
			delete(s.sessions, id)
		}
	}
}

// isExpired checks if a session has exceeded its TTL.
func (s *MemorySessionStore) isExpired(session *CheckoutSession) bool {
	return time.Since(session.CreatedAt) > s.ttl
}
