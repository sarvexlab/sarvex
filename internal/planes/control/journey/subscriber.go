// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package journey

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// JourneySignalPayload is the event payload for journey.signal events.
// (Same struct used by the sync plane's producer in internal/planes/sync/mapping.go)
type JourneySignalPayload struct {
	Signal string `json:"signal"`
}

// activationStateChangedPayload mirrors activation.StateChangedPayload
// field for field, redeclared rather than imported — same convention as
// surface.activationStateChangedPayload (internal/planes/control/surface/subscriber.go).
type activationStateChangedPayload struct {
	StoreID   string `json:"store_id"`
	FromState string `json:"from_state"`
	ToState   string `json:"to_state"`
	Signal    string `json:"signal"`
}

// ConnectorSyncRecorder records that a store's connector completed a full
// catalog sync, for GET /connectors' last_full_sync_at — the only place
// SignalFirstSyncComplete's completion timestamp is durably recorded.
// Optional on Subscriber: if nil, journey state still advances normally,
// the connector record just doesn't get this update.
type ConnectorSyncRecorder interface {
	RecordFullSync(ctx context.Context, storeID domain.StoreID, at time.Time) error
}

// Subscriber consumes journey.signal events from the eventbus and advances state.
type Subscriber struct {
	svc          Service
	connectors   ConnectorSyncRecorder
	log          *slog.Logger
	mu           sync.Mutex
	subs         []eventbus.Subscription
	workerCtx    context.Context
	workerCancel context.CancelFunc
	workerWG     sync.WaitGroup
	bus          eventbus.EventBus
}

// NewSubscriber constructs a journey signal subscriber. connectors may be
// nil (see ConnectorSyncRecorder).
func NewSubscriber(svc Service, connectors ConnectorSyncRecorder, bus eventbus.EventBus, log *slog.Logger) *Subscriber {
	if log == nil {
		log = slog.Default()
	}
	return &Subscriber{
		svc:        svc,
		connectors: connectors,
		log:        log,
		bus:        bus,
	}
}

// Handle processes a journey.signal event.
// Errors are logged but not propagated; signals are best-effort.
// The reconciliation sweeper and re-scoring cycles are the safety net
// for any missed signals.
func (s *Subscriber) Handle(ctx context.Context, e eventbus.DomainEvent) error {
	merchantID := domain.MerchantID(e.MerchantID())
	storeID := domain.StoreID(e.StoreID())

	var payload JourneySignalPayload
	if err := json.Unmarshal(e.Payload(), &payload); err != nil {
		s.log.Error("failed to unmarshal journey signal", "merchant_id", merchantID, "store_id", storeID, "err", err)
		return nil
	}

	sig := Signal(payload.Signal)
	if err := s.svc.HandleSignal(ctx, merchantID, storeID, sig); err != nil {
		s.log.Error("failed to handle journey signal", "merchant_id", merchantID, "store_id", storeID, "signal", sig, "err", err)
		return nil
	}

	// SignalFirstSyncComplete fires on every completed catalog import, not
	// just the store's first — see CatalogImportHandler.emitCompletion.
	if sig == SignalFirstSyncComplete && s.connectors != nil {
		if err := s.connectors.RecordFullSync(ctx, storeID, time.Now().UTC()); err != nil {
			s.log.Error("failed to record full sync on connector", "merchant_id", merchantID, "store_id", storeID, "err", err)
		}
	}

	s.log.Debug("journey signal handled", "merchant_id", merchantID, "store_id", storeID, "signal", sig)
	return nil
}

// HandleActivation processes a control.store.activation_state_changed event,
// advancing the journey to Live the moment activation reports the store's
// own UCP profile active — independent of any surface enrollment. Mirrors
// surface.Subscriber's activation subscription (same event, different
// reaction). Errors are logged, never propagated: best-effort, same as
// every other journey signal.
func (s *Subscriber) HandleActivation(ctx context.Context, e eventbus.DomainEvent) error {
	var payload activationStateChangedPayload
	if err := json.Unmarshal(e.Payload(), &payload); err != nil {
		s.log.Error("failed to unmarshal activation state changed event", "err", err)
		return nil
	}
	if payload.ToState != "active" {
		return nil
	}

	merchantID := domain.MerchantID(e.MerchantID())
	storeID := domain.StoreID(payload.StoreID)
	if storeID == "" {
		storeID = domain.StoreID(e.StoreID())
	}

	if err := s.svc.HandleSignal(ctx, merchantID, storeID, SignalStoreActivated); err != nil {
		s.log.Error("failed to advance journey on activation", "merchant_id", merchantID, "store_id", storeID, "err", err)
	}
	return nil
}

func (s *Subscriber) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.log.Info("starting serving plane service workers")

	s.workerCtx, s.workerCancel = context.WithCancel(context.Background())

	// Register subscriptions
	if err := s.registerSubscriptions(); err != nil {
		s.workerCancel()
		return fmt.Errorf("register serving subscriptions: %w", err)
	}

	s.log.Info("serving plane service workers started successfully")
	return nil
}

func (s *Subscriber) registerSubscriptions() error {
	subscriptions := []struct {
		topic   string
		handler eventbus.EventHandler
	}{
		// Sync plane signals product readiness
		{topic: ucp.EventJourneySignal, handler: s.Handle},
		// Activation signals the store's own UCP profile going active.
		{topic: ucp.EventStoreActivationStateChanged, handler: s.HandleActivation},
	}

	ctx := context.Background()
	for _, subDef := range subscriptions {
		sub, err := s.bus.Subscribe(ctx, subDef.topic, subDef.handler)
		if err != nil {
			return fmt.Errorf("subscribe %s: %w", subDef.topic, err)
		}
		s.subs = append(s.subs, sub)
	}

	return nil
}

func (s *Subscriber) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.log.Info("stopping serving plane service workers")

	// 1. Cancel background workers context
	if s.workerCancel != nil {
		s.workerCancel()
	}

	// 2. Unsubscribe all event streams
	for _, sub := range s.subs {
		s.log.Debug("unsubscribing topic", "topic", sub.Topic())
		if err := s.bus.Unsubscribe(ctx, sub); err != nil {
			s.log.Error("failed to unsubscribe from topic", "topic", sub.Topic(), "error", err)
		}
	}
	s.subs = nil

	// 3. Wait for background goroutines to drain
	done := make(chan struct{})
	go func() {
		s.workerWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.log.Info("serving plane service workers stopped cleanly")
		return nil
	case <-ctx.Done():
		s.log.Warn("serving plane service shutdown timed out")
		return ctx.Err()
	}
}
