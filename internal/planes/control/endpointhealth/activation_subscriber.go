// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

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
	"github.com/sarvexlab/sarvex/internal/planes/control/monitoring"
)

// onVerifiedProbeTimeout bounds the out-of-band probe this subscriber
// starts. Generous relative to the probe client's own 5s per-request
// timeout — the run issues several requests in sequence — but finite, so a
// store whose endpoints hang can never leak a goroutine.
const onVerifiedProbeTimeout = 2 * time.Minute

// storeRefResolver turns the ids an event carries into the StoreRef a probe
// run needs (the platform decides which connector shape the probe expects).
//
// Deliberately merchant-scoped rather than a bare store lookup: the
// resolver is handed both ids from the event, so the query underneath can
// be tenant-fenced (docs/TENANCY-PATTERNS.md ADR-2) instead of reusing the
// cross-tenant ListAll the cycle scheduler needs.
type storeRefResolver interface {
	ResolveStoreRef(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (monitoring.StoreRef, error)
}

// storeProber runs one store's probes. Narrower than *ProbeScheduler on
// purpose: the subscriber needs nothing else from it, and depending on the
// method alone lets these tests run without Redis or asynq.
type storeProber interface {
	ProbeStore(ctx context.Context, ref monitoring.StoreRef)
}

// ActivationSubscriber probes a store the moment it becomes verified.
//
// verified → active is driven by exactly one thing: a passing
// profile_reachable probe (activation.Subscriber.HandleProbeCompleted —
// "what active adds is that the endpoints are being watched"). The only
// producer of that evidence is ProbeScheduler's cycle, which runs every
// ProbeInterval. A store that verifies just after a cycle therefore waits
// most of an interval to go active — dead time on the last step of
// onboarding, with nothing happening and nothing to show the merchant.
//
// This closes the loop the other way. activation already publishes
// control.store.activation_state_changed on every accepted transition, so
// entering verified is evidence too: evidence that this store is now worth
// probing. No new producer — the same "derive from what already flows"
// pattern activation.Subscriber is itself built on.
type ActivationSubscriber struct {
	prober    storeProber
	resolver  storeRefResolver
	bus       eventbus.EventBus
	log       *slog.Logger
	probeWait sync.WaitGroup

	mu   sync.Mutex
	subs []eventbus.Subscription
}

// NewActivationSubscriber constructs an ActivationSubscriber.
func NewActivationSubscriber(prober storeProber, resolver storeRefResolver, bus eventbus.EventBus, log *slog.Logger) *ActivationSubscriber {
	if log == nil {
		log = slog.Default()
	}
	return &ActivationSubscriber{prober: prober, resolver: resolver, bus: bus, log: log}
}

// Start subscribes to activation state changes.
func (s *ActivationSubscriber) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.bus == nil {
		s.log.Warn("endpointhealth: no event bus wired; a verified store waits for the next probe cycle")
		return nil
	}

	sub, err := s.bus.Subscribe(ctx, ucp.EventStoreActivationStateChanged, s.handle)
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", ucp.EventStoreActivationStateChanged, err)
	}
	s.subs = append(s.subs, sub)
	return nil
}

// Stop unsubscribes and waits for any probe already in flight, so a
// shutdown does not race a run that is midway through publishing its
// results.
func (s *ActivationSubscriber) Stop(ctx context.Context) error {
	s.mu.Lock()
	for _, sub := range s.subs {
		if err := s.bus.Unsubscribe(ctx, sub); err != nil {
			s.log.Error("endpointhealth: activation unsubscribe failed", "topic", sub.Topic(), "error", err)
		}
	}
	s.subs = nil
	s.mu.Unlock()

	s.probeWait.Wait()
	return nil
}

// activationStateChangedPayload is the shape of
// control.store.activation_state_changed, declared here rather than
// imported from the activation package.
//
// That is this repo's convention for reading another control-plane
// service's events — activation.Subscriber decodes *this* package's
// probe_completed the same way, into its own local struct. Only the topic
// name is shared, and it comes from internal/pkg/ucp/event_types.go, which
// sarvex-v1-architecture.md names as the single source of truth for topics. The two
// services stay free to evolve their internals independently.
type activationStateChangedPayload struct {
	StoreID   string `json:"store_id"`
	FromState string `json:"from_state"`
	ToState   string `json:"to_state"`
}

// handle probes the store when, and only when, it has just entered
// verified.
//
// Returns nil on every path a retry could not fix: a malformed payload and
// an unknown store are both permanent, and returning an error would only
// have the bus redeliver them forever. A store missed here is not stranded
// either way — the next probe cycle still picks it up, which is exactly
// the behavior this shortens.
func (s *ActivationSubscriber) handle(ctx context.Context, e eventbus.DomainEvent) error {
	var payload activationStateChangedPayload
	if err := json.Unmarshal(e.Payload(), &payload); err != nil {
		s.log.Error("endpointhealth: unmarshal activation state change failed", "error", err)
		return nil
	}
	if payload.ToState != string(domain.UCPStateVerified) {
		return nil
	}

	storeID := payload.StoreID
	if storeID == "" {
		storeID = e.StoreID()
	}
	if storeID == "" {
		return nil
	}

	ref, err := s.resolver.ResolveStoreRef(ctx, domain.MerchantID(e.MerchantID()), domain.StoreID(storeID))
	if err != nil {
		s.log.Error("endpointhealth: resolve store for on-verified probe failed",
			"error", err, "store_id", storeID)
		return nil
	}

	// Detached from the handler: a probe run is several seconds of HTTP,
	// and holding the bus handler open for it would stall the topic. The
	// context is deliberately WithoutCancel — the delivery context ends
	// when handle returns, and inheriting that would cancel every probe
	// the instant it started.
	s.probeWait.Add(1)
	go func() {
		defer s.probeWait.Done()
		probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), onVerifiedProbeTimeout)
		defer cancel()
		s.log.Info("endpointhealth: probing newly verified store", "store_id", ref.StoreID)
		s.prober.ProbeStore(probeCtx, ref)
	}()
	return nil
}
