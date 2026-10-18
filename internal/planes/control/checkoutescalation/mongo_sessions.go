// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package checkoutescalation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ErrNotFound is returned for a checkout_id that doesn't exist for the
// given merchant_id — including a checkout that exists but belongs to a
// different merchant, which must collapse into the identical not-found
// response (R11 tenancy: cross-tenant read = 404, never a distinct
// "forbidden").
var ErrNotFound = errors.New("checkoutescalation: session not found")

// ErrInvalidCursor is returned (wrapped) when List's cursor param isn't
// one this package produced — a client-supplied garbage cursor, not a
// server error, so the API layer maps it to 422 rather than 500.
var ErrInvalidCursor = errors.New("checkoutescalation: invalid cursor")

var terminalStatesBSON = bson.A{string(StateCompleted), string(StateFailed), string(StateAbandoned)}

// ApplyEventParams is the input to SessionRepository.ApplyEvent — one
// lifecycle event's effect on the parent session document.
type ApplyEventParams struct {
	CheckoutID    string
	MerchantID    string
	StoreID       string
	Surface       string // only meaningful on first insert
	AgentPlatform string // only meaningful on first insert
	OccurredAt    time.Time
	NewState      State
	Stage         string
	Cart          *CartSummary  // nil = don't touch cart_summary
	Escalation    *Escalation   // non-nil only when this event itself is the escalation
	Failure       *Failure      // non-nil only when this event is the failure
	Diagnosis     *Diagnosis    // non-nil when this event's outcome is diagnosable immediately (failure)
	Terminal      bool          // true for failed/completed transitions
	Retention     time.Duration // only consulted when Terminal is true
}

// SessionRepository is the projection's Mongo-backed store for the parent
// session document.
type SessionRepository interface {
	// ApplyEvent atomically applies one lifecycle event: increments
	// event_count, advances last_activity_at (monotonically — a late event
	// can never move it backwards), and — subject to the terminal-state
	// guard (R3) — sets state/current_stage. Returns the event's assigned
	// seq (the post-increment event_count), which the caller uses as the
	// timeline entry's seq. Session-creation fields (merchant_id, store_id,
	// started_at, surface, agent_platform) are set only on first insert.
	ApplyEvent(ctx context.Context, p ApplyEventParams) (seq int64, err error)

	// MarkAbandoned is the sweep's write path (sweep.go) — same
	// terminal-state guard as ApplyEvent, applied without a new lifecycle
	// event (this is a time-derived transition, not event-derived).
	MarkAbandoned(ctx context.Context, checkoutID string, at time.Time, diagnosis Diagnosis, retention time.Duration) error

	// SetDiagnosis stores/replaces a session's diagnosis. Used for the
	// stalled-escalation flag (R9, doesn't change state — the session
	// stays "escalated") and, with evidence backfilled once its own event
	// has been assigned a seq, for a failed session's diagnosis.
	SetDiagnosis(ctx context.Context, checkoutID string, diagnosis Diagnosis) error

	// Get fetches one session scoped to merchantID.
	Get(ctx context.Context, merchantID, checkoutID string) (*Session, error)

	// List returns a page of sessions for merchantID matching filter,
	// newest last_activity_at first, plus the opaque cursor for the next
	// page (empty when there is none) and whether more results exist.
	List(ctx context.Context, merchantID string, filter ListFilter, cursor string, limit int) ([]Session, string, bool, error)

	// ListLive returns non-terminal sessions for merchantID with
	// last_activity_at inside [now-window, now], capped at limit+1 (so the
	// caller can detect truncation).
	ListLive(ctx context.Context, merchantID string, window time.Duration, limit int) ([]Session, bool, error)

	// ListStaleActive is the sweep's read path: every non-terminal session
	// (any merchant) whose last_activity_at is at or before cutoff.
	ListStaleActive(ctx context.Context, cutoff time.Time, limit int) ([]Session, error)

	// ListStalledEscalations is the sweep's read path for R9: escalated
	// sessions with continue_url_issued and no returned_at, escalated_at
	// at or before cutoff, that don't already carry the stalled diagnosis.
	ListStalledEscalations(ctx context.Context, cutoff time.Time, limit int) ([]Session, error)
}

// ListFilter is the read API's query filter (R11).
type ListFilter struct {
	State         State
	Surface       string
	StoreID       string
	Cause         string
	EscalatedOnly bool
	From, To      *time.Time
}

type mongoSessionRepo struct {
	col *mongo.Collection
}

// NewMongoSessionRepository creates a SessionRepository backed by MongoDB,
// with the indexes the read API (Part 3) and sweep (sweep.go) need, plus a
// TTL index on expires_at (R4 retention — set once a session goes
// terminal, absent while active, so an active session never expires).
func NewMongoSessionRepository(db *mongo.Database) (SessionRepository, error) {
	r := &mongoSessionRepo{col: db.Collection("control_checkout_sessions")}
	ctx := context.Background()
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "last_activity_at", Value: -1}}},
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "state", Value: 1}, {Key: "last_activity_at", Value: -1}}},
		{Keys: bson.D{{Key: "state", Value: 1}, {Key: "last_activity_at", Value: 1}}},
		{Keys: bson.D{{Key: "state", Value: 1}, {Key: "escalation.escalated_at", Value: 1}}},
		{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)},
	})
	if err != nil {
		return nil, fmt.Errorf("checkout session indexes: %w", err)
	}
	return r, nil
}

func (r *mongoSessionRepo) ApplyEvent(ctx context.Context, p ApplyEventParams) (int64, error) {
	setStage := bson.D{
		{Key: "event_count", Value: bson.D{{Key: "$add", Value: bson.A{bson.D{{Key: "$ifNull", Value: bson.A{"$event_count", 0}}}, 1}}}},
		{Key: "merchant_id", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$merchant_id", p.MerchantID}}}},
		{Key: "store_id", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$store_id", p.StoreID}}}},
		{Key: "started_at", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$started_at", p.OccurredAt}}}},
		{Key: "last_activity_at", Value: bson.D{{Key: "$cond", Value: bson.A{
			bson.D{{Key: "$or", Value: bson.A{
				bson.D{{Key: "$eq", Value: bson.A{"$last_activity_at", nil}}},
				bson.D{{Key: "$gt", Value: bson.A{p.OccurredAt, "$last_activity_at"}}},
			}}},
			p.OccurredAt, "$last_activity_at",
		}}}},
		{Key: "state", Value: bson.D{{Key: "$cond", Value: bson.A{
			bson.D{{Key: "$in", Value: bson.A{"$state", terminalStatesBSON}}},
			bson.D{{Key: "$ifNull", Value: bson.A{"$state", string(p.NewState)}}},
			string(p.NewState),
		}}}},
		{Key: "current_stage", Value: bson.D{{Key: "$cond", Value: bson.A{
			bson.D{{Key: "$in", Value: bson.A{"$state", terminalStatesBSON}}},
			bson.D{{Key: "$ifNull", Value: bson.A{"$current_stage", p.Stage}}},
			p.Stage,
		}}}},
		{Key: "updated_at", Value: time.Now().UTC()},
	}
	if p.Surface != "" {
		setStage = append(setStage, bson.E{Key: "surface", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$surface", p.Surface}}}})
	}
	if p.AgentPlatform != "" {
		setStage = append(setStage, bson.E{Key: "agent_platform", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$agent_platform", p.AgentPlatform}}}})
	}
	if p.Cart != nil {
		setStage = append(setStage, bson.E{Key: "cart_summary", Value: *p.Cart})
	}
	if p.Failure != nil {
		setStage = append(setStage, bson.E{Key: "failure", Value: *p.Failure})
	}
	if p.Diagnosis != nil {
		setStage = append(setStage, bson.E{Key: "diagnosis", Value: *p.Diagnosis})
	}
	if p.Terminal {
		setStage = append(setStage,
			bson.E{Key: "terminal_at", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$terminal_at", p.OccurredAt}}}},
			bson.E{Key: "escalation.resolved", Value: bson.D{{Key: "$cond", Value: bson.A{
				bson.D{{Key: "$ne", Value: bson.A{"$escalation", nil}}}, true, "$$REMOVE",
			}}}},
		)
		if p.Retention > 0 {
			setStage = append(setStage, bson.E{Key: "expires_at", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$expires_at", p.OccurredAt.Add(p.Retention)}}}})
		}
	}
	if p.Escalation != nil {
		setStage = append(setStage, bson.E{Key: "escalation", Value: *p.Escalation})
	} else if !p.Terminal {
		// Any non-escalation, non-terminal event observed after an
		// escalation marks the session as "returned" — nothing in the
		// checkout flow signals "the buyer clicked continue_url"
		// specifically, so any further activity is the proxy (see
		// package doc / diagnosis.go).
		setStage = append(setStage, bson.E{Key: "escalation.returned_at", Value: bson.D{{Key: "$cond", Value: bson.A{
			bson.D{{Key: "$and", Value: bson.A{
				bson.D{{Key: "$ne", Value: bson.A{"$escalation", nil}}},
				bson.D{{Key: "$eq", Value: bson.A{"$escalation.returned_at", nil}}},
			}}},
			p.OccurredAt,
			bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$ne", Value: bson.A{"$escalation", nil}}}, "$escalation.returned_at", "$$REMOVE"}}},
		}}}})
	}

	pipeline := mongo.Pipeline{bson.D{{Key: "$set", Value: setStage}}}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	var updated Session
	err := r.col.FindOneAndUpdate(ctx, bson.M{"_id": p.CheckoutID}, pipeline, opts).Decode(&updated)
	if err != nil {
		return 0, fmt.Errorf("apply event to session %s: %w", p.CheckoutID, err)
	}
	return updated.EventCount, nil
}

func (r *mongoSessionRepo) MarkAbandoned(ctx context.Context, checkoutID string, at time.Time, diagnosis Diagnosis, retention time.Duration) error {
	pipeline := mongo.Pipeline{bson.D{{Key: "$set", Value: bson.D{
		{Key: "state", Value: bson.D{{Key: "$cond", Value: bson.A{
			bson.D{{Key: "$in", Value: bson.A{"$state", terminalStatesBSON}}}, "$state", string(StateAbandoned),
		}}}},
		{Key: "terminal_at", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$terminal_at", at}}}},
		{Key: "expires_at", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$expires_at", at.Add(retention)}}}},
		{Key: "diagnosis", Value: diagnosis},
		{Key: "escalation.resolved", Value: bson.D{{Key: "$cond", Value: bson.A{
			bson.D{{Key: "$ne", Value: bson.A{"$escalation", nil}}}, true, "$$REMOVE",
		}}}},
		{Key: "updated_at", Value: time.Now().UTC()},
	}}}}
	res, err := r.col.UpdateOne(ctx, bson.M{"_id": checkoutID}, pipeline)
	if err != nil {
		return fmt.Errorf("mark session %s abandoned: %w", checkoutID, err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *mongoSessionRepo) SetDiagnosis(ctx context.Context, checkoutID string, diagnosis Diagnosis) error {
	res, err := r.col.UpdateOne(ctx, bson.M{"_id": checkoutID}, bson.M{
		"$set": bson.M{"diagnosis": diagnosis, "updated_at": time.Now().UTC()},
	})
	if err != nil {
		return fmt.Errorf("set stalled-escalation diagnosis for %s: %w", checkoutID, err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *mongoSessionRepo) Get(ctx context.Context, merchantID, checkoutID string) (*Session, error) {
	var s Session
	err := r.col.FindOne(ctx, bson.M{"_id": checkoutID, "merchant_id": merchantID}).Decode(&s)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get session %s: %w", checkoutID, err)
	}
	return &s, nil
}

func (r *mongoSessionRepo) List(ctx context.Context, merchantID string, filter ListFilter, cursor string, limit int) ([]Session, string, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	q := bson.M{"merchant_id": merchantID}
	if filter.State != "" {
		q["state"] = string(filter.State)
	}
	if filter.Surface != "" {
		q["surface"] = filter.Surface
	}
	if filter.StoreID != "" {
		q["store_id"] = filter.StoreID
	}
	if filter.Cause != "" {
		q["diagnosis.cause"] = filter.Cause
	}
	if filter.EscalatedOnly {
		q["escalation"] = bson.M{"$ne": nil}
	}
	if filter.From != nil || filter.To != nil {
		rng := bson.M{}
		if filter.From != nil {
			rng["$gte"] = *filter.From
		}
		if filter.To != nil {
			rng["$lte"] = *filter.To
		}
		q["last_activity_at"] = rng
	}
	if cursor != "" {
		c, err := decodeCursor(cursor)
		if err != nil {
			return nil, "", false, fmt.Errorf("%w: %w", ErrInvalidCursor, err)
		}
		// Keyset pagination on (last_activity_at desc, _id desc).
		q["$or"] = bson.A{
			bson.M{"last_activity_at": bson.M{"$lt": c.LastActivityAt}},
			bson.M{"last_activity_at": c.LastActivityAt, "_id": bson.M{"$lt": c.ID}},
		}
	}

	opts := options.Find().SetSort(bson.D{{Key: "last_activity_at", Value: -1}, {Key: "_id", Value: -1}}).SetLimit(int64(limit) + 1)
	cur, err := r.col.Find(ctx, q, opts)
	if err != nil {
		return nil, "", false, fmt.Errorf("list sessions: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()

	var sessions []Session
	if err := cur.All(ctx, &sessions); err != nil {
		return nil, "", false, fmt.Errorf("decode sessions: %w", err)
	}

	hasMore := len(sessions) > limit
	if hasMore {
		sessions = sessions[:limit]
	}
	nextCursor := ""
	if hasMore {
		last := sessions[len(sessions)-1]
		nextCursor = encodeCursor(listCursor{LastActivityAt: last.LastActivityAt, ID: last.CheckoutID})
	}
	return sessions, nextCursor, hasMore, nil
}

func (r *mongoSessionRepo) ListLive(ctx context.Context, merchantID string, window time.Duration, limit int) ([]Session, bool, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	q := bson.M{
		"merchant_id":      merchantID,
		"state":            bson.M{"$nin": terminalStatesBSON},
		"last_activity_at": bson.M{"$gte": time.Now().UTC().Add(-window)},
	}
	opts := options.Find().SetSort(bson.D{{Key: "last_activity_at", Value: -1}}).SetLimit(int64(limit) + 1)
	cur, err := r.col.Find(ctx, q, opts)
	if err != nil {
		return nil, false, fmt.Errorf("list live sessions: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()

	var sessions []Session
	if err := cur.All(ctx, &sessions); err != nil {
		return nil, false, fmt.Errorf("decode live sessions: %w", err)
	}
	hasMore := len(sessions) > limit
	if hasMore {
		sessions = sessions[:limit]
	}
	return sessions, hasMore, nil
}

// ListStaleActive returns non-terminal sessions whose last_activity_at is
// at or before cutoff — inclusive ($lte), not strictly before, so a
// session whose age lands exactly on a threshold is deterministically
// included rather than depending on which side of a clock tick the sweep
// happens to run on.
func (r *mongoSessionRepo) ListStaleActive(ctx context.Context, cutoff time.Time, limit int) ([]Session, error) {
	q := bson.M{"state": bson.M{"$nin": terminalStatesBSON}, "last_activity_at": bson.M{"$lte": cutoff}}
	cur, err := r.col.Find(ctx, q, options.Find().SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("list stale active sessions: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	var sessions []Session
	if err := cur.All(ctx, &sessions); err != nil {
		return nil, fmt.Errorf("decode stale active sessions: %w", err)
	}
	return sessions, nil
}

// ListStalledEscalations returns escalated sessions whose escalated_at is
// at or before cutoff — inclusive ($lte), same rationale as ListStaleActive:
// R9/A4 requires the threshold to fire exactly at the boundary, not one
// tick later.
func (r *mongoSessionRepo) ListStalledEscalations(ctx context.Context, cutoff time.Time, limit int) ([]Session, error) {
	q := bson.M{
		"state":                          string(StateEscalated),
		"escalation.continue_url_issued": true,
		"escalation.returned_at":         nil,
		"escalation.escalated_at":        bson.M{"$lte": cutoff},
		"$or": bson.A{
			bson.M{"diagnosis": nil},
			bson.M{"diagnosis.cause": bson.M{"$ne": "escalation_stalled"}},
		},
	}
	cur, err := r.col.Find(ctx, q, options.Find().SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("list stalled escalations: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	var sessions []Session
	if err := cur.All(ctx, &sessions); err != nil {
		return nil, fmt.Errorf("decode stalled escalations: %w", err)
	}
	return sessions, nil
}
