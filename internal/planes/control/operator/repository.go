// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"context"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// ManualStepRepository is the persistence interface for manual steps.
type ManualStepRepository interface {
	// Insert creates a new manual step.
	Insert(ctx context.Context, m *ManualStepModel) error
	// UpdateDisposition reclassifies a step's disposition.
	UpdateDisposition(ctx context.Context, id string, disposition string) (*ManualStepModel, error)
	// ListByMerchant returns steps for a specific merchant, newest-first.
	// afterID is the cursor for pagination; pass "" for the first page.
	// hasMore is true when more records exist beyond the returned slice.
	ListByMerchant(ctx context.Context, merchantID domain.MerchantID, afterID string, limit int) ([]*ManualStepModel, bool, error)
	// ListByDisposition returns steps across all merchants filtered by disposition, newest-first.
	// Pass nil disposition to list all steps (for full backlog review).
	// afterID is the cursor for pagination; pass "" for the first page.
	// hasMore is true when more records exist beyond the returned slice.
	ListByDisposition(ctx context.Context, disposition *string, afterID string, limit int) ([]*ManualStepModel, bool, error)
	// ListByStage returns steps filtered by engagement stage and optional disposition, newest-first (Phase 10C+).
	// Pass nil disposition to list all steps in a given stage.
	// afterID is the cursor for pagination; pass "" for the first page.
	ListByStage(ctx context.Context, stage string, disposition *string, afterID string, limit int) ([]*ManualStepModel, bool, error)
	// BulkUpdateDisposition updates multiple steps' dispositions in one operation (Phase 10C+).
	// Returns the number of updated documents.
	BulkUpdateDisposition(ctx context.Context, stepIDs []string, disposition string) (int, error)
	// CountByStageAndDay returns manual step counts grouped by stage and date for timeline visualization (Phase 10C+).
	// If merchantID is nil, counts are across all merchants.
	CountByStageAndDay(ctx context.Context, merchantID *domain.MerchantID, startDate, endDate string) ([]TimelineAggregation, error)
}

// TimelineAggregation represents the MongoDB aggregation result for timeline metrics.
type TimelineAggregation struct {
	Date           string // YYYY-MM-DD
	Stage          string
	Count          int
	AvgDaysInStage float64
}

// AuditLogRepository is the persistence interface for the audit log.
// Append-only: no updates, no deletes.
type AuditLogRepository interface {
	// Append inserts a new audit entry.
	Append(ctx context.Context, e *AuditEntryModel) error
	// List returns entries, newest-first. targetMerchant=nil means all merchants.
	// afterID is the cursor for pagination; pass "" for the first page.
	// hasMore is true when more records exist beyond the returned slice.
	List(ctx context.Context, targetMerchant *domain.MerchantID, afterID string, limit int) ([]*AuditEntryModel, bool, error)
}

// BoardRepository is the persistence interface for the operator board.
// Reads cross-tenant journey state and account data.
type BoardRepository interface {
	// ListCards returns board cards, one per store's journey record
	// (ordered by store_id) — see BoardCard.StoreID's doc comment for the
	// one-store-per-merchant caveat this implies today.
	// state=nil means all states. attention=true filters to stale-or-blocked.
	// afterStoreID is the cursor for pagination; pass "" for the first page.
	// hasMore is true when more records exist beyond the returned slice.
	ListCards(ctx context.Context, state *domain.JourneyState, attention *bool, afterStoreID string, limit int) ([]*BoardCardModel, bool, error)
}

// TaskService is the interface to the unified task spine for fetching merchant task summaries.
type TaskService interface {
	// List returns tasks for a merchant, optionally scoped to one store, filtered by owner and state.
	List(ctx context.Context, id domain.MerchantID, storeID *domain.StoreID, cursor string, limit int, owner *domain.TaskOwner, state *domain.TaskState) (domain.ListOf[domain.Task], error)
	// ListOpenByMerchants returns each store's open-task summary, keyed by
	// store_id, for every store owned by any of the given merchants — the
	// Board's batched alternative to calling List once per (merchant, owner)
	// pair. See BoardCard.StoreID's doc comment on why the result is keyed
	// by store_id, not merchant_id.
	ListOpenByMerchants(ctx context.Context, merchantIDs []domain.MerchantID) (map[domain.StoreID]domain.StoreTaskSummary, error)
}
