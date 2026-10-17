// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package journey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/samber/lo"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/engagement"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
)

// ValidationMilestoneRaiser raises the "run pre-validation" prescribed task
// once a store's readiness threshold is met. Satisfied by
// *validation.Orchestrator.
type ValidationMilestoneRaiser interface {
	EnsureMilestoneTask(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) error
}

// ActivationStateReader lets JourneyService check whether a store's UCP
// profile is already active, to self-heal the Ready -> Live race described
// on WithActivationStateReader. Satisfied by *activation.ActivationService.
type ActivationStateReader interface {
	Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Activation, error)
}

// JourneyService is the production implementation of Service.
type JourneyService struct {
	repo       Repository
	tasks      task.Service
	validation ValidationMilestoneRaiser
	activation ActivationStateReader
	bus        eventbus.EventBus
	log        *slog.Logger
}

// NewJourneyService constructs a Service backed by MongoDB. bus publishes
// control.journey.updated after every accepted transition, which the
// console-realtime hub (internal/planes/control/realtime) consumes to push
// live journey updates to any open console session for the store. Use
// WithValidationMilestoneRaiser to wire validation, since the validation
// orchestrator is constructed after the journey service in entry.go's
// dependency order.
func NewJourneyService(repo Repository, tasks task.Service, bus eventbus.EventBus, log *slog.Logger) *JourneyService {
	if log == nil {
		log = slog.Default()
	}
	return &JourneyService{
		repo:  repo,
		tasks: tasks,
		bus:   bus,
		log:   log,
	}
}

// WithValidationMilestoneRaiser wires validation milestone-task emission.
// Mirrors validation.Orchestrator.WithTaskService — a post-construction
// setter because entry.go builds the validation orchestrator after the
// journey service.
func (s *JourneyService) WithValidationMilestoneRaiser(v ValidationMilestoneRaiser) *JourneyService {
	s.validation = v
	return s
}

// WithActivationStateReader wires the Ready -> Live self-heal check.
// Post-construction setter because entry.go builds ActivationService after
// the journey service, same as WithValidationMilestoneRaiser above.
//
// control.store.activation_state_changed is published at most once per
// accepted activation transition, never on a timer (see
// activation.ActivationService's doc comment). journey/subscriber.go's
// HandleActivation reacts to that single event by firing SignalStoreActivated,
// and transitionTable only matches it from JourneyReady. If activation
// reaches "active" before the journey reaches Ready — readiness/catalog
// import still in flight — HandleSignal's table lookup misses, logs and
// no-ops, and nothing ever re-fires the signal: the journey is stuck at
// Ready forever even though the store is genuinely active.
//
// HandleSignal calls Get here the moment it lands on Ready, so a Ready
// that arrives after activation already went active finishes the Ready ->
// Live transition immediately instead of waiting on an event that already
// happened and will not happen again — the same "derive from what already
// flows" fix endpointhealth's ActivationSubscriber applies one layer down.
func (s *JourneyService) WithActivationStateReader(a ActivationStateReader) *JourneyService {
	s.activation = a
	return s
}

// transitionTable maps [current_state, signal] to next_state.
var transitionTable = map[[2]string]domain.JourneyState{
	{string(domain.JourneyProspect), string(SignalConnectorCreated)}:    domain.JourneyConnected,
	{string(domain.JourneyConnected), string(SignalFirstSyncComplete)}:  domain.JourneySynced,
	{string(domain.JourneySynced), string(SignalReadinessThresholdMet)}: domain.JourneyReady,
	{string(domain.JourneyReady), string(SignalReadinessThresholdLost)}: domain.JourneySynced,
	{string(domain.JourneyReady), string(SignalStoreActivated)}:         domain.JourneyLive,

	// Disconnecting removes the connector entirely (ConnectorService.Disconnect
	// — a deliberate merchant "unlink" action, not a transient heartbeat
	// blip; those are surfaced as connector.status degraded/down instead,
	// which the console treats as a Path task, not a setup redirect — see
	// web/console/src/router/needsConnect.ts). With no connector left, every
	// state from Connected onward regresses straight to Prospect: pairing
	// must happen again from scratch regardless of how far onboarding had
	// progressed.
	{string(domain.JourneyConnected), string(SignalConnectorDisconnected)}: domain.JourneyProspect,
	{string(domain.JourneySynced), string(SignalConnectorDisconnected)}:    domain.JourneyProspect,
	{string(domain.JourneyReady), string(SignalConnectorDisconnected)}:     domain.JourneyProspect,
	{string(domain.JourneyLive), string(SignalConnectorDisconnected)}:      domain.JourneyProspect,
}

func (s *JourneyService) Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Journey, error) {
	m, err := s.repo.Get(ctx, merchantID, storeID)
	if err != nil {
		return domain.Journey{}, err
	}

	j := m.ToDomain()

	// Self-heal: reconcileStageWork otherwise only fires on a transition
	// (HandleSignal), so a store that reached its current state before a
	// given prescription existed would never get that work raised. The pass
	// converges on the state and is an idempotent no-op once caught up, so
	// running it on every read is cheap and keeps the Path self-correcting
	// without a backfill migration. See stage_work.go.
	s.reconcileStageWork(ctx, merchantID, storeID, domain.JourneyState(m.State))

	criteria := engagement.GetStage(j.Stage)
	if criteria != nil {
		j.StageName = criteria.Name
		j.StageDescription = criteria.Description
		j.OperatorActions = criteria.OperatorActions
		j.TypicalDaysInStage = criteria.DaysInStage
	}
	j.StageElapsedDays = int(time.Since(j.EnteredStageAt).Hours() / 24)

	// Compute open task counts and next action from task service.
	// If the task service is not yet implemented, use zero values (graceful degradation).
	taskList, err := s.tasks.List(ctx, merchantID, &storeID, "", 100, nil, nil)
	if err != nil && !errors.Is(err, task.ErrNotImplemented) {
		s.log.Warn("failed to fetch tasks for journey", "merchant_id", merchantID, "store_id", storeID, "err", err)
	} else if err == nil {
		j.OpenTaskCounts = domain.TaskCounts{
			Merchant: countOpenTasksByOwner(taskList.Data, domain.TaskOwnerMerchant),
			Sarvex:   countOpenTasksByOwner(taskList.Data, domain.TaskOwnerSarvex),
			External: countOpenTasksByOwner(taskList.Data, domain.TaskOwnerExternal),
		}
		j.NextAction = nextAction(taskList.Data)
		if criteria != nil && len(criteria.RequiredTasks) > 0 {
			unresolved := criteria.UnresolvedRequiredOwners(taskList.Data)
			for _, owner := range criteria.RequiredTasks {
				j.RequiredTasksStatus = append(j.RequiredTasksStatus, domain.RequiredTaskStatus{
					Owner:    owner,
					Resolved: !slices.Contains(unresolved, owner),
				})
			}
		}
	}

	return j, nil
}

func (s *JourneyService) Initialize(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) error {
	// Check if journey already exists.
	_, err := s.repo.Get(ctx, merchantID, storeID)
	if err == nil {
		// Journey already exists; idempotent no-op.
		s.log.Info("journey already exists", "store_id", storeID)
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		s.log.Error("failed to check if journey exists", "error", err, "store_id", storeID)
		return err
	}

	// Create new journey in "prospect" state (pre-pairing).
	now := time.Now().UTC()
	m := &JourneyModel{
		ID:             string(storeID),
		MerchantID:     string(merchantID),
		State:          string(domain.JourneyProspect),
		History:        []JourneyHistoryItem{{State: string(domain.JourneyProspect), EnteredAt: now}},
		Stage:          string(domain.StageProsepct),
		EnteredStateAt: now,
		EnteredStageAt: now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	s.log.Info("creating journey", "store_id", storeID, "state", m.State)
	if err := s.repo.Upsert(ctx, m); err != nil {
		s.log.Error("failed to create journey", "error", err, "store_id", storeID)
		return err
	}
	s.log.Info("journey created successfully", "store_id", storeID)
	return nil
}

func (s *JourneyService) HandleSignal(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, sig Signal) error {
	s.log.Info("handling journey signal", "store_id", storeID, "signal", sig)
	m, err := s.repo.Get(ctx, merchantID, storeID)
	if err != nil {
		// If journey doesn't exist, no-op (signal arrived for a non-existent store;
		// likely a race between store creation and signal arrival).
		if errors.Is(err, ErrNotFound) {
			s.log.Warn("signal received for non-existent journey", "merchant_id", merchantID, "store_id", storeID, "signal", sig)
			return nil
		}
		s.log.Error("failed to get journey", "error", err, "store_id", storeID)
		return err
	}

	s.log.Info("current journey state", "store_id", storeID, "state", m.State, "signal", sig)
	nextState, ok := transitionTable[[2]string{m.State, string(sig)}]
	if !ok {
		// No transition defined for this (state, signal) pair; no-op.
		// This is idempotent and expected — the same signal may arrive multiple times,
		// or a signal may arrive in a state it doesn't apply to (e.g. readiness_threshold_met
		// arriving twice, or after state has already advanced).
		s.log.Warn("no transition defined for state/signal pair", "store_id", storeID, "state", m.State, "signal", sig)
		return nil
	}

	fromState := m.State
	m.State = string(nextState)
	m.EnteredStateAt = time.Now().UTC()
	m.UpdatedAt = m.EnteredStateAt
	m.History = append(m.History, JourneyHistoryItem{State: string(nextState), EnteredAt: m.EnteredStateAt})

	// Update engagement stage based on new state
	// Stage transitions happen when state advances to certain milestones
	newStage := engagement.ComputeStageFromState(nextState)
	if newStage != domain.EngagementStage(m.Stage) {
		m.Stage = string(newStage)
		m.EnteredStageAt = m.EnteredStateAt
	}

	if err := s.repo.Upsert(ctx, m); err != nil {
		return err
	}
	s.log.Info("journey transition applied", "store_id", storeID, "merchant_id", merchantID, "signal", sig, "from_state", fromState, "to_state", m.State)

	if err := s.publishUpdated(ctx, merchantID, storeID, nextState); err != nil {
		// The transition is already persisted; a failed push is a missed
		// freshness optimization, not a lost state change (the console falls
		// back to its normal fetch-on-load path). Log and continue.
		s.log.Warn("failed to publish journey.updated", "error", err, "store_id", storeID, "state", nextState)
	}

	s.reconcileStageWork(ctx, merchantID, storeID, nextState)

	// Self-heal the Ready -> Live race documented on WithActivationStateReader:
	// activation's own state-change event fires at most once, so if it fired
	// before this journey reached Ready, that earlier signal found no
	// matching transition and was dropped for good. Reaching Ready is the
	// other half of that race — check now whether activation already got
	// there first, and finish the transition immediately if so instead of
	// waiting on an event that will never come again.
	if nextState == domain.JourneyReady && s.activation != nil {
		act, err := s.activation.Get(ctx, merchantID, storeID)
		if err != nil {
			s.log.Warn("could not check activation state for ready->live self-heal", "error", err, "store_id", storeID)
		} else if act.UCPState == domain.UCPStateActive {
			if err := s.HandleSignal(ctx, merchantID, storeID, SignalStoreActivated); err != nil {
				s.log.Warn("failed to self-heal ready->live after activation already active", "error", err, "store_id", storeID)
			}
		}
	}

	return nil
}

func (s *JourneyService) publishUpdated(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, state domain.JourneyState) error {
	if s.bus == nil {
		return nil // Event bus not wired; skip silently.
	}

	payload, err := json.Marshal(journeyUpdatedPayload{StoreID: string(storeID), State: string(state)})
	if err != nil {
		return fmt.Errorf("marshal journey.updated payload: %w", err)
	}

	event := newDomainEvent(ucp.EventJourneyUpdated, string(merchantID), string(storeID), payload)
	if err := s.bus.Publish(ctx, event); err != nil {
		return fmt.Errorf("publish journey.updated: %w", err)
	}
	return nil
}

func countOpenTasksByOwner(tasks []domain.Task, owner domain.TaskOwner) int {
	return lo.CountBy(tasks, func(t domain.Task) bool { return t.Owner == owner && t.IsOpen() })
}

// nextAction picks the one task to put in front of the merchant. It must be
// theirs and it must be open: the field exists to answer "what do I do
// next", so a task owned by Sarvex or Google — or one already done — is
// never a valid answer, however near the top of the feed it sits. The list
// arrives in the spine's order (checkout-blocking first, then newest), so
// the first match is the most urgent one.
func nextAction(tasks []domain.Task) *domain.JourneyNextAction {
	t, ok := lo.Find(tasks, func(t domain.Task) bool {
		return t.Owner == domain.TaskOwnerMerchant && t.IsOpen()
	})
	if !ok {
		return nil
	}
	return &domain.JourneyNextAction{TaskID: t.TaskID, Title: t.Title}
}

var _ Service = (*JourneyService)(nil)
