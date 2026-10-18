// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package surface_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/surfaces"
	"github.com/sarvexlab/sarvex/internal/planes/sync/surface"
)

// connectTestMongo skips (rather than fails) when MONGO_URI is unset, like the
// rest of the sync plane's Mongo tests.
func connectTestMongo(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		t.Skip("MONGO_URI not set; skipping MongoDB surface artifact tests")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	db := client.Database("sync_surface_artifacts_test")
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return db
}

func TestMongoArtifactStore(t *testing.T) {
	db := connectTestMongo(t)
	ctx := context.Background()
	st, err := surface.NewMongoArtifactStore(ctx, db)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Millisecond)
	rec := surfaces.ArtifactRecord{
		MerchantID: "m1", StoreID: "s1", SurfaceID: "fixture", ArtifactID: "doc",
		Token: "tok-1", URL: "https://x/tok-1", Detail: map[string]string{"n": "1"}, GeneratedAt: &now,
	}
	saved, err := st.Save(ctx, rec)
	require.NoError(t, err)
	assert.Equal(t, "tok-1", saved.Token)

	// A later save must not rotate the token: the URL already given out has
	// to stay valid.
	rec.Token, rec.Detail = "tok-2", map[string]string{"n": "2"}
	saved, err = st.Save(ctx, rec)
	require.NoError(t, err)
	assert.Equal(t, "tok-1", saved.Token)
	assert.Equal(t, "2", saved.Detail["n"])

	// First fetch is observed exactly once.
	first, err := st.MarkFetched(ctx, saved, now)
	require.NoError(t, err)
	assert.True(t, first)
	first, err = st.MarkFetched(ctx, saved, now.Add(time.Minute))
	require.NoError(t, err)
	assert.False(t, first)
	got, err := st.FindByToken(ctx, "tok-1")
	require.NoError(t, err)
	assert.True(t, got.Observed(surfaces.SignalFirstFetch))
	assert.True(t, now.Equal(*got.FirstFetchedAt))
	assert.True(t, now.Add(time.Minute).Equal(*got.LastFetchedAt))

	// Saving again (a regeneration) keeps the fetch history.
	_, err = st.Save(ctx, rec)
	require.NoError(t, err)
	got, err = st.Get(ctx, "m1", "s1", "fixture", "doc")
	require.NoError(t, err)
	assert.NotNil(t, got.FirstFetchedAt)

	// Tenant fence: the same store id under another merchant sees nothing.
	_, err = st.Get(ctx, "m2", "s1", "fixture", "doc")
	assert.ErrorIs(t, err, surface.ErrNoRecord)

	require.NoError(t, st.Delete(ctx, "m1", "s1", "fixture", "doc"))
	_, err = st.Get(ctx, "m1", "s1", "fixture", "doc")
	assert.ErrorIs(t, err, surface.ErrNoRecord)
}

func TestMongoEnrollmentSource(t *testing.T) {
	db := connectTestMongo(t)
	ctx := context.Background()
	col := db.Collection("control_store_surfaces")
	_, err := col.InsertMany(ctx, []bson.M{
		{"_id": "m1:s1:fixture", "merchant_id": "m1", "store_id": "s1", "surface_id": "fixture", "state": "configured", "config": bson.M{"k": "v"}},
		{"_id": "m1:s2:fixture", "merchant_id": "m1", "store_id": "s2", "surface_id": "fixture", "state": "available"},
		{"_id": "m2:s1:other", "merchant_id": "m2", "store_id": "s1", "surface_id": "other", "state": "enrolled"},
	})
	require.NoError(t, err)
	src := surface.NewMongoEnrollmentSource(db)

	e, ok, err := src.Get(ctx, "m1", "s1", "fixture")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "v", e.Config["k"])

	_, ok, err = src.Get(ctx, "m1", "s2", "fixture")
	require.NoError(t, err)
	assert.False(t, ok, "available is not enrolled")

	_, ok, err = src.Get(ctx, "m2", "s1", "fixture")
	require.NoError(t, err)
	assert.False(t, ok, "tenant fence: same store id, other merchant, other surface")

	list, err := src.List(ctx, "fixture")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "s1", list[0].StoreID)
}
