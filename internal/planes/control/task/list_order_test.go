// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package task_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
)

// The feed's order is a product decision, not an incidental one: the
// merchant's first screen must lead with what is stopping their checkout.
// It used to sort on task id, which — ids being source:store:source_ref —
// ordered the Path alphabetically by source and could push a whole source
// off the first page.

func seededRepo(t *testing.T, models ...*task.TaskModel) task.Service {
	t.Helper()
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel, len(models))}
	for _, m := range models {
		repo.tasks[m.ID] = m
	}
	return task.NewTaskService(repo, nil, nil, nil)
}

func feedTask(id string, blocks bool, createdAt time.Time) *task.TaskModel {
	return &task.TaskModel{
		ID:             id,
		MerchantID:     "merchant-1",
		StoreID:        "store-1",
		Title:          id,
		Owner:          string(domain.TaskOwnerMerchant),
		Resolution:     string(domain.TaskResolutionGuided),
		State:          string(domain.TaskStateOpen),
		BlocksCheckout: blocks,
		Source:         string(domain.TaskSourceReadiness),
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	}
}

func TestTaskService_List_BlockingFirstThenNewest(t *testing.T) {
	base := time.Date(2026, 4, 8, 9, 0, 0, 0, time.UTC)
	svc := seededRepo(t,
		// Alphabetically last by id, so an id sort would put it first.
		feedTask("validation:store-1:old", false, base),
		feedTask("catalog_import:store-1:recent", false, base.Add(2*time.Hour)),
		feedTask("checklist:store-1:blocking", true, base.Add(-24*time.Hour)),
	)

	got, err := svc.List(context.Background(), "merchant-1", nil, "", 20, nil, nil)
	require.NoError(t, err)
	require.Len(t, got.Data, 3)

	// The oldest task leads because it is the one blocking checkout.
	assert.Equal(t, "checklist:store-1:blocking", got.Data[0].TaskID)
	assert.Equal(t, "catalog_import:store-1:recent", got.Data[1].TaskID)
	assert.Equal(t, "validation:store-1:old", got.Data[2].TaskID)
}

func TestTaskService_List_PagesWithCursor(t *testing.T) {
	base := time.Date(2026, 4, 8, 9, 0, 0, 0, time.UTC)
	svc := seededRepo(t,
		feedTask("t-a", false, base.Add(3*time.Hour)),
		feedTask("t-b", false, base.Add(2*time.Hour)),
		feedTask("t-c", false, base.Add(time.Hour)),
	)

	first, err := svc.List(context.Background(), "merchant-1", nil, "", 2, nil, nil)
	require.NoError(t, err)
	require.Len(t, first.Data, 2)
	require.True(t, first.Page.HasMore)
	require.NotNil(t, first.Page.Cursor)

	second, err := svc.List(context.Background(), "merchant-1", nil, *first.Page.Cursor, 2, nil, nil)
	require.NoError(t, err)
	require.Len(t, second.Data, 1)
	assert.False(t, second.Page.HasMore)
	assert.Nil(t, second.Page.Cursor)

	// No task appears on both pages, and none is skipped between them.
	assert.Equal(t, []string{"t-a", "t-b", "t-c"}, []string{
		first.Data[0].TaskID, first.Data[1].TaskID, second.Data[0].TaskID,
	})
}

// Tasks raised in the same batch share a created_at, so the id tiebreak has
// to carry the page boundary on its own.
func TestTaskService_List_PagesThroughIdenticalTimestamps(t *testing.T) {
	at := time.Date(2026, 4, 8, 9, 0, 0, 0, time.UTC)
	svc := seededRepo(t,
		feedTask("t-a", false, at),
		feedTask("t-b", false, at),
		feedTask("t-c", false, at),
	)

	first, err := svc.List(context.Background(), "merchant-1", nil, "", 2, nil, nil)
	require.NoError(t, err)
	require.Len(t, first.Data, 2)
	require.NotNil(t, first.Page.Cursor)

	second, err := svc.List(context.Background(), "merchant-1", nil, *first.Page.Cursor, 2, nil, nil)
	require.NoError(t, err)
	require.Len(t, second.Data, 1)
	// Descending id, matching the sort's last term.
	assert.Equal(t, "t-c", first.Data[0].TaskID)
	assert.Equal(t, "t-b", first.Data[1].TaskID)
	assert.Equal(t, "t-a", second.Data[0].TaskID)
}

func TestTaskService_List_LastPageHasNoCursor(t *testing.T) {
	svc := seededRepo(t, feedTask("t-a", false, time.Now().UTC()))

	got, err := svc.List(context.Background(), "merchant-1", nil, "", 20, nil, nil)
	require.NoError(t, err)
	assert.False(t, got.Page.HasMore)
	assert.Nil(t, got.Page.Cursor)
}

// A cursor the service didn't issue is an error, not a silent restart from
// page one — otherwise a client paginating with a stale cursor would
// re-read the top of the feed believing it had moved on.
func TestTaskService_List_RejectsInvalidCursor(t *testing.T) {
	svc := seededRepo(t, feedTask("t-a", false, time.Now().UTC()))

	_, err := svc.List(context.Background(), "merchant-1", nil, "readiness:store-1:HP-1042", 20, nil, nil)
	require.ErrorIs(t, err, task.ErrInvalidCursor)
}
