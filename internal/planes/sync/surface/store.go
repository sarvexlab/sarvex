// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package surface

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/surfaces"
)

// ErrNoRecord is returned when no artifact record matches.
var ErrNoRecord = errors.New("surface: no artifact record")

// ArtifactStore persists sync_surface_artifacts. The sync plane is its one
// writer.
type ArtifactStore interface {
	Get(ctx context.Context, merchantID, storeID, surfaceID, artifactID string) (*surfaces.ArtifactRecord, error)
	// Save writes the generated fields of rec, keeping any token and fetch
	// history already stored. rec.Token is used only when none exists yet.
	Save(ctx context.Context, rec surfaces.ArtifactRecord) (*surfaces.ArtifactRecord, error)
	FindByToken(ctx context.Context, token string) (*surfaces.ArtifactRecord, error)
	// MarkFetched records a fetch at t and reports whether it was the first.
	MarkFetched(ctx context.Context, rec *surfaces.ArtifactRecord, t time.Time) (first bool, err error)
	Delete(ctx context.Context, merchantID, storeID, surfaceID, artifactID string) error
}

type mongoStore struct{ col *mongo.Collection }

// NewMongoArtifactStore returns the Mongo-backed store and ensures its indexes.
func NewMongoArtifactStore(ctx context.Context, db *mongo.Database) (ArtifactStore, error) {
	col := db.Collection(surfaces.CollectionArtifacts)
	_, err := col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "token", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "store_id", Value: 1}}},
	})
	if err != nil {
		return nil, fmt.Errorf("%s indexes: %w", surfaces.CollectionArtifacts, err)
	}
	return &mongoStore{col: col}, nil
}

func (s *mongoStore) filter(merchantID, storeID, surfaceID, artifactID string) bson.M {
	// Query-level tenant fence: both ids are in the filter (ADR-2).
	return bson.M{
		"_id":         surfaces.ArtifactDocumentID(merchantID, storeID, surfaceID, artifactID),
		"merchant_id": merchantID,
		"store_id":    storeID,
	}
}

func (s *mongoStore) Get(ctx context.Context, merchantID, storeID, surfaceID, artifactID string) (*surfaces.ArtifactRecord, error) {
	var rec surfaces.ArtifactRecord
	err := s.col.FindOne(ctx, s.filter(merchantID, storeID, surfaceID, artifactID)).Decode(&rec)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNoRecord
	}
	if err != nil {
		return nil, fmt.Errorf("read artifact record: %w", err)
	}
	return &rec, nil
}

func (s *mongoStore) Save(ctx context.Context, rec surfaces.ArtifactRecord) (*surfaces.ArtifactRecord, error) {
	rec.ID = surfaces.ArtifactDocumentID(rec.MerchantID, rec.StoreID, rec.SurfaceID, rec.ArtifactID)
	now := time.Now().UTC()
	update := bson.M{
		"$set": bson.M{
			"merchant_id":  rec.MerchantID,
			"store_id":     rec.StoreID,
			"surface_id":   rec.SurfaceID,
			"artifact_id":  rec.ArtifactID,
			"url":          rec.URL,
			"detail":       rec.Detail,
			"generated_at": rec.GeneratedAt,
			"updated_at":   now,
		},
		// The token is the artifact's public address: it is minted once and
		// never rotates, so the URL the merchant gave Google stays valid.
		"$setOnInsert": bson.M{"token": rec.Token},
	}
	var out surfaces.ArtifactRecord
	err := s.col.FindOneAndUpdate(ctx, bson.M{"_id": rec.ID}, update,
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&out)
	if err != nil {
		return nil, fmt.Errorf("save artifact record: %w", err)
	}
	return &out, nil
}

func (s *mongoStore) FindByToken(ctx context.Context, token string) (*surfaces.ArtifactRecord, error) {
	var rec surfaces.ArtifactRecord
	err := s.col.FindOne(ctx, bson.M{"token": token}).Decode(&rec)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNoRecord
	}
	if err != nil {
		return nil, fmt.Errorf("find artifact by token: %w", err)
	}
	return &rec, nil
}

func (s *mongoStore) MarkFetched(ctx context.Context, rec *surfaces.ArtifactRecord, t time.Time) (bool, error) {
	f := s.filter(rec.MerchantID, rec.StoreID, rec.SurfaceID, rec.ArtifactID)
	// first_fetched_at is set only while unset, so exactly one caller sees
	// the first-fetch transition even under concurrent fetches.
	res, err := s.col.UpdateOne(ctx,
		bson.M{"_id": f["_id"], "merchant_id": rec.MerchantID, "store_id": rec.StoreID, "first_fetched_at": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"first_fetched_at": t, "last_fetched_at": t, "updated_at": t}})
	if err != nil {
		return false, fmt.Errorf("mark first fetch: %w", err)
	}
	if res.ModifiedCount == 1 {
		return true, nil
	}
	_, err = s.col.UpdateOne(ctx, f, bson.M{"$set": bson.M{"last_fetched_at": t, "updated_at": t}})
	if err != nil {
		return false, fmt.Errorf("mark fetch: %w", err)
	}
	return false, nil
}

func (s *mongoStore) Delete(ctx context.Context, merchantID, storeID, surfaceID, artifactID string) error {
	if _, err := s.col.DeleteOne(ctx, s.filter(merchantID, storeID, surfaceID, artifactID)); err != nil {
		return fmt.Errorf("delete artifact record: %w", err)
	}
	return nil
}
