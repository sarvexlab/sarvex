// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package task

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ListCursor is the position of the last task on a page, in the terms the
// feed is sorted by (see listSort). Keyset pagination rather than an offset:
// the spine is written to constantly (every rescore, every sweep), so an
// offset would skip or repeat tasks as rows are inserted underneath an open
// page.
//
// All three components are needed because all three are in the sort key —
// carrying only the id, as the first implementation did, cannot express
// "after this task" once the order is no longer id-based.
type ListCursor struct {
	CreatedAt      time.Time
	ID             string
	BlocksCheckout bool
}

// Timestamps are carried as milliseconds because that is the precision
// BSON datetimes are stored at: encoding anything finer would produce a
// cursor time that never compares equal to the stored value, and the
// id tiebreak for tasks raised in the same millisecond would never engage.
func (c ListCursor) encode() string {
	return encodeRaw(fmt.Sprintf("%s|%d|%s", strconv.FormatBool(c.BlocksCheckout), c.CreatedAt.UnixMilli(), c.ID))
}

// encodeRaw wraps the wire encoding so that cursors are opaque to clients —
// base64url specifically, so a cursor is safe in a query param untouched.
func encodeRaw(raw string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursor parses a cursor previously produced by encode. A cursor that
// isn't ours — truncated, hand-edited, or left over from the id-only
// format — is rejected rather than guessed at, so the caller can start from
// the first page instead of silently returning a wrong slice of the feed.
func decodeCursor(s string) (ListCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return ListCursor{}, fmt.Errorf("cursor is not valid base64: %w", err)
	}

	parts := strings.SplitN(string(decoded), "|", 3)
	if len(parts) != 3 {
		return ListCursor{}, fmt.Errorf("cursor has %d fields, want 3", len(parts))
	}

	blocks, err := strconv.ParseBool(parts[0])
	if err != nil {
		return ListCursor{}, fmt.Errorf("cursor blocks_checkout: %w", err)
	}
	millis, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return ListCursor{}, fmt.Errorf("cursor created_at: %w", err)
	}
	if parts[2] == "" {
		return ListCursor{}, fmt.Errorf("cursor task id is empty")
	}

	return ListCursor{
		BlocksCheckout: blocks,
		CreatedAt:      time.UnixMilli(millis).UTC(),
		ID:             parts[2],
	}, nil
}
