// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package task

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

type mongoTaskRepo struct {
	col *mongo.Collection
}

func NewMongoTaskRepository(db *mongo.Database) (TaskRepository, error) {
	r := &mongoTaskRepo{col: db.Collection("control_tasks")}
	if err := r.createIndexes(context.Background()); err != nil {
		return nil, fmt.Errorf("task indexes: %w", err)
	}
	return r, nil
}

func (r *mongoTaskRepo) createIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "merchant_id", Value: 1}}},
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "store_id", Value: 1}}},
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "state", Value: 1}}},
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "owner", Value: 1}}},
		// Backs the feed's sort key (see listSort): checkout-blocking work
		// first, newest next, _id only to break exact ties.
		{Keys: bson.D{
			{Key: "merchant_id", Value: 1},
			{Key: "blocks_checkout", Value: -1},
			{Key: "created_at", Value: -1},
			{Key: "_id", Value: -1},
		}},
	})
	return err
}

// listSort is the one order the task feed is ever read in: what blocks the
// merchant's checkout first, then most recent.
//
// It used to sort on _id alone, which — because ids are the compound key
// source:store:source_ref — ordered the merchant's Path alphabetically by
// source (validation above readiness above monitoring above checklist above
// catalog_import). That put a stale validation note above a checkout-blocking
// finding raised a minute ago, and, with the default page size of 20, could
// push every checklist task off the first page entirely.
var listSort = bson.D{
	{Key: "blocks_checkout", Value: -1},
	{Key: "created_at", Value: -1},
	{Key: "_id", Value: -1},
}

func (r *mongoTaskRepo) Insert(ctx context.Context, m *TaskModel) error {
	_, err := r.col.InsertOne(ctx, m)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrAlreadyExists
		}
		return fmt.Errorf("insert task: %w", err)
	}
	return nil
}

func (r *mongoTaskRepo) FindByMerchantAndID(ctx context.Context, merchantID domain.MerchantID, taskID string) (*TaskModel, error) {
	var m TaskModel
	err := r.col.FindOne(ctx, bson.M{
		"_id":         taskID,
		"merchant_id": string(merchantID),
	}).Decode(&m)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find task %s: %w", taskID, err)
	}
	return &m, nil
}

func (r *mongoTaskRepo) UpdateState(ctx context.Context, taskID string, state string, note *string, updatedAt time.Time) (*TaskModel, error) {
	update := bson.M{
		"state":      state,
		"updated_at": updatedAt,
	}
	if note != nil {
		update["resolution_note"] = note
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var m TaskModel
	err := r.col.FindOneAndUpdate(ctx,
		bson.M{"_id": taskID},
		bson.M{"$set": update},
		opts,
	).Decode(&m)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update task state %s: %w", taskID, err)
	}
	return &m, nil
}

func (r *mongoTaskRepo) ListByMerchant(ctx context.Context, merchantID domain.MerchantID, storeID *domain.StoreID, owner *domain.TaskOwner, state *domain.TaskState, after *ListCursor, limit int) ([]*TaskModel, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	filter := bson.M{"merchant_id": string(merchantID)}
	if storeID != nil {
		filter["store_id"] = string(*storeID)
	}
	if owner != nil {
		filter["owner"] = string(*owner)
	}
	if state != nil {
		filter["state"] = string(*state)
	}
	if after != nil {
		filter["$and"] = []bson.M{afterCursor(*after)}
	}

	opts := options.Find().
		SetSort(listSort).
		SetLimit(int64(limit + 1))

	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, false, fmt.Errorf("list tasks for merchant %s: %w", merchantID, err)
	}
	defer cursor.Close(ctx)

	var tasks []*TaskModel
	if err = cursor.All(ctx, &tasks); err != nil {
		return nil, false, fmt.Errorf("decode tasks for merchant %s: %w", merchantID, err)
	}

	hasMore := len(tasks) > limit
	if hasMore {
		tasks = tasks[:limit]
	}
	return tasks, hasMore, nil
}

// afterCursor is the keyset predicate for "strictly after this position" in
// listSort's order: a lower blocks_checkout, or the same with an older
// created_at, or both the same with a lower id. Written out longhand
// because Mongo has no tuple comparison.
func afterCursor(c ListCursor) bson.M {
	createdAt := c.CreatedAt.UTC()
	return bson.M{"$or": []bson.M{
		{"blocks_checkout": bson.M{"$lt": c.BlocksCheckout}},
		{"blocks_checkout": c.BlocksCheckout, "created_at": bson.M{"$lt": createdAt}},
		{"blocks_checkout": c.BlocksCheckout, "created_at": createdAt, "_id": bson.M{"$lt": c.ID}},
	}}
}

func (r *mongoTaskRepo) ListOpenByMerchants(ctx context.Context, merchantIDs []domain.MerchantID) ([]*TaskModel, error) {
	if len(merchantIDs) == 0 {
		return nil, nil
	}

	ids := make([]string, len(merchantIDs))
	for i, id := range merchantIDs {
		ids[i] = string(id)
	}

	// Every non-done state is open (domain.Task.IsOpen). Matching state ==
	// "open" exactly, as this did, dropped in_progress and blocked — which
	// is every prescribed milestone — so the operator board read zero
	// outstanding for a store that was mid-import or waiting on Google.
	filter := bson.M{
		"merchant_id": bson.M{"$in": ids},
		"state":       bson.M{"$ne": string(domain.TaskStateDone)},
	}
	opts := options.Find().SetSort(listSort)

	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("list open tasks for %d merchants: %w", len(merchantIDs), err)
	}
	defer cursor.Close(ctx)

	var tasks []*TaskModel
	if err = cursor.All(ctx, &tasks); err != nil {
		return nil, fmt.Errorf("decode open tasks: %w", err)
	}
	return tasks, nil
}
