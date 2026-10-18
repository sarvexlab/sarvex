// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package surface_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgdomain "github.com/sarvexlab/sarvex/internal/pkg/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/surface"
	"github.com/sarvexlab/sarvex/internal/planes/control/surface/directagent"
)

// R7, the test the whole abstraction exists to pass.
//
// A fixture surface with unusual prerequisites, no artifacts at
// all, and a completely different verification signal must enroll end to
// end without one line of core code changing. Everything below is written
// against the public registry API only: a descriptor, three small adapter
// types, and the same service every other surface uses.
//
// If making this pass ever requires touching activation, the enrollment
// state machine, the API layer or the console's routing, the abstraction is
// wrong and the fix belongs there — not here.

// ── The fixture surface: an unusual shape ─────────────────────────────

const fixtureSurfaceID = "fixture_marketplace"

// partnerAPICheck is a prerequisite of an unusual kind: it
// reads merchant-supplied per-surface config rather than a checklist item
// or a feed record.
type partnerAPICheck struct{}

func (partnerAPICheck) ID() string { return "fixture_partner_api_key" }

func (partnerAPICheck) Evaluate(_ context.Context, cc surface.CheckContext) domain.PrerequisiteResult {
	res := domain.PrerequisiteResult{
		LabelKey:       "fixture.prereq.apiKey",
		EvaluatedAt:    time.Now().UTC(),
		RemediationKey: "fixture.remediation.apiKey",
	}
	if cc.Config["partner_api_key"] == "" {
		res.Reason = "no partner API key configured"
		return res
	}
	res.Passed = true
	res.RemediationKey = ""
	return res
}

// regionCheck is a second, differently-shaped prerequisite — it consults
// the store's declared capabilities.
type regionCheck struct{}

func (regionCheck) ID() string { return "fixture_region_supported" }

func (regionCheck) Evaluate(_ context.Context, cc surface.CheckContext) domain.PrerequisiteResult {
	res := domain.PrerequisiteResult{
		LabelKey:    "fixture.prereq.region",
		EvaluatedAt: time.Now().UTC(),
	}
	for _, c := range cc.Capabilities {
		if c == string(pkgdomain.CapabilityTypeCart) {
			res.Passed = true
			return res
		}
	}
	res.Reason = "this marketplace requires cart support"
	return res
}

// partnerCallbackSignal is a verification signal of an entirely different
// kind from Google's "they fetched our feed": an inbound callback the
// partner makes, recorded as config.
type partnerCallbackSignal struct{}

func (partnerCallbackSignal) ID() string { return "fixture_partner_callback" }

func (partnerCallbackSignal) Verified(_ context.Context, cc surface.CheckContext) (bool, error) {
	return cc.Config["partner_callback_received"] == "true", nil
}

// fixtureRegistration is the whole of what adding this surface takes.
func fixtureRegistration() surface.Registration {
	return surface.Registration{
		Descriptor: domain.SurfaceDescriptor{
			SurfaceID:      fixtureSurfaceID,
			DisplayNameKey: "fixture.name",
			DescriptionKey: "fixture.description",
			Protocol:       domain.SurfaceProtocolUCP,
			// Different required capability from direct_agent, and no
			// required connector at all.
			RequiredCapabilities: []string{string(pkgdomain.CapabilityTypeCheckout)},
			PrerequisiteChecks: []string{
				"fixture_partner_api_key",
				"fixture_region_supported",
			},
			// No artifacts: Sarvex generates nothing for this surface.
			Artifacts: nil,
			ExternalSteps: []domain.SurfaceExternalStep{
				{
					StepID:   "fixture_partner_agreement",
					LabelKey: "fixture.step.agreement",
					URL:      "https://partner.example/agreement",
					Owner:    domain.SurfaceStepOwnerMerchant,
				},
			},
			VerificationSignal: "fixture_partner_callback",
		},
		Checks:       []surface.PrerequisiteCheck{partnerAPICheck{}, regionCheck{}},
		Verification: partnerCallbackSignal{},
	}
}

// ── The test ─────────────────────────────────────────────────────────────

// TestFixtureSurface_EnrollsEndToEndWithNoCoreChanges drives the fixture
// surface through its whole lifecycle using only the generic service.
func TestFixtureSurface_EnrollsEndToEndWithNoCoreChanges(t *testing.T) {
	// Registered alongside the real surfaces, not instead of them: adding
	// one must not disturb the others.
	registry, err := surface.NewRegistry(
		directagent.Registration(),
		fixtureRegistration(),
	)
	require.NoError(t, err)

	act := &stubActivation{
		state: domain.UCPStateActive,
		capabilities: []string{
			string(pkgdomain.CapabilityTypeCheckout),
			string(pkgdomain.CapabilityTypeCart),
		},
	}
	svc := surface.NewSurfaceService(registry, newMemRepo(), act, stubConnectors{}, nil, nil)
	ctx := context.Background()

	// It appears in the registry response with no special casing.
	descriptors := svc.Registry(ctx)
	ids := make([]string, 0, len(descriptors))
	for _, d := range descriptors {
		ids = append(ids, d.SurfaceID)
	}
	assert.Contains(t, ids, fixtureSurfaceID)
	assert.Contains(t, ids, directagent.SurfaceID)

	// Enrolling with no config: the API-key prerequisite fails, so it
	// holds at prerequisites_pending with a merchant-readable reason.
	e, err := svc.Enroll(ctx, testMerchant, testStore, fixtureSurfaceID)
	require.NoError(t, err)
	assert.Equal(t, domain.SurfacePrerequisitesPending, e.State)

	byID := prerequisitesByID(e)
	require.Contains(t, byID, "fixture_partner_api_key")
	assert.False(t, byID["fixture_partner_api_key"].Passed)
	assert.Equal(t, "no partner API key configured", byID["fixture_partner_api_key"].Reason)
	assert.True(t, byID["fixture_region_supported"].Passed)
	// The descriptor's required capability is enforced as an ordinary
	// prerequisite row, with no adapter of its own.
	assert.True(t, byID["capability:"+string(pkgdomain.CapabilityTypeCheckout)].Passed)

	// Supplying config satisfies the check and advances — through the same
	// generic config endpoint.
	e, err = svc.SetConfig(ctx, testMerchant, testStore, fixtureSurfaceID, map[string]string{
		"partner_api_key": "pk_test_123",
	})
	require.NoError(t, err)
	assert.Equal(t, domain.SurfaceConfigured, e.State)
	assert.Empty(t, e.Artifacts, "this surface generates nothing, and nothing invents an artifact for it")

	// External step submitted → verifying. The callback has not arrived.
	e, err = svc.Submit(ctx, testMerchant, testStore, fixtureSurfaceID)
	require.NoError(t, err)
	assert.Equal(t, domain.SurfaceVerifying, e.State)

	// The partner calls back — a signal with nothing in common with
	// Google's feed fetch.
	e, err = svc.SetConfig(ctx, testMerchant, testStore, fixtureSurfaceID, map[string]string{
		"partner_api_key":           "pk_test_123",
		"partner_callback_received": "true",
	})
	require.NoError(t, err)
	assert.Equal(t, domain.SurfaceEnrolled, e.State)
	require.NotNil(t, e.EnrolledAt)

	// Adding it changed nothing for the surfaces already there.
	da, err := svc.Get(ctx, testMerchant, testStore, directagent.SurfaceID)
	require.NoError(t, err)
	assert.Equal(t, domain.SurfaceAvailable, da.State)

	// And withdrawal works the same way it does for every other surface.
	require.NoError(t, svc.Withdraw(ctx, testMerchant, testStore, fixtureSurfaceID))
	e, err = svc.Get(ctx, testMerchant, testStore, fixtureSurfaceID)
	require.NoError(t, err)
	assert.Equal(t, domain.SurfaceAvailable, e.State)
}

// TestFixtureSurface_UsesOnlyPublicRegistryAPI is the structural half of
// R7: the fixture above is built entirely from exported types, so nothing
// about registering a surface depends on privileged access to this
// package's internals. If a future surface needed an unexported hook, this
// file would not compile.
func TestFixtureSurface_UsesOnlyPublicRegistryAPI(t *testing.T) {
	reg := fixtureRegistration()

	// Descriptor is plain data.
	assert.Equal(t, fixtureSurfaceID, reg.Descriptor.SurfaceID)
	assert.Len(t, reg.Descriptor.PrerequisiteChecks, 2)
	assert.Empty(t, reg.Descriptor.Artifacts)

	// Adapters satisfy the three exported interfaces and nothing more.
	var _ surface.PrerequisiteCheck = partnerAPICheck{}
	var _ surface.PrerequisiteCheck = regionCheck{}
	var _ surface.VerificationSignal = partnerCallbackSignal{}

	registry, err := surface.NewRegistry(reg)
	require.NoError(t, err)
	got, ok := registry.Get(fixtureSurfaceID)
	require.True(t, ok)
	assert.Equal(t, reg.Descriptor.VerificationSignal, got.Verification.ID())
}

// ── Registry validation ──────────────────────────────────────────────────

// TestRegistry_RejectsIncoherentDescriptors asserts a descriptor that names
// something with no adapter behind it fails at construction rather than
// producing a surface no store can ever be admitted to.
func TestRegistry_RejectsIncoherentDescriptors(t *testing.T) {
	cases := []struct {
		name string
		reg  surface.Registration
		want string
	}{
		{
			name: "check with no adapter",
			reg: surface.Registration{Descriptor: domain.SurfaceDescriptor{
				SurfaceID:          "x",
				PrerequisiteChecks: []string{"nobody_implements_this"},
			}},
			want: "has no adapter",
		},
		{
			name: "artifact with no provider",
			reg: surface.Registration{Descriptor: domain.SurfaceDescriptor{
				SurfaceID: "x",
				Artifacts: []string{"phantom"},
			}},
			want: "has no provider",
		},
		{
			name: "verification signal with no adapter",
			reg: surface.Registration{Descriptor: domain.SurfaceDescriptor{
				SurfaceID:          "x",
				VerificationSignal: "phantom",
			}},
			want: "has no adapter",
		},
		{
			name: "free-form capability identifier",
			reg: surface.Registration{Descriptor: domain.SurfaceDescriptor{
				SurfaceID:            "x",
				RequiredCapabilities: []string{"we.just.made.this.up"},
			}},
			want: "unknown capability identifier",
		},
		{
			name: "no surface id",
			reg:  surface.Registration{Descriptor: domain.SurfaceDescriptor{}},
			want: "no surface_id",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := surface.NewRegistry(tc.reg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestRegistry_RejectsDuplicateSurfaceID(t *testing.T) {
	_, err := surface.NewRegistry(namedRegistration("dupe"), namedRegistration("dupe"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate surface_id")
}

// TestRegistry_CapabilitiesAreCanonicalOnly asserts every registered
// surface's required capabilities come from internal/pkg/domain — the
// hard rule that capability identifiers are never free-form.
func TestRegistry_CapabilitiesAreCanonicalOnly(t *testing.T) {
	registry, err := surface.NewRegistry(
		directagent.Registration(),
		fixtureRegistration(),
	)
	require.NoError(t, err)

	canonical := map[string]bool{}
	for _, c := range []pkgdomain.CapabilityType{
		pkgdomain.CapabilityTypeCheckout,
		pkgdomain.CapabilityTypeOrder,
		pkgdomain.CapabilityTypeCatalog,
		pkgdomain.CapabilityTypeCart,
		pkgdomain.CapabilityTypeIdentityLinking,
	} {
		canonical[string(c)] = true
	}

	for _, d := range registry.Descriptors() {
		for _, capID := range d.RequiredCapabilities {
			assert.Truef(t, canonical[capID],
				"surface %s requires %q, which is not a canonical capability identifier", d.SurfaceID, capID)
		}
	}
}

func prerequisitesByID(e domain.SurfaceEnrollment) map[string]domain.PrerequisiteResult {
	out := make(map[string]domain.PrerequisiteResult, len(e.Prerequisites))
	for _, p := range e.Prerequisites {
		out[p.CheckID] = p
	}
	return out
}
