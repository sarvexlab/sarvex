// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package checkoutescalation

import (
	"github.com/sarvexlab/sarvex/internal/pkg/checkoutcause"
)

// Diagnose classifies a terminal-failed, abandoned, or stalled-escalation
// session (R8). Pure and idempotent (R10): the same signal + evidence
// always produces the same Diagnosis, so it's safe to call exactly once,
// at the moment a session goes terminal or is flagged stalled, and store
// the result rather than recomputing it at read time.
//
// signal is the raw classification key — a checkout_service.go
// SessionError.Code for a failure, or one of checkoutcause.SignalEscalationStalled
// / checkoutcause.SignalBuyerAbandoned for a time-derived diagnosis.
// stageReached is the session's current_stage at the moment of diagnosis.
// evidence is the timeline seq values the caller has already identified as
// backing the conclusion (typically: the failure/escalation event's own
// seq, plus the last observed activity for an abandonment).
func Diagnose(signal, stageReached string, evidence []int64) Diagnosis {
	cause, summary, action := checkoutcause.Classify(signal)
	return Diagnosis{
		StageReached:         stageReached,
		Cause:                string(cause),
		PlainLanguageSummary: summary,
		RecommendedAction:    action,
		Evidence:             evidence,
	}
}
