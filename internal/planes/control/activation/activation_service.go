// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package activation

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

// ActivationService is the production implementation of Service.
type ActivationService struct {
	repo     Repository
	stores   StoreReader
	verifier Verifier
	keys     KeyEnsurer
	bus      eventbus.EventBus
	log      *slog.Logger
}

// KeyEnsurer provisions a store's UCP signing keypair if it is missing and
// returns the active key id. Satisfied by store.StoreService.
type KeyEnsurer interface {
	EnsureSigningKeys(ctx context.Context, storeID string) (string, error)
}

// WithKeyEnsurer wires signing-key provisioning into the profile_published
// transition, so the state that claims "keys provisioned" actually records
// one. Left unwired, the transition behaves as before.
func (s *ActivationService) WithKeyEnsurer(k KeyEnsurer) *ActivationService {
	s.keys = k
	return s
}

// NewActivationService constructs a Service backed by MongoDB.
//
// bus publishes control.store.activation_state_changed after every accepted
// transition — the surface service consumes it to auto-enroll direct_agent
// when a store reaches active, and health/analytics consume it to key off
// activation without polling. bus, stores and verifier may all be nil: the
// service degrades to state-only operation (no events published, Verify
// returns an error) rather than failing construction, matching
// journey.NewJourneyService's convention.
func NewActivationService(repo Repository, stores StoreReader, verifier Verifier, bus eventbus.EventBus, log *slog.Logger) *ActivationService {
	if log == nil {
		log = slog.Default()
	}
	return &ActivationService{
		repo:     repo,
		stores:   stores,
		verifier: verifier,
		bus:      bus,
		log:      log,
	}
}

func (s *ActivationService) Get(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Activation, error) {
	m, err := s.repo.Get(ctx, merchantID, storeID)
	if err != nil {
		return domain.Activation{}, err
	}
	return m.ToDomain(), nil
}

func (s *ActivationService) Initialize(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) error {
	if _, err := s.repo.Get(ctx, merchantID, storeID); err == nil {
		return nil // already exists; idempotent no-op
	} else if !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("check activation exists: %w", err)
	}

	now := time.Now().UTC()
	m := &ActivationModel{
		ID:             string(storeID),
		MerchantID:     string(merchantID),
		UCPState:       string(domain.UCPStateDraft),
		Capabilities:   []string{},
		History:        []ActivationHistoryItem{{State: string(domain.UCPStateDraft), EnteredAt: now}},
		EnteredStateAt: now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.repo.Upsert(ctx, m); err != nil {
		return fmt.Errorf("create activation: %w", err)
	}
	s.log.Info("activation initialized", "merchant_id", merchantID, "store_id", storeID)
	return nil
}

func (s *ActivationService) HandleSignal(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, sig Signal) error {
	m, err := s.repo.Get(ctx, merchantID, storeID)
	if err != nil {
		return err
	}

	from := domain.UCPState(m.UCPState)
	to, ok := Next(from, sig)
	if !ok {
		// Explicit rather than silent: the journey machine this replaces
		// swallowed every unmatched pair, which made a genuinely wrong
		// signal indistinguishable from a duplicate one. Callers that
		// expect duplicates check for this error and move on.
		return ErrInvalidTransition{From: from, Signal: sig}
	}

	now := time.Now().UTC()
	m.UCPState = string(to)
	m.EnteredStateAt = now
	m.UpdatedAt = now
	m.History = append(m.History, ActivationHistoryItem{State: string(to), EnteredAt: now})

	switch to {
	case domain.UCPStateProfilePublished:
		// The state's own definition is "the profile serves with keys
		// provisioned and capabilities declared" — but nothing recorded the
		// key, so the record carried an empty signing_key_id while claiming
		// the store had cleared this gate. Ensuring here also repairs a store
		// whose keys went missing before it published.
		m.SigningKeyID = s.ensureSigningKeyID(ctx, storeID, m.SigningKeyID)
	case domain.UCPStateVerified:
		m.VerifiedAt = &now
		m.SuspensionReason = ""
	case domain.UCPStateSuspended:
		m.SuspensionReason = suspensionReason(sig)
	case domain.UCPStateActive:
		m.SuspensionReason = ""
	}

	if err := s.repo.Upsert(ctx, m); err != nil {
		return fmt.Errorf("persist activation transition: %w", err)
	}
	s.log.Info("activation transition applied",
		"merchant_id", merchantID, "store_id", storeID, "signal", sig, "from_state", from, "to_state", to)

	if err := s.publishStateChanged(ctx, merchantID, storeID, from, to, sig); err != nil {
		// The transition is already durable; a failed publish costs
		// consumers freshness, not correctness — they re-read on their own
		// schedule. Log and continue.
		s.log.Warn("failed to publish activation state change",
			"error", err, "merchant_id", merchantID, "store_id", storeID, "to_state", to)
	}
	return nil
}

// ensureSigningKeyID provisions the store's signing keypair if absent and
// returns its id, falling back to whatever the record already held.
//
// Best-effort: a store reaching profile_published without a recordable key id
// is worth a loud log, but not worth refusing a transition whose other
// evidence (the catalog scored past threshold) has already arrived. The
// scheduler's key reconciliation retries on its own interval.
func (s *ActivationService) ensureSigningKeyID(ctx context.Context, storeID domain.StoreID, current string) string {
	if s.keys == nil {
		return current
	}
	keyID, err := s.keys.EnsureSigningKeys(ctx, string(storeID))
	if err != nil {
		s.log.Error("failed to ensure signing keys on profile publish", "error", err, "store_id", storeID)
		return current
	}
	if keyID == "" {
		return current
	}
	return keyID
}

// SetProfile records the store's published profile URL, declared
// capabilities and signing-key reference. Called when the profile is
// published; these are facts about the store's own endpoints, which is why
// they live on the activation record rather than anywhere surface-aware.
//
// Capability identifiers must come from internal/pkg/domain.CapabilityType
// — the caller is responsible for that, and ValidateCapabilities is
// available to enforce it.
func (s *ActivationService) SetProfile(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, profileURL string, capabilities []string, signingKeyID string) error {
	m, err := s.repo.Get(ctx, merchantID, storeID)
	if err != nil {
		return err
	}
	m.ProfileURL = profileURL
	m.Capabilities = capabilities
	m.SigningKeyID = signingKeyID
	m.UpdatedAt = time.Now().UTC()
	return s.repo.Upsert(ctx, m)
}

func (s *ActivationService) Verify(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (VerificationReport, error) {
	if s.verifier == nil {
		return VerificationReport{}, errors.New("activation: no verifier configured")
	}

	// The probes run against the store's own platform-specific endpoints, so
	// the platform comes from the store record — never a guessed default.
	if s.stores == nil {
		return VerificationReport{}, errors.New("activation: no store reader configured")
	}
	st, err := s.stores.Get(ctx, merchantID, storeID)
	if err != nil {
		return VerificationReport{}, fmt.Errorf("read store for verification: %w", err)
	}

	checks, err := s.verifier.Verify(ctx, storeID, st.Platform)
	if err != nil {
		return VerificationReport{}, fmt.Errorf("run verification: %w", err)
	}

	passed := len(checks) > 0
	for _, c := range checks {
		if !c.Passed {
			passed = false
			break
		}
	}

	sig := SignalVerificationFailed
	if passed {
		sig = SignalVerificationPassed
	}
	// A verification run reports what it found even when the store is in a
	// state the signal doesn't apply to (re-running against an already
	// verified store, say). An invalid transition is not a failed run.
	if err := s.HandleSignal(ctx, merchantID, storeID, sig); err != nil {
		var invalid ErrInvalidTransition
		if !errors.As(err, &invalid) {
			return VerificationReport{}, err
		}
	}

	m, err := s.repo.Get(ctx, merchantID, storeID)
	if err != nil {
		return VerificationReport{}, err
	}

	report := VerificationReport{
		Passed: passed,
		State:  domain.UCPState(m.UCPState),
		Checks: checks,
		RanAt:  time.Now().UTC().Format(time.RFC3339),
	}

	// Persist the run onto the record. Verification is normally triggered as
	// part of a pre-validation run rather than by a direct call, so this is
	// where the console reads the checks from — GET .../activation, not the
	// response to a POST it never made. A failure to persist costs the
	// console the check detail, not the transition, which is already durable.
	m.LastVerification = &VerificationRunItem{
		Passed: report.Passed,
		State:  string(report.State),
		Checks: lo.Map(checks, func(c CheckResult, _ int) VerificationRunCheck {
			return VerificationRunCheck{CheckID: c.CheckID, Passed: c.Passed, Detail: c.Detail}
		}),
		RanAt: report.RanAt,
	}
	// Also backfills the key id for a store that cleared profile_published
	// before that transition started recording one — it is already past the
	// transition and will never take it again, so verification is where its
	// record catches up.
	m.SigningKeyID = s.ensureSigningKeyID(ctx, storeID, m.SigningKeyID)
	m.UpdatedAt = time.Now().UTC()
	if err := s.repo.Upsert(ctx, m); err != nil {
		s.log.Warn("failed to persist verification report",
			"error", err, "merchant_id", merchantID, "store_id", storeID)
	}

	return report, nil
}

func (s *ActivationService) publishStateChanged(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID, from, to domain.UCPState, sig Signal) error {
	if s.bus == nil {
		return nil
	}

	payload, err := json.Marshal(StateChangedPayload{
		StoreID:   string(storeID),
		FromState: string(from),
		ToState:   string(to),
		Signal:    string(sig),
	})
	if err != nil {
		return fmt.Errorf("marshal activation state change: %w", err)
	}

	event := newDomainEvent(ucp.EventStoreActivationStateChanged, string(merchantID), string(storeID), payload)
	if err := s.bus.Publish(ctx, event); err != nil {
		return fmt.Errorf("publish activation state change: %w", err)
	}
	return nil
}

// suspensionReason names why a store was suspended, as an i18n key. Both
// causes are store-level (R4) — conformance regressed, or health has been
// critical for a sustained period. No surface can put a store here.
func suspensionReason(sig Signal) string {
	switch sig {
	case SignalVerificationFailed:
		return "activation.suspension.conformance_regressed"
	case SignalHealthCritical:
		return "activation.suspension.health_critical"
	default:
		return "activation.suspension.unknown"
	}
}

var _ Service = (*ActivationService)(nil)
