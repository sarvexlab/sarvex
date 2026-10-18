// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package operator_test

import (
	"context"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/operator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func setupTestMongoDB(t *testing.T) *mongo.Database {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://admin:password@localhost:27017/?directConnection=true"))
	require.NoError(t, err)

	db := client.Database("sarvex_operator_test")

	_ = db.Drop(ctx)

	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})

	return db
}

func TestMongoManualStepRepo_Insert_Get(t *testing.T) {
	t.Skip("requires MongoDB running locally")

	db := setupTestMongoDB(t)
	repo, err := operator.NewMongoManualStepRepository(db)
	require.NoError(t, err)
	ctx := context.Background()

	m := &operator.ManualStepModel{
		ID:               "step1",
		TargetMerchantID: "merch1",
		OperatorUserID:   "op1",
		Description:      "Fixed issue",
		Disposition:      "unclassified",
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}

	err = repo.Insert(ctx, m)
	require.NoError(t, err)

	// List should return the step.
	steps, hasMore, err := repo.ListByMerchant(ctx, "merch1", "", 100)
	require.NoError(t, err)
	require.Equal(t, 1, len(steps))
	require.False(t, hasMore)
	require.Equal(t, "Fixed issue", steps[0].Description)
}

func TestMongoAuditLogRepo_Append_List(t *testing.T) {
	t.Skip("requires MongoDB running locally")

	db := setupTestMongoDB(t)
	repo, err := operator.NewMongoAuditLogRepository(db)
	require.NoError(t, err)
	ctx := context.Background()

	e := &operator.AuditEntryModel{
		ID:               "audit1",
		OperatorUserID:   "op1",
		TargetMerchantID: "merch1",
		Action:           "manual_step.captured",
		OccurredAt:       time.Now().UTC(),
	}

	err = repo.Append(ctx, e)
	require.NoError(t, err)

	entries, hasMore, err := repo.List(ctx, nil, "", 100)
	require.NoError(t, err)
	require.Equal(t, 1, len(entries))
	require.False(t, hasMore)
	require.Equal(t, "manual_step.captured", entries[0].Action)
}

// TestMongoBoardRepo_ListCards_ResolvesMerchantIDFromStore seeds real
// journey/store/account documents and verifies ListCards resolves the
// board card's merchant_id via the control_stores join — journey._id is
// the store_id, not the merchant_id (see journey.JourneyModel's doc
// comment), so a card must never surface the store_id where merchant_id
// belongs.
func TestMongoBoardRepo_ListCards_ResolvesMerchantIDFromStore(t *testing.T) {
	t.Skip("requires MongoDB running locally")

	db := setupTestMongoDB(t)
	repo, err := operator.NewMongoBoardRepository(db)
	require.NoError(t, err)
	ctx := context.Background()

	now := time.Now().UTC()
	_, err = db.Collection("control_journey").InsertOne(ctx, map[string]any{
		"_id": "store-1", "state": "connected", "stage": "onboarding",
		"entered_state_at": now, "entered_stage_at": now,
		"created_at": now, "updated_at": now,
	})
	require.NoError(t, err)
	_, err = db.Collection("control_stores").InsertOne(ctx, map[string]any{
		"_id": "store-1", "merchant_id": "merch-1", "display_name": "Acme Store",
	})
	require.NoError(t, err)
	_, err = db.Collection("control_accounts").InsertOne(ctx, map[string]any{
		"_id": "merch-1", "name": "Acme Pty Ltd",
	})
	require.NoError(t, err)

	state := domain.JourneyState("connected")
	cards, hasMore, err := repo.ListCards(ctx, &state, nil, "", 100)
	require.NoError(t, err)
	require.False(t, hasMore)
	require.Len(t, cards, 1)

	card := cards[0]
	assert.Equal(t, "merch-1", card.MerchantID)
	assert.Equal(t, "store-1", card.StoreID)
	assert.Equal(t, "Acme Pty Ltd", card.Name)
}

// TestMongoBoardRepo_ListCards_SkipsOrphanedJourney verifies a journey
// record whose store was deleted (or never linked to a merchant) is
// omitted rather than surfaced with an empty/wrong merchant_id.
func TestMongoBoardRepo_ListCards_SkipsOrphanedJourney(t *testing.T) {
	t.Skip("requires MongoDB running locally")

	db := setupTestMongoDB(t)
	repo, err := operator.NewMongoBoardRepository(db)
	require.NoError(t, err)
	ctx := context.Background()

	now := time.Now().UTC()
	_, err = db.Collection("control_journey").InsertOne(ctx, map[string]any{
		"_id": "store-orphan", "state": "connected", "stage": "onboarding",
		"entered_state_at": now, "entered_stage_at": now,
		"created_at": now, "updated_at": now,
	})
	require.NoError(t, err)
	// Deliberately no matching control_stores document.

	state := domain.JourneyState("connected")
	cards, _, err := repo.ListCards(ctx, &state, nil, "", 100)
	require.NoError(t, err)
	assert.Empty(t, cards)
}

func TestMongoConsistencyRepo_Save_ListByStore_CountByStore(t *testing.T) {
	t.Skip("requires MongoDB running locally")

	db := setupTestMongoDB(t)
	repo, err := operator.NewMongoConsistencyRepository(db)
	require.NoError(t, err)
	ctx := context.Background()

	now := time.Now().UTC()
	require.NoError(t, repo.Save(ctx, "store-1", &domain.ConsistencyFinding{
		FindingID: "f1", SKU: "SKU-1", Field: "price_rebuild_lag",
		Sources:  map[string]string{"store": "2500", "served": "2000"},
		Severity: domain.SeverityWarning, DetectedAt: now,
	}))
	require.NoError(t, repo.Save(ctx, "store-1", &domain.ConsistencyFinding{
		FindingID: "f2", SKU: "SKU-2", Field: "stock_safety",
		Severity: domain.SeverityCritical, DetectedAt: now.Add(time.Second),
	}))
	// A finding on a different store must not leak into store-1's results.
	require.NoError(t, repo.Save(ctx, "store-2", &domain.ConsistencyFinding{
		FindingID: "f3", SKU: "SKU-3", Field: "stock_safety",
		Severity: domain.SeverityCritical, DetectedAt: now,
	}))

	findings, hasMore, err := repo.ListByStore(ctx, "store-1", "", 20, nil)
	require.NoError(t, err)
	require.False(t, hasMore)
	require.Len(t, findings, 2)
	// Newest-detected first.
	require.Equal(t, "SKU-2", findings[0].SKU)

	critical := domain.SeverityCritical
	findings, _, err = repo.ListByStore(ctx, "store-1", "", 20, &critical)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, "SKU-2", findings[0].SKU)

	count, err := repo.CountByStore(ctx, "store-1", domain.SeverityCritical)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Re-detecting the same finding (same FindingID) upserts, not duplicates.
	require.NoError(t, repo.Save(ctx, "store-1", &domain.ConsistencyFinding{
		FindingID: "f1", SKU: "SKU-1", Field: "price_rebuild_lag",
		Severity: domain.SeverityCritical, DetectedAt: now.Add(2 * time.Second),
	}))
	findings, _, err = repo.ListByStore(ctx, "store-1", "", 20, nil)
	require.NoError(t, err)
	require.Len(t, findings, 2)

	count, err = repo.CountByStore(ctx, "store-1", domain.SeverityCritical)
	require.NoError(t, err)
	require.Equal(t, 2, count, "f1 upserted to critical severity")
}

func TestMongoHealthRepo_Save_GetByStore(t *testing.T) {
	t.Skip("requires MongoDB running locally")

	db := setupTestMongoDB(t)
	repo, err := operator.NewMongoHealthRepository(db)
	require.NoError(t, err)
	ctx := context.Background()

	now := time.Now().UTC()
	require.NoError(t, repo.Save(ctx, "store-1", domain.HealthStatement{
		Kind: domain.HealthConsistency, OK: true, Message: "No findings.", CheckedAt: now,
	}))
	require.NoError(t, repo.Save(ctx, "store-1", domain.HealthStatement{
		Kind: domain.HealthSemanticEndpoint, OK: false, Message: "catalog_search failed", CheckedAt: now,
	}))
	// A statement on a different store must not leak into store-1's results.
	require.NoError(t, repo.Save(ctx, "store-2", domain.HealthStatement{
		Kind: domain.HealthConsistency, OK: true, Message: "No findings.", CheckedAt: now,
	}))

	statements, err := repo.GetByStore(ctx, "store-1")
	require.NoError(t, err)
	require.Len(t, statements, 2)

	// Re-saving the same kind upserts, not duplicates.
	require.NoError(t, repo.Save(ctx, "store-1", domain.HealthStatement{
		Kind: domain.HealthConsistency, OK: false, Message: "1 critical finding.", CheckedAt: now.Add(time.Minute),
	}))
	statements, err = repo.GetByStore(ctx, "store-1")
	require.NoError(t, err)
	require.Len(t, statements, 2)
}
