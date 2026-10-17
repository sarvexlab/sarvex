// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package surface

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/commandbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// SurfaceService is the production implementation of Service.
type SurfaceService struct {
	registry   *Registry
	repo       Repository
	activation ActivationReader
	connectors ConnectorReader
	bus        eventbus.EventBus
	log        *slog.Logger
}

// NewSurfaceService constructs a Service. activation is required — it backs
// the one gate in R9. connectors and bus may be nil (checks that need
// connectors will see an empty list; no events are published).
func NewSurfaceService(
	registry *Registry,
	repo Repository,
	activationReader ActivationReader,
	connectors ConnectorReader,
	bus eventbus.EventBus,
	log *slog.Logger,
) *SurfaceService {
	if log == nil {
		log = slog.Default()
	}
	return &SurfaceService{
		registry:   registry,
		repo:       repo,
		activation: activationReader,
		connectors: connectors,
		bus:        bus,
		log:        log,
	}
}

func (s *SurfaceService) Registry(_ context.Context) []domain.SurfaceDescriptor {
	return s.registry.Descriptors()
}

func (s *SurfaceService) List(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) ([]domain.SurfaceEnrollment, error) {
	stored, err := s.repo.ListByStore(ctx, merchantID, storeID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*EnrollmentModel, len(stored))
	for _, m := range stored {
		byID[m.SurfaceID] = m
	}

	blocked, blockedReason := s.activationGate(ctx, merchantID, storeID)

	// The registry, not the enrollment table, decides what exists: a
	// surface nobody has touched still has to appear, as available.
	out := make([]domain.SurfaceEnrollment, 0, len(s.registry.IDs()))
	for _, id := range s.registry.IDs() {
		reg, _ := s.registry.Get(id)
		var e domain.SurfaceEnrollment
		if m, ok := byID[id]; ok {
			e = m.ToDomain()
		} else {
			e = domain.SurfaceEnrollment{
				SurfaceID: id,
				StoreID:   storeID,
				State:     domain.SurfaceAvailable,
			}
		}
		e.Descriptor = reg.Descriptor
		e.Blocked = blocked
		e.BlockedReason = blockedReason
		out = append(out, e)
	}
	return out, nil
}

func (s *SurfaceService) Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) (domain.SurfaceEnrollment, error) {
	reg, ok := s.registry.Get(surfaceID)
	if !ok {
		return domain.SurfaceEnrollment{}, ErrNotFound
	}

	m, err := s.repo.Get(ctx, merchantID, storeID, surfaceID)
	var e domain.SurfaceEnrollment
	switch {
	case err == nil:
		e = m.ToDomain()
	case errors.Is(err, ErrNotFound):
		e = domain.SurfaceEnrollment{SurfaceID: surfaceID, StoreID: storeID, State: domain.SurfaceAvailable}
	default:
		return domain.SurfaceEnrollment{}, err
	}

	e.Descriptor = reg.Descriptor
	e.Blocked, e.BlockedReason = s.activationGate(ctx, merchantID, storeID)
	return e, nil
}

func (s *SurfaceService) Enroll(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) (domain.SurfaceEnrollment, error) {
	if _, ok := s.registry.Get(surfaceID); !ok {
		return domain.SurfaceEnrollment{}, ErrNotFound
	}
	if err := s.requireActive(ctx, merchantID, storeID, surfaceID); err != nil {
		return domain.SurfaceEnrollment{}, err
	}

	m, err := s.loadOrInit(ctx, merchantID, storeID, surfaceID)
	if err != nil {
		return domain.SurfaceEnrollment{}, err
	}

	if domain.SurfaceState(m.State) == domain.SurfaceAvailable {
		if err := s.apply(ctx, merchantID, m, EventEnroll); err != nil {
			return domain.SurfaceEnrollment{}, err
		}
	}
	return s.evaluate(ctx, merchantID, storeID, surfaceID, m)
}

func (s *SurfaceService) EvaluatePrerequisites(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) (domain.SurfaceEnrollment, error) {
	if _, ok := s.registry.Get(surfaceID); !ok {
		return domain.SurfaceEnrollment{}, ErrNotFound
	}
	m, err := s.repo.Get(ctx, merchantID, storeID, surfaceID)
	if err != nil {
		return domain.SurfaceEnrollment{}, err
	}
	return s.evaluate(ctx, merchantID, storeID, surfaceID, m)
}

func (s *SurfaceService) SetConfig(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string, config map[string]string) (domain.SurfaceEnrollment, error) {
	reg, ok := s.registry.Get(surfaceID)
	if !ok {
		return domain.SurfaceEnrollment{}, ErrNotFound
	}
	if err := validateConfig(reg.Descriptor.ConfigSchema, config); err != nil {
		return domain.SurfaceEnrollment{}, err
	}
	if err := s.requireActive(ctx, merchantID, storeID, surfaceID); err != nil {
		return domain.SurfaceEnrollment{}, err
	}

	m, err := s.loadOrInit(ctx, merchantID, storeID, surfaceID)
	if err != nil {
		return domain.SurfaceEnrollment{}, err
	}
	m.Config = config
	m.UpdatedAt = time.Now().UTC()
	if err := s.repo.Upsert(ctx, m); err != nil {
		return domain.SurfaceEnrollment{}, err
	}
	return s.evaluate(ctx, merchantID, storeID, surfaceID, m)
}

func (s *SurfaceService) Submit(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) (domain.SurfaceEnrollment, error) {
	if _, ok := s.registry.Get(surfaceID); !ok {
		return domain.SurfaceEnrollment{}, ErrNotFound
	}
	if err := s.requireActive(ctx, merchantID, storeID, surfaceID); err != nil {
		return domain.SurfaceEnrollment{}, err
	}

	m, err := s.repo.Get(ctx, merchantID, storeID, surfaceID)
	if err != nil {
		return domain.SurfaceEnrollment{}, err
	}
	if err := s.apply(ctx, merchantID, m, EventSubmitted); err != nil {
		return domain.SurfaceEnrollment{}, err
	}
	if err := s.apply(ctx, merchantID, m, EventVerifying); err != nil {
		return domain.SurfaceEnrollment{}, err
	}
	return s.evaluate(ctx, merchantID, storeID, surfaceID, m)
}

func (s *SurfaceService) Withdraw(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) error {
	m, err := s.repo.Get(ctx, merchantID, storeID, surfaceID)
	if errors.Is(err, ErrNotFound) {
		return nil // never enrolled; idempotent no-op
	}
	if err != nil {
		return err
	}
	// Withdrawal is deliberately a state transition on this surface and
	// nothing else. No activation signal is emitted, no other enrollment
	// is read or written — see enrollment_isolation_test.go.
	if err := s.apply(ctx, merchantID, m, EventWithdrawn); err != nil {
		var invalid ErrInvalidTransition
		if errors.As(err, &invalid) {
			return nil
		}
		return err
	}
	return nil
}

func (s *SurfaceService) Suspend(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID, reason string) error {
	m, err := s.repo.Get(ctx, merchantID, storeID, surfaceID)
	if err != nil {
		return err
	}
	m.SuspensionReason = reason
	return s.apply(ctx, merchantID, m, EventSuspended)
}

func (s *SurfaceService) IsEnrolled(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) (bool, error) {
	m, err := s.repo.Get(ctx, merchantID, storeID, surfaceID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return domain.SurfaceState(m.State) == domain.SurfaceEnrolled, nil
}

func (s *SurfaceService) OnStoreActivated(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) error {
	for _, id := range s.registry.AutoEnrollIDs() {
		if _, err := s.Enroll(ctx, merchantID, storeID, id); err != nil {
			s.log.Warn("auto-enroll failed",
				"error", err, "merchant_id", merchantID, "store_id", storeID, "surface_id", id)
		}
	}
	return nil
}

// ── internals ────────────────────────────────────────────────────────────

// requireActive is the one cross-lifecycle dependency in the system:
// enrollment cannot progress past `available` unless the store is active
// (R9). It reads activation and never writes it.
func (s *SurfaceService) requireActive(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) error {
	if s.activation == nil {
		return ErrStoreNotActive{SurfaceID: surfaceID}
	}
	a, err := s.activation.Get(ctx, merchantID, storeID)
	if err != nil {
		return fmt.Errorf("read activation for enrollment gate: %w", err)
	}
	if a.UCPState != domain.UCPStateActive {
		return ErrStoreNotActive{SurfaceID: surfaceID, UCPState: a.UCPState}
	}
	return nil
}

// activationGate reports whether enrollment is currently held at available
// by the store not being active, for display. Never an error: a store with
// no activation record yet is simply blocked.
func (s *SurfaceService) activationGate(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (bool, string) {
	if s.activation == nil {
		return true, "surfaces.blocked.storeNotActive"
	}
	a, err := s.activation.Get(ctx, merchantID, storeID)
	if err != nil || a.UCPState != domain.UCPStateActive {
		return true, "surfaces.blocked.storeNotActive"
	}
	return false, ""
}

func (s *SurfaceService) loadOrInit(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string) (*EnrollmentModel, error) {
	m, err := s.repo.Get(ctx, merchantID, storeID, surfaceID)
	if err == nil {
		return m, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	now := time.Now().UTC()
	m = &EnrollmentModel{
		ID:         DocumentID(merchantID, storeID, surfaceID),
		MerchantID: string(merchantID),
		StoreID:    string(storeID),
		SurfaceID:  surfaceID,
		State:      string(domain.SurfaceAvailable),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.repo.Upsert(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

// apply moves an enrollment through one event, persists it, and publishes
// the change. An event that names no legal move returns
// ErrInvalidTransition and changes nothing.
func (s *SurfaceService) apply(ctx context.Context, merchantID domain.MerchantID, m *EnrollmentModel, ev Event) error {
	from := domain.SurfaceState(m.State)
	to, ok := nextEnrollmentState(from, ev)
	if !ok {
		return ErrInvalidTransition{SurfaceID: m.SurfaceID, From: from, Event: ev}
	}
	if to == from {
		return nil
	}

	now := time.Now().UTC()
	m.State = string(to)
	m.UpdatedAt = now
	switch to {
	case domain.SurfaceEnrolled:
		if m.EnrolledAt == nil {
			m.EnrolledAt = &now
		}
		m.LastVerifiedAt = &now
		m.SuspensionReason = ""
	case domain.SurfaceAvailable:
		// Withdrawal clears the record's history: re-enrolling later is a
		// fresh admission, not a resumption of an old one.
		m.EnrolledAt = nil
		m.LastVerifiedAt = nil
		m.SuspensionReason = ""
		m.Prerequisites = nil
		m.Artifacts = nil
	}

	if err := s.repo.Upsert(ctx, m); err != nil {
		return fmt.Errorf("persist enrollment transition: %w", err)
	}
	s.log.Info("surface enrollment transition",
		"merchant_id", merchantID, "store_id", m.StoreID, "surface_id", m.SurfaceID,
		"event", ev, "from_state", from, "to_state", to)

	if err := s.publishStateChanged(ctx, merchantID, m, from, to, ev); err != nil {
		s.log.Warn("failed to publish surface state change",
			"error", err, "store_id", m.StoreID, "surface_id", m.SurfaceID)
	}

	// Entering or leaving a surface changes which attributes and rules the
	// sync plane resolves and scores the store's catalog against (the sync
	// plane reads this enrollment, it does not subscribe to it). Ask it to
	// re-score so the catalog reflects the new surface set now, rather than
	// whenever each product next changes. Only crossing the available boundary
	// changes the set; moves between non-available states do not.
	if (from == domain.SurfaceAvailable) != (to == domain.SurfaceAvailable) {
		s.requestRescore(ctx, merchantID, m, ev)
	}
	return nil
}

// requestRescore asks the sync plane to re-score the store's catalog, using the
// existing request_audit command. Best effort, like the state-change event: the
// enrollment is already persisted, and a missed rescore self-heals on the next
// product change or audit.
func (s *SurfaceService) requestRescore(ctx context.Context, merchantID domain.MerchantID, m *EnrollmentModel, ev Event) {
	if s.bus == nil {
		return
	}
	jobID := fmt.Sprintf("surface-%s-%s-%d", m.SurfaceID, ev, time.Now().UTC().UnixNano())
	if err := s.bus.Publish(ctx, commandbus.NewAuditRequestedEvent(string(merchantID), m.StoreID, jobID)); err != nil {
		s.log.Warn("failed to request catalog rescore after surface change",
			"error", err, "store_id", m.StoreID, "surface_id", m.SurfaceID)
	}
}

// evaluate re-runs a surface's prerequisite checks and artifact providers,
// then advances or regresses the enrollment to match what it found.
//
// This is the whole of what makes prerequisites re-evaluatable on demand
// (R10): it is idempotent, safe to call repeatedly, and drives the state
// machine purely from adapter output — no surface-specific branching here.
func (s *SurfaceService) evaluate(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, surfaceID string, m *EnrollmentModel) (domain.SurfaceEnrollment, error) {
	reg, ok := s.registry.Get(surfaceID)
	if !ok {
		return domain.SurfaceEnrollment{}, ErrNotFound
	}

	// A surface nobody has started has nothing to evaluate.
	if domain.SurfaceState(m.State) == domain.SurfaceAvailable {
		return s.Get(ctx, merchantID, storeID, surfaceID)
	}

	cc, err := s.checkContext(ctx, merchantID, storeID, m)
	if err != nil {
		return domain.SurfaceEnrollment{}, err
	}

	results, allPassed := s.runChecks(ctx, reg, cc)
	m.Prerequisites = prerequisiteItems(results)

	artifacts, err := s.runArtifacts(ctx, reg, cc)
	if err != nil {
		return domain.SurfaceEnrollment{}, err
	}
	m.Artifacts = artifactItems(artifacts)
	m.UpdatedAt = time.Now().UTC()
	if err := s.repo.Upsert(ctx, m); err != nil {
		return domain.SurfaceEnrollment{}, err
	}

	if err := s.advance(ctx, merchantID, reg, m, cc, allPassed); err != nil {
		return domain.SurfaceEnrollment{}, err
	}
	return s.Get(ctx, merchantID, storeID, surfaceID)
}

// advance moves the enrollment as far as the evidence allows. Every branch
// here reads adapter output or descriptor data — never a surface id.
func (s *SurfaceService) advance(ctx context.Context, merchantID domain.MerchantID, reg Registration, m *EnrollmentModel, cc CheckContext, allPassed bool) error {
	state := domain.SurfaceState(m.State)

	if !allPassed {
		if state == domain.SurfaceConfigured {
			return s.ignoreInvalid(s.apply(ctx, merchantID, m, EventPrerequisitesFailed))
		}
		return nil
	}

	if state == domain.SurfacePrerequisitesPending {
		if err := s.ignoreInvalid(s.apply(ctx, merchantID, m, EventPrerequisitesMet)); err != nil {
			return err
		}
		state = domain.SurfaceState(m.State)
	}

	// A surface with no external steps has nothing to wait on between
	// configured and verifying — it moves straight through. One with
	// external steps waits for Submit.
	if state == domain.SurfaceConfigured && len(reg.Descriptor.ExternalSteps) == 0 {
		if err := s.ignoreInvalid(s.apply(ctx, merchantID, m, EventVerifying)); err != nil {
			return err
		}
		state = domain.SurfaceState(m.State)
	}

	if state != domain.SurfaceVerifying {
		return nil
	}

	// No verification adapter means admission is complete once
	// prerequisites pass — the auto-enroll case.
	if reg.Verification == nil {
		return s.ignoreInvalid(s.apply(ctx, merchantID, m, EventVerified))
	}

	verified, err := reg.Verification.Verified(ctx, cc)
	if err != nil {
		s.log.Warn("verification signal check failed",
			"error", err, "store_id", m.StoreID, "surface_id", m.SurfaceID)
		return nil
	}
	if verified {
		return s.ignoreInvalid(s.apply(ctx, merchantID, m, EventVerified))
	}
	return nil
}

// ignoreInvalid swallows ErrInvalidTransition. The converge-to-state pass
// above re-asserts moves that may already have happened; an already-applied
// move is not a failure.
func (s *SurfaceService) ignoreInvalid(err error) error {
	var invalid ErrInvalidTransition
	if errors.As(err, &invalid) {
		return nil
	}
	return err
}

func (s *SurfaceService) runChecks(ctx context.Context, reg Registration, cc CheckContext) ([]domain.PrerequisiteResult, bool) {
	byID := make(map[string]PrerequisiteCheck, len(reg.Checks))
	for _, c := range reg.Checks {
		byID[c.ID()] = c
	}

	results := make([]domain.PrerequisiteResult, 0, len(reg.Descriptor.PrerequisiteChecks))
	allPassed := true

	// Descriptor order, not adapter order: the descriptor is what the
	// merchant sees, and it decides the sequence.
	for _, name := range reg.Descriptor.PrerequisiteChecks {
		check, ok := byID[name]
		if !ok {
			// NewRegistry rejects this at boot; defensive only.
			continue
		}
		res := check.Evaluate(ctx, cc)
		res.CheckID = name
		if res.EvaluatedAt.IsZero() {
			res.EvaluatedAt = time.Now().UTC()
		}
		if !res.Passed {
			allPassed = false
		}
		results = append(results, res)
	}

	// Required connectors and capabilities are descriptor data, so they
	// are enforced here rather than duplicated as a check adapter in every
	// surface that has them.
	for _, ct := range reg.Descriptor.RequiredConnectors {
		res := requiredConnectorResult(ct, cc)
		if !res.Passed {
			allPassed = false
		}
		results = append(results, res)
	}
	for _, capID := range reg.Descriptor.RequiredCapabilities {
		res := requiredCapabilityResult(capID, cc)
		if !res.Passed {
			allPassed = false
		}
		results = append(results, res)
	}

	return results, allPassed
}

func (s *SurfaceService) runArtifacts(ctx context.Context, reg Registration, cc CheckContext) ([]domain.SurfaceArtifact, error) {
	byID := make(map[string]ArtifactProvider, len(reg.Artifacts))
	for _, a := range reg.Artifacts {
		byID[a.ID()] = a
	}

	out := make([]domain.SurfaceArtifact, 0, len(reg.Descriptor.Artifacts))
	for _, name := range reg.Descriptor.Artifacts {
		provider, ok := byID[name]
		if !ok {
			continue
		}
		art, err := provider.Artifact(ctx, cc)
		if err != nil {
			s.log.Warn("artifact lookup failed",
				"error", err, "store_id", cc.StoreID, "artifact_id", name)
			continue
		}
		if art == nil {
			continue
		}
		art.ArtifactID = name
		out = append(out, *art)
	}
	return out, nil
}

func (s *SurfaceService) checkContext(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, m *EnrollmentModel) (CheckContext, error) {
	cc := CheckContext{
		MerchantID: merchantID,
		StoreID:    storeID,
		Config:     m.Config,
	}
	if s.connectors != nil {
		conns, err := s.connectors.ListByStore(ctx, merchantID, storeID)
		if err != nil {
			return CheckContext{}, fmt.Errorf("list connectors for prerequisite checks: %w", err)
		}
		cc.Connectors = conns
	}
	if s.activation != nil {
		if a, err := s.activation.Get(ctx, merchantID, storeID); err == nil {
			cc.Capabilities = a.Capabilities
		}
	}
	return cc, nil
}

func (s *SurfaceService) publishStateChanged(ctx context.Context, merchantID domain.MerchantID, m *EnrollmentModel, from, to domain.SurfaceState, ev Event) error {
	if s.bus == nil {
		return nil
	}
	payload, err := json.Marshal(StateChangedPayload{
		StoreID:   m.StoreID,
		SurfaceID: m.SurfaceID,
		FromState: string(from),
		ToState:   string(to),
		Event:     string(ev),
	})
	if err != nil {
		return fmt.Errorf("marshal surface state change: %w", err)
	}
	event := newDomainEvent(ucp.EventStoreSurfaceStateChanged, string(merchantID), m.StoreID, payload)
	if err := s.bus.Publish(ctx, event); err != nil {
		return fmt.Errorf("publish surface state change: %w", err)
	}
	return nil
}

// requiredConnectorResult renders a descriptor's required-connector rule as
// an ordinary prerequisite result, so the console has nothing special to
// render for it.
func requiredConnectorResult(ct domain.ConnectorType, cc CheckContext) domain.PrerequisiteResult {
	res := domain.PrerequisiteResult{
		CheckID:     "connector:" + string(ct),
		LabelKey:    "surfaces.prereq.connector." + string(ct),
		EvaluatedAt: time.Now().UTC(),
	}
	for _, c := range cc.Connectors {
		if c.ConnectorType == ct && c.Status != domain.ConnectorStatusPairing {
			res.Passed = true
			return res
		}
	}
	res.Reason = fmt.Sprintf("no linked %s connector", ct)
	res.RemediationKey = "surfaces.remediation.connector." + string(ct)
	return res
}

func requiredCapabilityResult(capID string, cc CheckContext) domain.PrerequisiteResult {
	res := domain.PrerequisiteResult{
		CheckID:     "capability:" + capID,
		LabelKey:    "surfaces.prereq.capability",
		EvaluatedAt: time.Now().UTC(),
	}
	for _, declared := range cc.Capabilities {
		if declared == capID {
			res.Passed = true
			return res
		}
	}
	res.Reason = fmt.Sprintf("store does not declare capability %s", capID)
	return res
}

var _ Service = (*SurfaceService)(nil)
