// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/samber/lo"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/sarvexlab/sarvex/internal/pkg/concurrency"
	"github.com/sarvexlab/sarvex/internal/pkg/email"
	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/utils"
	"github.com/sarvexlab/sarvex/internal/planes/control/billing"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/monitoring"
	"github.com/sarvexlab/sarvex/internal/planes/control/store"
)

// This file wires the endpoint-health probe and evaluator workers as
// background components of the control-plane process (started from
// internal/planes/control/api/entry.go's Init) rather than separate
// --service values — every background worker in this repo now follows this
// convention (see internal/planes/control/ledger/entry.go's identical
// note).

// ProbeProfileRoutePath is where the probe's own reference profile is
// served, relative to the control plane's route group (see
// internal/planes/control/api/health.go's getProbeProfile) — reachable at a
// stable, documented URL regardless of how many control-plane replicas are
// running.
const ProbeProfileRoutePath = "/_probe/.well-known/ucp"

// storeLister adapts store.StoreRepository.ListAll to monitoring.StoreLister
// — the same cross-tenant scheduler-only read the control plane's own
// monitoring scheduler already uses (see control/api/entry.go's
// monitoringStoreLister, unexported there — this is that same small
// adapter, redefined here since the two packages don't import each other).
type storeLister struct{ repo store.StoreRepository }

func (l storeLister) ListAllStores(ctx context.Context, afterID string, limit int) ([]monitoring.StoreRef, bool, error) {
	models, hasMore, err := l.repo.ListAll(ctx, afterID, limit)
	if err != nil {
		return nil, false, err
	}
	refs := lo.Map(models, func(m *store.StoreModel, _ int) monitoring.StoreRef {
		return monitoring.StoreRef{MerchantID: m.MerchantID, StoreID: m.ID, Platform: string(m.Platform)}
	})
	return refs, hasMore, nil
}

// mongoStoreRefResolver adapts store.StoreRepository.Get to
// storeRefResolver, for probing a single store named by an event rather
// than iterating every store.
//
// Get, not GetByD: it is fenced on merchant_id as well as store_id
// (docs/TENANCY-PATTERNS.md ADR-2 — fence at the query, not at the
// handler), and the activation event carries both ids, so there is no
// reason to reach for the unfenced lookup here. storeLister above is
// cross-tenant because a cycle genuinely has no tenant; this does.
type mongoStoreRefResolver struct{ repo store.StoreRepository }

func (r mongoStoreRefResolver) ResolveStoreRef(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) (monitoring.StoreRef, error) {
	m, err := r.repo.Get(ctx, merchantID, storeID)
	if err != nil {
		return monitoring.StoreRef{}, err
	}
	return monitoring.StoreRef{MerchantID: m.MerchantID, StoreID: m.ID, Platform: string(m.Platform)}, nil
}

// preferencesAdapter reads alert delivery preferences directly from
// billing.NotificationPrefsRepository — deliberately not through the full
// billing.Service (which also needs a Stripe client the evaluator has no
// other reason to construct); notification preferences are a plain Mongo
// read with a documented "zero-value defaults when none exists" contract.
type preferencesAdapter struct {
	repo billing.NotificationPrefsRepository
}

func (a preferencesAdapter) Get(ctx context.Context, merchantID string) (Preferences, error) {
	m, err := a.repo.Get(ctx, domain.MerchantID(merchantID))
	if err != nil {
		return Preferences{}, err
	}
	prefs := m.ToDomain()
	floor := Severity(prefs.AlertSeverityFloor)
	if floor == "" {
		floor = SeverityCritical
	}
	var quiet *QuietHours
	if prefs.QuietHoursStartHour != nil && prefs.QuietHoursEndHour != nil {
		quiet = &QuietHours{StartHour: *prefs.QuietHoursStartHour, EndHour: *prefs.QuietHoursEndHour, Timezone: prefs.QuietHoursTimezone}
	}
	return Preferences{Recipients: prefs.AlertRecipients, SeverityFloor: floor, Quiet: quiet}, nil
}

// ── worker-health-probe ─────────────────────────────────────────────────

// ProbeWorkerDeps are the already-connected dependencies control/api/entry.go
// passes in — Identity is the same probe identity control/api/entry.go
// already loads to serve GET /_probe/.well-known/ucp, reused here rather
// than loaded a second time; StoreRepo is the same repository instance the
// control plane's other routes already use.
type ProbeWorkerDeps struct {
	Config    *utils.Configurations
	Log       *slog.Logger
	DB        *mongo.Database
	Identity  *Identity
	StoreRepo store.StoreRepository
	Redis     *redis.Client
	AsynqOpt  asynq.RedisConnOpt
}

// StartProbeWorker starts the endpoint-health synthetic probe worker: the
// ProbeScheduler and the HistorySubscriber. It owns its own EventBus
// connection (a distinct ConsumerGroup from the control plane's main bus)
// so that horizontally-scaled control-plane replicas still distribute probe
// work as competing consumers, exactly as when this ran as a separate
// process.
func StartProbeWorker(ctx context.Context, deps ProbeWorkerDeps) (stop func(context.Context) error, err error) {
	eventBus, err := eventbus.New(eventbus.Config{
		ConsumerGroup: "endpoint_health_consumer_group",
		Logger:        eventbus.StdLogger(deps.Config.LogLevel == "DEBUG"),
		NATS:          eventbus.NATSConfig{URL: deps.Config.NATSURL},
	})
	if err != nil {
		return nil, fmt.Errorf("create event bus: %w", err)
	}

	cfg := DefaultConfig()
	// ServingOrigin, not ServingBaseURL: NewProbeRunner builds store URLs with
	// ucp.StoreEndpointURL, which supplies the /ucp prefix itself. Passing the
	// raw config value requests /ucp/ucp/... and 404s every scheduled probe —
	// which silently prevents any store reaching active, since the passing
	// profile probe is the only thing that promotes it.
	runner := NewProbeRunner(deps.Config.ServingOrigin(), deps.Identity, cfg)
	lease := concurrency.NewRedisLease(deps.Redis, "endpointhealth:probe:")

	scheduler := NewProbeScheduler(deps.AsynqOpt, runner, storeLister{repo: deps.StoreRepo}, lease, eventBus, cfg.ProbeInterval, deps.Log)
	if err := scheduler.Start(); err != nil {
		return nil, fmt.Errorf("start probe scheduler: %w", err)
	}
	go scheduler.RunCycle(ctx) // immediate first pass, then ticks per its own interval

	historyRepo, err := NewMongoHistoryRepository(deps.DB)
	if err != nil {
		return nil, fmt.Errorf("history repository: %w", err)
	}
	historySub := NewHistorySubscriber(historyRepo, eventBus, deps.Log)
	if err := historySub.Start(ctx); err != nil {
		return nil, fmt.Errorf("start history subscriber: %w", err)
	}

	// Probes a store as soon as it verifies, rather than leaving it to the
	// next cycle up to ProbeInterval away — see ActivationSubscriber.
	activationSub := NewActivationSubscriber(scheduler, mongoStoreRefResolver{repo: deps.StoreRepo}, eventBus, deps.Log)
	if err := activationSub.Start(ctx); err != nil {
		return nil, fmt.Errorf("start activation subscriber: %w", err)
	}

	stop = func(ctx context.Context) error {
		scheduler.Stop()
		return errors.Join(activationSub.Stop(ctx), historySub.Stop(ctx), eventBus.Close())
	}
	return stop, nil
}

// ── worker-health-evaluator ─────────────────────────────────────────────

// EvaluatorWorkerDeps are the already-connected dependencies
// control/api/entry.go passes in — StatusRepo/AlertRepo/NotificationPrefsRepo
// are the same repository instances the control plane's read API and
// billing service already use. The evaluator builds its own AlertService
// wrapping AlertRepo with a real email notifier — deliberately separate
// from the log-only AlertService the control-plane read API uses for
// merchant-initiated acknowledge/resolve actions (a merchant acting on
// their own alert doesn't need to be emailed about it).
type EvaluatorWorkerDeps struct {
	Config                *utils.Configurations
	Log                   *slog.Logger
	StatusRepo            StatusRepository
	AlertRepo             AlertRepository
	NotificationPrefsRepo billing.NotificationPrefsRepository
	StoreRepo             store.StoreRepository
	Redis                 *redis.Client
	AsynqOpt              asynq.RedisConnOpt
}

func buildAlertService(deps EvaluatorWorkerDeps) (*AlertService, error) {
	emailClient, err := email.NewClient(email.Config{Mode: email.Mode(deps.Config.EmailMode), ResendAPIKey: deps.Config.ResendApiKey})
	if err != nil {
		deps.Log.Warn("endpointhealth: email client unavailable, alert delivery falling back to log-only", "error", err)
		return NewAlertService(deps.AlertRepo, NewLoggingAlertNotifier(deps.Log), deps.Log), nil
	}
	sender := email.NewSender(emailClient, deps.Config.SenderEmailAddress, deps.Config.SenderTitle)
	notifier := NewEmailAlertNotifier(sender, preferencesAdapter{repo: deps.NotificationPrefsRepo}, deps.Config.DashboardAddress, DefaultConfig().DegradedBatchWindow, deps.Log)
	return NewAlertService(deps.AlertRepo, notifier, deps.Log), nil
}

// StartEvaluatorWorker starts the endpoint-health real-traffic evaluator
// worker. It owns its own EventBus connection (a distinct ConsumerGroup
// from the control plane's main bus) so that horizontally-scaled
// control-plane replicas still distribute evaluation work as competing
// consumers, exactly as when this ran as a separate process.
func StartEvaluatorWorker(ctx context.Context, deps EvaluatorWorkerDeps) (stop func(context.Context) error, err error) {
	eventBus, err := eventbus.New(eventbus.Config{
		ConsumerGroup: "endpoint_health_consumer_group",
		Logger:        eventbus.StdLogger(deps.Config.LogLevel == "DEBUG"),
		NATS:          eventbus.NATSConfig{URL: deps.Config.NATSURL},
	})
	if err != nil {
		return nil, fmt.Errorf("create event bus: %w", err)
	}

	cfg := DefaultConfig()
	alertSvc, err := buildAlertService(deps)
	if err != nil {
		return nil, fmt.Errorf("alert service: %w", err)
	}
	statusEngine := NewStatusEngine(deps.StatusRepo, alertSvc, cfg, eventBus)
	window := NewWindow(deps.Redis, "endpointhealth:", cfg.RollingWindow)

	evaluator := NewEvaluator(deps.AsynqOpt, eventBus, window, statusEngine, storeLister{repo: deps.StoreRepo}, deps.Redis, cfg, deps.Log)
	if err := evaluator.Start(ctx); err != nil {
		return nil, fmt.Errorf("start evaluator: %w", err)
	}

	stop = func(ctx context.Context) error {
		evaluator.Stop(ctx)
		return eventBus.Close()
	}
	return stop, nil
}
