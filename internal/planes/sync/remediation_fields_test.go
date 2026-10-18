// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A rubric fix is only a rule fix if it can reach the merchant's store. Against
// the shipped rubric that is the title normalization; the defaults for
// description (empty), stock and shipping data (a 0 would zero shipping
// quotes) stay with the merchant.
func TestRemediable_ShippedRubric(t *testing.T) {
	rubric := testRubric(t)
	want := map[string]bool{
		"rem_title_normalize":             true,
		"rem_description_missing":         false, // default is ""
		"rem_inventory_missing":           false, // not a safe default to write
		"rem_shipping_weight_missing":     false,
		"rem_shipping_dimensions_missing": false,
		"rem_title_required":              false, // action error
		"rem_price_invalid":               false,
	}
	for id, applicable := range want {
		rem := rubric.FindRemediation(id)
		require.NotNil(t, rem, id)
		assert.Equal(t, applicable, remediationApplicable(rem), id)
	}
}

// Title normalization end to end: the record proposes the normalized title
// (not ""), applying it writes the connector's "name" field and updates the
// product so the free-text issue clears, and reverting restores the original
// in both places.
func TestTitleNormalize_ApplyAndRevertReachTheStore(t *testing.T) {
	conn := &recordingConnector{}
	svc, catalogStore, store := newRemediationTestService(t, conn)
	ctx := context.Background()
	rubric := testRubric(t)

	const original = "  Widget   Pro "
	p := scoredProduct()
	p.MerchantID, p.StoreID, p.SKU, p.Title = "merch-1", "store-1", "SKU-1", original
	require.NoError(t, catalogStore.UpsertProduct(ctx, p))
	require.NoError(t, svc.runScoreStage(ctx, p, rubric, true))

	const id = "store-1:SKU-1:rule_title_free_text"
	rec, ok := store.forSKU("SKU-1")[id]
	require.True(t, ok, "expected a remediation record, got %+v", store.forSKU("SKU-1"))
	assert.Equal(t, "rule", rec.Type)
	assert.Equal(t, original, rec.FromValue)
	assert.Equal(t, "Widget Pro", rec.ToValue)

	require.NoError(t, svc.handleRemediationsAppliedWith(ctx, applyCommand(t, "wb-1", id), rubric))
	require.Len(t, conn.updates, 1)
	assert.Equal(t, map[string]any{"name": "Widget Pro"}, conn.updates[0])
	got, _ := catalogStore.GetProduct(ctx, "store-1", "SKU-1")
	assert.Equal(t, "Widget Pro", got.Title)
	for _, i := range got.Issues {
		assert.NotEqual(t, "rule_title_free_text", i.IssueID, "the fix clears the issue")
	}
	assert.True(t, store.forSKU("SKU-1")[id].Applied)

	require.NoError(t, svc.handleRemediationsRevertedWith(ctx, revertCommand(t, "wb-1", id), rubric))
	require.Len(t, conn.updates, 2)
	assert.Equal(t, map[string]any{"name": original}, conn.updates[1])
	got, _ = catalogStore.GetProduct(ctx, "store-1", "SKU-1")
	assert.Equal(t, original, got.Title)
	assert.False(t, store.forSKU("SKU-1")[id].Applied)
}

// Gaps whose "default" is unsafe or empty are manual: the record offers no
// proposed value, applying it writes nothing to the store, and it stays open.
func TestUnsafeDefaults_AreManualAndNeverWrittenToTheStore(t *testing.T) {
	conn := &recordingConnector{}
	svc, catalogStore, store := newRemediationTestService(t, conn)
	ctx := context.Background()
	rubric := testRubric(t)

	p := scoredProduct()
	p.MerchantID, p.StoreID, p.SKU = "merch-1", "store-1", "SKU-1"
	p.Description = ""
	p.MappedFields = map[string]string{} // no weight, no dimensions
	require.NoError(t, catalogStore.UpsertProduct(ctx, p))
	require.NoError(t, svc.runScoreStage(ctx, p, rubric, true))

	recs := store.forSKU("SKU-1")
	require.NotEmpty(t, recs)
	ids := make([]string, 0, len(recs))
	for id, r := range recs {
		assert.Equal(t, "manual", r.Type, id)
		assert.Empty(t, r.ToValue, id)
		ids = append(ids, id)
	}
	for _, i := range p.Issues {
		assert.Equal(t, "manual", i.Remediable, i.IssueID)
	}

	require.NoError(t, svc.handleRemediationsAppliedWith(ctx, applyCommand(t, "wb-1", ids...), rubric))
	assert.Empty(t, conn.updates, "nothing may be written to the merchant's store")
	for id, r := range store.forSKU("SKU-1") {
		assert.False(t, r.Applied, id)
	}
}
