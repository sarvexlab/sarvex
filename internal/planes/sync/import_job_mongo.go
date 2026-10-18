// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongoopts "go.mongodb.org/mongo-driver/v2/mongo/options"
)

// importJobsCollection holds one document per store: the current or most
// recently finished catalog import job for that store.
const importJobsCollection = "sync_import_jobs"

// MongoImportJobRepository is a MongoDB-backed ImportJobRepository.
type MongoImportJobRepository struct {
	db *mongo.Database
}

// NewMongoImportJobRepository creates a MongoDB-backed import job repository.
func NewMongoImportJobRepository(db *mongo.Database) (*MongoImportJobRepository, error) {
	coll := db.Collection(importJobsCollection)

	// Index on status to make ListByStatus (startup resume scan) efficient.
	_, err := coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "status", Value: 1}},
	})
	if err != nil {
		return nil, fmt.Errorf("create import job indexes: %w", err)
	}

	return &MongoImportJobRepository{db: db}, nil
}

// Get retrieves the import job for a store.
func (r *MongoImportJobRepository) Get(ctx context.Context, storeID string) (*ImportJob, error) {
	var job ImportJob
	err := r.db.Collection(importJobsCollection).FindOne(ctx, bson.M{"_id": storeID}).Decode(&job)
	if err == mongo.ErrNoDocuments {
		return nil, ErrImportJobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get import job: %w", err)
	}
	return &job, nil
}

// Upsert persists the current state of an import job, keyed by store_id.
func (r *MongoImportJobRepository) Upsert(ctx context.Context, job *ImportJob) error {
	if job.StoreID == "" {
		return fmt.Errorf("sync: import job missing store_id")
	}
	opts := mongoopts.UpdateOne().SetUpsert(true)
	_, err := r.db.Collection(importJobsCollection).UpdateOne(ctx, bson.M{"_id": job.StoreID}, bson.M{"$set": job}, opts)
	if err != nil {
		return fmt.Errorf("upsert import job: %w", err)
	}
	return nil
}

// ListByStatus returns all jobs in the given status.
func (r *MongoImportJobRepository) ListByStatus(ctx context.Context, status ImportStatus) ([]*ImportJob, error) {
	cursor, err := r.db.Collection(importJobsCollection).Find(ctx, bson.M{"status": string(status)})
	if err != nil {
		return nil, fmt.Errorf("list import jobs: %w", err)
	}
	defer cursor.Close(ctx)

	jobs := make([]*ImportJob, 0)
	if err := cursor.All(ctx, &jobs); err != nil {
		return nil, fmt.Errorf("decode import jobs: %w", err)
	}
	return jobs, nil
}
