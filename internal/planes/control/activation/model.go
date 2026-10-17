// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package activation

import (
	"time"

	"github.com/samber/lo"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// ActivationModel is the MongoDB document stored in the
// control_store_activation collection. _id is the store_id — one document
// per store. MerchantID is denormalized onto every document so reads can be
// scoped by (merchant_id, store_id) together: per CLAUDE.md invariant 4, a
// store_id alone must never be a sufficient key for a tenant-scoped read.
//
// R14 asks for activation state on the store document. It lives in its own
// collection instead, for the same reason the journey does: control_stores
// is written by StoreService on every display-name edit and connector link,
// and a second writer on the same document is exactly what invariant 3
// forbids. The activation API composes the two at the HTTP boundary; the
// store response carries ucp_state as a projected read (see api/store.go),
// which is what every consumer actually wanted from "on the store
// document".
type ActivationModel struct {
	ID               string                  `bson:"_id"`
	MerchantID       string                  `bson:"merchant_id"`
	UCPState         string                  `bson:"ucp_state"`
	Capabilities     []string                `bson:"capabilities"`
	ProfileURL       string                  `bson:"profile_url,omitempty"`
	SigningKeyID     string                  `bson:"signing_key_id,omitempty"`
	VerifiedAt       *time.Time              `bson:"verified_at,omitempty"`
	SuspensionReason string                  `bson:"suspension_reason,omitempty"`
	History          []ActivationHistoryItem `bson:"history"`
	EnteredStateAt   time.Time               `bson:"entered_state_at"`
	// LastVerification is the most recent verification run's result. Kept on
	// the record because verification runs as part of pre-validation, so the
	// console reads the checks back from here rather than from the response
	// of a verify call it never made.
	LastVerification *VerificationRunItem `bson:"last_verification,omitempty"`
	CreatedAt        time.Time            `bson:"created_at"`
	UpdatedAt        time.Time            `bson:"updated_at"`
}

// VerificationRunItem is a persisted VerificationReport.
type VerificationRunItem struct {
	Passed bool                   `bson:"passed"`
	State  string                 `bson:"ucp_state"`
	Checks []VerificationRunCheck `bson:"checks"`
	RanAt  string                 `bson:"ran_at"`
}

// VerificationRunCheck is one persisted check result.
type VerificationRunCheck struct {
	CheckID string `bson:"check_id"`
	Passed  bool   `bson:"passed"`
	Detail  string `bson:"detail,omitempty"`
}

// ActivationHistoryItem is one entry in the append-only state history.
type ActivationHistoryItem struct {
	State     string    `bson:"state"`
	EnteredAt time.Time `bson:"entered_at"`
}

// ToDomain renders the stored model as the API-facing activation view,
// including the per-step detail the console draws its progress from.
func (m *ActivationModel) ToDomain() domain.Activation {
	history := lo.Map(m.History, func(h ActivationHistoryItem, _ int) domain.ActivationHistoryEntry {
		return domain.ActivationHistoryEntry{
			State:     domain.UCPState(h.State),
			EnteredAt: h.EnteredAt,
		}
	})

	return domain.Activation{
		StoreID:          domain.StoreID(m.ID),
		UCPState:         domain.UCPState(m.UCPState),
		Capabilities:     m.Capabilities,
		ProfileURL:       m.ProfileURL,
		SigningKeyID:     m.SigningKeyID,
		VerifiedAt:       m.VerifiedAt,
		SuspendedReason:  m.SuspensionReason,
		EnteredStateAt:   m.EnteredStateAt,
		Steps:            m.steps(history),
		History:          history,
		LastVerification: m.lastVerification(),
	}
}

// lastVerification renders the persisted verification run for the API, or
// nil when the store has never been verified.
func (m *ActivationModel) lastVerification() *domain.VerificationReport {
	if m.LastVerification == nil {
		return nil
	}
	checks := lo.Map(m.LastVerification.Checks, func(c VerificationRunCheck, _ int) domain.VerificationCheck {
		return domain.VerificationCheck{CheckID: c.CheckID, Passed: c.Passed, Detail: c.Detail}
	})
	return &domain.VerificationReport{
		Passed: m.LastVerification.Passed,
		State:  domain.UCPState(m.LastVerification.State),
		Checks: checks,
		RanAt:  m.LastVerification.RanAt,
	}
}

// steps renders one entry per lifecycle stage: whether the store has ever
// occupied it, when it first did, and — for the one stage it is currently
// working toward — what is holding it there.
//
// "Reached" is answered from history rather than from the current state so
// a store that regressed still shows the ground it covered. A store that
// fell from verified back to profile_published has genuinely published a
// profile; pretending otherwise would misreport its position.
func (m *ActivationModel) steps(history []domain.ActivationHistoryEntry) []domain.ActivationStep {
	firstEntry := make(map[domain.UCPState]time.Time, len(history))
	for _, h := range history {
		if _, seen := firstEntry[h.State]; !seen {
			firstEntry[h.State] = h.EnteredAt
		}
	}

	current := domain.UCPState(m.UCPState)
	steps := make([]domain.ActivationStep, 0, len(order))
	for _, state := range order {
		step := domain.ActivationStep{State: state}
		if at, ok := firstEntry[state]; ok {
			step.Reached = true
			step.ReachedAt = &at
		}
		if state == current {
			step.Blocking = blockingReason(state)
		}
		steps = append(steps, step)
	}

	// A suspended store is not on any step of the progression, so the
	// blocking reason has nowhere to land above. Attach it to the furthest
	// step it did reach, which is where the console draws the marker.
	if current == domain.UCPStateSuspended {
		for i := len(steps) - 1; i >= 0; i-- {
			if steps[i].Reached {
				steps[i].Blocking = blockingReason(domain.UCPStateSuspended)
				break
			}
		}
	}

	return steps
}
