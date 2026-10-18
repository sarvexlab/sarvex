// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package domain

import "time"

// TaskOwner identifies which party must act on a task.
type TaskOwner string

const (
	TaskOwnerMerchant TaskOwner = "merchant"
	TaskOwnerSarvex   TaskOwner = "sarvex"
	TaskOwnerExternal TaskOwner = "external"
)

// TaskState is the current lifecycle state of a task.
type TaskState string

const (
	TaskStateOpen       TaskState = "open"
	TaskStateInProgress TaskState = "in_progress"
	TaskStateBlocked    TaskState = "blocked"
	TaskStateDone       TaskState = "done"
)

// TaskResolution describes how the task can be completed.
type TaskResolution string

const (
	TaskResolutionOneClick     TaskResolution = "one_click"
	TaskResolutionGuided       TaskResolution = "guided"
	TaskResolutionAutomatic    TaskResolution = "automatic"
	TaskResolutionExternalWait TaskResolution = "external_wait"
)

// DeriveTaskState is the state a task starts in, given who owns it and how
// it gets finished.
//
// State is derived rather than chosen because the three fields answer one
// question between them — who is doing this, and what is it waiting on —
// and every raiser that picked its own state got a different answer. Google
// checklist items were raised open while the identical Google milestones
// were raised blocked; Sarvex's automatic monitoring tasks were raised open
// while Sarvex's automatic import milestone was raised in_progress. The
// console then rendered the same situation two ways, because it had been
// told two different things.
//
// The rule, in the merchant's terms:
//
//   - blocked — someone outside Sarvex has it (external_wait). Nothing
//     anyone here can do but wait for their answer.
//   - in_progress — Sarvex is doing it right now, without being asked
//     (automatic). Nothing for the merchant to do.
//   - open — it is waiting on a person: the merchant's guided steps, or an
//     operator's one click.
//
// done is never a starting state; it is only ever reached through Resolve
// or Complete.
func DeriveTaskState(owner TaskOwner, resolution TaskResolution) TaskState {
	switch {
	case resolution == TaskResolutionExternalWait:
		return TaskStateBlocked
	case owner == TaskOwnerSarvex && resolution == TaskResolutionAutomatic:
		return TaskStateInProgress
	default:
		return TaskStateOpen
	}
}

// TaskSource identifies which domain emitted the task.
type TaskSource string

const (
	TaskSourceReadiness     TaskSource = "readiness"
	TaskSourceChecklist     TaskSource = "checklist"
	TaskSourceValidation    TaskSource = "validation"
	TaskSourceConsistency   TaskSource = "consistency"
	TaskSourceMonitoring    TaskSource = "monitoring"
	TaskSourceCatalogImport TaskSource = "catalog_import"
)

// Task is the unified actionable item across all domains.
type Task struct {
	UpdatedAt      time.Time           `json:"updated_at"`
	CreatedAt      time.Time           `json:"created_at"`
	SourceRef      *string             `json:"source_ref"`
	Note           *string             `json:"note,omitempty"`
	Finding        *ConsistencyFinding `json:"finding,omitempty"`
	Source         TaskSource          `json:"source"`
	State          TaskState           `json:"state"`
	TaskID         string              `json:"task_id"`
	Resolution     TaskResolution      `json:"resolution"`
	Owner          TaskOwner           `json:"owner"`
	WhyItMatters   string              `json:"why_it_matters"`
	Title          string              `json:"title"`
	StoreID        StoreID             `json:"store_id"`
	Remediations   []Remediation       `json:"remediations,omitempty"`
	Issues         []ProductIssue      `json:"issues,omitempty"`
	BlocksCheckout bool                `json:"blocks_checkout"`
}

// Source refs for the prescribed tasks the funnel raises at a known point in
// every store's journey. They live here, rather than with the services that
// raise them, because more than one package has to agree on them: the
// journey raises them, the task service reads them to tell a milestone apart
// from a finding, and they are the last segment of the task id the console
// matches presentation on (web/console/src/components/task/widgets/registry.ts).
//
// Refs for prescribed tasks whose identity comes from data — a checklist
// item id, a SKU — are not here; only the fixed ones are.
const (
	TaskRefInitialImport = "initial_import" // catalog_import: the first import
	TaskRefInitialAudit  = "initial_audit"  // readiness: the first audit
	TaskRefSandbox       = "sandbox"        // validation: Google's sandbox verdict
	TaskRefProduction    = "production"     // validation: Google's go-live review
)

// HasProductDrillIn reports whether this task's source_ref names a product,
// and so whether the drill-in payload (remediations, open issues) can be
// looked up for it.
//
// Readiness tasks are per-SKU findings — except the initial-audit milestone,
// which is source=readiness but describes the audit itself. Without this
// distinction the task service looked up remediations and issues for a
// product called "initial_audit" on every read of that task, logging two
// errors a time, and the console's drawer offered the merchant a
// "no automatic fix, update it in your store" message for work that Sarvex
// was in the middle of doing for them.
func (t Task) HasProductDrillIn() bool {
	return t.Source == TaskSourceReadiness &&
		t.SourceRef != nil &&
		*t.SourceRef != TaskRefInitialAudit
}

// IsOpen reports whether the task still counts as outstanding work.
//
// The single definition of "open" for the whole spine. Everything that
// summarises tasks — the journey's open_task_counts, the operator board's
// per-store counts, the console's feed sections — must go through this, so
// that a merchant, an operator, and the Path can never disagree about how
// much is left. Every non-done state is outstanding: in_progress is work
// underway, blocked is work waiting on someone else, and both are still
// ahead of the store.
func (t Task) IsOpen() bool {
	return t.State != TaskStateDone
}

// ResolveTaskRequest is the optional body for POST /tasks/{id}/resolve.
type ResolveTaskRequest struct {
	Note *string `json:"note,omitempty"`
}
