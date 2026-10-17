// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
	"golang.org/x/sync/errgroup"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/concurrency"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/monitoring"
)

const (
	taskProbeCycle = "endpointhealth:probe:cycle"
	taskProbeStore = "endpointhealth:probe:store"

	asynqQueue = "endpointhealth"

	// storeConcurrency bounds how many stores' probe runs execute at once
	// per replica — mirrors monitoring.storeConcurrency's rationale
	// (independent per-store I/O, no reason to serialize).
	storeConcurrency = 10

	// probeOverlapTTL bounds how long a per-store probe-running lease is
	// held before self-expiring, in case a replica crashes mid-run.
	probeOverlapTTL = 2 * time.Minute
)

// ProbeScheduler drives the probe battery across every store on a fixed
// interval. Leader election for "who enqueues this cycle" is asynq's
// TaskID+Unique dedup (every replica registers the same periodic task;
// Redis rejects the duplicate), the same mechanism
// internal/planes/sync/job_server.go uses for the reconciliation sweep —
// not a Redis-lease-held-by-one-process scheme. The per-store overlap
// guard (two cycles racing on a slow store) is a concurrency.Lease, same
// primitive the sync-plane sweeper uses for the same purpose.
type ProbeScheduler struct {
	server    *asynq.Server
	scheduler *asynq.Scheduler
	client    *asynq.Client

	runner   *ProbeRunner
	stores   monitoring.StoreLister
	lease    concurrency.Lease
	events   eventbus.EventBus
	interval time.Duration
	log      *slog.Logger
}

// NewProbeScheduler constructs a ProbeScheduler. interval defaults to
// Config.ProbeInterval when zero.
func NewProbeScheduler(redisConnOpt asynq.RedisConnOpt, runner *ProbeRunner, stores monitoring.StoreLister, lease concurrency.Lease, events eventbus.EventBus, interval time.Duration, log *slog.Logger) *ProbeScheduler {
	if interval <= 0 {
		interval = DefaultConfig().ProbeInterval
	}
	if log == nil {
		log = slog.Default()
	}
	return &ProbeScheduler{
		server:    asynq.NewServer(redisConnOpt, asynq.Config{Concurrency: storeConcurrency, Queues: map[string]int{asynqQueue: 1}}),
		scheduler: asynq.NewScheduler(redisConnOpt, nil),
		client:    asynq.NewClient(redisConnOpt),
		runner:    runner,
		stores:    stores,
		lease:     lease,
		events:    events,
		interval:  interval,
		log:       log,
	}
}

// Start registers the asynq handler and periodic cycle, then starts both
// the worker pool and the scheduler.
func (s *ProbeScheduler) Start() error {
	mux := asynq.NewServeMux()
	mux.HandleFunc(taskProbeCycle, s.handleCycle)

	if err := s.server.Start(mux); err != nil {
		return fmt.Errorf("asynq server start: %w", err)
	}

	cronSpec := "@every " + s.interval.String()
	if _, err := s.scheduler.Register(cronSpec,
		asynq.NewTask(taskProbeCycle, nil),
		asynq.Queue(asynqQueue),
		asynq.TaskID("probe:cycle"),
		asynq.Unique(s.interval),
	); err != nil {
		s.server.Shutdown()
		return fmt.Errorf("register probe cycle: %w", err)
	}

	if err := s.scheduler.Start(); err != nil {
		s.server.Shutdown()
		return fmt.Errorf("asynq scheduler start: %w", err)
	}
	return nil
}

// Stop gracefully shuts down the scheduler then the worker pool.
func (s *ProbeScheduler) Stop() {
	s.scheduler.Shutdown()
	s.server.Shutdown()
	_ = s.client.Close()
}

func (s *ProbeScheduler) handleCycle(ctx context.Context, _ *asynq.Task) error {
	s.RunCycle(ctx)
	return nil
}

// RunCycle probes every store once, bounded by storeConcurrency. Exported
// so tests and an immediate-on-startup run (matching
// monitoring.Scheduler.Run's "run once, then tick" behavior) can invoke it
// directly.
func (s *ProbeScheduler) RunCycle(ctx context.Context) {
	cursor := ""
	for {
		refs, hasMore, err := s.stores.ListAllStores(ctx, cursor, 50)
		if err != nil {
			s.log.Error("endpointhealth: list stores failed", "error", err)
			return
		}

		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(storeConcurrency)
		for _, ref := range refs {
			g.Go(func() error {
				if gctx.Err() != nil {
					return gctx.Err()
				}
				s.runStore(gctx, ref)
				return nil
			})
		}
		_ = g.Wait()

		if ctx.Err() != nil {
			return
		}
		if !hasMore || len(refs) == 0 {
			return
		}
		cursor = refs[len(refs)-1].StoreID
	}
}

// ProbeStore runs one store's probes immediately, outside the cycle — what
// ActivationSubscriber calls when a store enters verified so it does not
// wait most of a ProbeInterval to go active.
//
// It is runStore, exported: the lease runStore already takes means an
// out-of-band probe and a cycle that reaches the same store cannot
// double-probe it, and across replicas only one wins. A parallel path
// would have had to reimplement that, and would have drifted.
func (s *ProbeScheduler) ProbeStore(ctx context.Context, ref monitoring.StoreRef) {
	s.runStore(ctx, ref)
}

func (s *ProbeScheduler) runStore(ctx context.Context, ref monitoring.StoreRef) {
	holder := "probe:" + ref.StoreID
	leaseKey := "health:probe:running:" + ref.StoreID
	ok, err := s.lease.TryAcquire(ctx, leaseKey, holder, probeOverlapTTL)
	if err != nil {
		s.log.Error("endpointhealth: lease acquire failed", "error", err, "store_id", ref.StoreID)
		return
	}
	if !ok {
		// Another replica (or a previous, still-running cycle) already has
		// this store — skip rather than double-probe.
		return
	}
	defer func() {
		if err := s.lease.Release(context.Background(), leaseKey, holder); err != nil {
			s.log.Warn("endpointhealth: lease release failed", "error", err, "store_id", ref.StoreID)
		}
	}()

	results := s.runner.Run(ctx, ref.MerchantID, ref.StoreID, ref.Platform)
	now := time.Now().UTC()
	for _, r := range results {
		ev := &ProbeCompletedEvent{
			StoreID_:    ref.StoreID,
			MerchantID_: ref.MerchantID,
			ProbeType:   r.ProbeType,
			Outcome:     r.Outcome,
			LatencyMs:   r.LatencyMs,
			FailureCode: r.FailureCode,
			Detail:      r.Detail,
			Synthetic:   true,
			ObservedAt_: now,
		}
		if s.events != nil {
			if err := s.events.Publish(ctx, ev); err != nil {
				s.log.Warn("endpointhealth: publish probe result failed", "error", err, "store_id", ref.StoreID, "probe_type", r.ProbeType)
			}
		}
	}
}
