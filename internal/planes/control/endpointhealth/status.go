// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"context"
	"fmt"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
)

// CheckState is one check's current state within a store's composite
// status document.
type CheckState struct {
	State                 Outcome
	LastPassAt            *time.Time
	LastFailAt            *time.Time
	CurrentValue          float64
	ConsecutiveBreaches   int
	ConsecutiveRecoveries int
}

// StatusDocument is the composite endpoint-health status for one store —
// the "endpoint_status" object composed into GET /stores/{id}/health
// alongside the existing consistency-focused domain.Health.
type StatusDocument struct {
	StoreID      string
	MerchantID   string
	Status       Severity
	EvaluatedAt  time.Time
	Checks       map[CheckType]CheckState
	ActiveAlerts []string
}

// StatusRepository persists the composite status document, one per store.
type StatusRepository interface {
	Get(ctx context.Context, merchantID, storeID string) (*StatusDocument, error)
	Upsert(ctx context.Context, doc *StatusDocument) error
}

// criticalChecks maps each check to the severity it produces when
// breaching, per R8's fixed mapping: profile_reachable and signature
// health are critical (Google downrank risk — the merchant is invisible
// or unusable to agents); everything else is degraded. No other levels
// exist.
func severityForCheck(check CheckType) Severity {
	switch check {
	case CheckProfileReachable, CheckSignatureRoundtrip, CheckSignatureFailureRate:
		return SeverityCritical
	default:
		return SeverityDegraded
	}
}

// StatusEngine applies one check's evaluation outcome to a store's
// composite status document with flap suppression: an alert opens only
// after FlapBreachThreshold consecutive breaching evaluations, and
// auto-resolves only after FlapRecoveryThreshold consecutive recovering
// evaluations. Recomputing the composite Status field from Checks is a
// pure function of the persisted per-check states (Evaluate itself is not
// pure — it mutates ConsecutiveBreaches/Recoveries and may open/resolve an
// alert — but re-deriving Status from a given Checks map always yields the
// same answer, which is what "idempotent" means for this document per R7).
type StatusEngine struct {
	repo   StatusRepository
	alerts *AlertService
	cfg    Config
	events eventbus.EventBus
}

// NewStatusEngine constructs a StatusEngine.
func NewStatusEngine(repo StatusRepository, alerts *AlertService, cfg Config, events eventbus.EventBus) *StatusEngine {
	return &StatusEngine{repo: repo, alerts: alerts, cfg: cfg, events: events}
}

// Evaluate folds one check's outcome (breaching or not, with the current
// metric value and a human-readable detail for a would-be alert) into
// storeID's composite status document, applying flap suppression and
// opening/resolving an alert on a threshold crossing.
func (e *StatusEngine) Evaluate(ctx context.Context, merchantID, storeID string, check CheckType, breaching bool, value float64, detail string, at time.Time) error {
	doc, err := e.repo.Get(ctx, merchantID, storeID)
	if err != nil {
		return fmt.Errorf("load status document: %w", err)
	}
	if doc == nil {
		doc = &StatusDocument{StoreID: storeID, MerchantID: merchantID, Checks: map[CheckType]CheckState{}}
	}
	if doc.Checks == nil {
		doc.Checks = map[CheckType]CheckState{}
	}

	state := doc.Checks[check]
	state.CurrentValue = value

	breachThreshold := e.cfg.FlapBreachThreshold
	if breachThreshold <= 0 {
		breachThreshold = DefaultConfig().FlapBreachThreshold
	}
	recoveryThreshold := e.cfg.FlapRecoveryThreshold
	if recoveryThreshold <= 0 {
		recoveryThreshold = DefaultConfig().FlapRecoveryThreshold
	}

	if breaching {
		state.ConsecutiveBreaches++
		state.ConsecutiveRecoveries = 0
		state.LastFailAt = &at
		if state.ConsecutiveBreaches >= breachThreshold {
			state.State = OutcomeFail
			if state.ConsecutiveBreaches == breachThreshold && e.alerts != nil {
				sev := severityForCheck(check)
				alert, isNew, err := e.alerts.OpenOrGetActive(ctx, merchantID, storeID, check, sev, detail)
				if err != nil {
					return fmt.Errorf("open alert: %w", err)
				}
				if isNew {
					doc.ActiveAlerts = appendUnique(doc.ActiveAlerts, alert.ID)
					if e.events != nil {
						_ = e.events.Publish(ctx, NewThresholdBreachedEvent(merchantID, storeID, check, sev, value, alert.ID))
					}
				}
			}
		} else {
			state.State = OutcomeDegraded
		}
	} else {
		state.ConsecutiveRecoveries++
		state.ConsecutiveBreaches = 0
		state.LastPassAt = &at
		// Mirrors the breach branch above: state.State only flips once the
		// threshold is actually met, not on the first recovering sample —
		// otherwise a single good sample would flip the displayed state to
		// "pass" while an alert for this exact check is still open, and
		// (worse) the *next* recovering sample would no longer be able to
		// tell "was this failing before?" from state.State, since it was
		// already overwritten — which is what let a real 2-recovery
		// auto-resolve regress into never firing.
		if state.ConsecutiveRecoveries >= recoveryThreshold {
			state.State = OutcomePass
			if state.ConsecutiveRecoveries == recoveryThreshold && e.alerts != nil {
				resolved, err := e.alerts.ResolveByCheck(ctx, merchantID, storeID, check)
				if err != nil {
					return fmt.Errorf("resolve alert: %w", err)
				}
				if resolved != nil {
					doc.ActiveAlerts = removeString(doc.ActiveAlerts, resolved.ID)
					if e.events != nil {
						_ = e.events.Publish(ctx, NewRecoveredEvent(merchantID, storeID, check, value, resolved.ID))
					}
				}
			}
		}
	}

	doc.Checks[check] = state
	doc.Status = deriveCompositeStatus(doc.Checks)
	doc.EvaluatedAt = at

	if err := e.repo.Upsert(ctx, doc); err != nil {
		return fmt.Errorf("save status document: %w", err)
	}
	return nil
}

// deriveCompositeStatus is the pure reduction from per-check states to the
// store's overall status: critical if any check in a critical-mapped
// category is failing, degraded if any check is failing/degraded,
// healthy otherwise.
func deriveCompositeStatus(checks map[CheckType]CheckState) Severity {
	anyDegraded := false
	for check, state := range checks {
		if state.State == OutcomeFail {
			if severityForCheck(check) == SeverityCritical {
				return SeverityCritical
			}
			anyDegraded = true
		}
		if state.State == OutcomeDegraded || state.State == OutcomeTimeout {
			anyDegraded = true
		}
	}
	if anyDegraded {
		return SeverityDegraded
	}
	return SeverityHealthy
}

func appendUnique(list []string, id string) []string {
	for _, existing := range list {
		if existing == id {
			return list
		}
	}
	return append(list, id)
}

func removeString(list []string, id string) []string {
	out := list[:0]
	for _, existing := range list {
		if existing != id {
			out = append(out, existing)
		}
	}
	return out
}
