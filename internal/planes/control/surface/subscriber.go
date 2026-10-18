// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package surface

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

// activationStateChangedPayload mirrors activation.StateChangedPayload
// field for field, redeclared rather than imported.
//
// The import would be harmless in one direction — this package already
// depends on activation for the enrollment gate — but declaring the wire
// shape locally keeps the coupling to the one narrow interface
// (ActivationReader) rather than spreading it across event handling too.
// Same convention as the payload structs in
// internal/planes/control/channel/consumer.go.
type activationStateChangedPayload struct {
	StoreID   string `json:"store_id"`
	FromState string `json:"from_state"`
	ToState   string `json:"to_state"`
	Signal    string `json:"signal"`
}

// Subscriber reacts to store activation transitions.
//
// It exists for exactly one behaviour: a store reaching `active`
// auto-enrolls every AutoEnroll surface — direct_agent in V1. That is what
// makes "active with no distribution partner" show up as an explicit card
// rather than an empty page.
//
// Note the direction. Activation publishes; this consumes. Nothing here
// writes activation state, and nothing in the activation package knows this
// subscriber exists.
type Subscriber struct {
	svc  Service
	bus  eventbus.EventBus
	log  *slog.Logger
	mu   sync.Mutex
	subs []eventbus.Subscription
}

// NewSubscriber constructs a subscriber over the activation state-change
// topic.
func NewSubscriber(svc Service, bus eventbus.EventBus, log *slog.Logger) *Subscriber {
	if log == nil {
		log = slog.Default()
	}
	return &Subscriber{svc: svc, bus: bus, log: log}
}

// Handle processes one activation state change. Errors are logged, never
// propagated: a missed auto-enroll is recovered on the next read, since
// enrollment is a converge-to-state pass rather than a one-shot action.
func (s *Subscriber) Handle(ctx context.Context, e eventbus.DomainEvent) error {
	var payload activationStateChangedPayload
	if err := json.Unmarshal(e.Payload(), &payload); err != nil {
		s.log.Error("surface: unmarshal activation state change failed", "error", err)
		return nil
	}

	if domain.UCPState(payload.ToState) != domain.UCPStateActive {
		return nil
	}

	merchantID := domain.MerchantID(e.MerchantID())
	storeID := domain.StoreID(payload.StoreID)
	if storeID == "" {
		storeID = domain.StoreID(e.StoreID())
	}

	if err := s.svc.OnStoreActivated(ctx, merchantID, storeID); err != nil {
		s.log.Error("surface: auto-enroll on activation failed",
			"error", err, "merchant_id", merchantID, "store_id", storeID)
	}
	return nil
}

// artifactUpdatedPayload is sync.surface_artifact.updated's wire shape,
// redeclared locally like activationStateChangedPayload.
type artifactUpdatedPayload struct {
	StoreID   string `json:"store_id"`
	SurfaceID string `json:"surface_id"`
}

// HandleArtifactUpdated re-evaluates a surface's enrollment when the sync
// plane generated or observed something new about its artifact — a feed now
// exists, Google fetched it. Without this a verifying enrollment would only
// notice its verification signal when the merchant next opened the console.
// Generic: it names no surface, and a store that never started on the surface
// has nothing to evaluate.
func (s *Subscriber) HandleArtifactUpdated(ctx context.Context, e eventbus.DomainEvent) error {
	var payload artifactUpdatedPayload
	if err := json.Unmarshal(e.Payload(), &payload); err != nil {
		s.log.Error("surface: unmarshal artifact update failed", "error", err)
		return nil
	}
	storeID := domain.StoreID(payload.StoreID)
	if storeID == "" {
		storeID = domain.StoreID(e.StoreID())
	}
	_, err := s.svc.EvaluatePrerequisites(ctx, domain.MerchantID(e.MerchantID()), storeID, payload.SurfaceID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		s.log.Error("surface: re-evaluate after artifact update failed",
			"error", err, "store_id", storeID, "surface_id", payload.SurfaceID)
	}
	return nil
}

// Start subscribes to control.store.activation_state_changed and
// sync.surface_artifact.updated.
func (s *Subscriber) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.bus == nil {
		s.log.Warn("surface: no event bus wired; auto-enroll will only happen on read")
		return nil
	}

	sub, err := s.bus.Subscribe(ctx, ucp.EventStoreActivationStateChanged, s.Handle)
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", ucp.EventStoreActivationStateChanged, err)
	}
	s.subs = append(s.subs, sub)

	artifactSub, err := s.bus.Subscribe(ctx, ucp.EventSurfaceArtifactUpdated, s.HandleArtifactUpdated)
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", ucp.EventSurfaceArtifactUpdated, err)
	}
	s.subs = append(s.subs, artifactSub)
	return nil
}
