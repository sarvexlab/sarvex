// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package domain

import "time"

// JourneyState is the single source of truth for where a merchant is in the
// onboarding lifecycle. The state machine in the control plane owns all
// transitions; nothing else may infer state by joining tables.
type JourneyState string

const (
	JourneyProspect  JourneyState = "prospect"  // Stage 0: pre-audit, pre-pairing
	JourneyConnected JourneyState = "connected" // Stage 1: pairing complete, first import queued
	JourneySynced    JourneyState = "synced"    // Stage 2-3: initial import + scoring done, remediation underway
	JourneyReady     JourneyState = "ready"     // Stage 2: readiness threshold met, pre-validation runs
	JourneyLive      JourneyState = "live"      // Stage 3: store's UCP profile is active (activation.UCPStateActive)
)

// EngagementStage tracks which of the 5 concierge stages the merchant is in.
//
// The store journey ends at Live once the store's own UCP profile is
// verified and active, independent of any surface enrollment.
type EngagementStage string

const (
	StageProsepct    EngagementStage = "prospect"    // Stage 0: pre-audit
	StageKickoff     EngagementStage = "kickoff"     // Stage 1: setup + pairing
	StageRemediation EngagementStage = "remediation" // Stage 2: readiness fixes
	StageValidation  EngagementStage = "validation"  // Stage 3: pre-validation against the store's own UCP endpoints
	StageGoLive      EngagementStage = "go_live"     // Stage 4: live + handover
)

// JourneyHistoryEntry records a single state the store occupied and when it
// was entered. History is append-only; entries are never edited or removed.
type JourneyHistoryEntry struct {
	State     JourneyState `json:"state"`
	EnteredAt time.Time    `json:"entered_at"`
}

// JourneyNextAction points to the highest-priority open task.
type JourneyNextAction struct {
	TaskID string `json:"task_id"`
	Title  string `json:"title"`
}

// TaskCounts summarises open tasks by owner.
type TaskCounts struct {
	Merchant int `json:"merchant"`
	Sarvex   int `json:"sarvex"`
	External int `json:"external"`
}

// RequiredTaskStatus reports whether a stage's required task owner still has
// unresolved (non-done) tasks blocking exit from the current stage.
type RequiredTaskStatus struct {
	Owner    TaskOwner `json:"owner"`
	Resolved bool      `json:"resolved"`
}

// Journey is the merchant's current position in the onboarding lifecycle.
type Journey struct {
	State          JourneyState          `json:"state"`
	History        []JourneyHistoryEntry `json:"history"`
	Stage          EngagementStage       `json:"stage"`
	EnteredStageAt time.Time             `json:"entered_stage_at"`
	NextAction     *JourneyNextAction    `json:"next_action"`
	OpenTaskCounts TaskCounts            `json:"open_task_counts"`
	EnteredStateAt time.Time             `json:"entered_state_at"`

	// Operator/console-facing stage info, computed from the engagement
	// workflow definition (internal/planes/control/engagement) for j.Stage.
	StageName           string               `json:"stage_name"`
	StageDescription    string               `json:"stage_description"`
	OperatorActions     []string             `json:"operator_actions,omitempty"`
	TypicalDaysInStage  int                  `json:"typical_days_in_stage,omitempty"`
	StageElapsedDays    int                  `json:"stage_elapsed_days"`
	RequiredTasksStatus []RequiredTaskStatus `json:"required_tasks_status,omitempty"`
}
