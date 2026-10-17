// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package ledger_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/ledger"
)

// A7: forward-only status transitions — open -> acknowledged -> resolved,
// never a reversal, never a no-op.
func TestCanTransition_ForwardOnly(t *testing.T) {
	cases := []struct {
		name string
		cur  ledger.Status
		next ledger.Status
		want bool
	}{
		{"open_to_acknowledged", ledger.StatusOpen, ledger.StatusAcknowledged, true},
		{"open_to_resolved_skip_ahead_allowed", ledger.StatusOpen, ledger.StatusResolved, true},
		{"acknowledged_to_resolved", ledger.StatusAcknowledged, ledger.StatusResolved, true},
		{"acknowledged_to_open_reversal_rejected", ledger.StatusAcknowledged, ledger.StatusOpen, false},
		{"resolved_to_acknowledged_reversal_rejected", ledger.StatusResolved, ledger.StatusAcknowledged, false},
		{"resolved_to_open_reversal_rejected", ledger.StatusResolved, ledger.StatusOpen, false},
		{"same_status_noop_rejected", ledger.StatusOpen, ledger.StatusOpen, false},
		{"unknown_status_rejected", ledger.Status("bogus"), ledger.StatusResolved, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ledger.CanTransition(tc.cur, tc.next))
		})
	}
}
