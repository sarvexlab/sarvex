// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
)

// taskRaisedEventsFor decodes every sync.task.raised event published to bus
// into TaskRaisedPayload, in publish order.
func taskRaisedEventsFor(t *testing.T, bus *fakeEventBus) []TaskRaisedPayload {
	t.Helper()
	var out []TaskRaisedPayload
	for _, e := range bus.published {
		if e.EventType() != ucp.EventSyncTaskRaised {
			continue
		}
		var p TaskRaisedPayload
		require.NoError(t, json.Unmarshal(e.Payload(), &p))
		out = append(out, p)
	}
	return out
}

func titleMissingRubric() *ScoringRubric {
	return &ScoringRubric{
		ScoringRules: []ScoringRule{
			{ID: "rule_title_missing", Dimension: "completeness", Field: "title", Condition: "missing", BlocksCheckout: true, Weight: 100},
			{ID: "rule_price_missing", Dimension: "completeness", Field: "price", Condition: "missing", BlocksCheckout: true, Weight: 100},
		},
	}
}

func TestRunScoreStage_RaisesTaskForNewBlockingIssue(t *testing.T) {
	catalogStore := newFakeCatalogStore()
	bus := newFakeEventBus()
	svc := &syncService{
		catalogStore:      catalogStore,
		bus:               bus,
		consistencyEngine: NewConsistencyEngine(slog.Default()),
		log:               slog.Default(),
	}

	p := &CatalogProduct{MerchantID: "merch-1", StoreID: "store-1", SKU: "SKU-1", Title: "", PriceMinor: 1999}
	require.NoError(t, svc.runScoreStage(context.Background(), p, titleMissingRubric(), true))

	events := taskRaisedEventsFor(t, bus)
	require.Len(t, events, 1)
	assert.Equal(t, "rule_title_missing", events[0].IssueID)
	assert.False(t, events[0].Resolved)
}

func TestRunScoreStage_ResolvesTaskWhenIssueClears(t *testing.T) {
	catalogStore := newFakeCatalogStore()
	bus := newFakeEventBus()
	svc := &syncService{
		catalogStore:      catalogStore,
		bus:               bus,
		consistencyEngine: NewConsistencyEngine(slog.Default()),
		log:               slog.Default(),
	}
	rubric := titleMissingRubric()

	p := &CatalogProduct{MerchantID: "merch-1", StoreID: "store-1", SKU: "SKU-1", Title: "", PriceMinor: 1999}
	require.NoError(t, svc.runScoreStage(context.Background(), p, rubric, true))
	require.Len(t, taskRaisedEventsFor(t, bus), 1, "first pass should raise the title-missing task")

	bus.published = nil
	p.Title = "Now has a title"
	require.NoError(t, svc.runScoreStage(context.Background(), p, rubric, true))

	events := taskRaisedEventsFor(t, bus)
	require.Len(t, events, 1, "second pass should only resolve the cleared issue, not raise anything new")
	assert.Equal(t, "rule_title_missing", events[0].IssueID)
	assert.True(t, events[0].Resolved)
}

func TestRunScoreStage_StillBlockingIssueIsNotResolved(t *testing.T) {
	catalogStore := newFakeCatalogStore()
	bus := newFakeEventBus()
	svc := &syncService{
		catalogStore:      catalogStore,
		bus:               bus,
		consistencyEngine: NewConsistencyEngine(slog.Default()),
		log:               slog.Default(),
	}
	rubric := titleMissingRubric()

	p := &CatalogProduct{MerchantID: "merch-1", StoreID: "store-1", SKU: "SKU-1", Title: "", PriceMinor: 0}
	require.NoError(t, svc.runScoreStage(context.Background(), p, rubric, true))
	require.Len(t, taskRaisedEventsFor(t, bus), 2, "both title and price should raise")

	bus.published = nil
	p.Title = "Now has a title" // price is still missing
	require.NoError(t, svc.runScoreStage(context.Background(), p, rubric, true))

	events := taskRaisedEventsFor(t, bus)
	require.Len(t, events, 2, "the cleared title issue resolves; the still-blocking price issue re-raises (existing idempotent-raise behavior, deduped downstream)")

	byIssue := make(map[string]TaskRaisedPayload, len(events))
	for _, e := range events {
		byIssue[e.IssueID] = e
	}
	require.Contains(t, byIssue, "rule_title_missing")
	assert.True(t, byIssue["rule_title_missing"].Resolved, "title cleared, must resolve")
	require.Contains(t, byIssue, "rule_price_missing")
	assert.False(t, byIssue["rule_price_missing"].Resolved, "price is still blocking and must not be resolved")
}

func TestBuildReadinessTaskRaisedPayload(t *testing.T) {
	issue := ProductIssue{
		IssueID: "missing_title",
		Field:   "title",
		Problem: "missing",
	}

	got := buildReadinessTaskRaisedPayload("SKU-1", issue)

	assert.Equal(t, "readiness", got.Source)
	assert.Equal(t, "SKU-1", got.SourceRef)
	assert.Equal(t, "missing_title", got.IssueID)
	assert.Equal(t, "merchant", got.Owner)
	assert.Equal(t, "Product SKU-1: title (missing)", got.Title)
	assert.True(t, got.BlocksCheckout)
}

func TestBuildReadinessTaskRaisedPayload_DistinctIssuesYieldDistinctIssueIDs(t *testing.T) {
	a := buildReadinessTaskRaisedPayload("SKU-1", ProductIssue{IssueID: "missing_title", Field: "title", Problem: "missing"})
	b := buildReadinessTaskRaisedPayload("SKU-1", ProductIssue{IssueID: "missing_description", Field: "description", Problem: "missing"})

	assert.Equal(t, a.SourceRef, b.SourceRef, "same product")
	assert.NotEqual(t, a.IssueID, b.IssueID, "distinct issues on the same product must carry distinct issue ids")
}
