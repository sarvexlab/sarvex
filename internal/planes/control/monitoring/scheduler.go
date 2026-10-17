// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package monitoring

import (
	"context"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"
)

// StoreRef is the minimal identity a scheduled monitoring run needs for one store.
type StoreRef struct {
	MerchantID string
	StoreID    string
	Platform   string
}

// StoreLister enumerates stores across every merchant for the scheduler to
// iterate — a system-level, cross-tenant read, never exposed via a
// tenant-facing API (see store.StoreRepository.ListAll's own doc comment).
type StoreLister interface {
	ListAllStores(ctx context.Context, afterID string, limit int) ([]StoreRef, bool, error)
}

// storeListPageSize bounds how many stores the scheduler holds in memory at
// once while paging through StoreLister.
const storeListPageSize = 50

// storeConcurrency bounds how many stores' probe/sweep runs execute at once.
// Each run is independent per-store I/O (HTTP probes against that store's
// serving-plane endpoint, or a catalog diff), so processing the store list
// fully sequentially wastes wall-clock time for no correctness reason; a
// fixed limit keeps outbound connection/goroutine counts bounded regardless
// of how large the store list grows.
const storeConcurrency = 10

// startupGrace delays the first probe pass after the process starts.
//
// The pass used to run the instant the scheduler did. Probes reach sibling
// services over HTTP — the serving plane, and the merchant's plugin doorway,
// which itself proxies back to the sync plane — none of which is necessarily
// listening yet when the control plane finishes booting. Every restart
// therefore manufactured a critical finding ("the plugin isn't responding",
// from a 502 the doorway returns while the sync plane is still coming up)
// that then stood for a full probe interval before the next pass cleared it.
//
// Long enough for siblings to bind their ports, short enough that a genuine
// outage at boot is still caught in the first minute.
const startupGrace = 30 * time.Second

// Scheduler runs MonitoringLoop's probe and sweep passes across every store
// on their configured intervals. Single-process only: unlike the sync
// plane's own ReconciliationSweeper (which holds a Redis lease so exactly
// one instance runs a sweep), this scheduler has no leader election, so
// running more than one control-plane instance would duplicate work. That
// is a scaling concern for a later pass, not a correctness one — duplicate
// probe/sweep runs are idempotent (findings/health statements are upserted).
// StoreKeyEnsurer repairs a store's UCP signing keypair if it is missing.
// Satisfied by store.StoreService.EnsureSigningKeys; kept as a one-method
// interface so this package does not depend on the store service's surface.
type StoreKeyEnsurer interface {
	EnsureSigningKeys(ctx context.Context, storeID string) (string, error)
}

type Scheduler struct {
	loop   *MonitoringLoop
	stores StoreLister
	keys   StoreKeyEnsurer
	logger *slog.Logger
	// startupGrace delays the first probe pass; see the constant. Settable so
	// tests can run a pass without waiting.
	startupGrace time.Duration
}

// NewScheduler constructs a Scheduler.
func NewScheduler(loop *MonitoringLoop, stores StoreLister, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{loop: loop, stores: stores, logger: logger, startupGrace: startupGrace}
}

// WithStartupGrace overrides the delay before the first probe pass. Zero runs
// the first pass immediately.
func (s *Scheduler) WithStartupGrace(d time.Duration) *Scheduler {
	s.startupGrace = d
	return s
}

// WithKeyEnsurer wires signing-key reconciliation. Left unwired, the pass is
// skipped and the scheduler behaves exactly as before.
func (s *Scheduler) WithKeyEnsurer(k StoreKeyEnsurer) *Scheduler {
	s.keys = k
	return s
}

// Run blocks, running a probe+sweep pass after the startup grace and then
// ticking on MonitoringLoop's configured intervals, until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	// Neither the sweep nor key reconciliation talks to a sibling service —
	// both are Mongo/Redis work — so they run straight away; only probes wait
	// out the startup grace. Keys are reconciled before the first probe pass
	// on purpose: the profile probe asserts signing_keys is present, and
	// there is no point measuring a thing we are about to repair.
	s.reconcileKeys(ctx)
	s.runSweep(ctx)

	if s.startupGrace > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.startupGrace):
		}
	}
	s.runProbes(ctx)

	probeTicker := time.NewTicker(s.loop.ProbeInterval())
	defer probeTicker.Stop()
	sweepTicker := time.NewTicker(s.loop.SweepInterval())
	defer sweepTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-probeTicker.C:
			s.reconcileKeys(ctx)
			s.runProbes(ctx)
		case <-sweepTicker.C:
			s.runSweep(ctx)
		}
	}
}

// reconcileKeys repairs any store missing its UCP signing keypair.
//
// Provisioning used to happen exactly once, inside store creation at pairing.
// Nothing re-ran it, so a store that lost its keys — Redis eviction under the
// default jwkmanagement mode, a flushed environment, a restore — published a
// profile with no signing_keys from then on, and never recovered. The loss is
// silent by construction: MongoCatalogSource.activeSigningKeys returns an
// empty slice rather than failing the rebuild, and the field is omitempty, so
// the profile stays well-formed while no agent can verify anything signed
// with it.
//
// Runs before each probe pass rather than inside it: the profile probe
// asserts signing_keys is present, and repairing the thing it asserts is a
// separate concern from measuring it. Idempotent — a store that already has
// an active key costs one read.
func (s *Scheduler) reconcileKeys(ctx context.Context) {
	if s.keys == nil {
		return
	}
	s.forEachStore(ctx, func(ctx context.Context, ref StoreRef) {
		if _, err := s.keys.EnsureSigningKeys(ctx, ref.StoreID); err != nil {
			s.logger.Error("failed to reconcile store signing keys", "error", err, "store_id", ref.StoreID)
		}
	})
}

func (s *Scheduler) runProbes(ctx context.Context) {
	s.forEachStore(ctx, func(ctx context.Context, ref StoreRef) {
		if _, err := s.loop.RunProbesForStore(ctx, ref.MerchantID, ref.StoreID, ref.Platform); err != nil {
			s.logger.Error("scheduled probe run failed", "error", err, "store_id", ref.StoreID)
		}
	})
}

func (s *Scheduler) runSweep(ctx context.Context) {
	s.forEachStore(ctx, func(ctx context.Context, ref StoreRef) {
		if _, err := s.loop.SweepStore(ctx, ref.MerchantID, ref.StoreID); err != nil {
			s.logger.Error("scheduled sweep run failed", "error", err, "store_id", ref.StoreID)
		}
	})
}

func (s *Scheduler) forEachStore(ctx context.Context, fn func(context.Context, StoreRef)) {
	cursor := ""
	for {
		refs, hasMore, err := s.stores.ListAllStores(ctx, cursor, storeListPageSize)
		if err != nil {
			s.logger.Error("scheduler: list stores failed", "error", err)
			return
		}

		// fn itself never returns an error (runProbes/runSweep already log
		// and swallow their own failures per store); the group is only used
		// to bound concurrency and to fan out ctx cancellation to every
		// in-flight store the moment it's cancelled.
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(storeConcurrency)
		for _, ref := range refs {
			g.Go(func() error {
				if gctx.Err() != nil {
					return gctx.Err()
				}
				fn(gctx, ref)
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
