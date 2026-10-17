// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package webbaseline

import (
	"context"
	"fmt"
	"log/slog"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

// This file wires the web baseline daily sync as a background component of
// the sync-plane process (started from internal/planes/sync/api/entry.go's
// Init) rather than a separate --service value — every background worker in
// this repo now follows this convention (see
// internal/planes/control/ledger/entry.go's identical note). Lives in the
// sync plane per R11 ("sync via the sync plane on a schedule").

// WorkerDeps are the already-connected dependencies the sync plane passes
// in — no separate Mongo/config loading happens here.
type WorkerDeps struct {
	DB  *mongo.Database
	Log *slog.Logger
}

// StartWorker starts the web baseline daily sync loop.
func StartWorker(ctx context.Context, deps WorkerDeps) (stop func(), err error) {
	store, err := NewMongoStore(deps.DB)
	if err != nil {
		return nil, fmt.Errorf("web baseline store: %w", err)
	}
	lister := NewMongoStoreLister(deps.DB)

	// NoSourceResolver: no connector in this codebase implements Source
	// today (see model.go's audit) — every day is honestly recorded
	// confidence=unavailable until a connector does. Swap this for a real
	// registry-backed resolver the day one does; no other change needed.
	syncer := NewSyncer(store, lister, NoSourceResolver, DefaultSyncInterval, deps.Log)
	syncCtx, cancel := context.WithCancel(ctx)
	go syncer.Run(syncCtx)

	return cancel, nil
}
