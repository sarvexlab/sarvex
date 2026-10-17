// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package webbaseline

import (
	"context"
	"log/slog"
	"time"
)

// DefaultSyncInterval is how often Syncer re-runs — daily (R11: "sync via
// the sync plane on a schedule (default daily), not on the hot path").
const DefaultSyncInterval = 24 * time.Hour

// defaultListBatchLimit bounds how many stores one ListAllStores page
// returns, mirroring monitoring.Scheduler's pagination.
const defaultListBatchLimit = 100

// Syncer iterates every store on a daily schedule and writes that store's
// BaselineDay for the most recently completed UTC day (yesterday relative
// to the run — "today" is still in progress and would misrepresent a
// partial day as final). For each store it resolves the connector via
// resolve and, only if that connector implements Source, records a
// connector_reported day; otherwise it honestly records confidence=
// unavailable (R12) rather than estimating.
type Syncer struct {
	store    Store
	lister   StoreLister
	resolve  SourceResolver
	log      *slog.Logger
	interval time.Duration
	now      func() time.Time
}

// NewSyncer constructs a Syncer. A zero interval falls back to
// DefaultSyncInterval.
func NewSyncer(store Store, lister StoreLister, resolve SourceResolver, interval time.Duration, log *slog.Logger) *Syncer {
	if log == nil {
		log = slog.Default()
	}
	if interval <= 0 {
		interval = DefaultSyncInterval
	}
	if resolve == nil {
		resolve = NoSourceResolver
	}
	return &Syncer{store: store, lister: lister, resolve: resolve, log: log, interval: interval, now: time.Now}
}

// Run blocks, running an immediate pass and then ticking on s.interval,
// until ctx is cancelled.
func (s *Syncer) Run(ctx context.Context) {
	s.runOnce(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

func (s *Syncer) runOnce(ctx context.Context) {
	target := s.now().UTC().AddDate(0, 0, -1) // yesterday: the most recently completed UTC day
	afterID := ""
	for {
		refs, hasMore, err := s.lister.ListAllStores(ctx, afterID, defaultListBatchLimit)
		if err != nil {
			s.log.Error("web baseline sync: list stores failed", "error", err)
			return
		}
		for _, ref := range refs {
			s.syncStore(ctx, ref, target)
			afterID = ref.StoreID
		}
		if !hasMore {
			return
		}
	}
}

func (s *Syncer) syncStore(ctx context.Context, ref StoreRef, date time.Time) {
	dateKey := date.Format("2006_01_02")
	day := BaselineDay{
		ID:         ref.StoreID + "__" + dateKey,
		MerchantID: ref.MerchantID,
		StoreID:    ref.StoreID,
		Date:       dateKey,
		Source:     ref.Platform,
		Confidence: ConfidenceUnavailable,
		SyncedAt:   s.now().UTC(),
	}

	src, ok, err := s.resolve(ctx, ref.StoreID, ref.Platform)
	if err != nil {
		s.log.Error("web baseline sync: resolve connector failed", "error", err, "store_id", ref.StoreID)
		// Falls through and still writes the unavailable day below — a
		// resolution failure must not silently leave yesterday
		// un-recorded (the comparison API needs a document to explain
		// *why* it's unavailable, not just absence of one).
	} else if ok {
		result, err := src.DailyBaseline(ctx, ref.StoreID, date)
		if err != nil {
			s.log.Error("web baseline sync: connector DailyBaseline failed", "error", err, "store_id", ref.StoreID)
		} else {
			day.Orders = result.Orders
			day.RevenueMinor = result.RevenueMinor
			day.AOVMinor = computeAOV(result.RevenueMinor, result.Orders)
			day.Currency = result.Currency
			day.Confidence = ConfidenceConnectorReported
			if result.Partial {
				day.Confidence = ConfidencePartial
			}
		}
	}

	if err := s.store.Upsert(ctx, day); err != nil {
		s.log.Error("web baseline sync: upsert failed", "error", err, "store_id", ref.StoreID)
	}
}
