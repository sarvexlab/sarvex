// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package task

import (
	"context"
	"time"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

type TaskRepository interface {
	Insert(ctx context.Context, m *TaskModel) error
	FindByMerchantAndID(ctx context.Context, merchantID domain.MerchantID, taskID string) (*TaskModel, error)
	UpdateState(ctx context.Context, taskID string, state string, note *string, updatedAt time.Time) (*TaskModel, error)
	// ListByMerchant returns one page of the merchant's feed in listSort's
	// order. after is the position to resume from (nil for the first page).
	ListByMerchant(ctx context.Context, merchantID domain.MerchantID, storeID *domain.StoreID, owner *domain.TaskOwner, state *domain.TaskState, after *ListCursor, limit int) ([]*TaskModel, bool, error)
	// ListOpenByMerchants returns every open (non-done) task across the given merchant
	// IDs in one query — the operator board's batched alternative to calling
	// ListByMerchant once per (merchant, owner) pair. Unbounded per store:
	// open tasks are raised from a bounded content model (scoring rubric
	// rules, checklist items, consistency findings), not user-generated, so
	// a store's true open count never approaches a scale worth paginating.
	ListOpenByMerchants(ctx context.Context, merchantIDs []domain.MerchantID) ([]*TaskModel, error)
}
