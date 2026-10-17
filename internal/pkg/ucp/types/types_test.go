// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package types

import "testing"

func TestVersionCompare(t *testing.T) {
	older, newer := Version("2026-04-08"), Version("2026-08-25")
	if older.Compare(newer) != -1 || newer.Compare(older) != 1 || older.Compare(older) != 0 {
		t.Error("Compare is not chronological")
	}
	if older.String() != "2026-04-08" {
		t.Errorf("String() = %q", older.String())
	}
}
