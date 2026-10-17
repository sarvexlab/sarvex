// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package domain

import "time"

// BoardCard is the operator board's view of a single merchant.
type BoardCard struct {
	MerchantID MerchantID `json:"merchant_id"`
	// StoreID is the store whose journey state this card reflects. V1 is
	// one store per merchant, so this is unambiguous today; a merchant
	// with multiple stores would need the board to aggregate across them
	// (which store's journey "represents" the merchant) — not built yet,
	// see mongoBoardRepo.ListCards.
	StoreID        StoreID        `json:"store_id"`
	Name           string         `json:"name"`
	State          JourneyState   `json:"state"`
	DaysInState    int            `json:"days_in_state"`
	OpenTaskCounts TaskCounts     `json:"open_task_counts"`
	OpenTaskIDs    TaskIDsByOwner `json:"open_task_ids"`
	Blocked        bool           `json:"blocked"`
	Stale          bool           `json:"stale"`
}

// TaskIDsByOwner groups task IDs by their owner for operator navigation.
type TaskIDsByOwner struct {
	Merchant []string `json:"merchant,omitempty"`
	Sarvex   []string `json:"sarvex,omitempty"`
	External []string `json:"external,omitempty"`
}

// StoreTaskSummary is one store's open-task breakdown by owner: the counts
// and IDs a board card needs. Returned per store_id (never per merchant_id —
// see BoardCard.StoreID's doc comment) so a batched multi-merchant task
// lookup can still be assembled back into one summary per card without
// conflating two stores belonging to the same merchant.
type StoreTaskSummary struct {
	Counts TaskCounts
	IDs    TaskIDsByOwner
}

// ManualStepDisposition classifies what should happen to a captured manual step.
type ManualStepDisposition string

const (
	DispositionUnclassified ManualStepDisposition = "unclassified"
	DispositionAutomate     ManualStepDisposition = "automate"
	DispositionTemplatise   ManualStepDisposition = "templatise"
	DispositionAcceptManual ManualStepDisposition = "accept_manual"
)

// ManualStep records an action an operator performed by hand.
// These accumulate into the Phase B automation backlog.
type ManualStep struct {
	ManualStepID         string                `json:"manual_step_id,omitempty"`
	TargetMerchantID     MerchantID            `json:"target_merchant_id,omitempty"`
	EngagementStage      EngagementStage       `json:"engagement_stage,omitempty"` // Stage when step was captured
	Description          string                `json:"description"`
	Disposition          ManualStepDisposition `json:"disposition"`
	CreatedAt            *time.Time            `json:"created_at,omitempty"`
	DaysInStageAtCapture int                   `json:"days_in_stage_at_capture,omitempty"` // For metrics: how long in stage when this step was needed?
}

// TimelineDatapoint represents manual effort by stage on a specific date.
type TimelineDatapoint struct {
	Date           string          `json:"date"`                        // RFC 3339 date (YYYY-MM-DD)
	Stage          EngagementStage `json:"stage"`                       // engagement stage
	Count          int             `json:"count"`                       // number of manual steps captured that day
	AvgDaysInStage int             `json:"avg_days_in_stage,omitempty"` // average days merchant had been in stage
}

// AuditEntry is an immutable record of an operator action.
type AuditEntry struct {
	EntryID          string         `json:"entry_id"`
	OperatorUserID   string         `json:"operator_user_id"`
	TargetMerchantID MerchantID     `json:"target_merchant_id"`
	Action           string         `json:"action"`
	Detail           map[string]any `json:"detail"`
	OccurredAt       time.Time      `json:"occurred_at"`
}

// ValidationStatus is the current state of all validation phases for a merchant.
type ValidationStatus struct {
	PreValidation     PreValidationState  `json:"pre_validation"`
	Sandbox           SandboxState        `json:"sandbox"`
	Production        ProductionState     `json:"production"`
	TargetSpecVersion string              `json:"target_spec_version"`
	LastRunAt         *time.Time          `json:"last_run_at"`
	OpenFailures      []ValidationFailure `json:"open_failures"`
}

type PreValidationState string

const (
	PreValidationNotRun  PreValidationState = "not_run"
	PreValidationPassing PreValidationState = "passing"
	PreValidationFailing PreValidationState = "failing"
)

type SandboxState string

const (
	SandboxNotSubmitted SandboxState = "not_submitted"
	SandboxPending      SandboxState = "pending"
	SandboxPassed       SandboxState = "passed"
	SandboxFailed       SandboxState = "failed"
)

type ProductionState string

const (
	ProductionNotSubmitted ProductionState = "not_submitted"
	ProductionPending      ProductionState = "pending"
	ProductionLive         ProductionState = "live"
)

// ValidationFailure is a single failing requirement from a conformance run.
type ValidationFailure struct {
	Requirement string         `json:"requirement"`
	SpecSection string         `json:"spec_section"`
	Expected    map[string]any `json:"expected"`
	Received    map[string]any `json:"received"`
	Summary     string         `json:"summary"`
}

// ConformanceResult is the pass/fail outcome of a single validation run.
type ConformanceResult string

const (
	ConformancePass ConformanceResult = "pass"
	ConformanceFail ConformanceResult = "fail"
)

// ConformanceRecord is the audit trail for a single validation run.
// Pinned to catalogue and spec versions to enable reproducibility and compliance audit.
type ConformanceRecord struct {
	RunID                string            `json:"run_id"`
	MerchantID           MerchantID        `json:"merchant_id"`
	StoreID              StoreID           `json:"store_id"`
	RanAt                time.Time         `json:"ran_at"`
	SpecVersion          string            `json:"spec_version"`
	CatalogueVersion     string            `json:"catalogue_version"`      // UCP definitions catalogue version
	ScoringRubricVersion string            `json:"scoring_rubric_version"` // Scoring rules version
	Result               ConformanceResult `json:"result"`
	Passed               int               `json:"passed"`
	Failed               int               `json:"failed"`
	Warnings             int               `json:"warnings"`
}
