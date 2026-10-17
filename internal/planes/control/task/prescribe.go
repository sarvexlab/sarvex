// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package task

import (
	"context"
	"errors"
	"fmt"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// Prescribed tasks — the work a store is given because of where it is,
// rather than because something was detected about it: the first import,
// the first audit, Google's two verdicts, each checklist item, the
// pre-validation run.
//
// Every prescriber wants the same two operations, and before this they each
// wrote them out: check whether the task is already there, raise it if not,
// swallow the not-found on the way back down, and get the error handling
// subtly different from the last one. Ensure and Retire are that idiom,
// once, so a prescriber only has to describe *what* the task is and *while
// what* it should exist.
//
// Both are idempotent and safe to call on every pass. That is the point:
// prescription is a converge-to-state loop (journey.reconcileStageWork runs
// on every read), not a sequence of one-shot commands, so a store that
// skipped a transition — or predates a prescription existing — catches up
// on the next read without a backfill.

// Prescription describes a task that should be on the spine while some
// condition holds. It is RaiseTaskRequest with the spine's own conventions
// enforced rather than restated: the id is derived from the identity
// triple, so a prescriber cannot accidentally invent an id that disagrees
// with its own source_ref.
type Prescription struct {
	StoreID        domain.StoreID
	Source         domain.TaskSource
	SourceRef      string
	Title          string
	WhyItMatters   string
	Owner          domain.TaskOwner
	Resolution     domain.TaskResolution
	BlocksCheckout bool
}

// TaskID is the spine's stable identity for this prescription:
// source:store:source_ref, the same shape deriveTaskID builds for detected
// tasks. Deterministic by construction — that is what makes Ensure and
// Retire idempotent across repeated reconciliation, and what lets the
// console match presentation on source_ref.
func (p Prescription) TaskID() string {
	return fmt.Sprintf("%s:%s:%s", p.Source, p.StoreID, p.SourceRef)
}

func (p Prescription) raiseRequest() RaiseTaskRequest {
	sourceRef := p.SourceRef
	return RaiseTaskRequest{
		TaskID:         p.TaskID(),
		StoreID:        string(p.StoreID),
		Title:          p.Title,
		WhyItMatters:   p.WhyItMatters,
		Owner:          string(p.Owner),
		Resolution:     string(p.Resolution),
		BlocksCheckout: p.BlocksCheckout,
		Source:         string(p.Source),
		SourceRef:      &sourceRef,
	}
}

// Ensure puts p on the spine if it isn't already there.
//
// "Already there" includes a prescription the merchant has since finished:
// re-raising a task someone watched complete would reopen work that is
// done, which is what a store regressing back through the funnel would
// otherwise do to every milestone behind it.
func Ensure(ctx context.Context, svc Service, merchantID domain.MerchantID, p Prescription) error {
	taskID := p.TaskID()

	_, err := svc.Get(ctx, merchantID, taskID)
	switch {
	case err == nil:
		return nil // already raised, open or since finished
	case !errors.Is(err, ErrNotFound):
		return fmt.Errorf("check existing prescribed task %s: %w", taskID, err)
	}

	if _, err := svc.Raise(ctx, merchantID, p.raiseRequest()); err != nil {
		return fmt.Errorf("raise prescribed task %s: %w", taskID, err)
	}
	return nil
}

// Retire closes a prescribed task whose condition has cleared.
//
// It goes through Complete rather than Resolve, because the caller is the
// system reporting that the work finished — Google's verdict landed, the
// store left the state, the checklist item was verified. Resolve would
// refuse the external_wait prescriptions, which are exactly the ones only
// the system can ever finish.
//
// A task that was never raised is not an error: not every store passes
// through every state with every prescription in place, and Retire is
// called on each pass regardless.
func Retire(ctx context.Context, svc Service, merchantID domain.MerchantID, taskID string) error {
	if _, err := svc.Complete(ctx, merchantID, taskID, nil); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("retire prescribed task %s: %w", taskID, err)
	}
	return nil
}
