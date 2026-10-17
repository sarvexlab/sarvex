// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package operator_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/operator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeManualStepRepo is a map-backed ManualStepRepository for tests.
type fakeManualStepRepo struct {
	docs map[string]*operator.ManualStepModel
}

func (r *fakeManualStepRepo) Insert(ctx context.Context, m *operator.ManualStepModel) error {
	r.docs[m.ID] = m
	return nil
}

func (r *fakeManualStepRepo) UpdateDisposition(ctx context.Context, id string, disposition string) (*operator.ManualStepModel, error) {
	m, ok := r.docs[id]
	if !ok {
		return nil, operator.ErrNotFound
	}
	m.Disposition = disposition
	m.UpdatedAt = time.Now().UTC()
	return m, nil
}

func (r *fakeManualStepRepo) ListByMerchant(ctx context.Context, merchantID domain.MerchantID, afterID string, limit int) ([]*operator.ManualStepModel, bool, error) {
	var items []*operator.ManualStepModel
	for _, m := range r.docs {
		if m.TargetMerchantID == string(merchantID) {
			items = append(items, m)
		}
	}
	return items, false, nil
}

func (r *fakeManualStepRepo) ListByDisposition(ctx context.Context, disposition *string, afterID string, limit int) ([]*operator.ManualStepModel, bool, error) {
	var items []*operator.ManualStepModel
	for _, m := range r.docs {
		if disposition == nil || m.Disposition == *disposition {
			items = append(items, m)
		}
	}
	return items, false, nil
}

func (r *fakeManualStepRepo) ListByStage(ctx context.Context, stage string, disposition *string, afterID string, limit int) ([]*operator.ManualStepModel, bool, error) {
	var items []*operator.ManualStepModel
	for _, m := range r.docs {
		if m.EngagementStage == stage && (disposition == nil || m.Disposition == *disposition) {
			items = append(items, m)
		}
	}
	return items, false, nil
}

func (r *fakeManualStepRepo) BulkUpdateDisposition(ctx context.Context, stepIDs []string, disposition string) (int, error) {
	count := 0
	for _, id := range stepIDs {
		if m, ok := r.docs[id]; ok {
			m.Disposition = disposition
			m.UpdatedAt = time.Now().UTC()
			count++
		}
	}
	return count, nil
}

func (r *fakeManualStepRepo) CountByStageAndDay(ctx context.Context, merchantID *domain.MerchantID, startDate, endDate string) ([]operator.TimelineAggregation, error) {
	// Simplified: group by stage and day (for testing).
	type key struct {
		date  string
		stage string
	}
	counts := make(map[key]*operator.TimelineAggregation)

	for _, m := range r.docs {
		if merchantID != nil && m.TargetMerchantID != string(*merchantID) {
			continue
		}
		dateStr := m.CreatedAt.Format("2006-01-02")
		k := key{date: dateStr, stage: m.EngagementStage}
		if counts[k] == nil {
			counts[k] = &operator.TimelineAggregation{
				Date:  dateStr,
				Stage: m.EngagementStage,
			}
		}
		counts[k].Count++
		counts[k].AvgDaysInStage += float64(m.DaysInStageAtCapture)
	}

	var result []operator.TimelineAggregation
	for _, agg := range counts {
		if agg.Count > 0 {
			agg.AvgDaysInStage /= float64(agg.Count)
		}
		result = append(result, *agg)
	}
	return result, nil
}

// fakeAuditLogRepo is a map-backed AuditLogRepository for tests.
type fakeAuditLogRepo struct {
	entries []*operator.AuditEntryModel
}

func (r *fakeAuditLogRepo) Append(ctx context.Context, e *operator.AuditEntryModel) error {
	r.entries = append(r.entries, e)
	return nil
}

func (r *fakeAuditLogRepo) List(ctx context.Context, targetMerchant *domain.MerchantID, afterID string, limit int) ([]*operator.AuditEntryModel, bool, error) {
	var filtered []*operator.AuditEntryModel
	for _, e := range r.entries {
		if targetMerchant == nil || e.TargetMerchantID == string(*targetMerchant) {
			filtered = append(filtered, e)
		}
	}
	return filtered, false, nil
}

// fakeBoardRepo is a map-backed BoardRepository for tests.
type fakeBoardRepo struct {
	cards []*operator.BoardCardModel
}

func (r *fakeBoardRepo) ListCards(ctx context.Context, state *domain.JourneyState, attention *bool, afterMerchantID string, limit int) ([]*operator.BoardCardModel, bool, error) {
	var filtered []*operator.BoardCardModel
	for _, card := range r.cards {
		if state == nil || domain.JourneyState(card.State) == *state {
			filtered = append(filtered, card)
		}
	}
	return filtered, false, nil
}

// fakeTaskService is a stub TaskService for tests (returns empty task lists).
// listCalls/listOpenByMerchantsCalls count invocations so Board() tests can
// assert it makes one batched call rather than one List call per card.
type fakeTaskService struct {
	listCalls                int
	listOpenByMerchantsCalls int
}

func (s *fakeTaskService) List(ctx context.Context, id domain.MerchantID, storeID *domain.StoreID, cursor string, limit int, owner *domain.TaskOwner, state *domain.TaskState) (domain.ListOf[domain.Task], error) {
	s.listCalls++
	return domain.ListOf[domain.Task]{
		Data: []domain.Task{},
		Page: domain.Page{Cursor: nil, HasMore: false},
	}, nil
}

func (s *fakeTaskService) ListOpenByMerchants(ctx context.Context, merchantIDs []domain.MerchantID) (map[domain.StoreID]domain.StoreTaskSummary, error) {
	s.listOpenByMerchantsCalls++
	return map[domain.StoreID]domain.StoreTaskSummary{}, nil
}

// fakeJourneyService is a stub JourneyService for tests.
type fakeJourneyService struct {
	journey domain.Journey
}

func (s *fakeJourneyService) Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Journey, error) {
	return s.journey, nil
}

func newTestOperatorService() (*fakeManualStepRepo, *fakeAuditLogRepo, *fakeBoardRepo, operator.Service) {
	steps := &fakeManualStepRepo{docs: make(map[string]*operator.ManualStepModel)}
	audit := &fakeAuditLogRepo{}
	board := &fakeBoardRepo{}
	tasks := &fakeTaskService{}
	journey := &fakeJourneyService{
		journey: domain.Journey{
			State: domain.JourneySynced,
			Stage: domain.StageRemediation,
		},
	}
	svc := operator.NewOperatorService(steps, audit, board, tasks, journey, nil, nil, nil, nil, nil, nil, nil, nil)
	return steps, audit, board, svc
}

func TestOperatorService_CaptureManualStep_OK(t *testing.T) {
	_, audit, _, svc := newTestOperatorService()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")
	operatorID := "op123"

	step := domain.ManualStep{
		Description: "Fixed database issue",
		Disposition: domain.ManualStepDisposition("unclassified"),
	}

	result, err := svc.CaptureManualStep(ctx, operatorID, id, step)
	require.NoError(t, err)
	assert.Equal(t, "Fixed database issue", result.Description)
	assert.Equal(t, id, result.TargetMerchantID)

	// Verify audit entry was created.
	assert.Equal(t, 1, len(audit.entries))
	assert.Equal(t, "manual_step.captured", audit.entries[0].Action)
	assert.Equal(t, operatorID, audit.entries[0].OperatorUserID)
}

func TestOperatorService_CaptureManualStep_EmptyDescription(t *testing.T) {
	_, _, _, svc := newTestOperatorService()
	ctx := context.Background()
	id := domain.MerchantID("merchant1")

	step := domain.ManualStep{
		Description: "",
	}

	_, err := svc.CaptureManualStep(ctx, "op1", id, step)
	assert.Error(t, err)
}

func TestOperatorService_AuditLog_All(t *testing.T) {
	_, audit, _, svc := newTestOperatorService()
	ctx := context.Background()

	// Manually add some audit entries.
	audit.entries = append(audit.entries, &operator.AuditEntryModel{
		ID:               "e1",
		OperatorUserID:   "op1",
		TargetMerchantID: "merch1",
		Action:           "manual_step.captured",
		OccurredAt:       time.Now().UTC(),
	})

	result, err := svc.AuditLog(ctx, nil, "", 100)
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.Data))
	assert.Equal(t, "manual_step.captured", result.Data[0].Action)
}

func TestOperatorService_AuditLog_FilterByMerchant(t *testing.T) {
	_, audit, _, svc := newTestOperatorService()
	ctx := context.Background()

	// Add entries for different merchants.
	audit.entries = append(audit.entries, &operator.AuditEntryModel{
		ID:               "e1",
		OperatorUserID:   "op1",
		TargetMerchantID: "merch1",
		Action:           "manual_step.captured",
		OccurredAt:       time.Now().UTC(),
	})
	audit.entries = append(audit.entries, &operator.AuditEntryModel{
		ID:               "e2",
		OperatorUserID:   "op1",
		TargetMerchantID: "merch2",
		Action:           "manual_step.captured",
		OccurredAt:       time.Now().UTC(),
	})

	targetID := domain.MerchantID("merch1")
	result, err := svc.AuditLog(ctx, &targetID, "", 100)
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.Data))
	assert.Equal(t, targetID, result.Data[0].TargetMerchantID)
}

func TestOperatorService_Board_FilterByState(t *testing.T) {
	_, _, board, svc := newTestOperatorService()
	ctx := context.Background()

	now := time.Now().UTC()
	board.cards = append(board.cards, &operator.BoardCardModel{
		MerchantID:     "merch1",
		Name:           "Test Store 1",
		State:          "connected",
		EnteredStateAt: now,
	})
	board.cards = append(board.cards, &operator.BoardCardModel{
		MerchantID:     "merch2",
		Name:           "Test Store 2",
		State:          "synced",
		EnteredStateAt: now,
	})

	state := domain.JourneyState("connected")
	result, err := svc.Board(ctx, &state, nil, "", 100)
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.Data))
	assert.Equal(t, "merch1", string(result.Data[0].MerchantID))
}

// TestOperatorService_Board_CarriesStoreID verifies a card's store_id
// (distinct from its merchant_id — see BoardCard.StoreID's doc comment)
// flows through to the domain type, since the console's "enter as
// merchant" flow needs the real store_id rather than guessing one.
func TestOperatorService_Board_CarriesStoreID(t *testing.T) {
	_, _, board, svc := newTestOperatorService()
	ctx := context.Background()

	board.cards = append(board.cards, &operator.BoardCardModel{
		MerchantID:     "merch1",
		StoreID:        "store1",
		Name:           "Test Store 1",
		State:          "connected",
		EnteredStateAt: time.Now().UTC(),
	})

	result, err := svc.Board(ctx, nil, nil, "", 100)
	require.NoError(t, err)
	require.Len(t, result.Data, 1)
	assert.Equal(t, "merch1", string(result.Data[0].MerchantID))
	assert.Equal(t, "store1", string(result.Data[0].StoreID))
}

func TestOperatorService_Board_ComputesDaysInState(t *testing.T) {
	_, _, board, svc := newTestOperatorService()
	ctx := context.Background()

	tenDaysAgo := time.Now().UTC().AddDate(0, 0, -10)
	board.cards = append(board.cards, &operator.BoardCardModel{
		MerchantID:     "merch1",
		Name:           "Test Store",
		State:          "ready",
		EnteredStateAt: tenDaysAgo,
	})

	result, err := svc.Board(ctx, nil, nil, "", 100)
	require.NoError(t, err)
	assert.Greater(t, result.Data[0].DaysInState, 9)
	assert.Less(t, result.Data[0].DaysInState, 12)
}

func TestOperatorService_Board_ComputesBlocked(t *testing.T) {
	_, _, board, svc := newTestOperatorService()
	ctx := context.Background()

	// Over 7 days in state → blocked.
	eightDaysAgo := time.Now().UTC().AddDate(0, 0, -8)
	board.cards = append(board.cards, &operator.BoardCardModel{
		MerchantID:     "merch1",
		Name:           "Test Store",
		State:          "ready",
		EnteredStateAt: eightDaysAgo,
	})

	result, err := svc.Board(ctx, nil, nil, "", 100)
	require.NoError(t, err)
	assert.True(t, result.Data[0].Blocked)
	assert.False(t, result.Data[0].Stale) // Not yet 14 days
}

func TestOperatorService_Board_ComputesStale(t *testing.T) {
	_, _, board, svc := newTestOperatorService()
	ctx := context.Background()

	// Over 14 days in state → stale.
	fifteenDaysAgo := time.Now().UTC().AddDate(0, 0, -15)
	board.cards = append(board.cards, &operator.BoardCardModel{
		MerchantID:     "merch1",
		Name:           "Test Store",
		State:          "ready",
		EnteredStateAt: fifteenDaysAgo,
	})

	result, err := svc.Board(ctx, nil, nil, "", 100)
	require.NoError(t, err)
	assert.True(t, result.Data[0].Blocked)
	assert.True(t, result.Data[0].Stale)
}

func TestOperatorService_Board_FetchesTaskCounts(t *testing.T) {
	_, _, board, _ := newTestOperatorService()
	ctx := context.Background()

	now := time.Now().UTC()
	board.cards = append(board.cards, &operator.BoardCardModel{
		MerchantID:     "merch1",
		Name:           "Test Store",
		State:          "ready",
		EnteredStateAt: now,
	})

	// Use a custom task service that returns specific counts.
	tasks := &taskServiceWithCounts{
		merchantTaskCounts: map[domain.MerchantID]domain.TaskCounts{
			"merch1": {Merchant: 2, Sarvex: 1, External: 0},
		},
	}
	steps := &fakeManualStepRepo{docs: make(map[string]*operator.ManualStepModel)}
	audit := &fakeAuditLogRepo{}
	journey := &fakeJourneyService{
		journey: domain.Journey{
			State: domain.JourneySynced,
			Stage: domain.StageRemediation,
		},
	}
	svc := operator.NewOperatorService(steps, audit, board, tasks, journey, nil, nil, nil, nil, nil, nil, nil, nil)

	result, err := svc.Board(ctx, nil, nil, "", 100)
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.Data))
	assert.Equal(t, 2, result.Data[0].OpenTaskCounts.Merchant)
	assert.Equal(t, 1, result.Data[0].OpenTaskCounts.Sarvex)
	assert.Equal(t, 0, result.Data[0].OpenTaskCounts.External)
}

func TestOperatorService_Board_IncludesTaskIDs(t *testing.T) {
	_, _, board, _ := newTestOperatorService()
	ctx := context.Background()

	now := time.Now().UTC()
	board.cards = append(board.cards, &operator.BoardCardModel{
		MerchantID:     "merch1",
		Name:           "Test Store",
		State:          "ready",
		EnteredStateAt: now,
	})

	// Use a custom task service that returns specific task IDs.
	tasks := &taskServiceWithCounts{
		merchantTaskCounts: map[domain.MerchantID]domain.TaskCounts{
			"merch1": {Merchant: 2, Sarvex: 1, External: 0},
		},
	}
	steps := &fakeManualStepRepo{docs: make(map[string]*operator.ManualStepModel)}
	audit := &fakeAuditLogRepo{}
	journey := &fakeJourneyService{
		journey: domain.Journey{
			State: domain.JourneySynced,
			Stage: domain.StageRemediation,
		},
	}
	svc := operator.NewOperatorService(steps, audit, board, tasks, journey, nil, nil, nil, nil, nil, nil, nil, nil)

	result, err := svc.Board(ctx, nil, nil, "", 100)
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.Data))
	card := result.Data[0]

	// Verify task IDs are captured and sorted by owner.
	assert.Equal(t, 2, len(card.OpenTaskIDs.Merchant), "should have 2 merchant task IDs")
	assert.Equal(t, 1, len(card.OpenTaskIDs.Sarvex), "should have 1 sarvex task ID")
	assert.Equal(t, 0, len(card.OpenTaskIDs.External), "should have 0 google task IDs")

	// Verify task IDs match the format from taskServiceWithCounts.
	assert.Equal(t, "task_0", card.OpenTaskIDs.Merchant[0])
	assert.Equal(t, "task_1", card.OpenTaskIDs.Merchant[1])
	assert.Equal(t, "task_0", card.OpenTaskIDs.Sarvex[0])
}

// TestOperatorService_Board_BatchesTaskLookup guards against the N+1 regressing:
// Board() must fetch every card's task summary with one ListOpenByMerchants
// call, never one List call per (card, owner).
func TestOperatorService_Board_BatchesTaskLookup(t *testing.T) {
	board := &fakeBoardRepo{}
	now := time.Now().UTC()
	for i, merchantID := range []string{"merch1", "merch2", "merch3"} {
		board.cards = append(board.cards, &operator.BoardCardModel{
			MerchantID:     merchantID,
			StoreID:        fmt.Sprintf("store%d", i),
			Name:           "Test Store",
			State:          "ready",
			EnteredStateAt: now,
		})
	}

	tasks := &fakeTaskService{}
	steps := &fakeManualStepRepo{docs: make(map[string]*operator.ManualStepModel)}
	audit := &fakeAuditLogRepo{}
	journey := &fakeJourneyService{
		journey: domain.Journey{State: domain.JourneySynced, Stage: domain.StageRemediation},
	}
	svc := operator.NewOperatorService(steps, audit, board, tasks, journey, nil, nil, nil, nil, nil, nil, nil, nil)

	result, err := svc.Board(context.Background(), nil, nil, "", 100)
	require.NoError(t, err)
	assert.Equal(t, 3, len(result.Data))
	assert.Equal(t, 1, tasks.listOpenByMerchantsCalls, "Board must batch every card's task lookup into one call")
	assert.Equal(t, 0, tasks.listCalls, "Board must not fall back to per-card/per-owner List calls")
}

func TestOperatorService_ListManualStepsFiltered_ByStage(t *testing.T) {
	steps, _, _, svc := newTestOperatorService()
	ctx := context.Background()

	// Add steps in different stages.
	steps.docs["s1"] = &operator.ManualStepModel{
		ID:              "s1",
		EngagementStage: string(domain.StageRemediation),
		Disposition:     string(domain.DispositionAutomate),
	}
	steps.docs["s2"] = &operator.ManualStepModel{
		ID:              "s2",
		EngagementStage: string(domain.StageRemediation),
		Disposition:     string(domain.DispositionTemplatise),
	}
	steps.docs["s3"] = &operator.ManualStepModel{
		ID:              "s3",
		EngagementStage: string(domain.StageValidation),
		Disposition:     string(domain.DispositionAutomate),
	}

	// List only remediation stage steps.
	stage := domain.StageRemediation
	result, err := svc.ListManualStepsFiltered(ctx, &stage, nil, "", 100)
	require.NoError(t, err)
	assert.Equal(t, 2, len(result.Data))
	for _, step := range result.Data {
		assert.Equal(t, domain.StageRemediation, step.EngagementStage)
	}
}

func TestOperatorService_ListManualStepsFiltered_ByStageAndDisposition(t *testing.T) {
	steps, _, _, svc := newTestOperatorService()
	ctx := context.Background()

	steps.docs["s1"] = &operator.ManualStepModel{
		ID:              "s1",
		EngagementStage: string(domain.StageRemediation),
		Disposition:     string(domain.DispositionAutomate),
	}
	steps.docs["s2"] = &operator.ManualStepModel{
		ID:              "s2",
		EngagementStage: string(domain.StageRemediation),
		Disposition:     string(domain.DispositionTemplatise),
	}

	// List only automate steps in remediation stage.
	stage := domain.StageRemediation
	disp := domain.DispositionAutomate
	result, err := svc.ListManualStepsFiltered(ctx, &stage, &disp, "", 100)
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.Data))
	assert.Equal(t, domain.DispositionAutomate, result.Data[0].Disposition)
}

func TestOperatorService_BulkRetagManualSteps_OK(t *testing.T) {
	steps, audit, _, svc := newTestOperatorService()
	ctx := context.Background()

	steps.docs["s1"] = &operator.ManualStepModel{
		ID:          "s1",
		Disposition: string(domain.DispositionUnclassified),
	}
	steps.docs["s2"] = &operator.ManualStepModel{
		ID:          "s2",
		Disposition: string(domain.DispositionUnclassified),
	}

	// Bulk retag to automate.
	err := svc.BulkRetagManualSteps(ctx, "op1", []string{"s1", "s2"}, domain.DispositionAutomate)
	require.NoError(t, err)

	// Verify both steps were updated.
	assert.Equal(t, string(domain.DispositionAutomate), steps.docs["s1"].Disposition)
	assert.Equal(t, string(domain.DispositionAutomate), steps.docs["s2"].Disposition)

	// Verify audit entries were created (one per step).
	assert.Greater(t, len(audit.entries), 0)
	for _, entry := range audit.entries {
		assert.Equal(t, "manual_step.bulk_retagged", entry.Action)
	}
}

func TestOperatorService_TimelineMetrics_OK(t *testing.T) {
	steps, _, _, svc := newTestOperatorService()
	ctx := context.Background()

	today := time.Now().UTC()
	steps.docs["s1"] = &operator.ManualStepModel{
		ID:                   "s1",
		EngagementStage:      string(domain.StageRemediation),
		CreatedAt:            today,
		DaysInStageAtCapture: 5,
	}
	steps.docs["s2"] = &operator.ManualStepModel{
		ID:                   "s2",
		EngagementStage:      string(domain.StageRemediation),
		CreatedAt:            today,
		DaysInStageAtCapture: 3,
	}
	steps.docs["s3"] = &operator.ManualStepModel{
		ID:                   "s3",
		EngagementStage:      string(domain.StageValidation),
		CreatedAt:            today,
		DaysInStageAtCapture: 10,
	}

	// Get timeline metrics for today.
	result, err := svc.TimelineMetrics(ctx, nil, "", "")
	require.NoError(t, err)
	assert.Greater(t, len(result), 0)

	// Check remediation stage had 2 steps with avg days-in-stage = 4.
	for _, dp := range result {
		if dp.Stage == domain.StageRemediation {
			assert.Equal(t, 2, dp.Count)
			assert.Equal(t, 4, dp.AvgDaysInStage)
		}
		if dp.Stage == domain.StageValidation {
			assert.Equal(t, 1, dp.Count)
			assert.Equal(t, 10, dp.AvgDaysInStage)
		}
	}
}

// taskServiceWithCounts allows tests to set specific task counts per merchant.
type taskServiceWithCounts struct {
	merchantTaskCounts map[domain.MerchantID]domain.TaskCounts
}

func (s *taskServiceWithCounts) List(ctx context.Context, id domain.MerchantID, storeID *domain.StoreID, cursor string, limit int, owner *domain.TaskOwner, state *domain.TaskState) (domain.ListOf[domain.Task], error) {
	counts, ok := s.merchantTaskCounts[id]
	if !ok {
		return domain.ListOf[domain.Task]{
			Data: []domain.Task{},
			Page: domain.Page{Cursor: nil, HasMore: false},
		}, nil
	}

	// Return tasks for the requested owner.
	var count int
	if owner != nil {
		switch *owner {
		case domain.TaskOwnerMerchant:
			count = counts.Merchant
		case domain.TaskOwnerSarvex:
			count = counts.Sarvex
		case domain.TaskOwnerExternal:
			count = counts.External
		}
	}

	tasks := make([]domain.Task, count)
	for i := 0; i < count; i++ {
		tasks[i] = domain.Task{TaskID: fmt.Sprintf("task_%d", i)}
	}
	return domain.ListOf[domain.Task]{
		Data: tasks,
		Page: domain.Page{Cursor: nil, HasMore: false},
	}, nil
}

func (s *taskServiceWithCounts) ListOpenByMerchants(ctx context.Context, merchantIDs []domain.MerchantID) (map[domain.StoreID]domain.StoreTaskSummary, error) {
	summaries := make(map[domain.StoreID]domain.StoreTaskSummary)
	for _, id := range merchantIDs {
		counts, ok := s.merchantTaskCounts[id]
		if !ok {
			continue
		}
		summary := domain.StoreTaskSummary{Counts: counts}
		for i := 0; i < counts.Merchant; i++ {
			summary.IDs.Merchant = append(summary.IDs.Merchant, fmt.Sprintf("task_%d", i))
		}
		for i := range counts.Sarvex {
			summary.IDs.Sarvex = append(summary.IDs.Sarvex, fmt.Sprintf("task_%d", i))
		}
		for i := range counts.External {
			summary.IDs.External = append(summary.IDs.External, fmt.Sprintf("task_%d", i))
		}
		// The Board tests that construct this fake never set
		// BoardCardModel.StoreID, so every card correlates to the
		// zero-value store ID.
		summaries[domain.StoreID("")] = summary
	}
	return summaries, nil
}
