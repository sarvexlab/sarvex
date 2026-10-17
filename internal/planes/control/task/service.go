// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package task owns the unified task spine that surfaces actionable items from
// all domains (readiness, checklist, validation, consistency, monitoring).
package task

import (
	"context"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// Service manages tasks across all emitting domains.
type Service interface {
	List(ctx context.Context, id domain.MerchantID, storeID *domain.StoreID, cursor string, limit int, owner *domain.TaskOwner, state *domain.TaskState) (domain.ListOf[domain.Task], error)
	Get(ctx context.Context, id domain.MerchantID, taskID string) (domain.Task, error)
	// Resolve closes a task on the merchant's behalf — the path behind
	// POST /tasks/{id}/resolve. It refuses anything the merchant has no
	// standing to close: an already-done task, or one whose resolution is
	// external_wait (only Google clears those). Callers that are the system
	// observing the work actually finish must use Complete instead.
	Resolve(ctx context.Context, id domain.MerchantID, taskID string, req domain.ResolveTaskRequest) (domain.Task, error)
	// Complete closes a task because the condition behind it has genuinely
	// cleared — a milestone's state was left behind, a checklist item was
	// verified, a rescore no longer finds the issue, Google came back. It is
	// the reconcilers' counterpart to Resolve and applies no
	// standing check, because the caller is the system reporting fact rather
	// than a merchant claiming completion. In particular it closes
	// external_wait tasks, which Resolve deliberately cannot: those are
	// exactly the tasks only the system can ever finish.
	Complete(ctx context.Context, id domain.MerchantID, taskID string, note *string) (domain.Task, error)
	// Reopen puts a finished task back to the state it started in, because the
	// condition behind it has recurred: an issue that was fixed (or marked
	// done) is detected again. The task keeps its id — raisers derive it from
	// the issue's identity — so without this a recurrence would find the done
	// task and be dropped as a duplicate. Reopening a task that is not done is
	// a no-op, so a recurrence seen on every pass is the common case, not an
	// error.
	Reopen(ctx context.Context, id domain.MerchantID, taskID string, note *string) (domain.Task, error)
	Raise(ctx context.Context, merchantID domain.MerchantID, req RaiseTaskRequest) (domain.Task, error)
	// ListOpenByMerchants returns each store's open-task summary (counts +
	// IDs by owner), keyed by store_id, for every store owned by any of the
	// given merchants — one batched call replacing what would otherwise be
	// List called once per (merchant, owner) pair. See BoardCard.StoreID's
	// doc comment on why the result is keyed by store_id, not merchant_id.
	ListOpenByMerchants(ctx context.Context, merchantIDs []domain.MerchantID) (map[domain.StoreID]domain.StoreTaskSummary, error)
}

// RaiseTaskRequest holds the parameters for raising a new task from domain events.
//
// There is no State field: the starting state follows from Owner and
// Resolution (domain.DeriveTaskState) and is applied by Raise. Letting each
// raiser choose meant identical situations arriving in different states —
// see that function's doc comment.
type RaiseTaskRequest struct {
	TaskID         string
	StoreID        string
	Title          string
	WhyItMatters   string
	Owner          string
	Resolution     string
	BlocksCheckout bool
	Source         string
	SourceRef      *string
	Finding        *domain.ConsistencyFinding
}
