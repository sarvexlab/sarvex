// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"encoding/json"
	"log/slog"
	gosync "sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/commandbus"
	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// memRemediationStore is an in-memory RemediationStore with the Mongo store's
// semantics: SyncForSKU never touches an applied record and retires the SKU's
// unapplied records that are no longer open.
type memRemediationStore struct {
	mu      gosync.Mutex
	records map[string]Remediation
}

func newMemRemediationStore() *memRemediationStore {
	return &memRemediationStore{records: map[string]Remediation{}}
}

func (m *memRemediationStore) SyncForSKU(_ context.Context, merchantID, storeID, sku string, open []Remediation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	keep := map[string]bool{}
	for _, r := range open {
		keep[r.ID] = true
		if cur, ok := m.records[r.ID]; ok && cur.Applied {
			continue
		}
		m.records[r.ID] = r
	}
	for id, r := range m.records {
		if r.MerchantID == merchantID && r.StoreID == storeID && r.SKU == sku && !r.Applied && !keep[id] {
			delete(m.records, id)
		}
	}
	return nil
}

func (m *memRemediationStore) GetByIDs(_ context.Context, merchantID, storeID string, ids []string) ([]Remediation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Remediation
	for _, id := range uniq(ids) {
		r, ok := m.records[id]
		if !ok || r.MerchantID != merchantID || r.StoreID != storeID {
			return nil, ErrRemediationsNotFound
		}
		out = append(out, r)
	}
	return out, nil
}

func (m *memRemediationStore) MarkApplied(_ context.Context, merchantID, storeID, id, writeBackID, fromValue string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.records[id]; ok && r.MerchantID == merchantID && r.StoreID == storeID {
		r.Applied, r.WriteBackID, r.FromValue = true, writeBackID, fromValue
		m.records[id] = r
	}
	return nil
}

func (m *memRemediationStore) MarkReverted(_ context.Context, merchantID, storeID, id, writeBackID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.records[id]; ok && r.MerchantID == merchantID && r.StoreID == storeID && r.Applied && r.WriteBackID == writeBackID {
		r.Applied, r.WriteBackID = false, ""
		m.records[id] = r
	}
	return nil
}

func (m *memRemediationStore) forSKU(sku string) map[string]Remediation {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]Remediation{}
	for id, r := range m.records {
		if r.SKU == sku {
			out[id] = r
		}
	}
	return out
}

// recordingConnector records the store write-backs a remediation makes.
type recordingConnector struct {
	base.Connector
	mu      gosync.Mutex
	updates []map[string]any
}

func (c *recordingConnector) UpdateProduct(_ context.Context, _ string, _ string, updates map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates = append(c.updates, updates)
	return nil
}

// remediationRubric has one rule-fixable issue (a missing description,
// defaulted) and one only the merchant can fix (a missing title).
func remediationRubric() *ScoringRubric {
	return &ScoringRubric{
		ScoringRules: []ScoringRule{
			{ID: "rule_title_missing", Dimension: "completeness", Field: "title", Condition: "missing", BlocksCheckout: true, Weight: 100, RemediationID: "rem_title_required"},
			{ID: "rule_description_missing", Dimension: "completeness", Field: "description", Condition: "missing", Weight: 50, RemediationID: "rem_description_default"},
		},
		Remediations: []RemediationRule{
			{ID: "rem_title_required", ScoringRuleID: "rule_title_missing", Field: "title", Action: "error"},
			{ID: "rem_description_default", ScoringRuleID: "rule_description_missing", Field: "description", Action: "default", Value: "No description provided."},
		},
	}
}

func newRemediationTestService(t *testing.T, conn base.Connector) (*syncService, *fakeCatalogStore, *memRemediationStore) {
	t.Helper()
	catalogStore := newFakeCatalogStore()
	reg, provider := newTestRegistry("store-1", conn)
	store := newMemRemediationStore()
	return &syncService{
		catalogStore:      catalogStore,
		bus:               newFakeEventBus(),
		consistencyEngine: NewConsistencyEngine(slog.Default()),
		registry:          reg,
		provider:          provider,
		remediations:      store,
		log:               slog.Default(),
	}, catalogStore, store
}

// The score stage keeps a product's remediation records in step with its
// issues: one per remediable issue, typed and trusted by whether a rule can fix
// it, and retired when the issue resolves.
func TestScoreStage_KeepsRemediationRecordsInStepWithIssues(t *testing.T) {
	svc, _, store := newRemediationTestService(t, &recordingConnector{})
	ctx := context.Background()
	p := &CatalogProduct{MerchantID: "merch-1", StoreID: "store-1", SKU: "SKU-1", PriceMinor: 1999, MappedFields: map[string]string{}}

	require.NoError(t, svc.runScoreStage(ctx, p, remediationRubric(), true))
	recs := store.forSKU("SKU-1")
	require.Len(t, recs, 2)
	desc := recs["store-1:SKU-1:rule_description_missing"]
	assert.Equal(t, "rule", desc.Type)
	assert.Equal(t, "high", desc.Trust)
	assert.Equal(t, "No description provided.", desc.ToValue)
	assert.Equal(t, "rem_description_default", desc.RemediationRuleID)
	title := recs["store-1:SKU-1:rule_title_missing"]
	assert.Equal(t, "manual", title.Type)
	assert.Equal(t, "low", title.Trust, "the contract's trust enum is high|low")
	assert.Empty(t, title.ToValue)

	p.Title = "A title"
	require.NoError(t, svc.runScoreStage(ctx, p, remediationRubric(), true))
	recs = store.forSKU("SKU-1")
	assert.Len(t, recs, 1, "the resolved title issue's record is retired")
	assert.Contains(t, recs, "store-1:SKU-1:rule_description_missing")
}

func applyCommand(t *testing.T, writeBackID string, ids ...string) eventbus.DomainEvent {
	t.Helper()
	payload, err := json.Marshal(commandbus.RemediationsAppliedPayload{WriteBackID: writeBackID, RemediationIDs: ids})
	require.NoError(t, err)
	return newDomainEvent(ucp.CommandApplyRemediations, "store-1", "merch-1", payload)
}

// Applying a remediation fixes exactly that record's product, writes the fix
// back to the store, marks the record applied under the write-back — and it
// stays applied (history) after the re-score resolves its issue. A manual fix
// is left open, and a redelivered command changes nothing.
func TestHandleRemediationsApplied_AppliesOnlyTheApprovedRecords(t *testing.T) {
	conn := &recordingConnector{}
	svc, catalogStore, store := newRemediationTestService(t, conn)
	ctx := context.Background()
	rubric := remediationRubric()

	target := &CatalogProduct{ID: "store-1:SKU-1", MerchantID: "merch-1", StoreID: "store-1", SKU: "SKU-1", PriceMinor: 1999, MappedFields: map[string]string{}}
	other := &CatalogProduct{ID: "store-1:SKU-2", MerchantID: "merch-1", StoreID: "store-1", SKU: "SKU-2", Title: "Other", PriceMinor: 1999, MappedFields: map[string]string{}}
	for _, p := range []*CatalogProduct{target, other} {
		require.NoError(t, catalogStore.UpsertProduct(ctx, p))
		require.NoError(t, svc.runScoreStage(ctx, p, rubric, true))
	}

	const descID, titleID = "store-1:SKU-1:rule_description_missing", "store-1:SKU-1:rule_title_missing"
	require.NoError(t, svc.handleRemediationsAppliedWith(ctx, applyCommand(t, "wb-1", descID, titleID), rubric))

	require.Len(t, conn.updates, 1, "one store write-back, for the one rule-fixable record")
	assert.Equal(t, map[string]any{"description": "No description provided."}, conn.updates[0])

	recs := store.forSKU("SKU-1")
	require.Contains(t, recs, descID, "an applied record survives the re-score that resolves its issue")
	assert.True(t, recs[descID].Applied)
	assert.Equal(t, "wb-1", recs[descID].WriteBackID)
	assert.False(t, recs[titleID].Applied, "a manual fix is left open")

	untouched, err := catalogStore.GetProduct(ctx, "store-1", "SKU-2")
	require.NoError(t, err)
	assert.Empty(t, untouched.Description, "other products are not touched")
	fixed, err := catalogStore.GetProduct(ctx, "store-1", "SKU-1")
	require.NoError(t, err)
	assert.Equal(t, "No description provided.", fixed.Description, "the product itself carries the fix")

	// Redelivery (sarvex-v1-architecture.md A10.6): nothing new is written.
	require.NoError(t, svc.handleRemediationsAppliedWith(ctx, applyCommand(t, "wb-1", descID, titleID), rubric))
	assert.Len(t, conn.updates, 1)
}

func TestHandleRemediationsApplied_UnknownRecordIsAnError(t *testing.T) {
	svc, _, _ := newRemediationTestService(t, &recordingConnector{})
	err := svc.handleRemediationsAppliedWith(context.Background(), applyCommand(t, "wb-1", "store-1:SKU-9:nope"), remediationRubric())
	require.ErrorIs(t, err, ErrRemediationsNotFound)
}

func revertCommand(t *testing.T, writeBackID string, ids ...string) eventbus.DomainEvent {
	t.Helper()
	payload, err := json.Marshal(commandbus.RemediationsRevertedPayload{WriteBackID: writeBackID, RemediationIDs: ids})
	require.NoError(t, err)
	return newDomainEvent(ucp.CommandRevertRemediations, "store-1", "merch-1", payload)
}

// Reverting a write-back restores the field's prior value on the product and
// in the merchant's store, and reopens the record, so its issue is open again.
// (Reverting used to only flag the write-back: the store kept the change and
// the record stayed applied.) A redelivered revert, or one for another
// write-back, changes nothing.
func TestHandleRemediationsReverted_RestoresThePriorValueAndReopens(t *testing.T) {
	conn := &recordingConnector{}
	svc, catalogStore, store := newRemediationTestService(t, conn)
	ctx := context.Background()
	rubric := remediationRubric()

	p := &CatalogProduct{ID: "store-1:SKU-1", MerchantID: "merch-1", StoreID: "store-1", SKU: "SKU-1", Title: "Mug", PriceMinor: 1999, MappedFields: map[string]string{}}
	require.NoError(t, catalogStore.UpsertProduct(ctx, p))
	require.NoError(t, svc.runScoreStage(ctx, p, rubric, true))

	const descID = "store-1:SKU-1:rule_description_missing"
	require.NoError(t, svc.handleRemediationsAppliedWith(ctx, applyCommand(t, "wb-1", descID), rubric))
	require.Len(t, conn.updates, 1)

	require.NoError(t, svc.handleRemediationsRevertedWith(ctx, revertCommand(t, "wb-other", descID), rubric))
	assert.Len(t, conn.updates, 1, "a revert for another write-back touches nothing")

	require.NoError(t, svc.handleRemediationsRevertedWith(ctx, revertCommand(t, "wb-1", descID), rubric))
	require.Len(t, conn.updates, 2, "the store gets its prior value back")
	assert.Equal(t, map[string]any{"description": ""}, conn.updates[1])

	got, err := catalogStore.GetProduct(ctx, "store-1", "SKU-1")
	require.NoError(t, err)
	assert.Empty(t, got.Description, "the product gets its prior value back")
	rec := store.forSKU("SKU-1")[descID]
	assert.False(t, rec.Applied, "the record is open again")
	assert.Empty(t, rec.WriteBackID)

	require.NoError(t, svc.handleRemediationsRevertedWith(ctx, revertCommand(t, "wb-1", descID), rubric))
	assert.Len(t, conn.updates, 2, "a redelivered revert changes nothing")
}
