// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package channel

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// mongoStore is a Store backed by three MongoDB collections:
// control_agent_sessions (Session documents), control_channel_daily
// (DailyAggregate documents), and control_channel_telemetry_dedup (a
// short-lived, TTL'd insert-only dedup set for the high-volume telemetry
// stream — see RecordDiscoveryRequest's doc comment for why it isn't
// folded into control_channel_daily the same way session events are).
// Mirrors ledger's mongoStore exactly: client (not just db) is required
// for the multi-document transaction FoldSessionEvent needs.
type mongoStore struct {
	client         *mongo.Client
	sessions       *mongo.Collection
	daily          *mongo.Collection
	telemetryDedup *mongo.Collection
}

// telemetryDedupTTL bounds how long a telemetry event_id is remembered for
// dedup purposes — long enough to cover any realistic at-least-once
// redelivery window, short enough that this collection never grows
// unbounded (unlike Session.ProcessedEventIDs, which is fine to keep
// forever since one checkout session emits only a handful of events).
const telemetryDedupTTL = 48 * time.Hour

// NewMongoStore creates a Store, with the indexes FoldSessionEvent/
// RecordDiscoveryRequest/ListDailyAggregates need.
func NewMongoStore(client *mongo.Client, db *mongo.Database) (Store, error) {
	s := &mongoStore{
		client:         client,
		sessions:       db.Collection("control_agent_sessions"),
		daily:          db.Collection("control_channel_daily"),
		telemetryDedup: db.Collection("control_channel_telemetry_dedup"),
	}
	ctx := context.Background()
	if _, err := s.sessions.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "store_id", Value: 1}, {Key: "date", Value: 1}}},
	}); err != nil {
		return nil, fmt.Errorf("control_agent_sessions indexes: %w", err)
	}
	if _, err := s.daily.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "store_id", Value: 1}, {Key: "date", Value: 1}}},
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "date", Value: 1}}},
	}); err != nil {
		return nil, fmt.Errorf("control_channel_daily indexes: %w", err)
	}
	if _, err := s.telemetryDedup.Indexes().CreateOne(ctx,
		mongo.IndexModel{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)},
	); err != nil {
		return nil, fmt.Errorf("control_channel_telemetry_dedup index: %w", err)
	}
	return s, nil
}

type telemetryDedupDoc struct {
	ID        string    `bson:"_id"`
	ExpiresAt time.Time `bson:"expires_at"`
}

func (s *mongoStore) FoldSessionEvent(ctx context.Context, ev SessionEvent) (bool, error) {
	sess, err := s.client.StartSession()
	if err != nil {
		return false, fmt.Errorf("start session: %w", err)
	}
	defer sess.EndSession(ctx)

	var applied bool
	_, err = sess.WithTransaction(ctx, func(sc context.Context) (any, error) {
		applied = false

		var existing Session
		findErr := s.sessions.FindOne(sc, bson.M{"_id": ev.SessionID}).Decode(&existing)
		notFound := errors.Is(findErr, mongo.ErrNoDocuments)
		if findErr != nil && !notFound {
			return nil, findErr
		}

		var existingPtr *Session
		if !notFound {
			existingPtr = &existing
		}
		updated, delta, duplicate := foldSessionEvent(existingPtr, ev, time.Now().UTC())
		if duplicate {
			// A1: redelivered event — no-op, not an error.
			return nil, nil
		}

		if _, err := s.sessions.ReplaceOne(sc, bson.M{"_id": updated.SessionID}, updated, options.Replace().SetUpsert(true)); err != nil {
			return nil, fmt.Errorf("upsert session: %w", err)
		}

		if !delta.isZero() {
			if err := s.applyDailyDelta(sc, updated.MerchantID, updated.StoreID, updated.Surface, updated.Date, updated.Currency, delta); err != nil {
				return nil, fmt.Errorf("fold daily aggregate: %w", err)
			}
		}

		applied = true
		return nil, nil
	})
	if err != nil {
		return false, fmt.Errorf("fold session event: %w", err)
	}
	return applied, nil
}

func (s *mongoStore) applyDailyDelta(ctx context.Context, merchantID, storeID string, surface Surface, date, currency string, d dailyDelta) error {
	id := DailyAggregateID(storeID, surface, date)
	now := time.Now().UTC()

	setOnInsert := bson.M{
		"_id": id, "merchant_id": merchantID, "store_id": storeID, "surface": surface, "date": date,
	}
	set := bson.M{"updated_at": now}
	if currency != "" {
		set["currency"] = currency
	}
	if _, err := s.daily.UpdateOne(ctx, bson.M{"_id": id},
		bson.M{"$setOnInsert": setOnInsert, "$set": bson.M{"updated_at": now}},
		options.UpdateOne().SetUpsert(true)); err != nil {
		return fmt.Errorf("ensure daily document: %w", err)
	}

	inc := bson.M{}
	addIfNonZero := func(field string, v int64) {
		if v != 0 {
			inc[field] = v
		}
	}
	addIfNonZero("sessions", d.sessions)
	addIfNonZero("checkouts_initiated", d.checkoutsInitiated)
	addIfNonZero("sessions_inferred", d.sessionsInferred)
	addIfNonZero("sessions_reached_cart", d.reachedCart)
	addIfNonZero("sessions_reached_checkout", d.reachedCheckout)
	addIfNonZero("checkouts_completed", d.completed)
	addIfNonZero("revenue_minor", d.revenueMinor)

	update := bson.M{"$set": set}
	if len(inc) > 0 {
		update["$inc"] = inc
	}
	if _, err := s.daily.UpdateOne(ctx, bson.M{"_id": id}, update); err != nil {
		return fmt.Errorf("increment daily document: %w", err)
	}
	return nil
}

// RecordDiscoveryRequest increments DailyAggregate.DiscoveryRequests,
// deduped by eventID via an insert-only TTL'd collection: a duplicate
// insert aborts with a duplicate-key error, which is treated as an
// already-recorded no-op — mirrors ledger's duplicate-event handling,
// just outside the (much heavier, per-request-volume-unsuitable)
// transaction FoldSessionEvent uses.
func (s *mongoStore) RecordDiscoveryRequest(ctx context.Context, eventID, merchantID, storeID string, surface Surface, occurredAt time.Time) error {
	_, err := s.telemetryDedup.InsertOne(ctx, telemetryDedupDoc{ID: eventID, ExpiresAt: time.Now().UTC().Add(telemetryDedupTTL)})
	if mongo.IsDuplicateKeyError(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("dedup discovery request: %w", err)
	}

	date := DateKey(occurredAt)
	if err := s.applyDailyDelta(ctx, merchantID, storeID, surface, date, "", dailyDelta{}); err != nil {
		return err
	}
	id := DailyAggregateID(storeID, surface, date)
	if _, err := s.daily.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$inc": bson.M{"discovery_requests": 1}}); err != nil {
		return fmt.Errorf("increment discovery_requests: %w", err)
	}
	return nil
}

func (s *mongoStore) GetDailyAggregate(ctx context.Context, merchantID, storeID string, surface Surface, date string) (*DailyAggregate, error) {
	var a DailyAggregate
	err := s.daily.FindOne(ctx, bson.M{"_id": DailyAggregateID(storeID, surface, date), "merchant_id": merchantID}).Decode(&a)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get daily aggregate: %w", err)
	}
	return &a, nil
}

func (s *mongoStore) ListDailyAggregates(ctx context.Context, merchantID, storeID string, surface Surface, from, to string) ([]DailyAggregate, error) {
	q := bson.M{"merchant_id": merchantID, "date": bson.M{"$gte": from, "$lte": to}}
	if storeID != "" {
		q["store_id"] = storeID
	}
	if surface != "" {
		q["surface"] = surface
	}
	cur, err := s.daily.Find(ctx, q, options.Find().SetSort(bson.D{{Key: "date", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("list daily aggregates: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()

	aggs := []DailyAggregate{}
	if err := cur.All(ctx, &aggs); err != nil {
		return nil, fmt.Errorf("decode daily aggregates: %w", err)
	}
	return aggs, nil
}

func (s *mongoStore) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	var sess Session
	err := s.sessions.FindOne(ctx, bson.M{"_id": sessionID}).Decode(&sess)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get session %s: %w", sessionID, err)
	}
	return &sess, nil
}
