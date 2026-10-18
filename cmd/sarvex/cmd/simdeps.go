// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"fmt"
	"github.com/sarvexlab/sarvex/internal/pkg/devinfra"
	"log/slog"
	"net/http"
	"os"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongoopts "go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
	"github.com/sarvexlab/sarvex/internal/pkg/simstore"
	"github.com/sarvexlab/sarvex/internal/planes/sync/connector"
	"github.com/sarvexlab/sarvex/internal/planes/sync/sim"
)

// simSeeder writes a starting catalog into the simulated store backend.
//
// Seeding is the one part of onboarding that is not an HTTP call: the
// catalog belongs to the merchant's store, not to Sarvex, and the simulated
// store keeps it in Mongo. Going through sim.CLI is deliberate — it is the
// same path `sim seed` uses, so the catalog the connector imports is
// produced exactly one way.
type simSeeder struct {
	cli *sim.CLI
}

func (s simSeeder) Seed(ctx context.Context, storeID string, products int, messy bool) error {
	_, err := s.cli.Seed(ctx, storeID, products, messy)
	return err
}

// simDeps holds the connections the simulated store needs, plus the
// store→merchant lookup order tracking needs.
type simDeps struct {
	seeder    simSeeder
	merchants connector.ConnectorRepository
	baseURL   string
	// newStoreHandler builds the simulated store's HTTP surface for a given
	// base URL. Deferred rather than built eagerly because an in-process run
	// does not know its own port until it has reserved one.
	newStoreHandler func(baseURL string) http.Handler
	close           func()
}

// newSimDeps connects to the same Mongo, Redis and Vault the
// planes use — configured entirely by environment variables, matching
// cmd/sim, so all processes in a dev stack agree without a config file.
func newSimDeps(ctx context.Context) (*simDeps, error) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	mongoURI := envOr("MONGO_CONNECTION_STRING", "mongodb://admin:password@localhost:27017/?directConnection=true")
	mongoClient, err := mongo.Connect(mongoopts.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, fmt.Errorf("connect mongo at %s: %w", mongoURI, err)
	}
	db := mongoClient.Database("sarvex")

	redisClient := redis.NewClient(&redis.Options{
		Addr:     envOr("REDIS_ADDRESS", "localhost:6379"),
		Password: os.Getenv("REDIS_PASSWORD"),
	})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("connect redis: %w", err)
	}

	secretMgmt, err := secretmanagement.New(ctx, devinfra.SecretConfig())
	if err != nil {
		return nil, fmt.Errorf("secret management: %w", err)
	}

	connectorRepo, err := connector.NewMongoConnectorRepository(db)
	if err != nil {
		return nil, fmt.Errorf("connector repository: %w", err)
	}

	stores := sim.NewMongoStoreRepository(db)
	signer := sim.NewSigner(envOr("SYNC_PLANE_BASE_URL", "http://localhost:8082/v1"), redisClient)
	cli := sim.NewCLI(stores, connectorRepo, secretMgmt, signer, log)

	return &simDeps{
		seeder:    simSeeder{cli: cli},
		merchants: connectorRepo,
		baseURL:   envOr("SIM_STORE_BASE_URL", simstore.DefaultBaseURL),
		newStoreHandler: func(baseURL string) http.Handler {
			return sim.NewServer(stores, baseURL, connectorRepo, secretMgmt, signer, log).Handler()
		},
		close: func() {
			_ = mongoClient.Disconnect(context.WithoutCancel(ctx))
			_ = redisClient.Close()
		},
	}, nil
}
