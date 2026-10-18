// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// reconciliationDiscrepancyPayload mirrors the wire shape of
// sync.reconciliation.discrepancy_found (sarvex-v1-syncing-plane-async.yaml
// components.messages.reconciliation_discrepancy_found), published by the
// sync plane's reconciliation sweeper (internal/planes/sync/sweep_handler.go).
type reconciliationDiscrepancyPayload struct {
	SKU         string `json:"sku"`
	Field       string `json:"field"`
	StoreValue  string `json:"store_value"`
	SarvexValue string `json:"sarvex_value"`
}

// FindingRepository persists a consistency finding, scoped to the store it
// was detected on, so it becomes readable via GET /stores/:store_id/health
// and .../health/consistency. Deliberately narrow (only Save) — see
// internal/planes/control/operator.ConsistencyRepository for the concrete
// Mongo-backed implementation and the read side; it satisfies this interface
// structurally.
type FindingRepository interface {
	Save(ctx context.Context, storeID string, finding *domain.ConsistencyFinding) error

	// DeleteRecoveredProbeFindings removes this store's probe-raised
	// findings that a completed probe pass no longer reports, identified by
	// the finding ids still failing.
	//
	// Findings were previously write-only: nothing ever removed one, so a
	// single failed pass left a permanent record. A transient blip — the
	// plugin restarting, a deploy — would leave GetHealth counting a
	// critical finding forever, while the probe statement one row above it
	// reported the same endpoint healthy. The recovered *task* was already
	// being closed (TaskServiceRouter.ReconcileRecoveredProbeFindings); this
	// is the other half of that same recovery.
	//
	// Scoped to probe SKUs and safe only with the complete result of a
	// RunProbesForStore pass, for exactly the reasons given on
	// ReconcileRecoveredProbeFindings.
	DeleteRecoveredProbeFindings(ctx context.Context, storeID string, probeSKUs []string, stillFailingIDs []string) error
}

// DiscrepancySubscriber consumes sync.reconciliation.discrepancy_found from
// the event bus, routes it to a task via FindingRouter, and persists it via
// FindingRepository — this is the genuine cross-plane leg for that channel
// (the sync plane's own reaction to a discrepancy, re-scoring the corrected
// product, is a same-process call; see productRescorer/RescoreProduct in
// internal/planes/sync).
type DiscrepancySubscriber struct {
	router FindingRouter
	repo   FindingRepository
	log    *slog.Logger

	mu   sync.Mutex
	subs []eventbus.Subscription
	bus  eventbus.EventBus
}

// NewDiscrepancySubscriber constructs a DiscrepancySubscriber backed by
// router (typically a *TaskServiceRouter) and repo (typically a Mongo-backed
// operator.ConsistencyRepository).
func NewDiscrepancySubscriber(router FindingRouter, repo FindingRepository, bus eventbus.EventBus, log *slog.Logger) *DiscrepancySubscriber {
	if log == nil {
		log = slog.Default()
	}
	return &DiscrepancySubscriber{
		router: router,
		repo:   repo,
		bus:    bus,
		log:    log,
	}
}

// Handle processes one sync.reconciliation.discrepancy_found event. Errors
// are logged but not propagated; a missed discrepancy task is not worth
// blocking bus redelivery over — the next scheduled sweep will find the same
// discrepancy again if it's still unresolved.
func (s *DiscrepancySubscriber) Handle(ctx context.Context, e eventbus.DomainEvent) error {
	merchantID := domain.MerchantID(e.MerchantID())

	var payload reconciliationDiscrepancyPayload
	if err := json.Unmarshal(e.Payload(), &payload); err != nil {
		s.log.Error("failed to unmarshal reconciliation discrepancy", "merchant_id", merchantID, "err", err)
		return nil
	}

	finding := &domain.ConsistencyFinding{
		FindingID: fmt.Sprintf("reconciliation:%s:%s:%s", e.StoreID(), payload.SKU, payload.Field),
		SKU:       payload.SKU,
		Field:     payload.Field,
		Sources: map[string]string{
			"store":  payload.StoreValue,
			"sarvex": payload.SarvexValue,
		},
		Severity:   domain.SeverityWarning,
		DetectedAt: time.Now().UTC(),
	}

	if err := s.repo.Save(ctx, e.StoreID(), finding); err != nil {
		s.log.Error("failed to persist reconciliation discrepancy finding", "merchant_id", merchantID, "store_id", e.StoreID(), "sku", payload.SKU, "field", payload.Field, "err", err)
		// Continue to route the finding to a task even if persistence
		// fails — a missed health/consistency-list entry is not worth
		// losing the task notification over.
	}

	if err := s.router.RouteFindings(ctx, merchantID, domain.StoreID(e.StoreID()), []*domain.ConsistencyFinding{finding}); err != nil {
		s.log.Error("failed to route reconciliation discrepancy to a task", "merchant_id", merchantID, "sku", payload.SKU, "field", payload.Field, "err", err)
		return nil
	}

	s.log.Debug("reconciliation discrepancy routed to task", "merchant_id", merchantID, "sku", payload.SKU, "field", payload.Field)
	return nil
}

func (s *DiscrepancySubscriber) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sub, err := s.bus.Subscribe(ctx, "sync.reconciliation.discrepancy_found", s.Handle)
	if err != nil {
		return fmt.Errorf("subscribe sync.reconciliation.discrepancy_found: %w", err)
	}
	s.subs = append(s.subs, sub)
	return nil
}

func (s *DiscrepancySubscriber) Stop(ctx context.Context) error {
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
