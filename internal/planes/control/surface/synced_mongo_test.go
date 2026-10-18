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
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/surfaces"
	"github.com/sarvexlab/sarvex/internal/planes/control/surface"
)

// The control plane reads sync_surface_artifacts by name; this proves the
// stored shape the sync plane writes is the shape this reader decodes, and
// that the read is fenced on both tenant ids. Skips without MONGO_URI.
func TestMongoSyncedArtifactReader(t *testing.T) {
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		t.Skip("MONGO_URI not set; skipping MongoDB synced artifact reader test")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	db := client.Database("control_synced_artifacts_test")
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	_, err = db.Collection(surfaces.CollectionArtifacts).InsertOne(ctx, surfaces.ArtifactRecord{
		ID:         surfaces.ArtifactDocumentID("m-1", "s-1", "example_surface", "example_artifact"),
		MerchantID: "m-1", StoreID: "s-1", SurfaceID: "example_surface", ArtifactID: "example_artifact",
		Token: "t", URL: "https://x/t.bin", Detail: map[string]string{"eligible_count": "3"},
		GeneratedAt: &now, FirstFetchedAt: &now,
	})
	require.NoError(t, err)

	r := surface.NewMongoSyncedArtifactReader(db)
	rec, err := r.Get(ctx, "m-1", "s-1", "example_surface", "example_artifact")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "https://x/t.bin", rec.URL)
	assert.True(t, rec.Observed(surfaces.SignalFirstFetch))

	rec, err = r.Get(ctx, "m-2", "s-1", "example_surface", "example_artifact")
	require.NoError(t, err)
	assert.Nil(t, rec, "another merchant's store id never matches")

	rec, err = r.Get(ctx, "m-1", "s-1", "example_surface", "nope")
	require.NoError(t, err)
	assert.Nil(t, rec)
}
