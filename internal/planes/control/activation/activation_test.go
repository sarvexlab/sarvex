// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package activation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sarvexlab/sarvex/internal/platform/woocommerce"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/activation"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

const (
	testMerchant = domain.MerchantID("m-1")
	testStore    = domain.StoreID("s-1")
)

// ── The happy path, one transition at a time ─────────────────────────────

// TestActivation_FullPath walks draft → active and asserts the store stops
// at each state on the way. The point is not that it arrives — it is that
// it cannot skip: every intermediate state is observed.
func TestActivation_FullPath(t *testing.T) {
	svc, ctx := newService(t)
	require.NoError(t, svc.Initialize(ctx, testMerchant, testStore))

	steps := []struct {
		signal activation.Signal
		want   domain.UCPState
	}{
		{activation.SignalConnectorLinked, domain.UCPStateConnectorLinked},
		{activation.SignalProfilePublished, domain.UCPStateProfilePublished},
		{activation.SignalVerificationPassed, domain.UCPStateVerified},
		{activation.SignalActivated, domain.UCPStateActive},
	}

	assert.Equal(t, domain.UCPStateDraft, state(t, svc, ctx))
	for _, step := range steps {
		require.NoError(t, svc.HandleSignal(ctx, testMerchant, testStore, step.signal))
		assert.Equal(t, step.want, state(t, svc, ctx), "after %s", step.signal)
	}
}

// TestActivation_CannotSkipStates asserts every forward signal is refused
// from every state except the one it belongs to. This is the difference
// from the journey machine this replaces, which silently ignored unmatched
// pairs and so could not tell a wrong signal from a duplicate one.
func TestActivation_CannotSkipStates(t *testing.T) {
	forward := map[activation.Signal]domain.UCPState{
		activation.SignalConnectorLinked:    domain.UCPStateDraft,
		activation.SignalProfilePublished:   domain.UCPStateConnectorLinked,
		activation.SignalVerificationPassed: domain.UCPStateProfilePublished,
		activation.SignalActivated:          domain.UCPStateVerified,
	}

	for sig, legalFrom := range forward {
		for _, from := range activation.AllStates() {
			_, ok := activation.Next(from, sig)
			if from == legalFrom {
				assert.Truef(t, ok, "%s must be legal from %s", sig, from)
				continue
			}
			// verification_passed also resumes a suspended store, which is
			// a deliberate second home for that one signal.
			if sig == activation.SignalVerificationPassed && from == domain.UCPStateSuspended {
				continue
			}
			assert.Falsef(t, ok, "%s must not be legal from %s", sig, from)
		}
	}
}

func TestActivation_InvalidTransitionIsRejected(t *testing.T) {
	svc, ctx := newService(t)
	require.NoError(t, svc.Initialize(ctx, testMerchant, testStore))

	// draft cannot be activated: three states have to happen first.
	err := svc.HandleSignal(ctx, testMerchant, testStore, activation.SignalActivated)

	var invalid activation.ErrInvalidTransition
	require.ErrorAs(t, err, &invalid, "an illegal signal must be an error, not a silent no-op")
	assert.Equal(t, domain.UCPStateDraft, invalid.From)
	assert.Equal(t, domain.UCPStateDraft, state(t, svc, ctx), "a rejected signal changes nothing")
}

// ── Regressions and suspension ───────────────────────────────────────────

func TestActivation_Regressions(t *testing.T) {
	cases := []struct {
		name   string
		reach  []activation.Signal
		signal activation.Signal
		want   domain.UCPState
	}{
		{
			name:   "failed verification from verified falls back to profile_published",
			reach:  toVerified(),
			signal: activation.SignalVerificationFailed,
			want:   domain.UCPStateProfilePublished,
		},
		{
			name:   "withdrawn profile falls back to connector_linked",
			reach:  toActive(),
			signal: activation.SignalProfileWithdrawn,
			want:   domain.UCPStateConnectorLinked,
		},
		{
			name:   "unlinking the connector returns an active store to draft",
			reach:  toActive(),
			signal: activation.SignalConnectorUnlinked,
			want:   domain.UCPStateDraft,
		},
		{
			name:   "critical health suspends an active store",
			reach:  toActive(),
			signal: activation.SignalHealthCritical,
			want:   domain.UCPStateSuspended,
		},
		{
			name:   "failed verification suspends an active store",
			reach:  toActive(),
			signal: activation.SignalVerificationFailed,
			want:   domain.UCPStateSuspended,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, ctx := newService(t)
			require.NoError(t, svc.Initialize(ctx, testMerchant, testStore))
			advance(t, svc, ctx, tc.reach...)

			require.NoError(t, svc.HandleSignal(ctx, testMerchant, testStore, tc.signal))
			assert.Equal(t, tc.want, state(t, svc, ctx))
		})
	}
}

// TestActivation_SuspensionOnlyFromVerifiedOrActive pins R4: a store that
// never got its endpoints working has nothing to suspend.
func TestActivation_SuspensionOnlyFromVerifiedOrActive(t *testing.T) {
	for _, from := range activation.AllStates() {
		_, ok := activation.Next(from, activation.SignalHealthCritical)
		want := from == domain.UCPStateVerified || from == domain.UCPStateActive
		assert.Equalf(t, want, ok, "health_critical from %s", from)
	}
}

func TestActivation_SuspensionRecordsReason(t *testing.T) {
	svc, ctx := newService(t)
	require.NoError(t, svc.Initialize(ctx, testMerchant, testStore))
	advance(t, svc, ctx, toActive()...)

	require.NoError(t, svc.HandleSignal(ctx, testMerchant, testStore, activation.SignalHealthCritical))
	a, err := svc.Get(ctx, testMerchant, testStore)
	require.NoError(t, err)
	assert.Equal(t, "activation.suspension.health_critical", a.SuspendedReason)

	// Clearing conformance again resumes, and clears the reason with it.
	require.NoError(t, svc.HandleSignal(ctx, testMerchant, testStore, activation.SignalResumed))
	a, err = svc.Get(ctx, testMerchant, testStore)
	require.NoError(t, err)
	assert.Equal(t, domain.UCPStateActive, a.UCPState)
	assert.Empty(t, a.SuspendedReason)
}

// ── active is a complete state in its own right (R3) ─────────────────────

// TestActivation_ActiveIsTerminalSuccess pins the product claim that
// carries the whole refactor: a store with zero surfaces enrolled is
// finished, not half-finished. Nothing in this package can express
// "active but incomplete", because there is nothing after active.
func TestActivation_ActiveIsTerminalSuccess(t *testing.T) {
	assert.True(t, activation.IsTerminalSuccess(domain.UCPStateActive))

	order := activation.Order()
	assert.Equal(t, domain.UCPStateActive, order[len(order)-1],
		"active must be the last step of the progression")

	// No forward signal leaves active. Every legal move from it is a
	// regression or a suspension — never progress toward something else.
	for _, sig := range activation.AllSignals() {
		next, ok := activation.Next(domain.UCPStateActive, sig)
		if !ok {
			continue
		}
		assert.NotEqual(t, domain.UCPStateActive, next)
		assert.Containsf(t, []domain.UCPState{
			domain.UCPStateConnectorLinked,
			domain.UCPStateDraft,
			domain.UCPStateSuspended,
		}, next, "signal %s moves active somewhere unexpected", sig)
	}
}

// ── Initialization and history ───────────────────────────────────────────

func TestActivation_InitializeIsIdempotent(t *testing.T) {
	svc, ctx := newService(t)
	require.NoError(t, svc.Initialize(ctx, testMerchant, testStore))
	advance(t, svc, ctx, activation.SignalConnectorLinked)

	// A second Initialize must not reset a store that has moved on.
	require.NoError(t, svc.Initialize(ctx, testMerchant, testStore))
	assert.Equal(t, domain.UCPStateConnectorLinked, state(t, svc, ctx))
}

func TestActivation_HistoryIsAppendOnly(t *testing.T) {
	svc, ctx := newService(t)
	require.NoError(t, svc.Initialize(ctx, testMerchant, testStore))
	advance(t, svc, ctx, toActive()...)

	a, err := svc.Get(ctx, testMerchant, testStore)
	require.NoError(t, err)

	got := make([]domain.UCPState, 0, len(a.History))
	for _, h := range a.History {
		got = append(got, h.State)
	}
	assert.Equal(t, []domain.UCPState{
		domain.UCPStateDraft,
		domain.UCPStateConnectorLinked,
		domain.UCPStateProfilePublished,
		domain.UCPStateVerified,
		domain.UCPStateActive,
	}, got)
}

// TestActivation_StepsReportGroundCovered asserts a regressed store still
// reports the steps it genuinely reached. A store that fell back from
// verified has published a profile; saying otherwise would misreport it.
func TestActivation_StepsReportGroundCovered(t *testing.T) {
	svc, ctx := newService(t)
	require.NoError(t, svc.Initialize(ctx, testMerchant, testStore))
	advance(t, svc, ctx, toVerified()...)
	require.NoError(t, svc.HandleSignal(ctx, testMerchant, testStore, activation.SignalVerificationFailed))

	a, err := svc.Get(ctx, testMerchant, testStore)
	require.NoError(t, err)
	require.Equal(t, domain.UCPStateProfilePublished, a.UCPState)

	reached := map[domain.UCPState]bool{}
	for _, s := range a.Steps {
		reached[s.State] = s.Reached
	}
	assert.True(t, reached[domain.UCPStateProfilePublished])
	assert.True(t, reached[domain.UCPStateVerified], "the store did reach verified once")
	assert.False(t, reached[domain.UCPStateActive])
}

func TestActivation_TenantIsolation(t *testing.T) {
	svc, ctx := newService(t)
	require.NoError(t, svc.Initialize(ctx, testMerchant, testStore))

	// The same store id under a different merchant is a different store.
	_, err := svc.Get(ctx, domain.MerchantID("m-2"), testStore)
	assert.ErrorIs(t, err, activation.ErrNotFound)
}

// ── Verification ─────────────────────────────────────────────────────────

func TestActivation_VerifyAdvancesOnPass(t *testing.T) {
	repo := newMemRepo()
	verifier := &stubVerifier{checks: []activation.CheckResult{
		{CheckID: "profile_reachable", Passed: true},
		{CheckID: "signature_roundtrip", Passed: true},
		{CheckID: "negotiation_handshake", Passed: true},
		{CheckID: "catalog_lookup", Passed: true},
	}}
	svc := activation.NewActivationService(repo, stubStores{}, verifier, nil, nil)
	ctx := context.Background()

	require.NoError(t, svc.Initialize(ctx, testMerchant, testStore))
	advance(t, svc, ctx, activation.SignalConnectorLinked, activation.SignalProfilePublished)

	report, err := svc.Verify(ctx, testMerchant, testStore)
	require.NoError(t, err)
	assert.True(t, report.Passed)
	assert.Equal(t, domain.UCPStateVerified, report.State)
	assert.Len(t, report.Checks, 4)
}

func TestActivation_VerifyHoldsOnFailure(t *testing.T) {
	repo := newMemRepo()
	verifier := &stubVerifier{checks: []activation.CheckResult{
		{CheckID: "profile_reachable", Passed: true},
		{CheckID: "signature_roundtrip", Passed: false, Detail: "signature invalid"},
	}}
	svc := activation.NewActivationService(repo, stubStores{}, verifier, nil, nil)
	ctx := context.Background()

	require.NoError(t, svc.Initialize(ctx, testMerchant, testStore))
	advance(t, svc, ctx, activation.SignalConnectorLinked, activation.SignalProfilePublished)

	report, err := svc.Verify(ctx, testMerchant, testStore)
	require.NoError(t, err)
	assert.False(t, report.Passed)
	assert.Equal(t, domain.UCPStateProfilePublished, report.State)
}

// The console reads the check results off the activation record, not off the
// response to a verify call — verification normally runs as part of a
// pre-validation run, so nothing client-side ever sees that response.
func TestActivation_VerifyPersistsTheReportOntoTheRecord(t *testing.T) {
	repo := newMemRepo()
	verifier := &stubVerifier{checks: []activation.CheckResult{
		{CheckID: "profile_reachable", Passed: true},
		{CheckID: "signature_roundtrip", Passed: false, Detail: "signature invalid"},
	}}
	svc := activation.NewActivationService(repo, stubStores{}, verifier, nil, nil)
	ctx := context.Background()

	require.NoError(t, svc.Initialize(ctx, testMerchant, testStore))
	advance(t, svc, ctx, activation.SignalConnectorLinked, activation.SignalProfilePublished)

	report, err := svc.Verify(ctx, testMerchant, testStore)
	require.NoError(t, err)

	got, err := svc.Get(ctx, testMerchant, testStore)
	require.NoError(t, err)
	require.NotNil(t, got.LastVerification, "the run must be readable from GET activation")
	assert.Equal(t, report.Passed, got.LastVerification.Passed)
	assert.Equal(t, report.RanAt, got.LastVerification.RanAt)
	require.Len(t, got.LastVerification.Checks, 2)
	assert.Equal(t, "signature_roundtrip", got.LastVerification.Checks[1].CheckID)
	assert.False(t, got.LastVerification.Checks[1].Passed)
	assert.Equal(t, "signature invalid", got.LastVerification.Checks[1].Detail)
}

// A store that has never been verified reports no run rather than an empty one.
func TestActivation_NoVerificationRunYet(t *testing.T) {
	svc := activation.NewActivationService(newMemRepo(), stubStores{}, nil, nil, nil)
	ctx := context.Background()

	require.NoError(t, svc.Initialize(ctx, testMerchant, testStore))

	got, err := svc.Get(ctx, testMerchant, testStore)
	require.NoError(t, err)
	assert.Nil(t, got.LastVerification)
}

// TestActivation_VerifyOnAlreadyVerifiedStoreIsNotAnError pins that a
// re-run reports its findings even when the resulting signal names no legal
// move. An invalid transition is not a failed verification run.
func TestActivation_VerifyOnAlreadyVerifiedStoreIsNotAnError(t *testing.T) {
	repo := newMemRepo()
	verifier := &stubVerifier{checks: []activation.CheckResult{{CheckID: "profile_reachable", Passed: true}}}
	svc := activation.NewActivationService(repo, stubStores{}, verifier, nil, nil)
	ctx := context.Background()

	require.NoError(t, svc.Initialize(ctx, testMerchant, testStore))
	advance(t, svc, ctx, toActive()...)

	report, err := svc.Verify(ctx, testMerchant, testStore)
	require.NoError(t, err)
	assert.True(t, report.Passed)
	assert.Equal(t, domain.UCPStateActive, report.State, "an active store stays active")
}

// ── helpers ──────────────────────────────────────────────────────────────

func newService(t *testing.T) (*activation.ActivationService, context.Context) {
	t.Helper()
	return activation.NewActivationService(newMemRepo(), stubStores{}, nil, nil, nil), context.Background()
}

func state(t *testing.T, svc activation.Service, ctx context.Context) domain.UCPState {
	t.Helper()
	a, err := svc.Get(ctx, testMerchant, testStore)
	require.NoError(t, err)
	return a.UCPState
}

func advance(t *testing.T, svc activation.Service, ctx context.Context, signals ...activation.Signal) {
	t.Helper()
	for _, s := range signals {
		require.NoError(t, svc.HandleSignal(ctx, testMerchant, testStore, s), "advancing with %s", s)
	}
}

func toVerified() []activation.Signal {
	return []activation.Signal{
		activation.SignalConnectorLinked,
		activation.SignalProfilePublished,
		activation.SignalVerificationPassed,
	}
}

func toActive() []activation.Signal {
	return append(toVerified(), activation.SignalActivated)
}

type stubVerifier struct {
	checks []activation.CheckResult
	err    error
}

func (s *stubVerifier) Verify(context.Context, domain.StoreID, domain.Platform) ([]activation.CheckResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.checks, nil
}

type stubStores struct{}

func (stubStores) Get(_ context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (domain.Store, error) {
	return domain.Store{
		StoreID:    storeID,
		MerchantID: merchantID,
		Platform:   woocommerce.PlatformName,
	}, nil
}

// memRepo is an in-memory Repository. Keyed by (merchant_id, store_id)
// together so the tenant-isolation test exercises the same key shape the
// Mongo repository enforces.
type memRepo struct {
	docs map[string]*activation.ActivationModel
}

func newMemRepo() *memRepo { return &memRepo{docs: map[string]*activation.ActivationModel{}} }

func (r *memRepo) key(merchantID domain.MerchantID, storeID domain.StoreID) string {
	return string(merchantID) + ":" + string(storeID)
}

func (r *memRepo) Get(_ context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (*activation.ActivationModel, error) {
	m, ok := r.docs[r.key(merchantID, storeID)]
	if !ok {
		return nil, activation.ErrNotFound
	}
	clone := *m
	clone.History = append([]activation.ActivationHistoryItem(nil), m.History...)
	return &clone, nil
}

func (r *memRepo) Upsert(_ context.Context, m *activation.ActivationModel) error {
	clone := *m
	clone.History = append([]activation.ActivationHistoryItem(nil), m.History...)
	r.docs[r.key(domain.MerchantID(m.MerchantID), domain.StoreID(m.ID))] = &clone
	return nil
}

func (r *memRepo) ListByState(_ context.Context, s domain.UCPState) ([]*activation.ActivationModel, error) {
	var out []*activation.ActivationModel
	for _, m := range r.docs {
		if m.UCPState == string(s) {
			out = append(out, m)
		}
	}
	return out, nil
}

func (r *memRepo) Delete(_ context.Context, merchantID domain.MerchantID, storeID domain.StoreID) error {
	delete(r.docs, r.key(merchantID, storeID))
	return nil
}

var _ = errors.Is

// stubKeyEnsurer stands in for store.StoreService.EnsureSigningKeys.
type stubKeyEnsurer struct {
	keyID  string
	err    error
	calls  int
	stores []string
}

func (s *stubKeyEnsurer) EnsureSigningKeys(_ context.Context, storeID string) (string, error) {
	s.calls++
	s.stores = append(s.stores, storeID)
	return s.keyID, s.err
}

// profile_published is defined as "the profile serves with keys provisioned
// and capabilities declared", but nothing recorded the key — SetProfile, the
// only writer of signing_key_id, had no callers at all. Stores reached active
// with signing_key_id empty, so the record asserted a gate it never checked.
func TestActivation_ProfilePublishedRecordsTheSigningKey(t *testing.T) {
	keys := &stubKeyEnsurer{keyID: "kid-abc"}
	svc := activation.NewActivationService(newMemRepo(), stubStores{}, nil, nil, nil).WithKeyEnsurer(keys)
	ctx := context.Background()

	require.NoError(t, svc.Initialize(ctx, testMerchant, testStore))
	advance(t, svc, ctx, activation.SignalConnectorLinked, activation.SignalProfilePublished)

	got, err := svc.Get(ctx, testMerchant, testStore)
	require.NoError(t, err)
	assert.Equal(t, "kid-abc", got.SigningKeyID)
	assert.Equal(t, 1, keys.calls, "keys should be ensured exactly once on the transition")
	assert.Equal(t, []string{string(testStore)}, keys.stores)
}

// A key that cannot be provisioned is logged, not fatal: the catalog evidence
// that drove the transition has already arrived, and the scheduler's key
// reconciliation retries on its own interval.
func TestActivation_ProfilePublishedSurvivesKeyFailure(t *testing.T) {
	keys := &stubKeyEnsurer{err: errors.New("keystore down")}
	svc := activation.NewActivationService(newMemRepo(), stubStores{}, nil, nil, nil).WithKeyEnsurer(keys)
	ctx := context.Background()

	require.NoError(t, svc.Initialize(ctx, testMerchant, testStore))
	advance(t, svc, ctx, activation.SignalConnectorLinked, activation.SignalProfilePublished)

	got, err := svc.Get(ctx, testMerchant, testStore)
	require.NoError(t, err)
	assert.Equal(t, domain.UCPStateProfilePublished, got.UCPState, "the transition must still stand")
	assert.Empty(t, got.SigningKeyID)
}

// Unwired, the transition behaves exactly as it did before.
func TestActivation_ProfilePublishedWithoutKeyEnsurer(t *testing.T) {
	svc := activation.NewActivationService(newMemRepo(), stubStores{}, nil, nil, nil)
	ctx := context.Background()

	require.NoError(t, svc.Initialize(ctx, testMerchant, testStore))
	require.NotPanics(t, func() {
		advance(t, svc, ctx, activation.SignalConnectorLinked, activation.SignalProfilePublished)
	})
}

// Verification probes the store's own platform-specific endpoints, so without
// a store reader it must refuse rather than guess a platform.
func TestActivation_VerifyWithoutStoreReaderRefusesToGuessThePlatform(t *testing.T) {
	verifier := &stubVerifier{checks: []activation.CheckResult{{CheckID: "profile_reachable", Passed: true}}}
	svc := activation.NewActivationService(newMemRepo(), nil, verifier, nil, nil)
	ctx := context.Background()
	require.NoError(t, svc.Initialize(ctx, testMerchant, testStore))

	_, err := svc.Verify(ctx, testMerchant, testStore)
	require.Error(t, err)
}
