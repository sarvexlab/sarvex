// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package journey

import (
	"context"
	"slices"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
)

// What each stage of the funnel puts on the merchant's Path.
//
// sarvex-v1-sequence.md: "on journey transition, reconcile prescribed tasks
// … Both detected and prescribed tasks live on the one spine, differ only by
// owner, and are created by the single task-service writer."
//
// This file is that reconciliation, and `stages` below is the whole of it:
// one table, one row per journey state, answering "what work does a store
// have because it is here". It replaces a split where the journey's own
// milestones lived in one map and the work it delegates lived in a switch
// several functions away — you had to read both to know what Ready did.
//
// Two kinds of work appear, because two kinds exist:
//
//   - a milestone: a task the journey itself owns, outstanding exactly
//     while the store sits in one state. Entering raises it, leaving
//     retires it, and nothing else needs to know.
//   - delegated work: tasks whose *content* belongs to another service —
//     the checklist's items, validation's pre-validation run. The journey
//     says when they start; the owning service says what they are and
//     retires them on its own terms (an item verified, a suite run). So
//     delegation is raise-only here.
//
// Not covered: "Connect your store". Pairing has no fitting value in the
// contract's task `source` enum (readiness | checklist | validation |
// consistency | monitoring | catalog_import), and inventing one would be a
// change to sarvex-v1-control-plane.yaml. The connector record is the proof
// that pairing happened, and the console reads it directly.

// delegated names a service that owns a stage's task content. A named
// constant rather than a closure in the table so `stages` stays plain data
// — readable at a glance, and free of the service wiring that would
// otherwise have to be available where the table is declared.
type delegated int

const (
	delegateNone delegated = iota
	delegateValidation
)

// milestone is a prescribed task outstanding while a store occupies one
// journey state. Its source/source_ref pair is the spine identity the
// console matches presentation on, so both are fixed here rather than
// derived at raise time.
type milestone struct {
	source     domain.TaskSource
	sourceRef  string
	title      string
	why        string
	owner      domain.TaskOwner
	resolution domain.TaskResolution
}

// stageWork is everything one journey state puts on the Path.
type stageWork struct {
	milestone *milestone
	delegate  delegated
}

// journeyOrder is the funnel in order. Used to decide which stages are
// behind the store and can therefore be retired. Kept separate from
// transitionTable, which describes legal moves rather than progression (it
// also carries the regressions back to Prospect).
var journeyOrder = []domain.JourneyState{
	domain.JourneyProspect,
	domain.JourneyConnected,
	domain.JourneySynced,
	domain.JourneyReady,
	domain.JourneyLive,
}

// stages is the single source of truth for stage-prescribed work.
//
// Prospect and Live are absent deliberately: nothing is prescribed before
// pairing, and Live is the quiet the merchant was working toward.
var stages = map[domain.JourneyState]stageWork{
	domain.JourneyConnected: {
		milestone: &milestone{
			source:     domain.TaskSourceCatalogImport,
			sourceRef:  domain.TaskRefInitialImport,
			title:      "Importing your catalog",
			why:        "We're reading your products out of your store so agents can see them. Nothing needed from you.",
			owner:      domain.TaskOwnerSarvex,
			resolution: domain.TaskResolutionAutomatic,
		},
	},
	domain.JourneySynced: {
		milestone: &milestone{
			source:     domain.TaskSourceReadiness,
			sourceRef:  domain.TaskRefInitialAudit,
			title:      "Auditing your catalog",
			why:        "Checking completeness, identifiers, categories, and policy facts across every product.",
			owner:      domain.TaskOwnerSarvex,
			resolution: domain.TaskResolutionAutomatic,
		},
	},
	// Once readiness is met, pre-validation runs against the store's own
	// UCP serving-plane endpoints (Sarvex's own suite — see
	// validation.Orchestrator.RunValidation). The store's own
	// endpoint verification is what the Ready stage waits on.
	domain.JourneyReady: {
		delegate: delegateValidation,
	},
}

// prescription renders the milestone as a spine prescription for one store.
func (m milestone) prescription(storeID domain.StoreID) task.Prescription {
	return task.Prescription{
		StoreID:      storeID,
		Source:       m.source,
		SourceRef:    m.sourceRef,
		Title:        m.title,
		WhyItMatters: m.why,
		Owner:        m.owner,
		Resolution:   m.resolution,
		// A milestone describes the store's progress, never a product
		// defect, so none of them blocks checkout.
		BlocksCheckout: false,
	}
}

// reconcileStageWork brings a store's prescribed tasks in line with the
// state it is now in: the current state's work is raised if missing, and
// every earlier state's milestone is retired.
//
// Written as a converge-to-state pass rather than a per-transition action so
// it is idempotent and self-healing — Get calls it on every read, so a store
// that skipped a transition, or predates a prescription existing, catches up
// without a backfill.
//
// Best-effort throughout: a failure is logged, never propagated. The
// transition itself is already durably persisted, and the next read runs
// this again.
func (s *JourneyService) reconcileStageWork(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, state domain.JourneyState) {
	if s.tasks == nil {
		return
	}

	idx := slices.Index(journeyOrder, state)
	if idx < 0 {
		return // unknown state; nothing safe to infer
	}

	// Everything behind the store is finished by definition of having moved
	// on. Only milestones are retired here: delegated work is retired by
	// the service that owns it, on its own evidence.
	for _, past := range journeyOrder[:idx] {
		if w, ok := stages[past]; ok && w.milestone != nil {
			s.retireMilestone(ctx, merchantID, storeID, *w.milestone)
		}
	}

	work, ok := stages[state]
	if !ok {
		return
	}
	if work.milestone != nil {
		s.raiseMilestone(ctx, merchantID, storeID, *work.milestone)
	}
	s.runDelegate(ctx, merchantID, storeID, work.delegate)
}

func (s *JourneyService) raiseMilestone(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, m milestone) {
	p := m.prescription(storeID)
	if err := task.Ensure(ctx, s.tasks, merchantID, p); err != nil {
		s.log.Error("failed to raise milestone task",
			"error", err, "merchant_id", merchantID, "store_id", storeID, "task_id", p.TaskID())
	}
}

func (s *JourneyService) retireMilestone(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, m milestone) {
	taskID := m.prescription(storeID).TaskID()
	if err := task.Retire(ctx, s.tasks, merchantID, taskID); err != nil {
		s.log.Error("failed to retire milestone task",
			"error", err, "merchant_id", merchantID, "store_id", storeID, "task_id", taskID)
	}
}

// runDelegate hands off to the service that owns a stage's task content.
// Both delegates are idempotent no-ops once caught up, so running them on
// every read costs nothing and keeps the Path self-correcting.
func (s *JourneyService) runDelegate(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, d delegated) {
	switch d {
	case delegateNone:
		return
	case delegateValidation:
		if s.validation == nil {
			return
		}
		if err := s.validation.EnsureMilestoneTask(ctx, merchantID, storeID); err != nil {
			s.log.Error("failed to raise validation milestone task", "error", err, "merchant_id", merchantID, "store_id", storeID)
		}
	}
}
