// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	servingpkg "github.com/sarvexlabs/sarvex.cp/internal/pkg/serving"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
)

// ServingCacheReader is the narrow read-only slice of serving cache
// GetPresence/GetManifest need — the profile document and freshness
// watermark. Deliberately not the full serving.ServingCache interface
// (Search/GetProduct are irrelevant here): a control-plane package should
// only depend on what it uses.
type ServingCacheReader interface {
	GetProfile(ctx context.Context, storeID string) (*servingpkg.CacheProfile, error)
	Watermark(ctx context.Context, storeID string) (*servingpkg.CacheWatermark, error)
}

// PresenceRecord is the materialized, per-store view of the serving plane's
// last cache swap — built from serving.snapshot.swapped, the one event the
// cache rebuilder already emits on every successful swap. Control plane has
// no other way to learn product_count (ServingCache itself doesn't expose a
// count) short of this.
type PresenceRecord struct {
	StoreID      string    `bson:"_id"`
	MerchantID   string    `bson:"merchant_id"`
	ProductCount int       `bson:"product_count"`
	BuiltAt      time.Time `bson:"built_at"`
	SourceSync   time.Time `bson:"source_sync"`
}

// ErrPresenceNotFound means no snapshot-swapped event has ever been recorded
// for this store — a brand-new store that hasn't completed its first sync.
var ErrPresenceNotFound = errors.New("operator: presence record not found")

// PresenceRepository is the read/write interface to PresenceRecord storage.
// The PresenceSubscriber is its one writer; OperatorService.GetPresence is a
// reader. Get filters by both storeID and merchantID to enforce tenant isolation.
type PresenceRepository interface {
	Upsert(ctx context.Context, rec *PresenceRecord) error
	Get(ctx context.Context, merchantID, storeID string) (*PresenceRecord, error)
}

// mongoPresenceRepo implements PresenceRepository.
type mongoPresenceRepo struct {
	col *mongo.Collection
}

// NewMongoPresenceRepository creates a PresenceRepository backed by MongoDB.
func NewMongoPresenceRepository(db *mongo.Database) (PresenceRepository, error) {
	r := &mongoPresenceRepo{col: db.Collection("control_presence")}
	if err := r.createIndexes(context.Background()); err != nil {
		return nil, fmt.Errorf("presence indexes: %w", err)
	}
	return r, nil
}

func (r *mongoPresenceRepo) createIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "merchant_id", Value: 1}},
	})
	return err
}

func (r *mongoPresenceRepo) Upsert(ctx context.Context, rec *PresenceRecord) error {
	_, err := r.col.ReplaceOne(ctx, bson.M{"_id": rec.StoreID}, rec, options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("upsert presence record: %w", err)
	}
	return nil
}

func (r *mongoPresenceRepo) Get(ctx context.Context, merchantID, storeID string) (*PresenceRecord, error) {
	var rec PresenceRecord
	err := r.col.FindOne(ctx, bson.M{"_id": storeID, "merchant_id": merchantID}).Decode(&rec)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrPresenceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get presence record: %w", err)
	}
	return &rec, nil
}

// snapshotSwappedPayload mirrors serving.SnapshotSwappedEvent's wire shape
// (internal/planes/serving/events.go) — redeclared here since control and
// serving share only the wire contract, not Go types, across the plane
// boundary.
type snapshotSwappedPayload struct {
	StoreID      string    `json:"store_id"`
	MerchantID   string    `json:"merchant_id"`
	ProductCount int       `json:"product_count"`
	BuiltAt      time.Time `json:"built_at"`
	SourceSync   time.Time `json:"source_sync"`
}

// PresenceSubscriber consumes serving.snapshot.swapped and maintains the
// materialized PresenceRecord GetPresence/GetManifest read from — the same
// shape as monitoring.DiscrepancySubscriber and task.Subscriber for their
// respective cross-plane events.
type PresenceSubscriber struct {
	repo PresenceRepository
	log  *slog.Logger

	mu   sync.Mutex
	subs []eventbus.Subscription
	bus  eventbus.EventBus
}

// NewPresenceSubscriber constructs a serving.snapshot.swapped subscriber.
func NewPresenceSubscriber(repo PresenceRepository, bus eventbus.EventBus, log *slog.Logger) *PresenceSubscriber {
	if log == nil {
		log = slog.Default()
	}
	return &PresenceSubscriber{repo: repo, bus: bus, log: log}
}

// Handle processes one serving.snapshot.swapped event. Errors are logged but
// not propagated; a missed update just means GetPresence/GetManifest serve a
// slightly stale product_count/built_at until the next swap.
func (s *PresenceSubscriber) Handle(ctx context.Context, e eventbus.DomainEvent) error {
	var payload snapshotSwappedPayload
	if err := json.Unmarshal(e.Payload(), &payload); err != nil {
		s.log.Error("failed to unmarshal snapshot.swapped", "store_id", e.StoreID(), "err", err)
		return nil
	}

	rec := &PresenceRecord{
		StoreID:      payload.StoreID,
		MerchantID:   payload.MerchantID,
		ProductCount: payload.ProductCount,
		BuiltAt:      payload.BuiltAt,
		SourceSync:   payload.SourceSync,
	}
	if err := s.repo.Upsert(ctx, rec); err != nil {
		s.log.Error("failed to upsert presence record", "store_id", payload.StoreID, "err", err)
		return nil
	}

	s.log.Debug("presence record updated from snapshot swap", "store_id", payload.StoreID, "product_count", payload.ProductCount)
	return nil
}

func (s *PresenceSubscriber) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sub, err := s.bus.Subscribe(ctx, ucp.EventSnapshotSwapped, s.Handle)
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", ucp.EventSnapshotSwapped, err)
	}
	s.subs = append(s.subs, sub)
	return nil
}
