// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/journey"
)

// journeyUpdatedPayload mirrors the wire shape of control.journey.updated
// (sarvex-v1-syncing-plane-async.yaml components.messages.journey_updated),
// published by JourneyService on every accepted transition.
type journeyUpdatedPayload struct {
	StoreID string `json:"store_id"`
	State   string `json:"state"`
}

// journeyStreamMessage is the frame pushed to WebSocket clients — the
// journey_stream_message schema in sarvex-v1-control-plane.yaml. It is a thin
// signal; clients re-fetch GET /stores/{store_id}/journey (journey.updated)
// or GET /tasks/{id} (task.updated) for the full record.
type journeyStreamMessage struct {
	Type       string `json:"type"`
	StoreID    string `json:"store_id"`
	TaskID     string `json:"task_id,omitempty"`
	State      string `json:"state"`
	OccurredAt string `json:"occurred_at"`
}

// taskUpdatedPayload mirrors the wire shape of control.task.updated
// (sarvex-v1-syncing-plane-async.yaml components.messages.task_updated),
// published by TaskService on raise/resolve/complete.
type taskUpdatedPayload struct {
	TaskID  string `json:"task_id"`
	StoreID string `json:"store_id"`
	State   string `json:"state"`
}

// activationStateChangedPayload mirrors the wire shape of control.store.activation_state_changed,
// published by ActivationService on every state transition.
type activationStateChangedPayload struct {
	StoreID   string `json:"store_id"`
	FromState string `json:"from_state"`
	ToState   string `json:"to_state"`
	Signal    string `json:"signal"`
}

// catalogProductsReadyPayload mirrors the wire shape of
// sync.catalog.products_ready, published by the sync plane once a rebuild has
// a full product snapshot (internal/planes/sync/service.go). Only the store id
// is read here — the frame is a thin signal, and the console re-reads
// readiness and products for itself.
type catalogProductsReadyPayload struct {
	StoreID string `json:"store_id"`
}

// JourneyLookup checks whether a store currently has an active journey
// record — the relevance filter for control.task.updated: a task change is
// only pushed to WebSocket clients for stores that have started onboarding.
// Satisfied by journey.Service; kept narrow so Subscriber doesn't depend on
// journey's full surface.
type JourneyLookup interface {
	Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Journey, error)
}

// Subscriber consumes control.journey.updated and control.task.updated from
// the event bus and fans them out through Hub to any WebSocket client
// watching that store. This is what makes the Hub correct across multiple
// control-plane instances: the transition may have been accepted by a
// different instance than the one holding a given browser's connection.
type Subscriber struct {
	hub      *Hub
	journeys JourneyLookup
	log      *slog.Logger

	mu   sync.Mutex
	subs []eventbus.Subscription
	bus  eventbus.EventBus
}

// NewSubscriber constructs a Subscriber that fans events out through hub.
// journeys backs the control.task.updated relevance check (see
// HandleTaskUpdated) and may be nil in tests that only exercise
// control.journey.updated.
func NewSubscriber(hub *Hub, journeys JourneyLookup, bus eventbus.EventBus, log *slog.Logger) *Subscriber {
	if log == nil {
		log = slog.Default()
	}
	return &Subscriber{hub: hub, journeys: journeys, bus: bus, log: log}
}

// Handle processes one control.journey.updated event. Errors are logged but
// not propagated — a missed push is a missed freshness optimization, not a
// lost state change; the console's normal fetch-on-load path is unaffected.
func (s *Subscriber) Handle(_ context.Context, e eventbus.DomainEvent) error {
	var payload journeyUpdatedPayload
	if err := json.Unmarshal(e.Payload(), &payload); err != nil {
		s.log.Error("failed to unmarshal journey.updated", "store_id", e.StoreID(), "err", err)
		return nil
	}

	msg, err := json.Marshal(journeyStreamMessage{
		Type:       "journey.updated",
		StoreID:    payload.StoreID,
		State:      payload.State,
		OccurredAt: e.OccurredAt().Format("2006-01-02T15:04:05Z07:00"),
	})
	if err != nil {
		s.log.Error("failed to marshal journey stream message", "store_id", payload.StoreID, "err", err)
		return nil
	}

	s.hub.Broadcast(payload.StoreID, msg)
	return nil
}

// HandleTaskUpdated processes one control.task.updated event. It only
// broadcasts if the task's store currently has an active journey record —
// per CLAUDE.md's "task belongs to the store" relevance rule, that's the
// signal a live console session could actually be watching this store's
// Path. A store with no journey yet (never paired, or since disconnected)
// gets no push. Errors are logged but not propagated, same as Handle.
func (s *Subscriber) HandleTaskUpdated(ctx context.Context, e eventbus.DomainEvent) error {
	var payload taskUpdatedPayload
	if err := json.Unmarshal(e.Payload(), &payload); err != nil {
		s.log.Error("failed to unmarshal task.updated", "store_id", e.StoreID(), "err", err)
		return nil
	}

	if s.journeys != nil {
		merchantID := domain.MerchantID(e.MerchantID())
		storeID := domain.StoreID(payload.StoreID)
		if _, err := s.journeys.Get(ctx, merchantID, storeID); err != nil {
			if !errors.Is(err, journey.ErrNotFound) {
				s.log.Error("failed to check journey for task.updated relevance", "store_id", payload.StoreID, "err", err)
			}
			return nil
		}
	}

	msg, err := json.Marshal(journeyStreamMessage{
		Type:       "task.updated",
		StoreID:    payload.StoreID,
		TaskID:     payload.TaskID,
		State:      payload.State,
		OccurredAt: e.OccurredAt().Format("2006-01-02T15:04:05Z07:00"),
	})
	if err != nil {
		s.log.Error("failed to marshal task stream message", "store_id", payload.StoreID, "err", err)
		return nil
	}

	s.hub.Broadcast(payload.StoreID, msg)
	return nil
}

// HandleActivationStateChanged processes one control.store.activation_state_changed
// event and pushes an activation.updated frame to any console session watching
// the store. The Path's stage bar is keyed on activation, not journey (see
// web/console/src/views/PathSurface.vue), so without this frame the bar sits on
// its last-fetched state until the merchant reloads the page. Errors are logged
// but not propagated, same as Handle.
func (s *Subscriber) HandleActivationStateChanged(ctx context.Context, e eventbus.DomainEvent) error {
	var payload activationStateChangedPayload
	if err := json.Unmarshal(e.Payload(), &payload); err != nil {
		s.log.Error("failed to unmarshal activation_state_changed", "store_id", e.StoreID(), "err", err)
		return nil
	}

	msg, err := json.Marshal(journeyStreamMessage{
		Type:       "activation.updated",
		StoreID:    payload.StoreID,
		State:      payload.ToState,
		OccurredAt: e.OccurredAt().Format("2006-01-02T15:04:05Z07:00"),
	})
	if err != nil {
		s.log.Error("failed to marshal activation stream message", "store_id", payload.StoreID, "err", err)
		return nil
	}

	s.hub.Broadcast(payload.StoreID, msg)
	return nil
}

// HandleCatalogProductsReady processes one sync.catalog.products_ready event
// and pushes a catalog.updated frame. Everything the console derives from the
// catalog — the topbar readiness pill, the import task's product count, the
// audit task's score — is loaded once per store and never re-read, so without
// this frame a finished import leaves all three showing their pre-import
// values until the merchant reloads.
//
// The sync plane publishes this after every rebuild (import completed, re-score,
// price/inventory change), which is exactly the set of things that move those
// numbers. Errors are logged but not propagated, same as Handle.
func (s *Subscriber) HandleCatalogProductsReady(_ context.Context, e eventbus.DomainEvent) error {
	var payload catalogProductsReadyPayload
	if err := json.Unmarshal(e.Payload(), &payload); err != nil {
		s.log.Error("failed to unmarshal catalog.products_ready", "store_id", e.StoreID(), "err", err)
		return nil
	}

	storeID := payload.StoreID
	if storeID == "" {
		storeID = e.StoreID()
	}

	msg, err := json.Marshal(journeyStreamMessage{
		Type:       "catalog.updated",
		StoreID:    storeID,
		OccurredAt: e.OccurredAt().Format("2006-01-02T15:04:05Z07:00"),
	})
	if err != nil {
		s.log.Error("failed to marshal catalog stream message", "store_id", storeID, "err", err)
		return nil
	}

	s.hub.Broadcast(storeID, msg)
	return nil
}

// HandleHealthChanged pushes a health.updated frame when anything the Health
// page renders has moved: a synthetic probe finished, or the evaluator opened
// or cleared an alert.
//
// The frame names no resource state — the page reads statements, endpoint
// status, alerts and consistency findings, and the client re-reads all of
// them. Health was the last surface with no push at all: it fetched on mount
// and then sat frozen, so a merchant watching it saw nothing move even as
// probes ran every few minutes underneath.
//
// The store id comes off the event envelope rather than a payload field —
// these three events carry different payload shapes and all set StoreID.
// Errors are logged but not propagated, same as Handle.
func (s *Subscriber) HandleHealthChanged(_ context.Context, e eventbus.DomainEvent) error {
	storeID := e.StoreID()
	if storeID == "" {
		return nil
	}

	msg, err := json.Marshal(journeyStreamMessage{
		Type:       "health.updated",
		StoreID:    storeID,
		OccurredAt: e.OccurredAt().Format("2006-01-02T15:04:05Z07:00"),
	})
	if err != nil {
		s.log.Error("failed to marshal health stream message", "store_id", storeID, "err", err)
		return nil
	}

	s.hub.Broadcast(storeID, msg)
	return nil
}

// HandleOrderCreated pushes an orders.updated frame when a new order lands.
// The Path's "Agent orders this week" tile rolls up GET /orders client-side
// and, like Health before this, only ever fetched once on mount — a
// merchant watching the Path while an agent checked out saw nothing move
// until they reloaded. The frame carries no order id or amount: the tile
// re-reads the whole order list for itself, same "thin event, fat pull"
// convention as every other frame on this stream.
//
// The store id comes off the event envelope rather than the payload, same
// as HandleHealthChanged. Errors are logged but not propagated, same as
// Handle.
func (s *Subscriber) HandleOrderCreated(_ context.Context, e eventbus.DomainEvent) error {
	storeID := e.StoreID()
	if storeID == "" {
		return nil
	}

	msg, err := json.Marshal(journeyStreamMessage{
		Type:       "orders.updated",
		StoreID:    storeID,
		OccurredAt: e.OccurredAt().Format("2006-01-02T15:04:05Z07:00"),
	})
	if err != nil {
		s.log.Error("failed to marshal orders stream message", "store_id", storeID, "err", err)
		return nil
	}

	s.hub.Broadcast(storeID, msg)
	return nil
}

func (s *Subscriber) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sub, err := s.bus.Subscribe(ctx, ucp.EventJourneyUpdated, s.Handle)
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", ucp.EventJourneyUpdated, err)
	}
	s.subs = append(s.subs, sub)

	taskSub, err := s.bus.Subscribe(ctx, ucp.EventTaskUpdated, s.HandleTaskUpdated)
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", ucp.EventTaskUpdated, err)
	}
	s.subs = append(s.subs, taskSub)

	activationSub, err := s.bus.Subscribe(ctx, ucp.EventStoreActivationStateChanged, s.HandleActivationStateChanged)
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", ucp.EventStoreActivationStateChanged, err)
	}
	s.subs = append(s.subs, activationSub)

	catalogSub, err := s.bus.Subscribe(ctx, ucp.EventCatalogProductsReady, s.HandleCatalogProductsReady)
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", ucp.EventCatalogProductsReady, err)
	}
	s.subs = append(s.subs, catalogSub)

	// Everything the Health page shows changes on one of these three.
	for _, topic := range []string{
		ucp.EventHealthProbeCompleted,
		ucp.EventHealthThresholdBreached,
		ucp.EventHealthRecovered,
	} {
		sub, err := s.bus.Subscribe(ctx, topic, s.HandleHealthChanged)
		if err != nil {
			return fmt.Errorf("subscribe %s: %w", topic, err)
		}
		s.subs = append(s.subs, sub)
	}

	orderSub, err := s.bus.Subscribe(ctx, ucp.EventOrderCreated, s.HandleOrderCreated)
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", ucp.EventOrderCreated, err)
	}
	s.subs = append(s.subs, orderSub)

	return nil
}

func (s *Subscriber) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, sub := range s.subs {
		if err := s.bus.Unsubscribe(ctx, sub); err != nil {
			s.log.Error("failed to unsubscribe from topic", "topic", sub.Topic(), "error", err)
		}
	}
	s.subs = nil
	return nil
}
