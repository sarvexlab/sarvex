// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package checkoutescalation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/utils"
)

// This file wires the checkout-session-projection worker as a background
// component of the control-plane process (started from
// internal/planes/control/api/entry.go's Init) rather than a separate
// --service value — every background worker in this repo now follows this
// convention (see ledger/entry.go's identical note).

// WorkerDeps are the already-connected dependencies the control plane
// passes in. SessionRepo/TimelineRepo are the same repository instances the
// control plane's Checkout Escalation read API uses, so there is exactly
// one repository per collection regardless of how many places read/write
// it.
type WorkerDeps struct {
	SessionRepo  SessionRepository
	TimelineRepo TimelineRepository
	Log          *slog.Logger
	Config       *utils.Configurations
}

// StartWorker starts the checkout-session-projection worker: the Projector
// event subscriber and the Sweeper abandonment/stalled-escalation ticker.
// It owns its own EventBus connection (a distinct ConsumerGroup from the
// control plane's main bus) so that horizontally-scaled control-plane
// replicas still distribute projection events as competing consumers,
// exactly as when this ran as a separate process.
func StartWorker(ctx context.Context, deps WorkerDeps) (stop func(context.Context) error, err error) {
	eventBus, err := eventbus.New(eventbus.Config{
		ConsumerGroup: "session_projection_consumer_group",
		Logger:        eventbus.StdLogger(deps.Config.LogLevel == "DEBUG"),
		NATS:          eventbus.NATSConfig{URL: deps.Config.NATSURL},
	})
	if err != nil {
		return nil, fmt.Errorf("create event bus: %w", err)
	}

	sweepCfg := DefaultSweepConfig()
	projector := NewProjector(deps.SessionRepo, deps.TimelineRepo, eventBus, sweepCfg.Retention, deps.Log)
	if err := projector.Start(ctx); err != nil {
		return nil, fmt.Errorf("start projector: %w", err)
	}

	sweeper := NewSweeper(deps.SessionRepo, sweepCfg, deps.Log)
	sweepCtx, cancelSweep := context.WithCancel(context.Background())
	go sweeper.Run(sweepCtx)

	stop = func(ctx context.Context) error {
		err := projector.Stop(ctx)
		cancelSweep()
		return errors.Join(err, eventBus.Close())
	}
	return stop, nil
}
