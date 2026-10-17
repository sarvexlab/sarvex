// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package task_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTaskRaisedEvent implements eventbus.DomainEvent for testing.
type fakeTaskRaisedEvent struct {
	merchantID string
	storeID    string
	eventType  string
	payload    []byte
}

func (e *fakeTaskRaisedEvent) EventType() string     { return e.eventType }
func (e *fakeTaskRaisedEvent) StoreID() string       { return e.storeID }
func (e *fakeTaskRaisedEvent) MerchantID() string    { return e.merchantID }
func (e *fakeTaskRaisedEvent) OccurredAt() time.Time { return time.Now().UTC() }
func (e *fakeTaskRaisedEvent) Payload() []byte       { return e.payload }

func newTaskRaisedEvent(merchantID, storeID string, payload task.TaskRaisedPayload) eventbus.DomainEvent {
	body, _ := json.Marshal(payload)
	return &fakeTaskRaisedEvent{
		merchantID: merchantID,
		storeID:    storeID,
		eventType:  "sync.task.raised",
		payload:    body,
	}
}

func TestSubscriber_Handle_RaisesTask(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	svc := task.NewTaskService(repo, nil, nil, nil)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	subscriber := task.NewSubscriber(svc, nil, log)
	ctx := context.Background()

	event := newTaskRaisedEvent("merchant-1", "store-1", task.TaskRaisedPayload{
		Source:         "readiness",
		SourceRef:      "SKU-1",
		Owner:          "merchant",
		Title:          "Product SKU-1: title missing",
		BlocksCheckout: true,
	})

	err := subscriber.Handle(ctx, event)
	require.NoError(t, err)

	tasks, err := svc.List(ctx, "merchant-1", nil, "", 10, nil, nil)
	require.NoError(t, err)
	require.Len(t, tasks.Data, 1)
	assert.Equal(t, "Product SKU-1: title missing", tasks.Data[0].Title)
	assert.Equal(t, domain.TaskOwner("merchant"), tasks.Data[0].Owner)
	assert.True(t, tasks.Data[0].BlocksCheckout)
	assert.Equal(t, domain.TaskSource("readiness"), tasks.Data[0].Source)
	assert.Equal(t, domain.StoreID("store-1"), tasks.Data[0].StoreID)
}

func TestSubscriber_Handle_DuplicateIsNoOp(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	svc := task.NewTaskService(repo, nil, nil, nil)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	subscriber := task.NewSubscriber(svc, nil, log)
	ctx := context.Background()

	payload := task.TaskRaisedPayload{
		Source:         "readiness",
		SourceRef:      "SKU-1",
		Owner:          "merchant",
		Title:          "Product SKU-1: title missing",
		BlocksCheckout: true,
	}
	event := newTaskRaisedEvent("merchant-1", "store-1", payload)

	require.NoError(t, subscriber.Handle(ctx, event))
	require.NoError(t, subscriber.Handle(ctx, event)) // re-published on every rescore

	tasks, err := svc.List(ctx, "merchant-1", nil, "", 10, nil, nil)
	require.NoError(t, err)
	require.Len(t, tasks.Data, 1, "duplicate (source, store, source_ref) must not create a second task")
}

func TestSubscriber_Handle_DistinctIssuesOnSameProductRaiseSeparateTasks(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	svc := task.NewTaskService(repo, nil, nil, nil)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	subscriber := task.NewSubscriber(svc, nil, log)
	ctx := context.Background()

	first := newTaskRaisedEvent("merchant-1", "store-1", task.TaskRaisedPayload{
		Source:         "readiness",
		SourceRef:      "SKU-1",
		IssueID:        "missing_title",
		Owner:          "merchant",
		Title:          "Product SKU-1: title (missing)",
		BlocksCheckout: true,
	})
	second := newTaskRaisedEvent("merchant-1", "store-1", task.TaskRaisedPayload{
		Source:         "readiness",
		SourceRef:      "SKU-1",
		IssueID:        "missing_description",
		Owner:          "merchant",
		Title:          "Product SKU-1: description (missing)",
		BlocksCheckout: true,
	})

	require.NoError(t, subscriber.Handle(ctx, first))
	require.NoError(t, subscriber.Handle(ctx, second))
	// Re-publishing the same issue (as rescoring does every pass) must still
	// collapse onto the existing task rather than creating a third.
	require.NoError(t, subscriber.Handle(ctx, first))

	tasks, err := svc.List(ctx, "merchant-1", nil, "", 10, nil, nil)
	require.NoError(t, err)
	require.Len(t, tasks.Data, 2, "two distinct blocking issues on one product must raise two tasks")

	titles := []string{tasks.Data[0].Title, tasks.Data[1].Title}
	assert.Contains(t, titles, "Product SKU-1: title (missing)")
	assert.Contains(t, titles, "Product SKU-1: description (missing)")
}

func TestSubscriber_Handle_ResolvedClosesOpenTask(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	svc := task.NewTaskService(repo, nil, nil, nil)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	subscriber := task.NewSubscriber(svc, nil, log)
	ctx := context.Background()

	raise := newTaskRaisedEvent("merchant-1", "store-1", task.TaskRaisedPayload{
		Source:         "readiness",
		SourceRef:      "SKU-1",
		IssueID:        "missing_title",
		Owner:          "merchant",
		Title:          "Product SKU-1: title (missing)",
		BlocksCheckout: true,
	})
	require.NoError(t, subscriber.Handle(ctx, raise))

	tasks, err := svc.List(ctx, "merchant-1", nil, "", 10, nil, nil)
	require.NoError(t, err)
	require.Len(t, tasks.Data, 1)
	taskID := tasks.Data[0].TaskID
	require.Equal(t, domain.TaskStateOpen, tasks.Data[0].State)

	resolve := newTaskRaisedEvent("merchant-1", "store-1", task.TaskRaisedPayload{
		Source:    "readiness",
		SourceRef: "SKU-1",
		IssueID:   "missing_title",
		Owner:     "merchant",
		Title:     "Product SKU-1: title (missing)",
		Resolved:  true,
	})
	require.NoError(t, subscriber.Handle(ctx, resolve))

	resolved, err := svc.Get(ctx, "merchant-1", taskID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStateDone, resolved.State)
}

func TestSubscriber_Handle_ResolvedWithNoMatchingTaskIsNoOp(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	svc := task.NewTaskService(repo, nil, nil, nil)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	subscriber := task.NewSubscriber(svc, nil, log)
	ctx := context.Background()

	resolve := newTaskRaisedEvent("merchant-1", "store-1", task.TaskRaisedPayload{
		Source:    "catalog_import",
		SourceRef: "initial",
		Owner:     "merchant",
		Title:     "Catalog import recovered",
		Resolved:  true,
	})

	err := subscriber.Handle(ctx, resolve)
	require.NoError(t, err, "resolving a task that was never raised must not propagate an error")

	tasks, err := svc.List(ctx, "merchant-1", nil, "", 10, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, tasks.Data)
}

func TestSubscriber_Handle_ResolvedOnAlreadyDoneTaskIsNoOp(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	svc := task.NewTaskService(repo, nil, nil, nil)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	subscriber := task.NewSubscriber(svc, nil, log)
	ctx := context.Background()

	payload := task.TaskRaisedPayload{
		Source:         "readiness",
		SourceRef:      "SKU-1",
		IssueID:        "missing_title",
		Owner:          "merchant",
		Title:          "Product SKU-1: title (missing)",
		BlocksCheckout: true,
	}
	require.NoError(t, subscriber.Handle(ctx, newTaskRaisedEvent("merchant-1", "store-1", payload)))

	resolvedPayload := payload
	resolvedPayload.Resolved = true
	resolveEvent := newTaskRaisedEvent("merchant-1", "store-1", resolvedPayload)
	require.NoError(t, subscriber.Handle(ctx, resolveEvent))
	// Second resolve of the same already-done task must also be a no-op, not an error.
	require.NoError(t, subscriber.Handle(ctx, resolveEvent))

	tasks, err := svc.List(ctx, "merchant-1", nil, "", 10, nil, nil)
	require.NoError(t, err)
	require.Len(t, tasks.Data, 1)
	assert.Equal(t, domain.TaskStateDone, tasks.Data[0].State)
}

func TestSubscriber_Handle_MalformedPayload(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	svc := task.NewTaskService(repo, nil, nil, nil)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	subscriber := task.NewSubscriber(svc, nil, log)
	ctx := context.Background()

	event := &fakeTaskRaisedEvent{
		merchantID: "merchant-1",
		storeID:    "store-1",
		eventType:  "sync.task.raised",
		payload:    []byte("not json"),
	}

	err := subscriber.Handle(ctx, event)
	require.NoError(t, err)

	tasks, err := svc.List(ctx, "merchant-1", nil, "", 10, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, tasks.Data)
}

// A fixed issue's task is done; if the same issue is detected again the task
// must reopen. The id is the issue's identity, so treating the done task as a
// duplicate would leave a blocking issue with no open task.
func TestSubscriber_Handle_RecurrenceReopensDoneTask(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	svc := task.NewTaskService(repo, nil, nil, nil)
	log := slog.New(slog.DiscardHandler)
	subscriber := task.NewSubscriber(svc, nil, log)
	ctx := context.Background()

	payload := task.TaskRaisedPayload{
		Source:         "readiness",
		SourceRef:      "SKU-1",
		IssueID:        "rule_images_missing",
		Owner:          "merchant",
		Title:          "Product SKU-1: images (missing)",
		BlocksCheckout: true,
	}
	require.NoError(t, subscriber.Handle(ctx, newTaskRaisedEvent("merchant-1", "store-1", payload)))
	tasks, err := svc.List(ctx, "merchant-1", nil, "", 10, nil, nil)
	require.NoError(t, err)
	require.Len(t, tasks.Data, 1)
	taskID := tasks.Data[0].TaskID

	resolved := payload
	resolved.Resolved = true
	require.NoError(t, subscriber.Handle(ctx, newTaskRaisedEvent("merchant-1", "store-1", resolved)))
	done, err := svc.Get(ctx, "merchant-1", taskID)
	require.NoError(t, err)
	require.Equal(t, domain.TaskStateDone, done.State)

	// The issue comes back.
	require.NoError(t, subscriber.Handle(ctx, newTaskRaisedEvent("merchant-1", "store-1", payload)))

	reopened, err := svc.Get(ctx, "merchant-1", taskID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStateOpen, reopened.State)
	require.NotNil(t, reopened.Note)
	assert.Contains(t, *reopened.Note, "Reopened")
	all, err := svc.List(ctx, "merchant-1", nil, "", 10, nil, nil)
	require.NoError(t, err)
	assert.Len(t, all.Data, 1, "reopening must not create a second task")
}

// Re-publishing an issue that is still open (every rescore does) changes nothing.
func TestSubscriber_Handle_RepeatOnOpenTaskDoesNotTouchIt(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	svc := task.NewTaskService(repo, nil, nil, nil)
	log := slog.New(slog.DiscardHandler)
	subscriber := task.NewSubscriber(svc, nil, log)
	ctx := context.Background()

	ev := newTaskRaisedEvent("merchant-1", "store-1", task.TaskRaisedPayload{
		Source: "readiness", SourceRef: "SKU-1", IssueID: "rule_images_missing",
		Owner: "merchant", Title: "t", BlocksCheckout: true,
	})
	require.NoError(t, subscriber.Handle(ctx, ev))
	require.NoError(t, subscriber.Handle(ctx, ev))

	all, err := svc.List(ctx, "merchant-1", nil, "", 10, nil, nil)
	require.NoError(t, err)
	require.Len(t, all.Data, 1)
	assert.Nil(t, all.Data[0].Note)
}
