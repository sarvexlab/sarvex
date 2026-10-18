// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package task_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func setupTestMongoDB(t *testing.T) *mongo.Database {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://admin:password@localhost:27017/?directConnection=true"))
	require.NoError(t, err)

	db := client.Database("sarvex_task_test")

	_ = db.Drop(ctx)

	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})

	return db
}

func TestMongoTaskRepo_Insert_FindByMerchantAndID(t *testing.T) {
	t.Skip("requires MongoDB running locally")

	db := setupTestMongoDB(t)
	repo, err := task.NewMongoTaskRepository(db)
	require.NoError(t, err)
	ctx := context.Background()

	merchantID := domain.MerchantID("merchant-1")
	m := &task.TaskModel{
		ID:             "task-1",
		MerchantID:     string(merchantID),
		Title:          "Test task",
		WhyItMatters:   "Important",
		Owner:          "merchant",
		Resolution:     "one_click",
		State:          "open",
		BlocksCheckout: true,
		Source:         "readiness",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}

	err = repo.Insert(ctx, m)
	require.NoError(t, err)

	found, err := repo.FindByMerchantAndID(ctx, merchantID, "task-1")
	require.NoError(t, err)
	require.NotNil(t, found)
	require.Equal(t, "task-1", found.ID)
	require.Equal(t, "Test task", found.Title)
}

func TestMongoTaskRepo_FindByMerchantAndID_NotFound(t *testing.T) {
	t.Skip("requires MongoDB running locally")

	db := setupTestMongoDB(t)
	repo, err := task.NewMongoTaskRepository(db)
	require.NoError(t, err)
	ctx := context.Background()

	merchantID := domain.MerchantID("merchant-1")
	_, err = repo.FindByMerchantAndID(ctx, merchantID, "nonexistent")
	require.ErrorIs(t, err, task.ErrNotFound)
}

func TestMongoTaskRepo_FindByMerchantAndID_TenantIsolation(t *testing.T) {
	t.Skip("requires MongoDB running locally")

	db := setupTestMongoDB(t)
	repo, err := task.NewMongoTaskRepository(db)
	require.NoError(t, err)
	ctx := context.Background()

	m := &task.TaskModel{
		ID:             "task-1",
		MerchantID:     "merchant-1",
		Title:          "Test task",
		WhyItMatters:   "Important",
		Owner:          "merchant",
		Resolution:     "one_click",
		State:          "open",
		BlocksCheckout: true,
		Source:         "readiness",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}

	err = repo.Insert(ctx, m)
	require.NoError(t, err)

	merchantID2 := domain.MerchantID("merchant-2")
	_, err = repo.FindByMerchantAndID(ctx, merchantID2, "task-1")
	require.ErrorIs(t, err, task.ErrNotFound)
}

func TestMongoTaskRepo_ListByMerchant(t *testing.T) {
	t.Skip("requires MongoDB running locally")

	db := setupTestMongoDB(t)
	repo, err := task.NewMongoTaskRepository(db)
	require.NoError(t, err)
	ctx := context.Background()

	merchantID := domain.MerchantID("merchant-1")
	now := time.Now().UTC()

	for i := range 3 {
		m := &task.TaskModel{
			ID:             "task-" + string(rune('1'+rune(i))),
			MerchantID:     string(merchantID),
			Title:          "Task " + string(rune('1'+i)),
			WhyItMatters:   "Important",
			Owner:          "merchant",
			Resolution:     "one_click",
			State:          "open",
			BlocksCheckout: false,
			Source:         "readiness",
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		err = repo.Insert(ctx, m)
		require.NoError(t, err)
	}

	tasks, hasMore, err := repo.ListByMerchant(ctx, merchantID, nil, nil, nil, nil, 20)
	require.NoError(t, err)
	require.Len(t, tasks, 3)
	require.False(t, hasMore)
}

func TestMongoTaskRepo_ListByMerchant_ScopedByStore(t *testing.T) {
	t.Skip("requires MongoDB running locally")

	db := setupTestMongoDB(t)
	repo, err := task.NewMongoTaskRepository(db)
	require.NoError(t, err)
	ctx := context.Background()

	merchantID := domain.MerchantID("merchant-1")
	now := time.Now().UTC()

	for i, storeID := range []string{"store-1", "store-1", "store-2"} {
		m := &task.TaskModel{
			ID:             fmt.Sprintf("task-%s-%d", storeID, i),
			MerchantID:     string(merchantID),
			StoreID:        storeID,
			Title:          "Task",
			WhyItMatters:   "Important",
			Owner:          "merchant",
			Resolution:     "one_click",
			State:          "open",
			BlocksCheckout: false,
			Source:         "readiness",
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		err = repo.Insert(ctx, m)
		require.NoError(t, err)
	}

	store1 := domain.StoreID("store-1")
	tasks, hasMore, err := repo.ListByMerchant(ctx, merchantID, &store1, nil, nil, nil, 20)
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	require.False(t, hasMore)
	for _, tk := range tasks {
		require.Equal(t, "store-1", tk.StoreID)
	}
}

func TestMongoTaskRepo_ListOpenByMerchants(t *testing.T) {
	t.Skip("requires MongoDB running locally")

	db := setupTestMongoDB(t)
	repo, err := task.NewMongoTaskRepository(db)
	require.NoError(t, err)
	ctx := context.Background()
	now := time.Now().UTC()

	seed := []*task.TaskModel{
		{ID: "t1", MerchantID: "merchant-1", StoreID: "store-1", Owner: "merchant", State: "open", CreatedAt: now, UpdatedAt: now},
		{ID: "t2", MerchantID: "merchant-1", StoreID: "store-1", Owner: "sarvex", State: "open", CreatedAt: now, UpdatedAt: now},
		{ID: "t3", MerchantID: "merchant-2", StoreID: "store-2", Owner: "external", State: "open", CreatedAt: now, UpdatedAt: now},
		// Not open: must be excluded.
		{ID: "t4", MerchantID: "merchant-1", StoreID: "store-1", Owner: "merchant", State: "done", CreatedAt: now, UpdatedAt: now},
		// Merchant outside the queried batch: must be excluded.
		{ID: "t5", MerchantID: "merchant-3", StoreID: "store-3", Owner: "merchant", State: "open", CreatedAt: now, UpdatedAt: now},
	}
	for _, m := range seed {
		require.NoError(t, repo.Insert(ctx, m))
	}

	tasks, err := repo.ListOpenByMerchants(ctx, []domain.MerchantID{"merchant-1", "merchant-2"})
	require.NoError(t, err)
	require.Len(t, tasks, 3)
	for _, tk := range tasks {
		require.Equal(t, "open", tk.State)
		require.NotEqual(t, "merchant-3", tk.MerchantID)
	}
}

func TestMongoTaskRepo_ListOpenByMerchants_EmptyInput(t *testing.T) {
	t.Skip("requires MongoDB running locally")

	db := setupTestMongoDB(t)
	repo, err := task.NewMongoTaskRepository(db)
	require.NoError(t, err)

	tasks, err := repo.ListOpenByMerchants(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, tasks)
}

func TestMongoTaskRepo_UpdateState(t *testing.T) {
	t.Skip("requires MongoDB running locally")

	db := setupTestMongoDB(t)
	repo, err := task.NewMongoTaskRepository(db)
	require.NoError(t, err)
	ctx := context.Background()

	merchantID := domain.MerchantID("merchant-1")
	m := &task.TaskModel{
		ID:             "task-1",
		MerchantID:     string(merchantID),
		Title:          "Test task",
		WhyItMatters:   "Important",
		Owner:          "merchant",
		Resolution:     "one_click",
		State:          "open",
		BlocksCheckout: true,
		Source:         "readiness",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}

	err = repo.Insert(ctx, m)
	require.NoError(t, err)

	note := "Task completed"
	updated, err := repo.UpdateState(ctx, "task-1", "done", &note, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, "done", updated.State)
	require.NotNil(t, updated.ResolutionNote)
	require.Equal(t, note, *updated.ResolutionNote)
}
