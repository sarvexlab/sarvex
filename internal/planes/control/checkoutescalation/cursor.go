// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package checkoutescalation

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// listCursor is the position of the last session on a page, in the terms
// the list is sorted by (last_activity_at desc, _id desc — see
// SessionRepository.List). Keyset rather than offset pagination: sessions'
// last_activity_at changes on every event, so an offset would skip or
// repeat rows as the underlying order shifts under an open page.
type listCursor struct {
	LastActivityAt time.Time
	ID             string
}

// Timestamps are carried as milliseconds — the precision BSON datetimes
// are stored at (same rationale as internal/planes/control/task/cursor.go).
func encodeCursor(c listCursor) string {
	raw := fmt.Sprintf("%d|%s", c.LastActivityAt.UnixMilli(), c.ID)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursor parses a cursor previously produced by encodeCursor. A
// cursor that isn't ours is rejected rather than guessed at, so the caller
// can fall back to the first page instead of silently returning a wrong
// slice of the list.
func decodeCursor(s string) (listCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return listCursor{}, fmt.Errorf("cursor is not valid base64: %w", err)
	}
	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 {
		return listCursor{}, fmt.Errorf("cursor has unexpected shape")
	}
	millis, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return listCursor{}, fmt.Errorf("cursor timestamp is not an integer: %w", err)
	}
	if parts[1] == "" {
		return listCursor{}, fmt.Errorf("cursor id is empty")
	}
	return listCursor{LastActivityAt: time.UnixMilli(millis).UTC(), ID: parts[1]}, nil
}
