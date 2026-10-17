// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package task

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

// TaskRaisedPayload is the event payload for sync.task.raised, matching
// sarvex-v1-syncing-plane-async.yaml's task_raised message exactly
// ({source, source_ref, issue_id, owner, title, blocks_checkout, resolved}).
// Published by multiple sync-plane sources (mapping/scoring workers, the
// reconciliation sweeper) when they detect something needing a task, or
// when a previously-detected condition has cleared (resolved: true). issue_id
// is only set by readiness sources; see deriveTaskID.
type TaskRaisedPayload struct {
	Source         string `json:"source"`
	SourceRef      string `json:"source_ref"`
	IssueID        string `json:"issue_id,omitempty"`
	Owner          string `json:"owner"`
	Title          string `json:"title"`
	BlocksCheckout bool   `json:"blocks_checkout"`
	Resolved       bool   `json:"resolved,omitempty"`
}

// Subscriber consumes sync.task.raised from the event bus and creates a
// control-plane Task via Service.Raise — the cross-plane leg for readiness
// issues found during catalog scoring (mirrors journey.Subscriber and
// monitoring.DiscrepancySubscriber for their respective events).
type Subscriber struct {
	svc  Service
	log  *slog.Logger
	mu   sync.Mutex
	subs []eventbus.Subscription
	bus  eventbus.EventBus
}

// NewSubscriber constructs a sync.task.raised subscriber.
func NewSubscriber(svc Service, bus eventbus.EventBus, log *slog.Logger) *Subscriber {
	if log == nil {
		log = slog.Default()
	}
	return &Subscriber{
		svc: svc,
		bus: bus,
		log: log,
	}
}

// deriveTaskID builds the stable, idempotent task id for a raised event.
// Rescoring re-publishes task.raised on every pass a product remains
// blocking, not only when the issue is first found, so the same
// (source, store_id, source_ref) triple recurs constantly — the id is
// derived from that triple so a repeat publish is treated as the existing
// task rather than a new one. issueID (readiness only, see
// TaskRaisedPayload) extends the key so that two distinct blocking issues on
// the same product (same source_ref = sku) don't collide onto one task.
func deriveTaskID(source, storeID, sourceRef, issueID string) string {
	if issueID == "" {
		return fmt.Sprintf("%s:%s:%s", source, storeID, sourceRef)
	}
	return fmt.Sprintf("%s:%s:%s:%s", source, storeID, sourceRef, issueID)
}

// Handle processes one sync.task.raised event. Errors are logged but not
// propagated; a missed task is not worth blocking bus redelivery over.
func (s *Subscriber) Handle(ctx context.Context, e eventbus.DomainEvent) error {
	merchantID := domain.MerchantID(e.MerchantID())
	storeID := e.StoreID()

	var payload TaskRaisedPayload
	if err := json.Unmarshal(e.Payload(), &payload); err != nil {
		s.log.Error("failed to unmarshal task.raised", "merchant_id", merchantID, "store_id", storeID, "err", err)
		return nil
	}

	taskID := deriveTaskID(payload.Source, string(storeID), payload.SourceRef, payload.IssueID)

	if payload.Resolved {
		s.resolveTask(ctx, merchantID, taskID)
		return nil
	}

	if existing, err := s.svc.Get(ctx, merchantID, taskID); err == nil {
		if existing.State == domain.TaskStateDone {
			// The id is the issue's identity, so a done task here means the
			// issue was fixed (or marked done) and has been detected again.
			s.reopenTask(ctx, merchantID, taskID)
			return nil
		}
		s.log.Debug("task already raised, skipping duplicate", "merchant_id", merchantID, "task_id", taskID)
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		s.log.Error("failed to check existing task", "merchant_id", merchantID, "task_id", taskID, "err", err)
		return nil
	}

	sourceRef := payload.SourceRef
	req := RaiseTaskRequest{
		TaskID:         taskID,
		StoreID:        string(storeID),
		Title:          payload.Title,
		WhyItMatters:   payload.Title,
		Owner:          payload.Owner,
		Resolution:     string(domain.TaskResolutionGuided),
		BlocksCheckout: payload.BlocksCheckout,
		Source:         payload.Source,
		SourceRef:      &sourceRef,
	}

	if _, err := s.svc.Raise(ctx, merchantID, req); err != nil {
		s.log.Error("failed to raise task from sync.task.raised", "merchant_id", merchantID, "task_id", taskID, "err", err)
		return nil
	}

	s.log.Info("task raised from sync signal", "merchant_id", merchantID, "task_id", taskID, "owner", payload.Owner)
	return nil
}

// reopenTask reopens a finished taskID because its issue was detected again.
func (s *Subscriber) reopenTask(ctx context.Context, merchantID domain.MerchantID, taskID string) {
	note := "Reopened — issue detected again."
	if _, err := s.svc.Reopen(ctx, merchantID, taskID, &note); err != nil {
		s.log.Error("failed to reopen task from sync.task.raised", "merchant_id", merchantID, "task_id", taskID, "err", err)
		return
	}
	s.log.Info("task reopened from sync signal", "merchant_id", merchantID, "task_id", taskID)
}

// resolveTask closes taskID in response to a resolved:true task.raised
// event. The rescore no longer finding the issue is the system observing
// the work finish, so this goes through Complete rather than Resolve. A
// task that was never raised is an expected outcome (not every resolve
// signal has a matching task) and is logged at Debug rather than treated as
// a failure.
func (s *Subscriber) resolveTask(ctx context.Context, merchantID domain.MerchantID, taskID string) {
	note := "Automatically resolved — issue no longer detected on rescore."
	_, err := s.svc.Complete(ctx, merchantID, taskID, &note)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.log.Debug("resolve signal had no matching task", "merchant_id", merchantID, "task_id", taskID, "err", err)
			return
		}
		s.log.Error("failed to auto-resolve task from sync.task.raised", "merchant_id", merchantID, "task_id", taskID, "err", err)
		return
	}
	s.log.Info("task auto-resolved from sync signal", "merchant_id", merchantID, "task_id", taskID)
}

func (s *Subscriber) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sub, err := s.bus.Subscribe(ctx, ucp.EventSyncTaskRaised, s.Handle)
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", ucp.EventSyncTaskRaised, err)
	}
	s.subs = append(s.subs, sub)
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
