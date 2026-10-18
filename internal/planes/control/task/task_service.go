// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/samber/lo"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// ReadinessLookup resolves the typed drill-in for a readiness-sourced task:
// the rule-based fixes available for its product, and — for the issues that
// have no automatic fix — the underlying issue detail (field, problem) so
// the merchant knows what to change by hand. Satisfied by catalog.Service;
// kept narrow here to avoid the task package depending on all of catalog.
type ReadinessLookup interface {
	ListRemediationsForSKU(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, sku string) ([]domain.Remediation, error)
	ListIssuesForSKU(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, sku string) ([]domain.ProductIssue, error)
}

type TaskService struct {
	repo      TaskRepository
	readiness ReadinessLookup
	bus       eventbus.EventBus
	log       *slog.Logger
}

// NewTaskService constructs a Service backed by repo. bus publishes
// control.task.updated after every raise/resolve/complete, which the
// console-realtime hub (internal/planes/control/realtime) consumes — after
// checking the task's store has an active journey — to push live task
// updates to any open console session for the store. bus may be nil (events
// simply won't be published), matching journey.NewJourneyService's convention.
func NewTaskService(repo TaskRepository, readiness ReadinessLookup, bus eventbus.EventBus, log *slog.Logger) Service {
	if log == nil {
		log = slog.Default()
	}
	return &TaskService{
		repo:      repo,
		readiness: readiness,
		bus:       bus,
		log:       log,
	}
}

func (s *TaskService) List(ctx context.Context, id domain.MerchantID, storeID *domain.StoreID, cursor string, limit int, owner *domain.TaskOwner, state *domain.TaskState) (domain.ListOf[domain.Task], error) {
	var after *ListCursor
	if cursor != "" {
		c, err := decodeCursor(cursor)
		if err != nil {
			return domain.ListOf[domain.Task]{}, fmt.Errorf("%w: %w", ErrInvalidCursor, err)
		}
		after = &c
	}

	models, hasMore, err := s.repo.ListByMerchant(ctx, id, storeID, owner, state, after, limit)
	if err != nil {
		return domain.ListOf[domain.Task]{}, err
	}

	items := lo.Map(models, func(m *TaskModel, _ int) domain.Task { return m.ToDomain() })

	var nextCursor *string
	if hasMore && len(models) > 0 {
		last := models[len(models)-1]
		next := ListCursor{
			BlocksCheckout: last.BlocksCheckout,
			CreatedAt:      last.CreatedAt,
			ID:             last.ID,
		}.encode()
		nextCursor = &next
	}

	return domain.ListOf[domain.Task]{
		Data: items,
		Page: domain.Page{Cursor: nextCursor, HasMore: hasMore},
	}, nil
}

func (s *TaskService) ListOpenByMerchants(ctx context.Context, merchantIDs []domain.MerchantID) (map[domain.StoreID]domain.StoreTaskSummary, error) {
	models, err := s.repo.ListOpenByMerchants(ctx, merchantIDs)
	if err != nil {
		return nil, err
	}

	summaries := make(map[domain.StoreID]domain.StoreTaskSummary, len(merchantIDs))
	for _, m := range models {
		storeID := domain.StoreID(m.StoreID)
		summary := summaries[storeID]
		switch domain.TaskOwner(m.Owner) {
		case domain.TaskOwnerMerchant:
			summary.Counts.Merchant++
			summary.IDs.Merchant = append(summary.IDs.Merchant, m.ID)
		case domain.TaskOwnerSarvex:
			summary.Counts.Sarvex++
			summary.IDs.Sarvex = append(summary.IDs.Sarvex, m.ID)
		case domain.TaskOwnerExternal:
			summary.Counts.External++
			summary.IDs.External = append(summary.IDs.External, m.ID)
		}
		summaries[storeID] = summary
	}
	return summaries, nil
}

func (s *TaskService) Get(ctx context.Context, id domain.MerchantID, taskID string) (domain.Task, error) {
	m, err := s.repo.FindByMerchantAndID(ctx, id, taskID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return domain.Task{}, ErrNotFound
		}
		return domain.Task{}, err
	}

	t := m.ToDomain()
	s.attachReadinessDrillIn(ctx, id, &t)
	return t, nil
}

// attachReadinessDrillIn populates the typed drill-in payload for a
// readiness-sourced task (source_ref == sku): the available rule-based
// fixes, and the underlying issue detail for whatever's left with no
// automatic fix. Best-effort per lookup: a failure is logged, not
// propagated — the task is still usable without that half of its drill-in.
//
// Skipped for readiness tasks whose ref isn't a product — see
// domain.Task.HasProductDrillIn.
func (s *TaskService) attachReadinessDrillIn(ctx context.Context, merchantID domain.MerchantID, t *domain.Task) {
	if !t.HasProductDrillIn() || s.readiness == nil {
		return
	}

	rems, err := s.readiness.ListRemediationsForSKU(ctx, merchantID, t.StoreID, *t.SourceRef)
	if err != nil {
		s.log.Error("failed to attach remediations to task", "error", err, "merchant_id", merchantID, "task_id", t.TaskID)
	} else {
		t.Remediations = rems
	}

	issues, err := s.readiness.ListIssuesForSKU(ctx, merchantID, t.StoreID, *t.SourceRef)
	if err != nil {
		s.log.Error("failed to attach issues to task", "error", err, "merchant_id", merchantID, "task_id", t.TaskID)
		return
	}
	t.Issues = issues
}

func (s *TaskService) Resolve(ctx context.Context, id domain.MerchantID, taskID string, req domain.ResolveTaskRequest) (domain.Task, error) {
	m, err := s.repo.FindByMerchantAndID(ctx, id, taskID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return domain.Task{}, ErrNotFound
		}
		return domain.Task{}, err
	}

	if m.State == string(domain.TaskStateDone) {
		return domain.Task{}, ErrNotResolvable
	}

	// external_wait and automatic both mean the task finishes on its own —
	// external_wait when someone outside Sarvex says so (Google's sandbox
	// verdict, its go-live review, the waitlist), automatic when Sarvex's own
	// system observes the work is done (e.g. attr-eligibility: only Google's
	// own feed fetch, not the merchant clicking a button, proves the feed is
	// live). A merchant marking either done would be asserting something
	// they can't actually know, so the merchant-facing path refuses both;
	// the system closes these through Complete once the real signal lands.
	// This holds regardless of owner: an automatic task can still be
	// owner=merchant (attr-eligibility is — the merchant did the underlying
	// work of pasting the feed URL, but doesn't get to self-attest that
	// Google actually picked it up).
	if m.Resolution == string(domain.TaskResolutionExternalWait) || m.Resolution == string(domain.TaskResolutionAutomatic) {
		return domain.Task{}, ErrNotResolvable
	}

	return s.markDone(ctx, taskID, req.Note)
}

// Complete is the reconcilers' close: the work is observably finished, so
// the only failure mode worth reporting is "no such task". Already-done is
// success, not a conflict — reconciliation converges on a state and runs
// repeatedly, so re-closing a closed task is the common case, not an error.
func (s *TaskService) Complete(ctx context.Context, id domain.MerchantID, taskID string, note *string) (domain.Task, error) {
	m, err := s.repo.FindByMerchantAndID(ctx, id, taskID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return domain.Task{}, ErrNotFound
		}
		return domain.Task{}, err
	}

	if m.State == string(domain.TaskStateDone) {
		return m.ToDomain(), nil
	}

	return s.markDone(ctx, taskID, note)
}

// Reopen is the counterpart to Complete for a condition that has come back.
func (s *TaskService) Reopen(ctx context.Context, id domain.MerchantID, taskID string, note *string) (domain.Task, error) {
	m, err := s.repo.FindByMerchantAndID(ctx, id, taskID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return domain.Task{}, ErrNotFound
		}
		return domain.Task{}, err
	}

	if m.State != string(domain.TaskStateDone) {
		return m.ToDomain(), nil
	}

	state := domain.DeriveTaskState(domain.TaskOwner(m.Owner), domain.TaskResolution(m.Resolution))
	updated, err := s.repo.UpdateState(ctx, taskID, string(state), note, time.Now().UTC())
	if err != nil {
		return domain.Task{}, err
	}
	s.publishUpdated(ctx, domain.MerchantID(updated.MerchantID), updated.ID, updated.StoreID, updated.State)
	return updated.ToDomain(), nil
}

func (s *TaskService) markDone(ctx context.Context, taskID string, note *string) (domain.Task, error) {
	updated, err := s.repo.UpdateState(ctx, taskID, string(domain.TaskStateDone), note, time.Now().UTC())
	if err != nil {
		return domain.Task{}, err
	}
	s.publishUpdated(ctx, domain.MerchantID(updated.MerchantID), updated.ID, updated.StoreID, updated.State)
	return updated.ToDomain(), nil
}

// publishUpdated announces a task change so the console-realtime hub can push
// a live update to any open console session. Best-effort: the mutation is
// already persisted, so a failure here is a missed freshness optimization,
// not a lost state change — the console's normal fetch-on-load path is
// unaffected. Mirrors journey.JourneyService.publishUpdated.
func (s *TaskService) publishUpdated(ctx context.Context, merchantID domain.MerchantID, taskID, storeID, state string) {
	if s.bus == nil {
		return
	}

	payload, err := json.Marshal(taskUpdatedPayload{TaskID: taskID, StoreID: storeID, State: state})
	if err != nil {
		s.log.Warn("failed to marshal task.updated payload", "error", err, "task_id", taskID)
		return
	}

	event := newDomainEvent(ucp.EventTaskUpdated, string(merchantID), storeID, payload)
	if err := s.bus.Publish(ctx, event); err != nil {
		s.log.Warn("failed to publish task.updated", "error", err, "task_id", taskID)
	}
}

// Raise creates a new task for a merchant from a domain event (validation, readiness, consistency, etc).
func (s *TaskService) Raise(ctx context.Context, merchantID domain.MerchantID, req RaiseTaskRequest) (domain.Task, error) {
	now := time.Now().UTC()
	m := &TaskModel{
		ID:             req.TaskID,
		MerchantID:     string(merchantID),
		StoreID:        req.StoreID,
		Title:          req.Title,
		WhyItMatters:   req.WhyItMatters,
		Owner:          req.Owner,
		Resolution:     req.Resolution,
		State:          string(domain.DeriveTaskState(domain.TaskOwner(req.Owner), domain.TaskResolution(req.Resolution))),
		BlocksCheckout: req.BlocksCheckout,
		Source:         req.Source,
		SourceRef:      req.SourceRef,
		Finding:        req.Finding,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.repo.Insert(ctx, m); err != nil {
		if errors.Is(err, ErrAlreadyExists) {
			// Lost the race to another raiser of this same deterministic id
			// (task.Ensure, the sync.task.raised subscriber, and the
			// monitoring loop all check-then-insert). The task is on the
			// spine either way, so this is the same outcome as if this call
			// had seen it there first.
			existing, findErr := s.repo.FindByMerchantAndID(ctx, merchantID, req.TaskID)
			if findErr != nil {
				return domain.Task{}, fmt.Errorf("find task raised concurrently %s: %w", req.TaskID, findErr)
			}
			return existing.ToDomain(), nil
		}
		s.log.Error("failed to raise task", "error", err, "merchant_id", merchantID, "task_id", req.TaskID)
		return domain.Task{}, err
	}

	s.log.Debug("task raised", "merchant_id", merchantID, "task_id", req.TaskID, "source", req.Source)
	s.publishUpdated(ctx, merchantID, m.ID, m.StoreID, m.State)
	return m.ToDomain(), nil
}

var _ Service = (*TaskService)(nil)
