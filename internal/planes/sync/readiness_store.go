// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongoopts "go.mongodb.org/mongo-driver/v2/mongo/options"
)

// catalogReadinessCollection is the store-level readiness aggregate's
// collection name. Shared wire identity with
// control/catalog.mongoReadinessRepo ("sync_catalog_readiness"), which reads
// this collection read-only (architecture §4.3 "one writer per store").
const catalogReadinessCollection = "sync_catalog_readiness"

// storeReadinessListPageSize bounds each ListByStore page while computing
// the store-level aggregate.
const storeReadinessListPageSize = 500

// ErrStoreReadinessNotFound is returned when a store has never been scored
// — a legitimately absent record, not an error.
var ErrStoreReadinessNotFound = errors.New("sync: store readiness not found")

// StoreReadinessRecord is the catalog store's per-store readiness aggregate
// document: field-for-field mirror of control/catalog.ReadinessModel (this
// is that read model's write side — the score stage is its single writer),
// plus BlockingSKUCount, the "blocking SKUs surfaced separately" figure the
// control-plane read model doesn't carry yet.
type StoreReadinessRecord struct {
	ID               string    `bson:"_id"` // "<merchant_id>:<store_id>"
	MerchantID       string    `bson:"merchant_id"`
	StoreID          string    `bson:"store_id"`
	Overall          int       `bson:"overall"`
	Completeness     int       `bson:"completeness"`
	Consistency      int       `bson:"consistency"`
	AttrQuality      int       `bson:"attribute_quality"`
	DeliveryData     int       `bson:"delivery_data"`
	ProductCount     int       `bson:"product_count"`
	BlockingSKUCount int       `bson:"blocking_sku_count"`
	LastAuditAt      time.Time `bson:"last_audit_at"`
	UpdatedAt        time.Time `bson:"updated_at"`
}

// ReadinessAggregateStore is the write seam for the store-level readiness
// aggregate. One writer: the score stage (ScoreAndPersist).
type ReadinessAggregateStore interface {
	// GetStoreReadiness returns ErrStoreReadinessNotFound if the store has
	// never been scored.
	GetStoreReadiness(ctx context.Context, storeID string) (*StoreReadinessRecord, error)
	// UpsertStoreReadiness atomically stores or replaces the aggregate.
	UpsertStoreReadiness(ctx context.Context, rec *StoreReadinessRecord) error
}

// MongoReadinessAggregateStore is a MongoDB-backed ReadinessAggregateStore.
type MongoReadinessAggregateStore struct {
	db *mongo.Database
}

// NewMongoReadinessAggregateStore creates a MongoDB-backed store.
func NewMongoReadinessAggregateStore(db *mongo.Database) *MongoReadinessAggregateStore {
	return &MongoReadinessAggregateStore{db: db}
}

// GetStoreReadiness retrieves the aggregate for storeID.
func (s *MongoReadinessAggregateStore) GetStoreReadiness(ctx context.Context, storeID string) (*StoreReadinessRecord, error) {
	coll := s.db.Collection(catalogReadinessCollection)

	var rec StoreReadinessRecord
	err := coll.FindOne(ctx, bson.M{"store_id": storeID}).Decode(&rec)
	if err == mongo.ErrNoDocuments {
		return nil, ErrStoreReadinessNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get store readiness: %w", err)
	}
	return &rec, nil
}

// UpsertStoreReadiness stores or replaces the aggregate document.
func (s *MongoReadinessAggregateStore) UpsertStoreReadiness(ctx context.Context, rec *StoreReadinessRecord) error {
	coll := s.db.Collection(catalogReadinessCollection)

	opts := mongoopts.UpdateOne().SetUpsert(true)
	_, err := coll.UpdateOne(ctx, bson.M{"_id": rec.ID}, bson.M{"$set": rec}, opts)
	if err != nil {
		return fmt.Errorf("upsert store readiness: %w", err)
	}
	return nil
}

// computeStoreReadinessRecord rebuilds the store-level aggregate by scanning
// every product currently in the catalog store for storeID: store overall
// and each dimension are the mean of the per-SKU scores; blocking SKUs are
// counted (and surfaced) separately rather than folded into the mean.
func computeStoreReadinessRecord(ctx context.Context, store CatalogStore, merchantID, storeID string) (*StoreReadinessRecord, error) {
	var count, blocking, sumOverall, sumCompleteness, sumConsistency, sumAttrQuality, sumDeliveryData int

	cursor := ""
	for {
		products, next, err := store.ListByStore(ctx, storeID, storeReadinessListPageSize, cursor)
		if err != nil {
			return nil, fmt.Errorf("list products for store readiness aggregate: %w", err)
		}
		for _, p := range products {
			count++
			sumOverall += p.ReadinessScore
			sumCompleteness += p.Completeness
			sumConsistency += p.Consistency
			sumAttrQuality += p.AttributeQuality
			sumDeliveryData += p.DeliveryData
			if p.BlocksCheckout {
				blocking++
			}
		}
		if next == nil {
			break
		}
		cursor = *next
	}

	now := time.Now().UTC()
	rec := &StoreReadinessRecord{
		ID:               fmt.Sprintf("%s:%s", merchantID, storeID),
		MerchantID:       merchantID,
		StoreID:          storeID,
		ProductCount:     count,
		BlockingSKUCount: blocking,
		LastAuditAt:      now,
		UpdatedAt:        now,
	}
	if count > 0 {
		rec.Overall = sumOverall / count
		rec.Completeness = sumCompleteness / count
		rec.Consistency = sumConsistency / count
		rec.AttrQuality = sumAttrQuality / count
		rec.DeliveryData = sumDeliveryData / count
	}
	return rec, nil
}

// recomputeStoreReadinessAndSignal rebuilds storeID's readiness aggregate,
// persists it, and emits the journey threshold signal exactly when the
// store's zero-blockers state actually crosses (not on every score): first
// reaching zero blocking SKUs emits readiness_threshold_met; regaining a
// blocker after having none emits readiness_threshold_lost. A store that was
// already in the same state (blocked→blocked, clear→clear) emits nothing —
// this is what keeps the signal a one-time crossing event rather than a
// per-score echo.
//
// freshConnection must be true when this is a store's first computation in a
// new connection lifecycle (CatalogImportHandler passes job.Reason ==
// "initial") — never for a steady-state rescore. ConnectorService.Disconnect
// deletes the connector and resets the journey to Prospect, but has no
// reason to touch this store's readiness aggregate too: it's sync-plane-
// owned data, and the normal case (a store reconnecting the same catalog it
// already scored) has no need to recompute from scratch. But that leaves a
// crossing-detection bug on reconnect: prev still holds the old aggregate
// from before the disconnect — most commonly BlockingSKUCount==0, since a
// store index only gets disconnected/reconnected after it was already
// working — so wasZeroBlockers reads true on the very first post-reconnect
// computation even though the journey has genuinely never earned
// readiness_threshold_met in this cycle. isZeroBlockers then matches it, no
// crossing is detected, no signal fires, and the store is stranded on
// Synced permanently — nothing ever recomputes this aggregate again except
// another rescore that can't detect a crossing that already silently
// happened. freshConnection forces wasZeroBlockers to false so a store that
// reconnects already-clean still gets its one, real readiness_threshold_met.
func recomputeStoreReadinessAndSignal(ctx context.Context, deps ScoreDeps, merchantID, storeID string, freshConnection bool) error {
	prev, err := deps.Readiness.GetStoreReadiness(ctx, storeID)
	if err != nil && !errors.Is(err, ErrStoreReadinessNotFound) {
		return fmt.Errorf("get previous store readiness: %w", err)
	}
	wasZeroBlockers := !freshConnection && prev != nil && prev.BlockingSKUCount == 0

	rec, err := computeStoreReadinessRecord(ctx, deps.Store, merchantID, storeID)
	if err != nil {
		return err
	}
	if err := deps.Readiness.UpsertStoreReadiness(ctx, rec); err != nil {
		return fmt.Errorf("persist store readiness: %w", err)
	}

	isZeroBlockers := rec.BlockingSKUCount == 0
	if wasZeroBlockers == isZeroBlockers {
		return nil
	}

	signal := "readiness_threshold_lost"
	if isZeroBlockers {
		signal = "readiness_threshold_met"
	}
	journeyBytes, err := json.Marshal(JourneySignalPayload{Signal: signal})
	if err != nil {
		return fmt.Errorf("marshal journey.signal: %w", err)
	}
	if err := deps.Bus.Publish(ctx, newDomainEvent(ucp.EventJourneySignal, storeID, merchantID, journeyBytes)); err != nil {
		return fmt.Errorf("publish journey.signal: %w", err)
	}
	return nil
}
