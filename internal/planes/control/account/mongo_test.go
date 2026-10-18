// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package account_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/planes/control/account"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

func connectTestMongo(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		t.Skip("MONGO_URI not set; skipping MongoDB integration tests")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	db := client.Database("control_test")
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return db
}

// ── AccountRepository ─────────────────────────────────────────────────────────

func TestMongoAccountRepo_GetNotFound(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := account.NewMongoAccountRepository(db)
	require.NoError(t, err)

	_, err = repo.Get(context.Background(), "does-not-exist")
	assert.ErrorIs(t, err, account.ErrNotFound)
}

func TestMongoAccountRepo_UpsertAndGet(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := account.NewMongoAccountRepository(db)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Millisecond)
	m := &account.AccountModel{
		ID:       "merch-mongo-1",
		Name:     "Mongo Store",
		Timezone: "UTC",
		BusinessFacts: account.BusinessFactsModel{
			LegalEntityName:    "Mongo Pty Ltd",
			SupportEmail:       "support@mongo.com",
			FulfillmentRegions: []string{"AU"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	require.NoError(t, repo.Upsert(context.Background(), m))

	got, err := repo.Get(context.Background(), domain.MerchantID("merch-mongo-1"))
	require.NoError(t, err)
	assert.Equal(t, "merch-mongo-1", got.ID)
	assert.Equal(t, "Mongo Store", got.Name)
	assert.Equal(t, "Mongo Pty Ltd", got.BusinessFacts.LegalEntityName)
	assert.Equal(t, []string{"AU"}, got.BusinessFacts.FulfillmentRegions)
}

func TestMongoAccountRepo_UpsertUpdatesExisting(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := account.NewMongoAccountRepository(db)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Millisecond)
	original := &account.AccountModel{ID: "merch-2", Name: "Original", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, repo.Upsert(context.Background(), original))

	updated := &account.AccountModel{ID: "merch-2", Name: "Updated", Timezone: "Australia/Sydney", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, repo.Upsert(context.Background(), updated))

	got, err := repo.Get(context.Background(), "merch-2")
	require.NoError(t, err)
	assert.Equal(t, "Updated", got.Name)
	assert.Equal(t, "Australia/Sydney", got.Timezone)
}

// ── MemberRepository ──────────────────────────────────────────────────────────

func TestMongoMemberRepo_ListEmpty(t *testing.T) {
	db := connectTestMongo(t)
	repo, err := account.NewMongoMemberRepository(db)
	require.NoError(t, err)

	members, hasMore, err := repo.ListByMerchant(context.Background(), "merch-empty", "", 20)
	require.NoError(t, err)
	assert.Empty(t, members)
	assert.False(t, hasMore)
}

func TestMongoMemberRepo_ListAndPaginate(t *testing.T) {
	db := connectTestMongo(t)
	// Seed directly via account repo upsert pattern — members collection needs documents.
	memberRepo, err := account.NewMongoMemberRepository(db)
	require.NoError(t, err)

	// Insert via the underlying collection through an AccountService + fake.
	// For integration tests we seed by inserting MemberModels via a helper that
	// calls Upsert indirectly. Since MemberRepository only exposes List, we
	// seed by constructing an AccountService with a fake account repo and the
	// real member repo — members are seeded by the invite flow (out of scope).
	// Instead, insert directly through the raw collection by casting via a
	// concrete accessor. We expose a helper for tests only.
	ctx := context.Background()
	col := db.Collection("control_members")
	seeds := []any{
		account.MemberModel{UserID: "u-1", MerchantID: "merch-pg", Email: "a@test.com", Name: "Alice", Roles: []string{"owner"}, Status: "active", CountryCode: "AU"},
		account.MemberModel{UserID: "u-2", MerchantID: "merch-pg", Email: "b@test.com", Name: "Bob", Roles: []string{"staff"}, Status: "active", CountryCode: "AU"},
		account.MemberModel{UserID: "u-3", MerchantID: "merch-pg", Email: "c@test.com", Name: "Carol", Roles: []string{"staff"}, Status: "invited", CountryCode: "NZ"},
		account.MemberModel{UserID: "u-4", MerchantID: "other-merch", Email: "d@test.com", Name: "Dave", Roles: []string{"owner"}, Status: "active", CountryCode: "US"},
	}
	_, err = col.InsertMany(ctx, seeds)
	require.NoError(t, err)

	// First page
	page1, hasMore, err := memberRepo.ListByMerchant(ctx, "merch-pg", "", 2)
	require.NoError(t, err)
	assert.Len(t, page1, 2)
	assert.True(t, hasMore)
	assert.Equal(t, "u-1", page1[0].UserID)
	assert.Equal(t, "u-2", page1[1].UserID)

	// Second page
	page2, hasMore, err := memberRepo.ListByMerchant(ctx, "merch-pg", page1[1].UserID, 2)
	require.NoError(t, err)
	assert.Len(t, page2, 1)
	assert.False(t, hasMore)
	assert.Equal(t, "u-3", page2[0].UserID)

	// Isolation: other merchant not returned
	all, _, err := memberRepo.ListByMerchant(ctx, "other-merch", "", 20)
	require.NoError(t, err)
	assert.Len(t, all, 1)
	assert.Equal(t, "u-4", all[0].UserID)
}
