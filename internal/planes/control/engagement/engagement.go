// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package engagement carries the operator-facing labelling of the five
// concierge stages a merchant moves through from prospect to live — the
// pure store journey (creation, connection, sync, verification, alive).
//
// It is descriptive, not decisive. domain.JourneyState is the lifecycle
// truth and its transition table owns every move; this package only supplies
// the display text the console shows alongside it — stage name, description,
// typical duration, and the actions an operator usually performs there.
//
// It used to also carry EntryCheck/ExitCheck predicates: a second,
// divergent statement of when a store may move between stages, which
// nothing ever called. They contradicted the journey's transition table in
// places and were removed rather than reconciled — a rule the state machine
// does not consult is not a rule.
package engagement

import (
	"slices"

	"github.com/samber/lo"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// StageCriteria is one stage's display text and its task-owner rollup.
type StageCriteria struct {
	Stage       domain.EngagementStage
	Name        string
	Description string
	// RequiredTasks/OptionalTasks name task owners (domain.TaskOwner), not
	// task types: a stage's required owner must have no open (non-done)
	// tasks before the stage is considered clear. Stages whose completion
	// the journey's own transition already settles leave these nil rather
	// than answering the same question a second time — see the notes on the
	// checklist and validation stages below.
	RequiredTasks   []domain.TaskOwner
	OptionalTasks   []domain.TaskOwner
	DaysInStage     int      // Typical duration (for staleness detection)
	OperatorActions []string // Actions operator typically performs
}

// UnresolvedRequiredOwners returns the subset of c.RequiredTasks that still
// have at least one non-done task among openTasks. An empty result means
// every required owner has cleared their action items (or never had any).
func (c StageCriteria) UnresolvedRequiredOwners(openTasks []domain.Task) []domain.TaskOwner {
	return lo.Filter(c.RequiredTasks, func(owner domain.TaskOwner, _ int) bool {
		return lo.SomeBy(openTasks, func(t domain.Task) bool {
			return t.Owner == owner && t.State != domain.TaskStateDone
		})
	})
}

// WorkflowStages defines the 6-stage engagement workflow.
var WorkflowStages = []StageCriteria{
	{
		Stage:       domain.StageProsepct,
		Name:        "Prospect",
		Description: "Pre-audit, shortlisted merchant, pre-pairing",
		DaysInStage: 7,
		OperatorActions: []string{
			"run_internal_audit",
			"compute_readiness_score",
			"identify_top_blockers",
			"prepare_pitch_deck",
		},
	},
	{
		Stage:       domain.StageKickoff,
		Name:        "Kickoff",
		Description: "Setup fee invoiced, plugin installed & paired, first sync queued",
		DaysInStage: 1,
		OperatorActions: []string{
			"generate_pairing_code",
			"invoice_setup_fee",
			"capture_business_facts",
			"trigger_initial_catalog_import",
		},
	},
	{
		Stage:         domain.StageRemediation,
		Name:          "Readiness Remediation",
		Description:   "Operator applies fixes, assigns merchant-action items, hand-resolves mappings",
		DaysInStage:   5,
		RequiredTasks: []domain.TaskOwner{domain.TaskOwnerMerchant}, // Merchant must resolve their action items
		OptionalTasks: []domain.TaskOwner{domain.TaskOwnerSarvex},   // Sarvex remediations may still be open
		OperatorActions: []string{
			"apply_batch_fixes",
			"assign_merchant_actions",
			"hand_resolve_mappings",
			"tag_learned_mappings",
		},
	},
	{
		Stage:       domain.StageValidation,
		Name:        "Validation",
		Description: "Pre-validation suite runs against the store's own UCP endpoints, failures resolved",
		DaysInStage: 3,
		// No RequiredTasks: validation raises its own spine tasks (the "run
		// pre-validation" milestone and, on failure, a detected task), and
		// SignalStoreActivated is the authority on whether the store cleared
		// this stage — a task-owner rollup here would be a second, laggier
		// answer to a question the transition has already settled.
		OperatorActions: []string{
			"run_validation_suite",
			"resolve_validation_failures",
		},
	},
	{
		Stage:         domain.StageGoLive,
		Name:          "Go-Live & Handover",
		Description:   "Store's UCP profile verified and active, monitoring live, operator walkthrough, convert to subscription",
		DaysInStage:   1,
		OptionalTasks: []domain.TaskOwner{domain.TaskOwnerMerchant, domain.TaskOwnerSarvex}, // May still have minor items
		OperatorActions: []string{
			"confirm_profile_active",
			"activate_monitoring",
			"operator_console_walkthrough",
			"capture_case_study_seed",
			"convert_to_subscription",
		},
	},
}

// GetStage returns the criteria for a given stage.
func GetStage(stage domain.EngagementStage) *StageCriteria {
	i := slices.IndexFunc(WorkflowStages, func(s StageCriteria) bool { return s.Stage == stage })
	if i < 0 {
		return nil
	}
	return &WorkflowStages[i]
}

// ComputeStageFromState derives a store's engagement stage from its journey
// state. It is the only thing that ever sets the stored stage
// (journey_service.go, on every accepted transition) — so it is the
// definition, and worth reading as one.
func ComputeStageFromState(state domain.JourneyState) domain.EngagementStage {
	switch state {
	case domain.JourneyProspect:
		return domain.StageProsepct
	case domain.JourneyConnected:
		return domain.StageKickoff
	case domain.JourneySynced:
		return domain.StageRemediation
	case domain.JourneyReady:
		return domain.StageValidation // Pre-validation runs once readiness is met.
	case domain.JourneyLive:
		return domain.StageGoLive
	default:
		return domain.StageProsepct
	}
}
