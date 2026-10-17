// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package surface_test

import (
	"context"
	"time"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/surface"
)

const (
	testMerchant = domain.MerchantID("m-1")
	testStore    = domain.StoreID("s-1")
)

// memRepo is an in-memory surface Repository, keyed the same way the Mongo
// one is: (merchant_id, store_id, surface_id) together.
type memRepo struct {
	docs map[string]*surface.EnrollmentModel
}

func newMemRepo() *memRepo { return &memRepo{docs: map[string]*surface.EnrollmentModel{}} }

func (r *memRepo) Get(_ context.Context, m domain.MerchantID, s domain.StoreID, surfaceID string) (*surface.EnrollmentModel, error) {
	doc, ok := r.docs[surface.DocumentID(m, s, surfaceID)]
	if !ok {
		return nil, surface.ErrNotFound
	}
	clone := *doc
	return &clone, nil
}

func (r *memRepo) ListByStore(_ context.Context, m domain.MerchantID, s domain.StoreID) ([]*surface.EnrollmentModel, error) {
	var out []*surface.EnrollmentModel
	for _, doc := range r.docs {
		if doc.MerchantID == string(m) && doc.StoreID == string(s) {
			clone := *doc
			out = append(out, &clone)
		}
	}
	return out, nil
}

func (r *memRepo) Upsert(_ context.Context, m *surface.EnrollmentModel) error {
	clone := *m
	r.docs[surface.DocumentID(domain.MerchantID(m.MerchantID), domain.StoreID(m.StoreID), m.SurfaceID)] = &clone
	return nil
}

func (r *memRepo) Delete(_ context.Context, m domain.MerchantID, s domain.StoreID, surfaceID string) error {
	delete(r.docs, surface.DocumentID(m, s, surfaceID))
	return nil
}

func (r *memRepo) ListStoresBySurfaceState(_ context.Context, surfaceID string, state domain.SurfaceState) ([]string, error) {
	var out []string
	for _, doc := range r.docs {
		if doc.SurfaceID == surfaceID && doc.State == string(state) {
			out = append(out, doc.StoreID)
		}
	}
	return out, nil
}

// stubActivation is the activation gate. It reports one state for every
// store — the whole of what the surface service is allowed to know about
// activation.
type stubActivation struct {
	state        domain.UCPState
	capabilities []string
}

func (s *stubActivation) Get(_ context.Context, m domain.MerchantID, storeID domain.StoreID) (domain.Activation, error) {
	return domain.Activation{
		StoreID:      storeID,
		UCPState:     s.state,
		Capabilities: s.capabilities,
	}, nil
}

// stubConnectors returns a fixed connector list.
type stubConnectors struct{ conns []domain.Connector }

func (s stubConnectors) ListByStore(context.Context, domain.MerchantID, domain.StoreID) ([]domain.Connector, error) {
	return s.conns, nil
}

// ── Fixture check/artifact/signal adapters ───────────────────────────────

// fixedCheck passes or fails on command. Used to drive prerequisite
// transitions deterministically.
type fixedCheck struct {
	id     string
	passed bool
	reason string
}

func (c *fixedCheck) ID() string { return c.id }

func (c *fixedCheck) Evaluate(context.Context, surface.CheckContext) domain.PrerequisiteResult {
	return domain.PrerequisiteResult{
		LabelKey:       "test.prereq." + c.id,
		Passed:         c.passed,
		Reason:         c.reason,
		RemediationKey: "test.remediation." + c.id,
		EvaluatedAt:    time.Now().UTC(),
	}
}

// flagSignal is a verification signal driven by a bool a test flips.
type flagSignal struct {
	id       string
	verified bool
}

func (s *flagSignal) ID() string { return s.id }

func (s *flagSignal) Verified(context.Context, surface.CheckContext) (bool, error) {
	return s.verified, nil
}
