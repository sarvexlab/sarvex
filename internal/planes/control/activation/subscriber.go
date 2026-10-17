// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package activation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// Subscriber advances activation state from evidence already flowing on the
// bus.
//
// It deliberately introduces no new producers. Every signal below is
// derived from something the system already publishes when the underlying
// fact becomes true:
//
//   - a connector paired or unlinked (control.journey.signal, from the
//     connector service)
//   - a store's catalog scored past the readiness threshold, which is the
//     point its profile is served with keys provisioned and capabilities
//     declared (same topic, from the sync plane)
//   - a health probe reporting the store's own profile endpoint reachable,
//     which is what "live and monitored" means (ops.health.probe_completed,
//     from the endpoint-health probe worker)
//
// Reusing existing evidence is what keeps this a refactor: no plane emits
// anything new, and the facts driving activation are the same facts that
// drove the journey — read for what they say about the store rather than
// about its progress toward Google.
type Subscriber struct {
	svc  Service
	bus  eventbus.EventBus
	log  *slog.Logger
	mu   sync.Mutex
	subs []eventbus.Subscription
}

// NewSubscriber constructs an activation subscriber.
func NewSubscriber(svc Service, bus eventbus.EventBus, log *slog.Logger) *Subscriber {
	if log == nil {
		log = slog.Default()
	}
	return &Subscriber{svc: svc, bus: bus, log: log}
}

// journeySignalToActivation maps the journey signals that also say
// something about the store's own endpoints onto activation signals.
//
// Three of the journey's seven signals are missing here, deliberately:
// checklist_complete, sandbox_validated and production_live are Google's
// verdicts, and say nothing about whether the store's endpoints work. That
// omission is the refactor, expressed as a table.
var journeySignalToActivation = map[string]Signal{
	"connector_created":      SignalConnectorLinked,
	"connector_disconnected": SignalConnectorUnlinked,
	// The readiness threshold is met once the catalog is imported and
	// scored, which is the point the store's profile is served with keys
	// provisioned and capabilities declared.
	"readiness_threshold_met": SignalProfilePublished,
	// Losing the threshold means the catalog regressed below what the
	// profile promises, so the profile is withdrawn until it recovers.
	"readiness_threshold_lost": SignalProfileWithdrawn,
}

type journeySignalPayload struct {
	Signal string `json:"signal"`
}

// HandleJourneySignal maps a journey signal onto an activation transition.
func (s *Subscriber) HandleJourneySignal(ctx context.Context, e eventbus.DomainEvent) error {
	var payload journeySignalPayload
	if err := json.Unmarshal(e.Payload(), &payload); err != nil {
		s.log.Error("activation: unmarshal journey signal failed", "error", err)
		return nil
	}

	sig, ok := journeySignalToActivation[payload.Signal]
	if !ok {
		return nil // says nothing about the store's own endpoints
	}

	s.apply(ctx, domain.MerchantID(e.MerchantID()), domain.StoreID(e.StoreID()), sig)
	return nil
}

type probeCompletedPayload struct {
	StoreID    string `json:"store_id"`
	MerchantID string `json:"merchant_id"`
	ProbeType  string `json:"probe_type"`
	Outcome    string `json:"outcome"`
}

// HandleProbeCompleted advances a verified store to active on the first
// passing profile probe.
//
// A verified store's endpoints are known good; what `active` adds is that
// they are being watched. A probe result is exactly that evidence — it can
// only exist because monitoring is running — so the first passing one is
// what promotes the store. Applying it repeatedly costs nothing: the
// transition is only legal from `verified`, and every later probe is a
// no-op.
func (s *Subscriber) HandleProbeCompleted(ctx context.Context, e eventbus.DomainEvent) error {
	var payload probeCompletedPayload
	if err := json.Unmarshal(e.Payload(), &payload); err != nil {
		s.log.Error("activation: unmarshal probe result failed", "error", err)
		return nil
	}
	if payload.ProbeType != "profile_reachable" || payload.Outcome != "pass" {
		return nil
	}

	merchantID := domain.MerchantID(payload.MerchantID)
	if merchantID == "" {
		merchantID = domain.MerchantID(e.MerchantID())
	}
	storeID := domain.StoreID(payload.StoreID)
	if storeID == "" {
		storeID = domain.StoreID(e.StoreID())
	}

	s.apply(ctx, merchantID, storeID, SignalActivated)
	return nil
}

// apply runs one transition, treating an illegal one as the ordinary case
// it is. These signals are derived from repeating evidence — every
// heartbeat, every probe cycle — so the same signal arrives many times and
// only the first can be legal.
func (s *Subscriber) apply(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, sig Signal) {
	if storeID == "" {
		return
	}
	err := s.svc.HandleSignal(ctx, merchantID, storeID, sig)
	if err == nil {
		return
	}

	var invalid ErrInvalidTransition
	if errors.As(err, &invalid) || errors.Is(err, ErrNotFound) {
		s.log.Debug("activation: signal not applicable",
			"merchant_id", merchantID, "store_id", storeID, "signal", sig)
		return
	}
	s.log.Error("activation: failed to apply signal",
		"error", err, "merchant_id", merchantID, "store_id", storeID, "signal", sig)
}

// Start subscribes to the evidence topics.
func (s *Subscriber) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.bus == nil {
		s.log.Warn("activation: no event bus wired; state advances only via the API")
		return nil
	}

	subscriptions := []struct {
		topic   string
		handler eventbus.EventHandler
	}{
		{ucp.EventJourneySignal, s.HandleJourneySignal},
		{ucp.EventHealthProbeCompleted, s.HandleProbeCompleted},
	}

	for _, sub := range subscriptions {
		registered, err := s.bus.Subscribe(ctx, sub.topic, sub.handler)
		if err != nil {
			return fmt.Errorf("subscribe %s: %w", sub.topic, err)
		}
		s.subs = append(s.subs, registered)
	}
	return nil
}

// Stop unsubscribes.
func (s *Subscriber) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, sub := range s.subs {
		if err := s.bus.Unsubscribe(ctx, sub); err != nil {
			s.log.Error("activation: unsubscribe failed", "topic", sub.Topic(), "error", err)
		}
	}
	s.subs = nil
	return nil
}
