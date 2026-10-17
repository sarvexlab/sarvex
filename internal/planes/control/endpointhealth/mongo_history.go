// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/samber/lo"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
)

// historyRetention bounds how long history entries are kept — matches the
// 30-day cap GET /stores/{id}/health/history enforces on query ranges.
const historyRetention = 30 * 24 * time.Hour

// HistoryEntry is one row in a store's endpoint-health timeline: either a
// probe outcome or a status-check transition.
type HistoryEntry struct {
	StoreID   string
	Kind      string // "probe_completed" | "threshold_breached" | "recovered"
	ProbeType ProbeType
	CheckType CheckType
	Outcome   Outcome
	Severity  Severity
	Detail    string
	At        time.Time
}

// HistoryRepository persists and queries the endpoint-health timeline.
type HistoryRepository interface {
	Insert(ctx context.Context, e HistoryEntry) error
	List(ctx context.Context, storeID string, from, to time.Time) ([]HistoryEntry, error)
}

type historyModel struct {
	StoreID   string    `bson:"store_id"`
	Kind      string    `bson:"kind"`
	ProbeType string    `bson:"probe_type,omitempty"`
	CheckType string    `bson:"check_type,omitempty"`
	Outcome   string    `bson:"outcome,omitempty"`
	Severity  string    `bson:"severity,omitempty"`
	Detail    string    `bson:"detail,omitempty"`
	At        time.Time `bson:"at"`
}

type mongoHistoryRepo struct {
	col *mongo.Collection
}

// NewMongoHistoryRepository creates a HistoryRepository backed by MongoDB,
// with a TTL index expiring entries after historyRetention.
func NewMongoHistoryRepository(db *mongo.Database) (HistoryRepository, error) {
	r := &mongoHistoryRepo{col: db.Collection("control_endpoint_health_history")}
	ctx := context.Background()
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "store_id", Value: 1}, {Key: "at", Value: -1}}},
		{Keys: bson.D{{Key: "at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(int32(historyRetention.Seconds()))},
	})
	if err != nil {
		return nil, fmt.Errorf("endpoint health history indexes: %w", err)
	}
	return r, nil
}

func (r *mongoHistoryRepo) Insert(ctx context.Context, e HistoryEntry) error {
	m := historyModel{
		StoreID:   e.StoreID,
		Kind:      e.Kind,
		ProbeType: string(e.ProbeType),
		CheckType: string(e.CheckType),
		Outcome:   string(e.Outcome),
		Severity:  string(e.Severity),
		Detail:    e.Detail,
		At:        e.At,
	}
	if _, err := r.col.InsertOne(ctx, m); err != nil {
		return fmt.Errorf("insert history entry: %w", err)
	}
	return nil
}

func (r *mongoHistoryRepo) List(ctx context.Context, storeID string, from, to time.Time) ([]HistoryEntry, error) {
	q := bson.M{"store_id": storeID, "at": bson.M{"$gte": from, "$lte": to}}
	c, err := r.col.Find(ctx, q, options.Find().SetSort(bson.D{{Key: "at", Value: -1}}))
	if err != nil {
		return nil, fmt.Errorf("list history for %s: %w", storeID, err)
	}
	defer c.Close(ctx)

	var docs []historyModel
	if err := c.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode history for %s: %w", storeID, err)
	}
	return lo.Map(docs, func(m historyModel, _ int) HistoryEntry {
		return HistoryEntry{
			StoreID:   m.StoreID,
			Kind:      m.Kind,
			ProbeType: ProbeType(m.ProbeType),
			CheckType: CheckType(m.CheckType),
			Outcome:   Outcome(m.Outcome),
			Severity:  Severity(m.Severity),
			Detail:    m.Detail,
			At:        m.At,
		}
	}), nil
}

// HistorySubscriber persists ops.health.* events to HistoryRepository. Kept
// as a separate subscriber (rather than writing inline from
// ProbeScheduler/StatusEngine) so a slow/unavailable history write can
// never block a probe cycle or an alert transition — matches
// monitoring.persistFindings's "log, don't fail the caller" convention.
type HistorySubscriber struct {
	repo HistoryRepository
	bus  eventbus.EventBus
	log  *slog.Logger

	mu   sync.Mutex
	subs []eventbus.Subscription
}

// NewHistorySubscriber constructs a HistorySubscriber.
func NewHistorySubscriber(repo HistoryRepository, bus eventbus.EventBus, log *slog.Logger) *HistorySubscriber {
	if log == nil {
		log = slog.Default()
	}
	return &HistorySubscriber{repo: repo, bus: bus, log: log}
}

// Start subscribes to every ops.health.* event.
func (s *HistorySubscriber) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	topics := []string{ucp.EventHealthProbeCompleted, ucp.EventHealthThresholdBreached, ucp.EventHealthRecovered}
	for _, topic := range topics {
		sub, err := s.bus.Subscribe(ctx, topic, s.handle)
		if err != nil {
			return fmt.Errorf("subscribe %s: %w", topic, err)
		}
		s.subs = append(s.subs, sub)
	}
	return nil
}

// Stop unsubscribes from every topic.
func (s *HistorySubscriber) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sub := range s.subs {
		if err := s.bus.Unsubscribe(ctx, sub); err != nil {
			s.log.Error("endpointhealth: history unsubscribe failed", "topic", sub.Topic(), "error", err)
		}
	}
	s.subs = nil
	return nil
}

func (s *HistorySubscriber) handle(ctx context.Context, ev eventbus.DomainEvent) error {
	var entry HistoryEntry
	switch ev.EventType() {
	case ucp.EventHealthProbeCompleted:
		var payload struct {
			StoreID     string      `json:"store_id"`
			ProbeType   ProbeType   `json:"probe_type"`
			Outcome     Outcome     `json:"outcome"`
			Detail      string      `json:"detail"`
			FailureCode FailureCode `json:"failure_code"`
		}
		if err := json.Unmarshal(ev.Payload(), &payload); err != nil {
			s.log.Error("endpointhealth: unmarshal history payload failed", "error", err)
			return nil
		}
		entry = HistoryEntry{StoreID: payload.StoreID, Kind: "probe_completed", ProbeType: payload.ProbeType, Outcome: payload.Outcome, Detail: payload.Detail, At: ev.OccurredAt()}
	case ucp.EventHealthThresholdBreached, ucp.EventHealthRecovered:
		var payload struct {
			StoreID   string    `json:"store_id"`
			CheckType CheckType `json:"check_type"`
			Severity  Severity  `json:"severity"`
		}
		if err := json.Unmarshal(ev.Payload(), &payload); err != nil {
			s.log.Error("endpointhealth: unmarshal history payload failed", "error", err)
			return nil
		}
		kind := "threshold_breached"
		if ev.EventType() == ucp.EventHealthRecovered {
			kind = "recovered"
		}
		entry = HistoryEntry{StoreID: payload.StoreID, Kind: kind, CheckType: payload.CheckType, Severity: payload.Severity, At: ev.OccurredAt()}
	default:
		return nil
	}

	if err := s.repo.Insert(ctx, entry); err != nil {
		s.log.Error("endpointhealth: insert history entry failed", "error", err, "store_id", entry.StoreID)
	}
	return nil
}
