// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package surface_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/surface"
	"github.com/sarvexlab/sarvex/internal/planes/control/surface/directagent"
)

// ── The enrollment lifecycle end to end ──────────────────────────────────

// TestEnrollment_FullLifecycle walks available → enrolled through every
// intermediate state, driving each transition with the evidence that state
// actually waits on: a failing check, then a passing one, then the
// merchant's external-step submission, then the verification signal.
func TestEnrollment_FullLifecycle(t *testing.T) {
	check := &fixedCheck{id: "thing", passed: false, reason: "not done yet"}
	signal := &flagSignal{id: "sig"}
	svc, ctx := newServiceWith(t, domain.UCPStateActive, surface.Registration{
		Descriptor: domain.SurfaceDescriptor{
			SurfaceID:          "test_surface",
			DisplayNameKey:     "test.name",
			Protocol:           domain.SurfaceProtocolUCP,
			PrerequisiteChecks: []string{"thing"},
			ExternalSteps: []domain.SurfaceExternalStep{
				{StepID: "form", LabelKey: "test.step.form", Owner: domain.SurfaceStepOwnerMerchant},
			},
			VerificationSignal: "sig",
		},
		Checks:       []surface.PrerequisiteCheck{check},
		Verification: signal,
	})

	// Nothing started: the registry still reports the surface, as available.
	e, err := svc.Get(ctx, testMerchant, testStore, "test_surface")
	require.NoError(t, err)
	assert.Equal(t, domain.SurfaceAvailable, e.State)

	// Enrolling with a failing check lands in prerequisites_pending, and
	// says why in terms the merchant can act on.
	e, err = svc.Enroll(ctx, testMerchant, testStore, "test_surface")
	require.NoError(t, err)
	assert.Equal(t, domain.SurfacePrerequisitesPending, e.State)
	require.Len(t, e.Prerequisites, 1)
	assert.False(t, e.Prerequisites[0].Passed)
	assert.Equal(t, "not done yet", e.Prerequisites[0].Reason)
	assert.NotEmpty(t, e.Prerequisites[0].RemediationKey)

	// The merchant fixes it and re-runs the checks — without restarting
	// enrollment (R10).
	check.passed = true
	e, err = svc.EvaluatePrerequisites(ctx, testMerchant, testStore, "test_surface")
	require.NoError(t, err)
	assert.Equal(t, domain.SurfaceConfigured, e.State,
		"a surface with external steps waits at configured for the merchant")

	// Submitting the external steps moves to verifying; the signal has not
	// arrived, so it stays there.
	e, err = svc.Submit(ctx, testMerchant, testStore, "test_surface")
	require.NoError(t, err)
	assert.Equal(t, domain.SurfaceVerifying, e.State)

	// The signal arrives.
	signal.verified = true
	e, err = svc.EvaluatePrerequisites(ctx, testMerchant, testStore, "test_surface")
	require.NoError(t, err)
	assert.Equal(t, domain.SurfaceEnrolled, e.State)
	require.NotNil(t, e.EnrolledAt)
}

// TestEnrollment_PrerequisiteRegression asserts a check that starts failing
// again pulls a configured enrollment back, rather than leaving it claiming
// readiness it no longer has.
func TestEnrollment_PrerequisiteRegression(t *testing.T) {
	check := &fixedCheck{id: "thing", passed: true}
	svc, ctx := newServiceWith(t, domain.UCPStateActive, simpleRegistration(check, nil))

	e, err := svc.Enroll(ctx, testMerchant, testStore, "test_surface")
	require.NoError(t, err)
	require.Equal(t, domain.SurfaceEnrolled, e.State)

	check.passed = false
	check.reason = "broke again"
	e, err = svc.EvaluatePrerequisites(ctx, testMerchant, testStore, "test_surface")
	require.NoError(t, err)
	assert.Equal(t, domain.SurfaceEnrolled, e.State,
		"an already-enrolled store is not un-enrolled by a local check regressing — only the surface can withdraw it")
	assert.False(t, e.Prerequisites[0].Passed, "but the failing check is reported")
}

// ── The one cross-lifecycle rule (R9) ────────────────────────────────────

// TestEnrollment_RequiresActiveStore pins the single dependency between the
// two lifecycles: enrollment cannot progress past available unless the
// store is active.
func TestEnrollment_RequiresActiveStore(t *testing.T) {
	notActive := []domain.UCPState{
		domain.UCPStateDraft,
		domain.UCPStateConnectorLinked,
		domain.UCPStateProfilePublished,
		domain.UCPStateVerified,
		domain.UCPStateSuspended,
	}

	for _, state := range notActive {
		t.Run(string(state), func(t *testing.T) {
			svc, ctx := newServiceWith(t, state, simpleRegistration(&fixedCheck{id: "thing", passed: true}, nil))

			_, err := svc.Enroll(ctx, testMerchant, testStore, "test_surface")
			var notActiveErr surface.ErrStoreNotActive
			require.ErrorAs(t, err, &notActiveErr)
			assert.Equal(t, state, notActiveErr.UCPState)

			// And it still reports the surface, marked blocked with a reason
			// — a merchant needs to see why, not just find nothing.
			e, err := svc.Get(ctx, testMerchant, testStore, "test_surface")
			require.NoError(t, err)
			assert.Equal(t, domain.SurfaceAvailable, e.State)
			assert.True(t, e.Blocked)
			assert.NotEmpty(t, e.BlockedReason)
		})
	}
}

// TestEnrollment_SurfacesAreIndependent is R9's other half: suspending one
// surface must not touch any other surface, and must not touch activation.
func TestEnrollment_SurfacesAreIndependent(t *testing.T) {
	repo := newMemRepo()
	act := &stubActivation{state: domain.UCPStateActive}
	registry, err := surface.NewRegistry(
		namedRegistration("surface_a"),
		namedRegistration("surface_b"),
	)
	require.NoError(t, err)
	svc := surface.NewSurfaceService(registry, repo, act, stubConnectors{}, nil, nil)
	ctx := context.Background()

	for _, id := range []string{"surface_a", "surface_b"} {
		e, err := svc.Enroll(ctx, testMerchant, testStore, id)
		require.NoError(t, err)
		require.Equal(t, domain.SurfaceEnrolled, e.State)
	}

	require.NoError(t, svc.Suspend(ctx, testMerchant, testStore, "surface_a", "surface said no"))

	a, err := svc.Get(ctx, testMerchant, testStore, "surface_a")
	require.NoError(t, err)
	assert.Equal(t, domain.SurfaceSuspended, a.State)
	assert.Equal(t, "surface said no", a.SuspensionReason)

	b, err := svc.Get(ctx, testMerchant, testStore, "surface_b")
	require.NoError(t, err)
	assert.Equal(t, domain.SurfaceEnrolled, b.State,
		"one surface's suspension must not reach another")

	assert.Equal(t, domain.UCPStateActive, act.state,
		"no surface may write activation state")
}

// TestEnrollment_WithdrawalLeavesActivationIntact pins the withdrawal
// semantics R16 requires: DELETE returns the surface to available and
// changes nothing about the store's activation or its other surfaces.
func TestEnrollment_WithdrawalLeavesActivationIntact(t *testing.T) {
	repo := newMemRepo()
	act := &stubActivation{state: domain.UCPStateActive}
	registry, err := surface.NewRegistry(namedRegistration("surface_a"), namedRegistration("surface_b"))
	require.NoError(t, err)
	svc := surface.NewSurfaceService(registry, repo, act, stubConnectors{}, nil, nil)
	ctx := context.Background()

	for _, id := range []string{"surface_a", "surface_b"} {
		_, err := svc.Enroll(ctx, testMerchant, testStore, id)
		require.NoError(t, err)
	}

	require.NoError(t, svc.Withdraw(ctx, testMerchant, testStore, "surface_a"))

	a, err := svc.Get(ctx, testMerchant, testStore, "surface_a")
	require.NoError(t, err)
	assert.Equal(t, domain.SurfaceAvailable, a.State)
	assert.Nil(t, a.EnrolledAt, "withdrawal clears the enrollment, not just the state")

	b, err := svc.Get(ctx, testMerchant, testStore, "surface_b")
	require.NoError(t, err)
	assert.Equal(t, domain.SurfaceEnrolled, b.State)

	assert.Equal(t, domain.UCPStateActive, act.state)

	// Withdrawing something never enrolled is a no-op, not an error.
	require.NoError(t, svc.Withdraw(ctx, testMerchant, testStore, "surface_b"))
	require.NoError(t, svc.Withdraw(ctx, testMerchant, testStore, "surface_b"))
}

// ── direct_agent (R6) ────────────────────────────────────────────────────

// TestDirectAgent_AutoEnrollsOnActive asserts the null surface enrolls
// itself the moment a store reaches active, with no merchant action — the
// mechanism that makes "active with no distribution partner" an explicit,
// visible condition rather than an empty page.
func TestDirectAgent_AutoEnrollsOnActive(t *testing.T) {
	repo := newMemRepo()
	act := &stubActivation{state: domain.UCPStateActive}
	registry, err := surface.NewRegistry(directagent.Registration())
	require.NoError(t, err)
	svc := surface.NewSurfaceService(registry, repo, act, stubConnectors{}, nil, nil)
	ctx := context.Background()

	require.NoError(t, svc.OnStoreActivated(ctx, testMerchant, testStore))

	e, err := svc.Get(ctx, testMerchant, testStore, directagent.SurfaceID)
	require.NoError(t, err)
	assert.Equal(t, domain.SurfaceEnrolled, e.State,
		"direct_agent requires nothing, so reaching active is the whole of its admission")
	assert.Empty(t, e.Descriptor.PrerequisiteChecks)
	assert.Empty(t, e.Descriptor.ExternalSteps)
	assert.Empty(t, e.Descriptor.Artifacts)

	// Idempotent: activation may be re-asserted at any time.
	require.NoError(t, svc.OnStoreActivated(ctx, testMerchant, testStore))
	e, err = svc.Get(ctx, testMerchant, testStore, directagent.SurfaceID)
	require.NoError(t, err)
	assert.Equal(t, domain.SurfaceEnrolled, e.State)
}

func TestDirectAgent_NotAutoEnrolledBeforeActive(t *testing.T) {
	repo := newMemRepo()
	registry, err := surface.NewRegistry(directagent.Registration())
	require.NoError(t, err)
	svc := surface.NewSurfaceService(registry, repo, &stubActivation{state: domain.UCPStateVerified}, stubConnectors{}, nil, nil)
	ctx := context.Background()

	require.NoError(t, svc.OnStoreActivated(ctx, testMerchant, testStore),
		"auto-enroll never fails loudly; it just doesn't happen")

	e, err := svc.Get(ctx, testMerchant, testStore, directagent.SurfaceID)
	require.NoError(t, err)
	assert.Equal(t, domain.SurfaceAvailable, e.State)
}

// ── Registry reporting ───────────────────────────────────────────────────

// TestEnrollment_ListReportsEveryRegisteredSurface asserts the registry —
// not the enrollment table — decides what exists. A surface nobody has
// touched still has to appear.
func TestEnrollment_ListReportsEveryRegisteredSurface(t *testing.T) {
	repo := newMemRepo()
	registry, err := surface.NewRegistry(
		directagent.Registration(),
		namedRegistration("surface_a"),
		namedRegistration("surface_b"),
	)
	require.NoError(t, err)
	svc := surface.NewSurfaceService(registry, repo, &stubActivation{state: domain.UCPStateActive}, stubConnectors{}, nil, nil)

	list, err := svc.List(context.Background(), testMerchant, testStore)
	require.NoError(t, err)
	require.Len(t, list, 3)

	for _, e := range list {
		assert.Equal(t, domain.SurfaceAvailable, e.State)
		assert.NotEmpty(t, e.Descriptor.SurfaceID, "every entry carries its descriptor so the console needs no per-surface knowledge")
		assert.NotEmpty(t, e.Descriptor.DisplayNameKey)
	}
}

func TestEnrollment_TenantIsolation(t *testing.T) {
	repo := newMemRepo()
	registry, err := surface.NewRegistry(namedRegistration("surface_a"))
	require.NoError(t, err)
	svc := surface.NewSurfaceService(registry, repo, &stubActivation{state: domain.UCPStateActive}, stubConnectors{}, nil, nil)
	ctx := context.Background()

	_, err = svc.Enroll(ctx, testMerchant, testStore, "surface_a")
	require.NoError(t, err)

	// The same store id under a different merchant sees nothing enrolled.
	e, err := svc.Get(ctx, domain.MerchantID("m-2"), testStore, "surface_a")
	require.NoError(t, err)
	assert.Equal(t, domain.SurfaceAvailable, e.State)
}

// ── helpers ──────────────────────────────────────────────────────────────

func newServiceWith(t *testing.T, state domain.UCPState, reg surface.Registration) (surface.Service, context.Context) {
	t.Helper()
	registry, err := surface.NewRegistry(reg)
	require.NoError(t, err)
	svc := surface.NewSurfaceService(registry, newMemRepo(), &stubActivation{state: state}, stubConnectors{}, nil, nil)
	return svc, context.Background()
}

// simpleRegistration builds a surface with one check, no external steps and
// no verification signal — so passing prerequisites is the whole admission
// and it reaches enrolled in a single call.
func simpleRegistration(check surface.PrerequisiteCheck, artifact surface.ArtifactProvider) surface.Registration {
	d := domain.SurfaceDescriptor{
		SurfaceID:          "test_surface",
		DisplayNameKey:     "test.name",
		Protocol:           domain.SurfaceProtocolUCP,
		PrerequisiteChecks: []string{check.ID()},
	}
	reg := surface.Registration{Descriptor: d, Checks: []surface.PrerequisiteCheck{check}}
	if artifact != nil {
		reg.Descriptor.Artifacts = []string{artifact.ID()}
		reg.Artifacts = []surface.ArtifactProvider{artifact}
	}
	return reg
}

// namedRegistration builds a trivially-admitting surface under a given id.
func namedRegistration(id string) surface.Registration {
	return surface.Registration{
		Descriptor: domain.SurfaceDescriptor{
			SurfaceID:      id,
			DisplayNameKey: "test.name." + id,
			Protocol:       domain.SurfaceProtocolUCP,
		},
	}
}
