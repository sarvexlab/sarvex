// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package ledger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/utils"
)

// This file wires the Lost Revenue Ledger worker as a background component
// of the control-plane process (started from
// internal/planes/control/api/entry.go's Init) rather than a separate
// --service value — every background worker in this repo now follows this
// convention (see channel/entry.go's identical note).

// WorkerDeps are the already-connected dependencies the control plane
// passes in — no separate Mongo/config loading happens here.
type WorkerDeps struct {
	DB     *mongo.Database
	Log    *slog.Logger
	Config *utils.Configurations
}

// StartWorker starts the Lost Revenue Ledger worker: the Consumer event
// subscriber consuming serving.checkout.failed and
// control.checkout.abandoned. It owns its own EventBus connection (a
// distinct ConsumerGroup from the control plane's main bus) so that
// horizontally-scaled control-plane replicas still distribute ledger events
// as competing consumers, exactly as when this ran as a separate process.
// The returned Store also backs the control-plane's ledger read API.
func StartWorker(ctx context.Context, deps WorkerDeps) (store Store, stop func(context.Context) error, err error) {
	eventBus, err := eventbus.New(eventbus.Config{
		ConsumerGroup: "ledger_consumer_group",
		Logger:        eventbus.StdLogger(deps.Config.LogLevel == "DEBUG"),
		NATS:          eventbus.NATSConfig{URL: deps.Config.NATSURL},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create event bus: %w", err)
	}

	store, err = NewMongoStore(deps.DB.Client(), deps.DB)
	if err != nil {
		return nil, nil, fmt.Errorf("ledger store: %w", err)
	}

	consumer := NewConsumer(store, eventBus, deps.Log)
	if err := consumer.Start(ctx); err != nil {
		return nil, nil, fmt.Errorf("start ledger consumer: %w", err)
	}

	stop = func(ctx context.Context) error {
		return errors.Join(consumer.Stop(ctx), eventBus.Close())
	}
	return store, stop, nil
}
