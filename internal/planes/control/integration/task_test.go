// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedTask inserts a task directly into the repository and returns its ID.
func seedTask(t *testing.T, merchantID, title string) string {
	t.Helper()
	taskID := uuid.New().String()
	now := time.Now()
	err := env.taskRepo.Insert(context.Background(), &task.TaskModel{
		ID:           taskID,
		MerchantID:   merchantID,
		Title:        title,
		WhyItMatters: "integration test task",
		Owner:        "merchant",
		Resolution:   "one_click",
		State:        "open",
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	require.NoError(t, err)
	return taskID
}

// TestTask_List verifies GET /tasks returns a paginated list scoped to the
// calling merchant and follows the standard list envelope.
func TestTask_List(t *testing.T) {
	// Seed 3 tasks for merchantA.
	for i := 0; i < 3; i++ {
		seedTask(t, string(merchantAID), "Task A-"+string(rune('1'+i)))
	}

	resp := apiGet(t, "/v1/tasks?limit=2", tokenMerchantA)
	data, _, _ := assertListEnvelope(t, resp)
	assert.GreaterOrEqual(t, len(data), 1, "at least one task must be returned")
}

// TestTask_List_Empty verifies that a merchant with no tasks gets an empty list
// (not 404 or an error).
func TestTask_List_Empty(t *testing.T) {
	// merchantB has no tasks seeded in this test.
	resp := apiGet(t, "/v1/tasks", tokenMerchantB)
	data, hasMore, _ := assertListEnvelope(t, resp)
	assert.Len(t, data, 0)
	assert.False(t, hasMore)
}

// TestTask_Get verifies GET /tasks/:id returns the task for its owning merchant.
func TestTask_Get(t *testing.T) {
	taskID := seedTask(t, string(merchantAID), "Get This Task")

	t.Run("owner_gets_task", func(t *testing.T) {
		resp := apiGet(t, "/v1/tasks/"+taskID, tokenMerchantA)
		var result map[string]any
		decodeJSON(t, resp, http.StatusOK, &result)
		assert.Equal(t, taskID, result["task_id"])
		assert.Equal(t, "Get This Task", result["title"])
		assert.Equal(t, "open", result["state"])
	})

	t.Run("wrong_merchant_gets_404", func(t *testing.T) {
		resp := apiGet(t, "/v1/tasks/"+taskID, tokenMerchantB)
		assertErrorEnvelope(t, resp, http.StatusNotFound)
	})

	t.Run("nonexistent_task", func(t *testing.T) {
		resp := apiGet(t, "/v1/tasks/does-not-exist", tokenMerchantA)
		assertErrorEnvelope(t, resp, http.StatusNotFound)
	})
}

// TestTask_Resolve verifies POST /tasks/:id/resolve transitions the task state.
func TestTask_Resolve(t *testing.T) {
	taskID := seedTask(t, string(merchantAID), "Resolve This Task")

	resp := apiPost(t, "/v1/tasks/"+taskID+"/resolve", tokenMerchantA,
		map[string]any{"note": "fixed manually"})

	var result map[string]any
	decodeJSON(t, resp, http.StatusOK, &result)
	assert.Equal(t, "done", result["state"],
		"task state must transition to 'done' after resolve")
	require.NotNil(t, result["task_id"])

	// Confirm state persisted.
	resp2 := apiGet(t, "/v1/tasks/"+taskID, tokenMerchantA)
	var result2 map[string]any
	decodeJSON(t, resp2, http.StatusOK, &result2)
	assert.Equal(t, "done", result2["state"])
}

// TestTask_Resolve_WrongMerchant verifies that resolving another merchant's task
// returns 404.
func TestTask_Resolve_WrongMerchant(t *testing.T) {
	taskID := seedTask(t, string(merchantAID), "Not Your Task")

	resp := apiPost(t, "/v1/tasks/"+taskID+"/resolve", tokenMerchantB,
		map[string]any{"note": "attempt"})
	assertErrorEnvelope(t, resp, http.StatusNotFound)
}
