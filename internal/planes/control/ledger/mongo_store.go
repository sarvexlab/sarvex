// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// mongoStore is a Store backed by two MongoDB collections. There is no
// literal Firestore here (nor anywhere in this codebase) — merchants/{id}/…
// (R10/R11) is a path convention only; every document carries merchant_id
// as a field and is indexed on it (mirrors every other per-merchant
// collection in this codebase, e.g.
// internal/planes/control/checkoutescalation/mongo_sessions.go).
type mongoStore struct {
	client  *mongo.Client
	entries *mongo.Collection
	rollups *mongo.Collection
}

// NewMongoStore creates a Store, with the indexes ListEntries/InsertEntryAndRollup
// need. client is required (not just db) because InsertEntryAndRollup uses a
// multi-document transaction spanning both collections (R11), which the
// driver only exposes via Client.StartSession.
func NewMongoStore(client *mongo.Client, db *mongo.Database) (Store, error) {
	s := &mongoStore{
		client:  client,
		entries: db.Collection("control_ledger_entries"),
		rollups: db.Collection("control_ledger_rollups"),
	}
	ctx := context.Background()
	if _, err := s.entries.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "event_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "lost_revenue_minor", Value: -1}, {Key: "occurred_at", Value: -1}, {Key: "_id", Value: -1}}},
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "occurred_at", Value: -1}, {Key: "_id", Value: -1}}},
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "cause", Value: 1}}},
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "store_id", Value: 1}}},
	}); err != nil {
		return nil, fmt.Errorf("ledger entry indexes: %w", err)
	}
	if _, err := s.rollups.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "month", Value: 1}, {Key: "currency", Value: 1}}, Options: options.Index().SetUnique(true)},
	}); err != nil {
		return nil, fmt.Errorf("ledger rollup indexes: %w", err)
	}
	return s, nil
}

func rollupID(merchantID, month, currency string) string {
	return merchantID + ":" + month + "__" + currency
}

// InsertEntryAndRollup: see Store's doc comment for the atomicity contract.
// A duplicate e.EventID aborts the transaction with a duplicate-key error,
// which is translated to inserted=false, not returned as err — that's the
// expected, idempotent shape of a redelivered event (R1/A1), and letting it
// abort the transaction (rather than trying to catch it and keep going
// inside the same transaction) is deliberate: MongoDB marks a transaction
// aborted server-side on the first operation error, so a caught error
// cannot be safely followed by more operations in that same transaction.
func (s *mongoStore) InsertEntryAndRollup(ctx context.Context, e Entry) (bool, error) {
	sess, err := s.client.StartSession()
	if err != nil {
		return false, fmt.Errorf("start session: %w", err)
	}
	defer sess.EndSession(ctx)

	var duplicate bool
	_, err = sess.WithTransaction(ctx, func(sc context.Context) (any, error) {
		duplicate = false
		if _, insErr := s.entries.InsertOne(sc, e); insErr != nil {
			if mongo.IsDuplicateKeyError(insErr) {
				duplicate = true
			}
			return nil, insErr
		}
		return nil, s.foldIntoRollup(sc, e)
	})
	if duplicate {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("insert ledger entry and rollup: %w", err)
	}
	return true, nil
}

// foldIntoRollup applies e's contribution to its merchant/month/currency
// rollup: ensures the rollup document exists (upsert), then increments the
// matching cause's breakdown if present, or appends a new one otherwise.
// Both steps run inside the caller's transaction — see InsertEntryAndRollup.
func (s *mongoStore) foldIntoRollup(ctx context.Context, e Entry) error {
	month := e.OccurredAt.UTC().Format("2006_01")
	id := rollupID(e.MerchantID, month, e.Currency)
	now := time.Now().UTC()

	unknownInc := int64(0)
	if e.RevenueUnknown {
		unknownInc = 1
	}

	_, err := s.rollups.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$setOnInsert": bson.M{
			"_id": id, "merchant_id": e.MerchantID, "month": month, "currency": e.Currency,
			"causes": bson.A{},
		},
		"$set": bson.M{"updated_at": now},
	}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("ensure rollup document: %w", err)
	}

	incRes, err := s.rollups.UpdateOne(ctx,
		bson.M{"_id": id, "causes.cause": e.Cause},
		bson.M{"$inc": bson.M{
			"causes.$.count":               1,
			"causes.$.lost_revenue_minor":  e.LostRevenueMinor,
			"total_count":                  1,
			"total_lost_revenue_minor":     e.LostRevenueMinor,
			"entries_with_unknown_revenue": unknownInc,
		}, "$set": bson.M{"updated_at": now}},
	)
	if err != nil {
		return fmt.Errorf("increment existing cause breakdown: %w", err)
	}
	if incRes.MatchedCount > 0 {
		return nil
	}

	// No existing breakdown for this cause yet — append one.
	_, err = s.rollups.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$push": bson.M{"causes": CauseBreakdown{Cause: e.Cause, Count: 1, LostRevenueMinor: e.LostRevenueMinor}},
		"$inc": bson.M{
			"total_count":                  1,
			"total_lost_revenue_minor":     e.LostRevenueMinor,
			"entries_with_unknown_revenue": unknownInc,
		},
		"$set": bson.M{"updated_at": now},
	})
	if err != nil {
		return fmt.Errorf("append new cause breakdown: %w", err)
	}
	return nil
}

func (s *mongoStore) GetEntry(ctx context.Context, merchantID, entryID string) (*Entry, error) {
	var e Entry
	err := s.entries.FindOne(ctx, bson.M{"_id": entryID, "merchant_id": merchantID}).Decode(&e)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get ledger entry %s: %w", entryID, err)
	}
	return &e, nil
}

func (s *mongoStore) ListEntries(ctx context.Context, merchantID string, filter ListFilter, sortMode, cursor string, limit int) ([]Entry, string, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	if sortMode == "" {
		sortMode = SortRevenueDesc
	}

	q := bson.M{"merchant_id": merchantID}
	if filter.Cause != "" {
		q["cause"] = filter.Cause
	}
	if filter.Status != "" {
		q["status"] = filter.Status
	}
	if filter.StoreID != "" {
		q["store_id"] = filter.StoreID
	}
	if filter.Surface != "" {
		q["surface"] = filter.Surface
	}
	if filter.From != nil || filter.To != nil {
		rng := bson.M{}
		if filter.From != nil {
			rng["$gte"] = *filter.From
		}
		if filter.To != nil {
			rng["$lte"] = *filter.To
		}
		q["occurred_at"] = rng
	}

	var sortKeys bson.D
	if sortMode == SortOccurredAtDesc {
		sortKeys = bson.D{{Key: "occurred_at", Value: -1}, {Key: "_id", Value: -1}}
	} else {
		sortMode = SortRevenueDesc
		sortKeys = bson.D{{Key: "lost_revenue_minor", Value: -1}, {Key: "occurred_at", Value: -1}, {Key: "_id", Value: -1}}
	}

	if cursor != "" {
		c, err := decodeCursor(cursor, sortMode)
		if err != nil {
			return nil, "", false, fmt.Errorf("%w: %w", ErrInvalidCursor, err)
		}
		if sortMode == SortOccurredAtDesc {
			q["$or"] = bson.A{
				bson.M{"occurred_at": bson.M{"$lt": c.OccurredAt}},
				bson.M{"occurred_at": c.OccurredAt, "_id": bson.M{"$lt": c.ID}},
			}
		} else {
			q["$or"] = bson.A{
				bson.M{"lost_revenue_minor": bson.M{"$lt": c.LostRevenueMinor}},
				bson.M{"lost_revenue_minor": c.LostRevenueMinor, "occurred_at": bson.M{"$lt": c.OccurredAt}},
				bson.M{"lost_revenue_minor": c.LostRevenueMinor, "occurred_at": c.OccurredAt, "_id": bson.M{"$lt": c.ID}},
			}
		}
	}

	opts := options.Find().SetSort(sortKeys).SetLimit(int64(limit) + 1)
	cur, err := s.entries.Find(ctx, q, opts)
	if err != nil {
		return nil, "", false, fmt.Errorf("list ledger entries: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()

	var entries []Entry
	if err := cur.All(ctx, &entries); err != nil {
		return nil, "", false, fmt.Errorf("decode ledger entries: %w", err)
	}

	hasMore := len(entries) > limit
	if hasMore {
		entries = entries[:limit]
	}
	nextCursor := ""
	if hasMore {
		last := entries[len(entries)-1]
		nextCursor = encodeCursor(listCursor{Sort: sortMode, LostRevenueMinor: last.LostRevenueMinor, OccurredAt: last.OccurredAt, ID: last.EntryID})
	}
	return entries, nextCursor, hasMore, nil
}

func (s *mongoStore) UpdateEntryStatus(ctx context.Context, merchantID, entryID string, next Status, at time.Time) (*Entry, error) {
	current, err := s.GetEntry(ctx, merchantID, entryID)
	if err != nil {
		return nil, err
	}
	if !CanTransition(current.Status, next) {
		return nil, ErrInvalidTransition
	}
	res := s.entries.FindOneAndUpdate(ctx,
		bson.M{"_id": entryID, "merchant_id": merchantID, "status": current.Status},
		bson.M{"$set": bson.M{"status": next, "status_changed_at": at}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	)
	var updated Entry
	if err := res.Decode(&updated); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			// Lost a race with another status change between the read
			// above and this write — report it as an invalid transition
			// rather than a bare not-found, since the entry does exist.
			return nil, ErrInvalidTransition
		}
		return nil, fmt.Errorf("update ledger entry status %s: %w", entryID, err)
	}
	return &updated, nil
}

func (s *mongoStore) GetRollup(ctx context.Context, merchantID, month, currency string) (*Rollup, error) {
	var r Rollup
	err := s.rollups.FindOne(ctx, bson.M{"_id": rollupID(merchantID, month, currency)}).Decode(&r)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get ledger rollup %s/%s/%s: %w", merchantID, month, currency, err)
	}
	return &r, nil
}

func (s *mongoStore) ListRollups(ctx context.Context, merchantID, month string) ([]Rollup, error) {
	cur, err := s.rollups.Find(ctx,
		bson.M{"merchant_id": merchantID, "month": month},
		options.Find().SetSort(bson.D{{Key: "total_lost_revenue_minor", Value: -1}}),
	)
	if err != nil {
		return nil, fmt.Errorf("list ledger rollups %s/%s: %w", merchantID, month, err)
	}
	defer func() { _ = cur.Close(ctx) }()

	rollups := []Rollup{}
	if err := cur.All(ctx, &rollups); err != nil {
		return nil, fmt.Errorf("decode ledger rollups %s/%s: %w", merchantID, month, err)
	}
	return rollups, nil
}
