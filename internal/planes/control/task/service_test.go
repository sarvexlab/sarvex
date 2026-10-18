// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package task_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
)

type fakeTaskRepo struct {
	tasks map[string]*task.TaskModel
}

func (r *fakeTaskRepo) Insert(ctx context.Context, m *task.TaskModel) error {
	r.tasks[m.ID] = m
	return nil
}

func (r *fakeTaskRepo) FindByMerchantAndID(ctx context.Context, merchantID domain.MerchantID, taskID string) (*task.TaskModel, error) {
	m, ok := r.tasks[taskID]
	if !ok {
		return nil, task.ErrNotFound
	}
	if m.MerchantID != string(merchantID) {
		return nil, task.ErrNotFound
	}
	return m, nil
}

func (r *fakeTaskRepo) UpdateState(ctx context.Context, taskID string, state string, note *string, updatedAt time.Time) (*task.TaskModel, error) {
	m, ok := r.tasks[taskID]
	if !ok {
		return nil, task.ErrNotFound
	}
	m.State = state
	m.ResolutionNote = note
	m.UpdatedAt = updatedAt
	return m, nil
}

// ListByMerchant mirrors the Mongo repo's contract in memory: same sort
// (checkout-blocking first, then newest, then id), same keyset paging, same
// limit+1 lookahead for has_more. A fake that ignored order and paging
// would let the service's cursor handling go untested.
func (r *fakeTaskRepo) ListByMerchant(ctx context.Context, merchantID domain.MerchantID, storeID *domain.StoreID, owner *domain.TaskOwner, state *domain.TaskState, after *task.ListCursor, limit int) ([]*task.TaskModel, bool, error) {
	var items []*task.TaskModel
	for _, m := range r.tasks {
		if m.MerchantID != string(merchantID) {
			continue
		}
		if storeID != nil && m.StoreID != string(*storeID) {
			continue
		}
		if owner != nil && m.Owner != string(*owner) {
			continue
		}
		if state != nil && m.State != string(*state) {
			continue
		}
		items = append(items, m)
	}

	slices.SortFunc(items, func(a, b *task.TaskModel) int {
		if a.BlocksCheckout != b.BlocksCheckout {
			if a.BlocksCheckout {
				return -1
			}
			return 1
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return b.CreatedAt.Compare(a.CreatedAt)
		}
		return strings.Compare(b.ID, a.ID)
	})

	if after != nil {
		items = lo.Filter(items, func(m *task.TaskModel, _ int) bool {
			return afterCursorPosition(m, *after)
		})
	}

	if limit <= 0 || limit > 100 {
		limit = 20
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

// afterCursorPosition is the in-memory twin of the repo's keyset predicate.
func afterCursorPosition(m *task.TaskModel, c task.ListCursor) bool {
	if m.BlocksCheckout != c.BlocksCheckout {
		return !m.BlocksCheckout // false sorts after true
	}
	// Cursors carry millisecond precision, matching BSON datetimes.
	created := m.CreatedAt.Truncate(time.Millisecond)
	if !created.Equal(c.CreatedAt) {
		return created.Before(c.CreatedAt)
	}
	return m.ID < c.ID
}

func (r *fakeTaskRepo) ListOpenByMerchants(ctx context.Context, merchantIDs []domain.MerchantID) ([]*task.TaskModel, error) {
	wanted := make(map[string]struct{}, len(merchantIDs))
	for _, id := range merchantIDs {
		wanted[string(id)] = struct{}{}
	}
	var items []*task.TaskModel
	for _, m := range r.tasks {
		if _, ok := wanted[m.MerchantID]; !ok {
			continue
		}
		// Mirrors the real repo: open is every non-done state.
		if m.State == string(domain.TaskStateDone) {
			continue
		}
		items = append(items, m)
	}
	return items, nil
}

type fakeRemediationLookup struct {
	rems         map[string][]domain.Remediation
	issues       map[string][]domain.ProductIssue
	err          error
	issuesErr    error
	callCount    int
	issuesCalled int
}

func (f *fakeRemediationLookup) ListRemediationsForSKU(_ context.Context, _ domain.MerchantID, _ domain.StoreID, sku string) ([]domain.Remediation, error) {
	f.callCount++
	if f.err != nil {
		return nil, f.err
	}
	return f.rems[sku], nil
}

func (f *fakeRemediationLookup) ListIssuesForSKU(_ context.Context, _ domain.MerchantID, _ domain.StoreID, sku string) ([]domain.ProductIssue, error) {
	f.issuesCalled++
	if f.issuesErr != nil {
		return nil, f.issuesErr
	}
	return f.issues[sku], nil
}

func TestTaskService_Get_AttachesRemediationsForReadinessSource(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	sourceRef := "SKU-1"
	repo.tasks["task-1"] = &task.TaskModel{
		ID:         "task-1",
		MerchantID: "merchant-1",
		StoreID:    "store-1",
		Title:      "Fix product",
		Owner:      "merchant",
		State:      "open",
		Source:     "readiness",
		SourceRef:  &sourceRef,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	lookup := &fakeRemediationLookup{rems: map[string][]domain.Remediation{
		"SKU-1": {{RemediationID: "r-1", SKU: "SKU-1", Field: "color", FromValue: "Rot", ToValue: "Red"}},
	}}
	svc := task.NewTaskService(repo, lookup, nil, nil)
	ctx := context.Background()

	got, err := svc.Get(ctx, domain.MerchantID("merchant-1"), "task-1")
	require.NoError(t, err)
	require.Len(t, got.Remediations, 1)
	assert.Equal(t, "r-1", got.Remediations[0].RemediationID)
	assert.Equal(t, 1, lookup.callCount)
}

func TestTaskService_Get_AttachesManualIssuesForReadinessSource(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	sourceRef := "SKU-1"
	repo.tasks["task-1"] = &task.TaskModel{
		ID:         "task-1",
		MerchantID: "merchant-1",
		StoreID:    "store-1",
		Title:      "Fix product",
		Owner:      "merchant",
		State:      "open",
		Source:     "readiness",
		SourceRef:  &sourceRef,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	lookup := &fakeRemediationLookup{issues: map[string][]domain.ProductIssue{
		"SKU-1": {{IssueID: "iss-1", Field: "description", Problem: domain.ProblemFreeText, Remediable: domain.RemediableByManual}},
	}}
	svc := task.NewTaskService(repo, lookup, nil, nil)
	ctx := context.Background()

	got, err := svc.Get(ctx, domain.MerchantID("merchant-1"), "task-1")
	require.NoError(t, err)
	require.Len(t, got.Issues, 1)
	assert.Equal(t, "iss-1", got.Issues[0].IssueID)
	assert.Equal(t, domain.ProblemFreeText, got.Issues[0].Problem)
	assert.Equal(t, 1, lookup.issuesCalled)
}

func TestTaskService_Get_IssueLookupErrorIsNonFatal(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	sourceRef := "SKU-1"
	repo.tasks["task-1"] = &task.TaskModel{
		ID:         "task-1",
		MerchantID: "merchant-1",
		StoreID:    "store-1",
		Title:      "Fix product",
		Owner:      "merchant",
		State:      "open",
		Source:     "readiness",
		SourceRef:  &sourceRef,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	lookup := &fakeRemediationLookup{issuesErr: errors.New("catalog store unavailable")}
	svc := task.NewTaskService(repo, lookup, nil, nil)
	ctx := context.Background()

	got, err := svc.Get(ctx, domain.MerchantID("merchant-1"), "task-1")
	require.NoError(t, err)
	assert.Empty(t, got.Issues)
}

func TestTaskService_Get_SkipsRemediationLookupForNonReadinessSource(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	sourceRef := "item-1"
	repo.tasks["task-1"] = &task.TaskModel{
		ID:         "task-1",
		MerchantID: "merchant-1",
		StoreID:    "store-1",
		Title:      "Verify checklist item",
		Owner:      "merchant",
		State:      "open",
		Source:     "checklist",
		SourceRef:  &sourceRef,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	lookup := &fakeRemediationLookup{}
	svc := task.NewTaskService(repo, lookup, nil, nil)
	ctx := context.Background()

	got, err := svc.Get(ctx, domain.MerchantID("merchant-1"), "task-1")
	require.NoError(t, err)
	assert.Empty(t, got.Remediations)
	assert.Equal(t, 0, lookup.callCount)
}

func TestTaskService_Get_RemediationLookupErrorIsNonFatal(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	sourceRef := "SKU-1"
	repo.tasks["task-1"] = &task.TaskModel{
		ID:         "task-1",
		MerchantID: "merchant-1",
		StoreID:    "store-1",
		Title:      "Fix product",
		Owner:      "merchant",
		State:      "open",
		Source:     "readiness",
		SourceRef:  &sourceRef,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	lookup := &fakeRemediationLookup{err: errors.New("catalog store unavailable")}
	svc := task.NewTaskService(repo, lookup, nil, nil)
	ctx := context.Background()

	got, err := svc.Get(ctx, domain.MerchantID("merchant-1"), "task-1")
	require.NoError(t, err)
	assert.Empty(t, got.Remediations)
}

func TestTaskService_Get_NotFound(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	svc := task.NewTaskService(repo, nil, nil, nil)
	ctx := context.Background()

	_, err := svc.Get(ctx, domain.MerchantID("merchant-1"), "nonexistent")
	require.ErrorIs(t, err, task.ErrNotFound)
}

func TestTaskService_Get_TenantIsolation(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	m := &task.TaskModel{
		ID:         "task-1",
		MerchantID: "merchant-1",
		Title:      "Test",
		Owner:      "merchant",
		State:      "open",
	}
	repo.tasks["task-1"] = m

	svc := task.NewTaskService(repo, nil, nil, nil)
	ctx := context.Background()

	_, err := svc.Get(ctx, domain.MerchantID("merchant-2"), "task-1")
	require.ErrorIs(t, err, task.ErrNotFound)
}

func TestTaskService_Get_Success(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
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
	repo.tasks["task-1"] = m

	svc := task.NewTaskService(repo, nil, nil, nil)
	ctx := context.Background()

	got, err := svc.Get(ctx, domain.MerchantID("merchant-1"), "task-1")
	require.NoError(t, err)
	assert.Equal(t, "task-1", got.TaskID)
	assert.Equal(t, "Test task", got.Title)
	assert.Equal(t, domain.TaskOwnerMerchant, got.Owner)
}

func TestTaskService_Raise_CarriesFindingPayload(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	svc := task.NewTaskService(repo, nil, nil, nil)
	ctx := context.Background()

	sku := "system:plugin_gateway"
	detectedAt := time.Now().UTC()
	finding := &domain.ConsistencyFinding{
		FindingID:  "probe:store-1:system:plugin_gateway:profile_format",
		SKU:        sku,
		Field:      "profile_format",
		Severity:   domain.SeverityWarning,
		Sources:    map[string]string{"error": "Invalid JSON: unexpected end of JSON input"},
		DetectedAt: detectedAt,
	}

	got, err := svc.Raise(ctx, domain.MerchantID("merchant-1"), task.RaiseTaskRequest{
		TaskID:     "monitoring_warning_store-1_system:plugin_gateway_profile_format",
		StoreID:    "store-1",
		Title:      "Warning: profile_format mismatch on system:plugin_gateway",
		Owner:      "sarvex",
		Resolution: "automatic",
		Source:     "monitoring",
		SourceRef:  &sku,
		Finding:    finding,
	})
	require.NoError(t, err)
	require.NotNil(t, got.Finding)
	assert.Equal(t, finding.Field, got.Finding.Field)
	assert.Equal(t, finding.Severity, got.Finding.Severity)
	assert.Equal(t, finding.Sources, got.Finding.Sources)

	// Confirm it round-trips through a fresh Get too (not just the Raise return value).
	reloaded, err := svc.Get(ctx, domain.MerchantID("merchant-1"), got.TaskID)
	require.NoError(t, err)
	require.NotNil(t, reloaded.Finding)
	assert.Equal(t, finding.Field, reloaded.Finding.Field)
}

// raceLosingTaskRepo simulates another raiser winning the insert for the
// same deterministic task id between this caller's existence check and its
// own insert — the check-then-insert race every Raise caller (task.Ensure,
// the sync.task.raised subscriber, the monitoring loop) is exposed to.
type raceLosingTaskRepo struct {
	fakeTaskRepo
	winner *task.TaskModel
}

func (r *raceLosingTaskRepo) Insert(ctx context.Context, m *task.TaskModel) error {
	r.tasks[r.winner.ID] = r.winner
	return task.ErrAlreadyExists
}

func TestTaskService_Raise_LosingTheInsertRaceReturnsTheWinnersTask(t *testing.T) {
	winner := &task.TaskModel{
		ID:         "readiness:store-1:initial_audit",
		MerchantID: "merchant-1",
		StoreID:    "store-1",
		Title:      "Initial audit",
		Owner:      "sarvex",
		Resolution: "automatic",
		State:      "in_progress",
		Source:     "readiness",
	}
	repo := &raceLosingTaskRepo{
		fakeTaskRepo: fakeTaskRepo{tasks: make(map[string]*task.TaskModel)},
		winner:       winner,
	}
	svc := task.NewTaskService(repo, nil, nil, nil)
	ctx := context.Background()

	got, err := svc.Raise(ctx, domain.MerchantID("merchant-1"), task.RaiseTaskRequest{
		TaskID:     winner.ID,
		StoreID:    winner.StoreID,
		Title:      winner.Title,
		Owner:      winner.Owner,
		Resolution: winner.Resolution,
		Source:     winner.Source,
	})

	require.NoError(t, err, "losing the insert race is not this caller's failure — the task is on the spine either way")
	assert.Equal(t, winner.ID, got.TaskID)
}

func TestTaskService_List_WithFilters(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	now := time.Now().UTC()

	repo.tasks["task-1"] = &task.TaskModel{
		ID:         "task-1",
		MerchantID: "merchant-1",
		Title:      "Task 1",
		Owner:      "merchant",
		State:      "open",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	repo.tasks["task-2"] = &task.TaskModel{
		ID:         "task-2",
		MerchantID: "merchant-1",
		Title:      "Task 2",
		Owner:      "sarvex",
		State:      "open",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	repo.tasks["task-3"] = &task.TaskModel{
		ID:         "task-3",
		MerchantID: "merchant-1",
		Title:      "Task 3",
		Owner:      "merchant",
		State:      "done",
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	svc := task.NewTaskService(repo, nil, nil, nil)
	ctx := context.Background()
	merchantID := domain.MerchantID("merchant-1")

	// List all tasks
	all, err := svc.List(ctx, merchantID, nil, "", 20, nil, nil)
	require.NoError(t, err)
	assert.Len(t, all.Data, 3)

	// Filter by owner
	ownerFilter := domain.TaskOwnerMerchant
	merchantTasks, err := svc.List(ctx, merchantID, nil, "", 20, &ownerFilter, nil)
	require.NoError(t, err)
	assert.Len(t, merchantTasks.Data, 2)

	// Filter by state
	openState := domain.TaskStateOpen
	openTasks, err := svc.List(ctx, merchantID, nil, "", 20, nil, &openState)
	require.NoError(t, err)
	assert.Len(t, openTasks.Data, 2)

	// Both filters
	openMerchantTasks, err := svc.List(ctx, merchantID, nil, "", 20, &ownerFilter, &openState)
	require.NoError(t, err)
	assert.Len(t, openMerchantTasks.Data, 1)
}

func TestTaskService_List_ScopedByStore(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	now := time.Now().UTC()

	repo.tasks["task-1"] = &task.TaskModel{
		ID: "task-1", MerchantID: "merchant-1", StoreID: "store-1",
		Title: "Task 1", Owner: "merchant", State: "open", CreatedAt: now, UpdatedAt: now,
	}
	repo.tasks["task-2"] = &task.TaskModel{
		ID: "task-2", MerchantID: "merchant-1", StoreID: "store-2",
		Title: "Task 2", Owner: "merchant", State: "open", CreatedAt: now, UpdatedAt: now,
	}

	svc := task.NewTaskService(repo, nil, nil, nil)
	ctx := context.Background()
	merchantID := domain.MerchantID("merchant-1")
	storeID := domain.StoreID("store-1")

	scoped, err := svc.List(ctx, merchantID, &storeID, "", 20, nil, nil)
	require.NoError(t, err)
	require.Len(t, scoped.Data, 1)
	assert.Equal(t, "task-1", scoped.Data[0].TaskID)
	assert.Equal(t, domain.StoreID("store-1"), scoped.Data[0].StoreID)

	unscoped, err := svc.List(ctx, merchantID, nil, "", 20, nil, nil)
	require.NoError(t, err)
	assert.Len(t, unscoped.Data, 2)
}

func TestTaskService_Resolve_Success(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	m := &task.TaskModel{
		ID:         "task-1",
		MerchantID: "merchant-1",
		Title:      "Test task",
		Owner:      "merchant",
		Resolution: "one_click",
		State:      "open",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	repo.tasks["task-1"] = m

	svc := task.NewTaskService(repo, nil, nil, nil)
	ctx := context.Background()

	note := "Resolved by user"
	req := domain.ResolveTaskRequest{Note: &note}
	resolved, err := svc.Resolve(ctx, domain.MerchantID("merchant-1"), "task-1", req)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStateDone, resolved.State)
	assert.NotNil(t, resolved.Note)
	assert.Equal(t, note, *resolved.Note)
}

func TestTaskService_Resolve_AlreadyDone(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	m := &task.TaskModel{
		ID:         "task-1",
		MerchantID: "merchant-1",
		Title:      "Test task",
		Owner:      "merchant",
		Resolution: "one_click",
		State:      "done",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	repo.tasks["task-1"] = m

	svc := task.NewTaskService(repo, nil, nil, nil)
	ctx := context.Background()

	req := domain.ResolveTaskRequest{}
	_, err := svc.Resolve(ctx, domain.MerchantID("merchant-1"), "task-1", req)
	require.ErrorIs(t, err, task.ErrNotResolvable)
}

func TestTaskService_ListOpenByMerchants_GroupsByStoreAndOwner(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	now := time.Now().UTC()
	repo.tasks["t1"] = &task.TaskModel{ID: "t1", MerchantID: "m1", StoreID: "s1", Owner: "merchant", State: "open", CreatedAt: now, UpdatedAt: now}
	repo.tasks["t2"] = &task.TaskModel{ID: "t2", MerchantID: "m1", StoreID: "s1", Owner: "merchant", State: "open", CreatedAt: now, UpdatedAt: now}
	repo.tasks["t3"] = &task.TaskModel{ID: "t3", MerchantID: "m1", StoreID: "s1", Owner: "sarvex", State: "open", CreatedAt: now, UpdatedAt: now}
	repo.tasks["t4"] = &task.TaskModel{ID: "t4", MerchantID: "m2", StoreID: "s2", Owner: "external", State: "open", CreatedAt: now, UpdatedAt: now}
	// Not open — must be excluded from every count.
	repo.tasks["t5"] = &task.TaskModel{ID: "t5", MerchantID: "m1", StoreID: "s1", Owner: "merchant", State: "done", CreatedAt: now, UpdatedAt: now}
	// A merchant outside the requested batch — must be excluded entirely.
	repo.tasks["t6"] = &task.TaskModel{ID: "t6", MerchantID: "m3", StoreID: "s3", Owner: "merchant", State: "open", CreatedAt: now, UpdatedAt: now}

	svc := task.NewTaskService(repo, nil, nil, nil)
	ctx := context.Background()

	got, err := svc.ListOpenByMerchants(ctx, []domain.MerchantID{"m1", "m2"})
	require.NoError(t, err)
	require.Len(t, got, 2)

	s1 := got[domain.StoreID("s1")]
	assert.Equal(t, 2, s1.Counts.Merchant)
	assert.Equal(t, 1, s1.Counts.Sarvex)
	assert.Equal(t, 0, s1.Counts.External)
	assert.ElementsMatch(t, []string{"t1", "t2"}, s1.IDs.Merchant)
	assert.ElementsMatch(t, []string{"t3"}, s1.IDs.Sarvex)

	s2 := got[domain.StoreID("s2")]
	assert.Equal(t, 1, s2.Counts.External)
	assert.ElementsMatch(t, []string{"t4"}, s2.IDs.External)

	_, ok := got[domain.StoreID("s3")]
	assert.False(t, ok, "a store belonging to a merchant outside the batch must not appear")
}

func TestTaskService_ListOpenByMerchants_EmptyInputReturnsEmptyMap(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	svc := task.NewTaskService(repo, nil, nil, nil)

	got, err := svc.ListOpenByMerchants(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestTaskService_Resolve_ExternalWait(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	m := &task.TaskModel{
		ID:         "task-1",
		MerchantID: "merchant-1",
		Title:      "Test task",
		Owner:      "external",
		Resolution: "external_wait",
		State:      "open",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	repo.tasks["task-1"] = m

	svc := task.NewTaskService(repo, nil, nil, nil)
	ctx := context.Background()

	req := domain.ResolveTaskRequest{}
	_, err := svc.Resolve(ctx, domain.MerchantID("merchant-1"), "task-1", req)
	require.ErrorIs(t, err, task.ErrNotResolvable)
}

// TestTaskService_Resolve_RefusesAutomaticRegardlessOfOwner covers
// attr-eligibility specifically: its task is owner=merchant (they did the
// underlying work of pasting the feed URL) but resolution=automatic (only
// Google's own feed fetch, observed via VerifyItem, proves it landed). The
// merchant-facing Resolve path must refuse it exactly like external_wait —
// owner=merchant is not license to self-attest an automatic task.
func TestTaskService_Resolve_RefusesAutomaticRegardlessOfOwner(t *testing.T) {
	repo := &fakeTaskRepo{tasks: make(map[string]*task.TaskModel)}
	m := &task.TaskModel{
		ID:         "task-1",
		MerchantID: "merchant-1",
		Title:      "Complete the surface's external steps",
		Owner:      "merchant",
		Resolution: "automatic",
		State:      "open",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	repo.tasks["task-1"] = m

	svc := task.NewTaskService(repo, nil, nil, nil)
	ctx := context.Background()

	req := domain.ResolveTaskRequest{}
	_, err := svc.Resolve(ctx, domain.MerchantID("merchant-1"), "task-1", req)
	require.ErrorIs(t, err, task.ErrNotResolvable)
}
