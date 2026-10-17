// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package insights

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// connectTestMongo mirrors ledger/channel's identical helper.
func connectTestMongo(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		t.Skip("MONGO_URI not set")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)

	db := client.Database("insights_test")
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return db
}

// TestMongoStore_FoldCatalogQuery_RealUpsert proves the $setOnInsert/$inc
// upsert pipeline against a real server: two distinct events touching an
// overlapping product set correctly sum request_count and
// surface_breakdown, and a redelivered event_id is a true no-op (A1/A2).
func TestMongoStore_FoldCatalogQuery_RealUpsert(t *testing.T) {
	db := connectTestMongo(t)
	store, err := NewMongoStore(db)
	require.NoError(t, err)
	ctx := context.Background()
	day := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)

	require.NoError(t, store.FoldCatalogQuery(ctx, CatalogQuery{
		EventID: "evt-1", MerchantID: "merch-1", StoreID: "store-1",
		ProductIDs: []string{"sku-1", "sku-2"}, AgentPlatformHost: "gemini.google.com", OccurredAt: day,
	}))
	require.NoError(t, store.FoldCatalogQuery(ctx, CatalogQuery{
		EventID: "evt-2", MerchantID: "merch-1", StoreID: "store-1",
		ProductIDs: []string{"sku-1"}, AgentPlatformHost: "google.com", OccurredAt: day,
	}))
	// Redelivery of evt-1 must not double-count.
	require.NoError(t, store.FoldCatalogQuery(ctx, CatalogQuery{
		EventID: "evt-1", MerchantID: "merch-1", StoreID: "store-1",
		ProductIDs: []string{"sku-1", "sku-2"}, AgentPlatformHost: "gemini.google.com", OccurredAt: day,
	}))

	rows, err := store.ListProductDemand(ctx, "merch-1", "", "2026_08_01", "2026_08_31")
	require.NoError(t, err)
	require.Len(t, rows, 2)

	byID := map[string]ProductDemandDaily{}
	for _, r := range rows {
		byID[r.ProductID] = r
	}
	assert.Equal(t, int64(2), byID["sku-1"].RequestCount) // evt-1 + evt-2, not evt-1 twice
	assert.Equal(t, int64(1), byID["sku-2"].RequestCount)
	assert.Equal(t, int64(1), byID["sku-1"].SurfaceBreakdown[string(SurfaceGoogleGemini)])
	assert.Equal(t, int64(1), byID["sku-1"].SurfaceBreakdown[string(SurfaceGoogleAIMode)])
}

// TestMongoStore_FoldNegotiationOutcome_CapabilityGapRealUpsert proves the
// control_capability_gaps_monthly $addToSet/$inc pipeline against a real server.
func TestMongoStore_FoldNegotiationOutcome_CapabilityGapRealUpsert(t *testing.T) {
	db := connectTestMongo(t)
	store, err := NewMongoStore(db)
	require.NoError(t, err)
	ctx := context.Background()
	day := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)

	base := NegotiationOutcome{
		MerchantID: "merch-1", StoreID: "store-1", OccurredAt: day,
		Succeeded: false, FailureReason: NegotiationFailureCapabilitiesIncompatible,
		RequestedCapabilities: []string{"dev.ucp.shopping.loyalty"},
		DeclaredCapabilities:  []string{},
	}
	first := base
	first.DedupKey = "k1"
	first.PlatformProfileURI = "https://gemini.google.com/.well-known/ucp"
	require.NoError(t, store.FoldNegotiationOutcome(ctx, first))

	second := base
	second.DedupKey = "k2"
	second.PlatformProfileURI = "https://aimode.google.com/.well-known/ucp"
	require.NoError(t, store.FoldNegotiationOutcome(ctx, second))

	gaps, err := store.ListCapabilityGaps(ctx, "merch-1", "", "2026_08")
	require.NoError(t, err)
	require.Len(t, gaps, 1)
	assert.Equal(t, int64(2), gaps[0].RequestCount)
	assert.Len(t, gaps[0].DistinctPlatformURIs, 2, "two distinct platform_profile_uris should both be recorded via $addToSet")
	assert.Equal(t, int64(0), gaps[0].EstimatedImpactMinor)
	assert.Equal(t, ImpactUnmeasurable, gaps[0].ImpactConfidence)
}

// TestMongoStore_TenancyIsolation is A6/R13: no List* method may return a
// row belonging to a different merchant, even when queried across every
// store.
func TestMongoStore_TenancyIsolation(t *testing.T) {
	db := connectTestMongo(t)
	store, err := NewMongoStore(db)
	require.NoError(t, err)
	ctx := context.Background()
	day := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)

	require.NoError(t, store.FoldCatalogQuery(ctx, CatalogQuery{
		EventID: "evt-a", MerchantID: "merch-a", StoreID: "store-a", ProductIDs: []string{"sku-1"}, OccurredAt: day,
	}))
	require.NoError(t, store.FoldCatalogQuery(ctx, CatalogQuery{
		EventID: "evt-b", MerchantID: "merch-b", StoreID: "store-b", ProductIDs: []string{"sku-1"}, OccurredAt: day,
	}))

	rowsA, err := store.ListProductDemand(ctx, "merch-a", "", "2026_08_01", "2026_08_31")
	require.NoError(t, err)
	require.Len(t, rowsA, 1)
	assert.Equal(t, "merch-a", rowsA[0].MerchantID)
}
