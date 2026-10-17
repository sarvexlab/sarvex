// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package insights_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/insights"
)

// TestStore_TenancyIsolation is A6/R13's fast, always-run counterpart to
// the integration test's real-Mongo version: no List* method returns a
// row belonging to a different merchant, exercised against the in-memory
// Store so it runs without MongoDB.
func TestStore_TenancyIsolation(t *testing.T) {
	store := insights.NewInMemoryStoreForTest()
	ctx := context.Background()
	day := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)

	require.NoError(t, store.FoldCatalogQuery(ctx, insights.CatalogQuery{
		EventID: "evt-a", MerchantID: "merch-a", StoreID: "store-a", ProductIDs: []string{"sku-1"}, OccurredAt: day,
	}))
	require.NoError(t, store.FoldCatalogQuery(ctx, insights.CatalogQuery{
		EventID: "evt-b", MerchantID: "merch-b", StoreID: "store-b", ProductIDs: []string{"sku-1"}, OccurredAt: day,
	}))
	require.NoError(t, store.FoldNegotiationOutcome(ctx, insights.NegotiationOutcome{
		DedupKey: "neg-a", MerchantID: "merch-a", StoreID: "store-a", OccurredAt: day,
		Succeeded: false, FailureReason: insights.NegotiationFailureCapabilitiesIncompatible,
		RequestedCapabilities: []string{"dev.ucp.shopping.loyalty"}, DeclaredCapabilities: []string{},
	}))
	require.NoError(t, store.FoldNegotiationOutcome(ctx, insights.NegotiationOutcome{
		DedupKey: "neg-b", MerchantID: "merch-b", StoreID: "store-b", OccurredAt: day,
		Succeeded: false, FailureReason: insights.NegotiationFailureCapabilitiesIncompatible,
		RequestedCapabilities: []string{"dev.ucp.shopping.loyalty"}, DeclaredCapabilities: []string{},
	}))

	rowsA, err := store.ListProductDemand(ctx, "merch-a", "", "2026_08_01", "2026_08_31")
	require.NoError(t, err)
	if assert.Len(t, rowsA, 1) {
		assert.Equal(t, "merch-a", rowsA[0].MerchantID)
	}

	gapsA, err := store.ListCapabilityGaps(ctx, "merch-a", "", "2026_08")
	require.NoError(t, err)
	if assert.Len(t, gapsA, 1) {
		assert.Equal(t, "merch-a", gapsA[0].MerchantID)
	}

	// A merchant with no data of its own must see nothing — not merch-a's
	// or merch-b's rows leaking through an unfiltered scan.
	rowsC, err := store.ListProductDemand(ctx, "merch-c", "", "2026_08_01", "2026_08_31")
	require.NoError(t, err)
	assert.Empty(t, rowsC)
}

// bannedFieldSubstrings are field-name fragments that would indicate PII
// leaking into an aggregate document (R12/A6). Checked against every bson
// tag on every exported struct this package persists.
var bannedFieldSubstrings = []string{
	"ip_address", "ip", "user_agent", "email", "buyer", "customer_name",
	"raw_query", "query_text", "search_text", "phone", "address",
}

// TestAggregateStructs_NoPIIFields walks every persisted aggregate type's
// bson tags via reflection and fails if any field name contains a banned
// PII-shaped substring — a structural guard against a future field
// addition accidentally reintroducing PII, not just a point-in-time check.
func TestAggregateStructs_NoPIIFields(t *testing.T) {
	types := []reflect.Type{
		reflect.TypeOf(insights.ProductDemandDaily{}),
		reflect.TypeOf(insights.NegotiationDaily{}),
		reflect.TypeOf(insights.PaymentHandlerMismatchDaily{}),
		reflect.TypeOf(insights.CapabilityGapMonthly{}),
	}
	for _, typ := range types {
		for i := range typ.NumField() {
			field := typ.Field(i)
			tag := field.Tag.Get("bson")
			name := strings.Split(tag, ",")[0]
			lower := strings.ToLower(name)
			for _, banned := range bannedFieldSubstrings {
				if lower == banned || strings.Contains(lower, "_"+banned) || strings.HasPrefix(lower, banned+"_") {
					t.Errorf("%s.%s (bson tag %q) looks like a PII field — not allowed in an aggregate document (R12)", typ.Name(), field.Name, tag)
				}
			}
		}
	}
}
