// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/oklog/ulid"
)

// AlertStatus is the alert lifecycle state. Transitions are forward-only:
// open -> acknowledged -> resolved, or open -> resolved directly
// (auto-resolve). There is no path backward.
type AlertStatus string

const (
	AlertOpen         AlertStatus = "open"
	AlertAcknowledged AlertStatus = "acknowledged"
	AlertResolved     AlertStatus = "resolved"
)

// ErrInvalidTransition is returned when a caller requests a backward or
// otherwise disallowed alert status transition.
var ErrInvalidTransition = errors.New("endpointhealth: invalid alert status transition")

// ErrAlertNotFound is returned when no alert exists for the given id
// (scoped to the caller's merchant).
var ErrAlertNotFound = errors.New("endpointhealth: alert not found")

// Alert is one open/acknowledged/resolved incident for a {store, check}
// pair. At most one open alert exists per (store_id, check_type) at a
// time — AlertService.OpenOrGetActive enforces this by looking up any
// existing open alert before creating a new one.
type Alert struct {
	ID             string
	MerchantID     string
	StoreID        string
	CheckType      CheckType
	Severity       Severity
	Status         AlertStatus
	Detail         string
	OpenedAt       time.Time
	AcknowledgedAt *time.Time
	AcknowledgedBy string
	ResolvedAt     *time.Time
	AutoResolved   bool
}

// AlertRepository persists alerts.
type AlertRepository interface {
	Insert(ctx context.Context, a *Alert) error
	// GetOpenByCheck returns the currently open (or acknowledged — still
	// "active") alert for (storeID, checkType), if any.
	GetOpenByCheck(ctx context.Context, storeID string, checkType CheckType) (*Alert, error)
	Get(ctx context.Context, merchantID, alertID string) (*Alert, error)
	Update(ctx context.Context, a *Alert) error
	List(ctx context.Context, merchantID string, filter AlertListFilter, cursor string, limit int) ([]*Alert, bool, error)
}

// AlertListFilter narrows GET /alerts.
type AlertListFilter struct {
	Status   AlertStatus // "" = any
	Severity Severity    // "" = any
	StoreID  string      // "" = any
}

// AlertService owns alert lifecycle transitions and delivery. It is the
// only component that mutates alert status (status.go's flap-suppression
// engine calls into it; the control-plane HTTP handlers for acknowledge/
// resolve call into it too).
type AlertService struct {
	repo     AlertRepository
	notifier AlertNotifier
	log      *slog.Logger
}

// NewAlertService constructs an AlertService.
func NewAlertService(repo AlertRepository, notifier AlertNotifier, log *slog.Logger) *AlertService {
	if log == nil {
		log = slog.Default()
	}
	return &AlertService{repo: repo, notifier: notifier, log: log}
}

// OpenOrGetActive opens a new alert for (storeID, checkType) unless one is
// already open/acknowledged, in which case the existing alert is returned
// unchanged (idempotent — the flap-suppression engine calls this exactly
// once per breach transition, but redelivery of the underlying event must
// not open a second alert). isNew reports whether a new alert was created.
func (s *AlertService) OpenOrGetActive(ctx context.Context, merchantID, storeID string, checkType CheckType, severity Severity, detail string) (alert *Alert, isNew bool, err error) {
	existing, err := s.repo.GetOpenByCheck(ctx, storeID, checkType)
	if err != nil {
		return nil, false, fmt.Errorf("check existing alert: %w", err)
	}
	if existing != nil {
		return existing, false, nil
	}

	id := "alt_" + ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
	a := &Alert{
		ID:         id,
		MerchantID: merchantID,
		StoreID:    storeID,
		CheckType:  checkType,
		Severity:   severity,
		Status:     AlertOpen,
		Detail:     detail,
		OpenedAt:   time.Now().UTC(),
	}
	if err := s.repo.Insert(ctx, a); err != nil {
		return nil, false, fmt.Errorf("insert alert: %w", err)
	}

	s.notify(ctx, a, false)
	return a, true, nil
}

// Acknowledge transitions an open alert to acknowledged. Forward-only:
// fails with ErrInvalidTransition if the alert isn't currently open.
func (s *AlertService) Acknowledge(ctx context.Context, merchantID, alertID, by string) (*Alert, error) {
	a, err := s.repo.Get(ctx, merchantID, alertID)
	if err != nil {
		return nil, err
	}
	if a.Status != AlertOpen {
		return nil, ErrInvalidTransition
	}
	now := time.Now().UTC()
	a.Status = AlertAcknowledged
	a.AcknowledgedAt = &now
	a.AcknowledgedBy = by
	if err := s.repo.Update(ctx, a); err != nil {
		return nil, fmt.Errorf("update alert: %w", err)
	}
	return a, nil
}

// Resolve transitions an open or acknowledged alert to resolved. auto
// marks whether this was a system auto-resolve (flap-recovery) versus an
// operator/merchant action. Forward-only: fails if already resolved.
func (s *AlertService) Resolve(ctx context.Context, merchantID, alertID string, auto bool) (*Alert, error) {
	a, err := s.repo.Get(ctx, merchantID, alertID)
	if err != nil {
		return nil, err
	}
	if a.Status == AlertResolved {
		return nil, ErrInvalidTransition
	}
	now := time.Now().UTC()
	a.Status = AlertResolved
	a.ResolvedAt = &now
	a.AutoResolved = auto
	if err := s.repo.Update(ctx, a); err != nil {
		return nil, fmt.Errorf("update alert: %w", err)
	}
	s.notify(ctx, a, true)
	return a, nil
}

// ResolveByCheck auto-resolves the currently open alert for (storeID,
// checkType), if any. A no-op (not an error) when none is open — the
// flap-suppression engine calls this on every recovery-threshold crossing,
// and a check that never had an open alert is an expected outcome.
func (s *AlertService) ResolveByCheck(ctx context.Context, merchantID, storeID string, checkType CheckType) (*Alert, error) {
	existing, err := s.repo.GetOpenByCheck(ctx, storeID, checkType)
	if err != nil {
		return nil, fmt.Errorf("check existing alert: %w", err)
	}
	if existing == nil {
		return nil, nil
	}
	return s.Resolve(ctx, merchantID, existing.ID, true)
}

// List returns a merchant's alerts, paginated, per filter.
func (s *AlertService) List(ctx context.Context, merchantID string, filter AlertListFilter, cursor string, limit int) ([]*Alert, bool, error) {
	return s.repo.List(ctx, merchantID, filter, cursor, limit)
}

// ListOpenForStore returns every open/acknowledged alert for one store —
// used to populate GET /stores/{id}/health's active_alerts. hasMore is
// always false: a store realistically has a handful of open alerts at
// once (one per check_type), never enough to need pagination.
func (s *AlertService) ListOpenForStore(ctx context.Context, merchantID, storeID string) ([]*Alert, bool, error) {
	alerts, _, err := s.repo.List(ctx, merchantID, AlertListFilter{StoreID: storeID}, "", 100)
	if err != nil {
		return nil, false, err
	}
	active := make([]*Alert, 0, len(alerts))
	for _, a := range alerts {
		if a.Status == AlertOpen || a.Status == AlertAcknowledged {
			active = append(active, a)
		}
	}
	return active, false, nil
}

func (s *AlertService) notify(ctx context.Context, a *Alert, resolved bool) {
	if s.notifier == nil {
		return
	}
	if err := s.notifier.Notify(ctx, a, resolved); err != nil {
		s.log.Warn("endpointhealth: alert notification failed", "error", err, "alert_id", a.ID)
	}
}
