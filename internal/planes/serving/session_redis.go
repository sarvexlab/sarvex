// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisSessionStore is a distributed session store backed by Redis.
// Supports horizontal scaling across multiple serving instances.
type RedisSessionStore struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisSessionStore creates a distributed session store backed by Redis.
func NewRedisSessionStore(client *redis.Client, ttl time.Duration) SessionStore {
	return &RedisSessionStore{
		client: client,
		ttl:    ttl,
	}
}

// Close stops the cleanup loop.
func (s *RedisSessionStore) Close() {
}

// Create stores a new session with idempotency key deduplication.
func (rs *RedisSessionStore) Create(ctx context.Context, session *CheckoutSession) error {
	if session == nil || session.SessionID == "" || session.StoreID == "" {
		return ErrSessionInvalid
	}

	sessionKey := fmt.Sprintf("session:%s:%s", session.StoreID, session.SessionID)
	sessionJSON, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	// Check idempotency key conflict (if provided).
	if session.IdempotencyKey != "" {
		idempotencyKey := fmt.Sprintf("idempotency:%s:%s", session.StoreID, session.IdempotencyKey)

		// Try to set the idempotency key atomically; if it already exists, check for conflict.
		result, err := rs.client.SetNX(ctx, idempotencyKey, session.SessionID, rs.ttl).Result()
		if err != nil {
			return fmt.Errorf("check idempotency: %w", err)
		}

		if !result {
			// Idempotency key already exists; retrieve the existing session and compare.
			existingSessionID, err := rs.client.Get(ctx, idempotencyKey).Result()
			if err != nil {
				return fmt.Errorf("get existing session id: %w", err)
			}

			existingSession, err := rs.Get(ctx, session.StoreID, existingSessionID)
			if err != nil {
				return fmt.Errorf("get existing session: %w", err)
			}

			// Compare request data to detect conflicts.
			if !sessionsEqual(existingSession, session) {
				return ErrIdempotencyKeyConflict
			}

			// Same data, safe to return the existing session.
			*session = *existingSession
			return nil
		}
	}

	if session.OrderID != "" {
		orderKey := fmt.Sprintf("order:%s:%s", session.StoreID, session.OrderID)
		if err := rs.client.Set(ctx, orderKey, session.SessionID, rs.ttl).Err(); err != nil {
			return fmt.Errorf("set order index: %w", err)
		}
	}

	// Store the session with TTL.
	if err := rs.client.Set(ctx, sessionKey, sessionJSON, rs.ttl).Err(); err != nil {
		return fmt.Errorf("set session: %w", err)
	}

	return nil
}

// Get retrieves a session by ID.
func (rs *RedisSessionStore) Get(ctx context.Context, storeID, sessionID string) (*CheckoutSession, error) {
	sessionKey := fmt.Sprintf("session:%s:%s", storeID, sessionID)

	sessionJSON, err := rs.client.Get(ctx, sessionKey).Result()
	if err == redis.Nil {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}

	var session CheckoutSession
	if err := json.Unmarshal([]byte(sessionJSON), &session); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}

	// Check if session has expired (Redis TTL handles this, but verify consistency).
	if time.Since(session.CreatedAt) > rs.ttl {
		return nil, ErrSessionExpired
	}

	return &session, nil
}

// Update replaces a session with idempotency key management.
func (rs *RedisSessionStore) Update(ctx context.Context, session *CheckoutSession) error {
	if session == nil || session.SessionID == "" {
		return ErrSessionNotFound
	}

	sessionKey := fmt.Sprintf("session:%s:%s", session.StoreID, session.SessionID)

	// Retrieve existing session to manage idempotency keys.
	existing, err := rs.Get(ctx, session.StoreID, session.SessionID)
	if err != nil {
		return err
	}

	// If idempotency key changed, update mappings.
	if session.IdempotencyKey != "" && session.IdempotencyKey != existing.IdempotencyKey {
		newIdempotencyKey := fmt.Sprintf("idempotency:%s:%s", session.StoreID, session.IdempotencyKey)

		// Check if new key already exists.
		exists, err := rs.client.Exists(ctx, newIdempotencyKey).Result()
		if err != nil {
			return fmt.Errorf("check new idempotency key: %w", err)
		}

		if exists > 0 {
			return ErrIdempotencyKeyConflict
		}

		// Remove old idempotency key and add new one.
		if existing.IdempotencyKey != "" {
			oldIdempotencyKey := fmt.Sprintf("idempotency:%s:%s", session.StoreID, existing.IdempotencyKey)
			if err := rs.client.Del(ctx, oldIdempotencyKey).Err(); err != nil {
				return fmt.Errorf("delete old idempotency key: %w", err)
			}
		}

		if err := rs.client.Set(ctx, newIdempotencyKey, session.SessionID, rs.ttl).Err(); err != nil {
			return fmt.Errorf("set new idempotency key: %w", err)
		}
	}

	// Index the order ID, if this write is the one that set it (Complete)
	// — idempotent to repeat, so no need to check whether it's already
	// there.
	if session.OrderID != "" {
		orderKey := fmt.Sprintf("order:%s:%s", session.StoreID, session.OrderID)
		if err := rs.client.Set(ctx, orderKey, session.SessionID, rs.ttl).Err(); err != nil {
			return fmt.Errorf("set order index: %w", err)
		}
	}

	// Update the session.
	sessionJSON, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	if err := rs.client.Set(ctx, sessionKey, sessionJSON, rs.ttl).Err(); err != nil {
		return fmt.Errorf("update session: %w", err)
	}

	return nil
}

// GetByOrderID retrieves the session that recorded the given order ID at
// Complete — see Update's order-index write.
func (rs *RedisSessionStore) GetByOrderID(ctx context.Context, storeID, orderID string) (*CheckoutSession, error) {
	orderKey := fmt.Sprintf("order:%s:%s", storeID, orderID)

	sessionID, err := rs.client.Get(ctx, orderKey).Result()
	if errors.Is(err, redis.Nil) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get session id by order id: %w", err)
	}

	return rs.Get(ctx, storeID, sessionID)
}

// GetByIdempotencyKey retrieves a session by idempotency key.
func (rs *RedisSessionStore) GetByIdempotencyKey(ctx context.Context, storeID, idempotencyKey string) (*CheckoutSession, error) {
	idempotencyKeyStr := fmt.Sprintf("idempotency:%s:%s", storeID, idempotencyKey)

	sessionID, err := rs.client.Get(ctx, idempotencyKeyStr).Result()
	if err == redis.Nil {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get session id by idempotency key: %w", err)
	}

	return rs.Get(ctx, storeID, sessionID)
}

// ListAbandonmentCandidates scans the session key space (Redis has no
// secondary index on state/updated_at — a checkout session store is small
// and short-lived enough, TTL-bounded, that a bounded SCAN per sweep pass
// is preferable to introducing a new index structure to maintain) and
// returns up to limit non-terminal, not-yet-notified sessions last
// updated at or before cutoff.
func (rs *RedisSessionStore) ListAbandonmentCandidates(ctx context.Context, cutoff time.Time, limit int) ([]*CheckoutSession, error) {
	var candidates []*CheckoutSession
	var scanCursor uint64
	for {
		keys, next, err := rs.client.Scan(ctx, scanCursor, "session:*", 200).Result()
		if err != nil {
			return nil, fmt.Errorf("scan sessions: %w", err)
		}
		for _, key := range keys {
			raw, err := rs.client.Get(ctx, key).Result()
			if errors.Is(err, redis.Nil) {
				continue // expired/deleted between SCAN and GET
			}
			if err != nil {
				return nil, fmt.Errorf("get session %q: %w", key, err)
			}
			var session CheckoutSession
			if err := json.Unmarshal([]byte(raw), &session); err != nil {
				return nil, fmt.Errorf("unmarshal session %q: %w", key, err)
			}
			if session.State.Terminal() {
				continue
			}
			if session.AbandonedNotifiedAt != nil {
				continue
			}
			if session.UpdatedAt.After(cutoff) {
				continue
			}
			candidates = append(candidates, &session)
			if len(candidates) >= limit {
				return candidates, nil
			}
		}
		scanCursor = next
		if scanCursor == 0 {
			break
		}
	}
	return candidates, nil
}

// sessionsEqual compares two sessions for idempotency conflict detection.
// sessionsEqual checks if two sessions have equivalent request bodies for idempotency.
// If both have BodyHash set, compares hashes; otherwise falls back to line item comparison.
// Body hashes allow efficient, normalized comparison (e.g., canonical JSON) without
// requiring full struct equality, which would compare metadata fields like CreatedAt.
func sessionsEqual(s1, s2 *CheckoutSession) bool {
	if s1 == nil || s2 == nil {
		return s1 == s2
	}

	// If both sessions have a body hash (computed from normalized request), compare those.
	// This is the preferred path: hash provides a clean semantic boundary.
	if s1.BodyHash != "" && s2.BodyHash != "" {
		return s1.BodyHash == s2.BodyHash
	}

	// Fallback: compare line items (the primary idempotency key payload).
	// This handles older sessions without BodyHash, or calls that don't provide one.
	if len(s1.LineItems) != len(s2.LineItems) {
		return false
	}

	for i, item1 := range s1.LineItems {
		item2 := s2.LineItems[i]
		if item1.Item.ID != item2.Item.ID || item1.Quantity != item2.Quantity {
			return false
		}
	}

	return true
}
