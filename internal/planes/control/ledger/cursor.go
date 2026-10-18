// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package ledger

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SortRevenueDesc is the default, product-invariant sort (R14):
// lost_revenue_minor descending, occurred_at descending as tiebreak.
// SortOccurredAtDesc is the one alternate sort a client may request (R14's
// "a client may request another sort").
const (
	SortRevenueDesc    = "lost_revenue_minor"
	SortOccurredAtDesc = "occurred_at"
)

// listCursor is the position of the last entry on a page, in the terms the
// active sort orders by. The sort mode itself travels in the cursor so a
// client can't silently get a nonsensical page by changing sort mid-walk.
type listCursor struct {
	Sort             string
	LostRevenueMinor int64
	OccurredAt       time.Time
	ID               string
}

func encodeCursor(c listCursor) string {
	raw := fmt.Sprintf("%s|%d|%d|%s", c.Sort, c.LostRevenueMinor, c.OccurredAt.UnixMilli(), c.ID)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursor parses a cursor previously produced by encodeCursor. A
// cursor that isn't ours (or doesn't match the requested sort) is rejected
// rather than guessed at.
func decodeCursor(s, wantSort string) (listCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return listCursor{}, fmt.Errorf("cursor is not valid base64: %w", err)
	}
	parts := strings.SplitN(string(decoded), "|", 4)
	if len(parts) != 4 {
		return listCursor{}, fmt.Errorf("cursor has unexpected shape")
	}
	if parts[0] != wantSort {
		return listCursor{}, fmt.Errorf("cursor was issued for a different sort")
	}
	revenue, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return listCursor{}, fmt.Errorf("cursor revenue is not an integer: %w", err)
	}
	millis, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return listCursor{}, fmt.Errorf("cursor timestamp is not an integer: %w", err)
	}
	if parts[3] == "" {
		return listCursor{}, fmt.Errorf("cursor id is empty")
	}
	return listCursor{Sort: wantSort, LostRevenueMinor: revenue, OccurredAt: time.UnixMilli(millis).UTC(), ID: parts[3]}, nil
}
