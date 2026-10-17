// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/utils"
)

// This file wires the Agent Behavior Insights worker as a background
// component of the control-plane process (started from
// internal/planes/control/api/entry.go's Init) rather than a separate
// --service value — every background worker in this repo now follows this
// convention (see ledger/entry.go's identical note).

// WorkerDeps are the already-connected dependencies the control plane
// passes in. Store is the same Store instance the control plane's Agent
// Behavior Insights read API uses, so there is exactly one store regardless
// of how many places read/write it.
type WorkerDeps struct {
	Store  Store
	Log    *slog.Logger
	Config *utils.Configurations
}

// StartWorker starts the Agent Behavior Insights worker: the Consumer event
// subscriber consuming serving.catalog.query_recorded and
// ucp.negotiation.succeeded/failed. It owns its own EventBus connection (a
// distinct ConsumerGroup from the control plane's main bus) so that
// horizontally-scaled control-plane replicas still distribute these events
// as competing consumers, exactly as when this ran as a separate process.
func StartWorker(ctx context.Context, deps WorkerDeps) (stop func(context.Context) error, err error) {
	eventBus, err := eventbus.New(eventbus.Config{
		ConsumerGroup: "behavior_insights_consumer_group",
		Logger:        eventbus.StdLogger(deps.Config.LogLevel == "DEBUG"),
		NATS:          eventbus.NATSConfig{URL: deps.Config.NATSURL},
	})
	if err != nil {
		return nil, fmt.Errorf("create event bus: %w", err)
	}

	consumer := NewConsumer(deps.Store, eventBus, deps.Log)
	if err := consumer.Start(ctx); err != nil {
		return nil, fmt.Errorf("start insights consumer: %w", err)
	}

	stop = func(ctx context.Context) error {
		return errors.Join(consumer.Stop(ctx), eventBus.Close())
	}
	return stop, nil
}
