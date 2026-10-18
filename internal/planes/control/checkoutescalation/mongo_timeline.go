// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package checkoutescalation

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TimelineRepository is the projection's Mongo-backed store for each
// session's append-only, redacted event timeline (R5/R7).
type TimelineRepository interface {
	// Insert appends entry, first as the idempotency gate: a duplicate
	// (checkout_id, event_id) — the definition of "this exact lifecycle
	// event has already been processed" (R3) — is a silent no-op, reported
	// back via the inserted bool so the caller knows not to also apply the
	// event to the parent session document. entry.Seq is filled in after a
	// successful insert (see projection.go: the parent doc's atomic
	// event_count increment is the seq source, applied after the insert
	// succeeds, then backfilled here).
	Insert(ctx context.Context, entry TimelineEntry) (inserted bool, err error)

	// SetSeq backfills the seq assigned by the parent session document's
	// atomic increment (projection.go) onto an already-inserted entry.
	SetSeq(ctx context.Context, checkoutID, eventID string, seq int64) error

	// SetExpiry backfills expires_at onto every existing timeline entry for
	// checkoutID once the parent session goes terminal (R4) — the whole
	// timeline must expire atomically with its parent, not on each entry's
	// own write-time TTL (entries are written throughout an active
	// session, long before terminal_at).
	SetExpiry(ctx context.Context, checkoutID string, expiresAt time.Time) error

	// List returns a page of timeline entries for checkoutID ordered by
	// seq ascending, starting after afterSeq (0 = from the beginning).
	List(ctx context.Context, checkoutID string, afterSeq int64, limit int) ([]TimelineEntry, bool, error)

	// ListEvidence returns the entries among checkoutID's timeline whose
	// event_type is in eventTypes — used by diagnosis.go to gather the
	// specific seq values backing a conclusion (R8's evidence[]) without
	// pulling the whole timeline.
	ListEvidence(ctx context.Context, checkoutID string, eventTypes []string) ([]TimelineEntry, error)
}

type mongoTimelineRepo struct {
	col *mongo.Collection
}

// NewMongoTimelineRepository creates a TimelineRepository backed by
// MongoDB. Retention is driven by SetExpiry (called once the parent
// session goes terminal), not a fixed-relative TTL like
// endpointhealth/mongo_history.go's — a plain relative TTL would expire
// early entries before the session even reaches terminal_at, since this
// timeline is written to throughout an active session's life. The TTL
// index still uses Mongo's native SetExpireAfterSeconds(0) variant
// ("expire at the stored timestamp"), just against a value this package
// computes explicitly rather than the index computing it relative to
// insertion time.
func NewMongoTimelineRepository(db *mongo.Database) (TimelineRepository, error) {
	r := &mongoTimelineRepo{col: db.Collection("control_checkout_session_timeline")}
	ctx := context.Background()
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "checkout_id", Value: 1}, {Key: "event_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "checkout_id", Value: 1}, {Key: "seq", Value: 1}}},
		{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)},
	})
	if err != nil {
		return nil, fmt.Errorf("checkout session timeline indexes: %w", err)
	}
	return r, nil
}

func (r *mongoTimelineRepo) Insert(ctx context.Context, entry TimelineEntry) (bool, error) {
	_, err := r.col.InsertOne(ctx, entry)
	if err == nil {
		return true, nil
	}
	if mongo.IsDuplicateKeyError(err) {
		return false, nil
	}
	return false, fmt.Errorf("insert timeline entry for %s: %w", entry.CheckoutID, err)
}

func (r *mongoTimelineRepo) SetSeq(ctx context.Context, checkoutID, eventID string, seq int64) error {
	_, err := r.col.UpdateOne(ctx,
		bson.M{"checkout_id": checkoutID, "event_id": eventID},
		bson.M{"$set": bson.M{"seq": seq}},
	)
	if err != nil {
		return fmt.Errorf("set seq for %s/%s: %w", checkoutID, eventID, err)
	}
	return nil
}

func (r *mongoTimelineRepo) SetExpiry(ctx context.Context, checkoutID string, expiresAt time.Time) error {
	_, err := r.col.UpdateMany(ctx,
		bson.M{"checkout_id": checkoutID},
		bson.M{"$set": bson.M{"expires_at": expiresAt}},
	)
	if err != nil {
		return fmt.Errorf("set timeline expiry for %s: %w", checkoutID, err)
	}
	return nil
}

func (r *mongoTimelineRepo) List(ctx context.Context, checkoutID string, afterSeq int64, limit int) ([]TimelineEntry, bool, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	q := bson.M{"checkout_id": checkoutID, "seq": bson.M{"$gt": afterSeq}}
	opts := options.Find().SetSort(bson.D{{Key: "seq", Value: 1}}).SetLimit(int64(limit) + 1)
	cur, err := r.col.Find(ctx, q, opts)
	if err != nil {
		return nil, false, fmt.Errorf("list timeline for %s: %w", checkoutID, err)
	}
	defer func() { _ = cur.Close(ctx) }()

	var entries []TimelineEntry
	if err := cur.All(ctx, &entries); err != nil {
		return nil, false, fmt.Errorf("decode timeline for %s: %w", checkoutID, err)
	}
	hasMore := len(entries) > limit
	if hasMore {
		entries = entries[:limit]
	}
	return entries, hasMore, nil
}

func (r *mongoTimelineRepo) ListEvidence(ctx context.Context, checkoutID string, eventTypes []string) ([]TimelineEntry, error) {
	q := bson.M{"checkout_id": checkoutID, "event_type": bson.M{"$in": eventTypes}}
	cur, err := r.col.Find(ctx, q, options.Find().SetSort(bson.D{{Key: "seq", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("list evidence for %s: %w", checkoutID, err)
	}
	defer func() { _ = cur.Close(ctx) }()
	var entries []TimelineEntry
	if err := cur.All(ctx, &entries); err != nil {
		return nil, fmt.Errorf("decode evidence for %s: %w", checkoutID, err)
	}
	return entries, nil
}
