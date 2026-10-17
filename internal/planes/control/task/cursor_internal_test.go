// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package task

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Internal because the cursor is deliberately opaque to callers: its shape
// is an implementation detail of the feed's sort key, and nothing outside
// this package may construct or parse one.

func TestListCursor_RoundTrip(t *testing.T) {
	// Truncated to milliseconds because that is what survives the encoding —
	// see the note on encode.
	want := ListCursor{
		BlocksCheckout: true,
		CreatedAt:      time.Date(2026, 4, 8, 12, 30, 45, 123_000_000, time.UTC),
		ID:             "readiness:store-1:HP-1042",
	}

	got, err := decodeCursor(want.encode())
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// Task ids are compound keys full of colons, and cursors travel as a query
// param — so the encoding has to survive both without the id's own
// punctuation being mistaken for a field separator.
func TestListCursor_RoundTripPreservesCompoundID(t *testing.T) {
	want := ListCursor{
		CreatedAt: time.UnixMilli(0).UTC(),
		ID:        "checklist:store-1:gpay-setup|weird",
	}

	got, err := decodeCursor(want.encode())
	require.NoError(t, err)
	assert.Equal(t, want.ID, got.ID)
}

func TestDecodeCursor_Rejects(t *testing.T) {
	tests := []struct {
		name   string
		cursor string
	}{
		{"not base64", "!!!not base64!!!"},
		{"too few fields", encodeRaw("true|123")},
		{"bad bool", encodeRaw("maybe|123|task-1")},
		{"bad timestamp", encodeRaw("true|not-a-number|task-1")},
		{"empty id", encodeRaw("true|123|")},
		// The pre-keyset format was a bare task id. Accepting it would
		// silently return the wrong slice of the feed, since a bare id says
		// nothing about blocks_checkout or created_at.
		{"legacy id-only cursor", "readiness:store-1:HP-1042"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeCursor(tt.cursor)
			assert.Error(t, err)
		})
	}
}
