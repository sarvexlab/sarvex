// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package ledger

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned for an entry_id/rollup that doesn't exist for the
// given merchant_id — including one that exists but belongs to a different
// merchant, which must collapse into the identical not-found response (R13
// tenancy: cross-tenant read is 404, never a distinct "forbidden").
var ErrNotFound = errors.New("ledger: not found")

// ErrInvalidCursor is returned (wrapped) when a client-supplied cursor
// isn't one this package produced.
var ErrInvalidCursor = errors.New("ledger: invalid cursor")

// ErrInvalidTransition is returned when a status PATCH would move an entry
// backwards or sideways (R13: forward-only open -> acknowledged ->
// resolved, never a reversal).
var ErrInvalidTransition = errors.New("ledger: invalid status transition")

// Store is the ledger's Mongo-backed persistence, covering both entries and
// their monthly rollups. Store is deliberately one interface, not two: R11
// requires the entry write and its rollup update to happen in the same
// transaction, which only the concrete type spanning both collections can
// actually guarantee — splitting this into EntryRepository/RollupRepository
// interfaces would let a caller assemble them without that guarantee.
type Store interface {
	// InsertEntryAndRollup idempotently inserts e (by e.EventID's unique
	// index) and, only if it was newly inserted, atomically folds it into
	// its monthly/currency rollup — both writes inside one Mongo
	// transaction (R11): a crash between them leaves neither committed,
	// never one without the other. inserted=false for a duplicate
	// event_id is expected, idempotent behavior (R1/A1), not an error.
	InsertEntryAndRollup(ctx context.Context, e Entry) (inserted bool, err error)

	// GetEntry fetches one entry scoped to merchantID.
	GetEntry(ctx context.Context, merchantID, entryID string) (*Entry, error)

	// ListEntries returns a page of entries for merchantID matching
	// filter. Default order is lost_revenue_minor descending, occurred_at
	// descending as tiebreak (R14) unless sort requests otherwise.
	ListEntries(ctx context.Context, merchantID string, filter ListFilter, sort, cursor string, limit int) ([]Entry, string, bool, error)

	// UpdateEntryStatus applies a forward-only status transition (R13).
	// Returns ErrNotFound if the entry doesn't exist for merchantID, and
	// ErrInvalidTransition if next is not strictly ahead of the entry's
	// current status.
	UpdateEntryStatus(ctx context.Context, merchantID, entryID string, next Status, at time.Time) (*Entry, error)

	// GetRollup fetches the merchantID/month/currency rollup. Returns
	// ErrNotFound if no entry has ever landed in that month/currency for
	// this merchant.
	GetRollup(ctx context.Context, merchantID, month, currency string) (*Rollup, error)

	// ListRollups fetches every currency rollup merchantID has for month —
	// the ledger's own record of which currencies actually had lost
	// checkouts that month, not a guess derived from store configuration.
	// Backs GET /ledger/summary's no-currency-filter mode (R9: each
	// currency's rollup stays separate in the result; never summed).
	// Empty (not an error) if the merchant has no rollup for month at all.
	ListRollups(ctx context.Context, merchantID, month string) ([]Rollup, error)
}
