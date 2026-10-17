// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package journey

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// Internal, because these assert properties of the `stages` table itself
// rather than behaviour observable through the service. Behaviour lives in
// milestones_test.go.

// Every state named in the table has to be a state the funnel can actually
// reach, or its work would silently never be raised.
func TestStages_AllKeysAreInJourneyOrder(t *testing.T) {
	for state := range stages {
		assert.Contains(t, journeyOrder, state, "stage %q is not part of the funnel", state)
	}
}

// The task id is source:store:source_ref and the console matches
// presentation on source_ref (widgets/registry.ts). Deriving the id from the
// prescription is what keeps them in step; this pins that they do.
func TestStages_MilestoneIDsMatchTheirSourceRef(t *testing.T) {
	for state, work := range stages {
		if work.milestone == nil {
			continue
		}
		t.Run(string(state), func(t *testing.T) {
			p := work.milestone.prescription("store1")
			assert.Equal(t, fmt.Sprintf("%s:store1:%s", work.milestone.source, work.milestone.sourceRef), p.TaskID())
		})
	}
}

// Two states raising the same task id would have them fighting over one row
// on the spine — each converging pass would undo the other's.
func TestStages_MilestoneIDsAreUnique(t *testing.T) {
	seen := map[string]domain.JourneyState{}
	for state, work := range stages {
		if work.milestone == nil {
			continue
		}
		id := work.milestone.prescription("store1").TaskID()
		if prev, dup := seen[id]; dup {
			t.Fatalf("stages %q and %q both prescribe task id %q", prev, state, id)
		}
		seen[id] = state
	}
}

// A milestone describes where the store is, never a defect in a product.
func TestStages_MilestonesNeverBlockCheckout(t *testing.T) {
	for state, work := range stages {
		if work.milestone == nil {
			continue
		}
		assert.False(t, work.milestone.prescription("store1").BlocksCheckout,
			"stage %q prescribes a checkout-blocking milestone", state)
	}
}

// Owner and resolution have to agree about who can finish the work: anything
// owned by Google finishes when Google says so, which is external_wait, and
// external_wait is what Resolve refuses and Retire closes.
func TestStages_GoogleMilestonesAreExternalWait(t *testing.T) {
	for state, work := range stages {
		if work.milestone == nil || work.milestone.owner != domain.TaskOwnerExternal {
			continue
		}
		assert.Equal(t, domain.TaskResolutionExternalWait, work.milestone.resolution,
			"stage %q is owned by Google but does not wait on them", state)
	}
}

// Prospect and Live are deliberately absent: nothing is prescribed before
// pairing, and Live is meant to be quiet.
func TestStages_ProspectAndLivePrescribeNothing(t *testing.T) {
	for _, state := range []domain.JourneyState{domain.JourneyProspect, domain.JourneyLive} {
		_, ok := stages[state]
		assert.False(t, ok, "stage %q should prescribe no work", state)
	}
}

// Ready's work is pre-validation against the store's own UCP endpoints — it
// has no milestone of its own, only delegated content.
func TestStages_ReadyDelegatesToValidation(t *testing.T) {
	work, ok := stages[domain.JourneyReady]
	require.True(t, ok)
	assert.Nil(t, work.milestone)
	assert.Equal(t, delegateValidation, work.delegate)
}
