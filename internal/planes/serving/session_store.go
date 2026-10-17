// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrSessionInvalid is returned when a session does not exist.
	ErrSessionInvalid = errors.New("serving: session is invalid")
	// ErrSessionNotFound is returned when a session does not exist.
	ErrSessionNotFound = errors.New("serving: session not found")
	// ErrSessionExpired is returned when a session has exceeded its TTL.
	ErrSessionExpired = errors.New("serving: session expired")
	// ErrIdempotencyKeyConflict is returned when an idempotency key is reused with different data.
	ErrIdempotencyKeyConflict = errors.New("serving: idempotency key conflict")
)

// SessionStore manages checkout session state (one writer per store).
type SessionStore interface {
	// Create stores a new session, returns ErrIdempotencyKeyConflict if idempotency key exists.
	Create(ctx context.Context, session *CheckoutSession) error
	// Get retrieves a session by ID, returns ErrSessionNotFound or ErrSessionExpired.
	Get(ctx context.Context, storeID, sessionID string) (*CheckoutSession, error)
	// Update replaces the session, returns ErrSessionNotFound or ErrIdempotencyKeyConflict.
	Update(ctx context.Context, session *CheckoutSession) error
	// GetByIdempotencyKey retrieves a session by idempotency key, returns ErrSessionNotFound.
	GetByIdempotencyKey(ctx context.Context, storeID, idempotencyKey string) (*CheckoutSession, error)
	// GetByOrderID retrieves the session whose Complete placed the given
	// order (session.OrderID), returns ErrSessionNotFound if no session has
	// recorded that order — the read side of the Orders resource
	// (order.go/api/order_contract.go).
	GetByOrderID(ctx context.Context, storeID, orderID string) (*CheckoutSession, error)
	// ListAbandonmentCandidates returns up to limit non-terminal sessions
	// (SessionState.Terminal() false) last updated at or before cutoff and
	// not yet notified (AbandonedNotifiedAt nil) — the abandonment sweep's
	// (abandonment_sweep.go) read side. Order is unspecified.
	ListAbandonmentCandidates(ctx context.Context, cutoff time.Time, limit int) ([]*CheckoutSession, error)
	//Close cleans up any resources used by the store (e.g. background goroutines).
	Close()
}
