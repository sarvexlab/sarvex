// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Command sim is a runtime dev tool, not a test — it does not run under
// `go test` and must not be wired into CI. It gives a human operating the
// real web console a persistent, controllable fake merchant store: a
// WooCommerce-shaped REST backend the real store-API connector genuinely
// pulls from, plus the plugin's heartbeat/signal and profile-doorway legs
// (GET /stores/{id}/.well-known/ucp fetches and serves the real,
// HMAC-signed GET /connector/profile response, so the monitoring plane's
// profile-edge probe has something real to check against a paired store's
// storefront_url). Every store-side action here (seed a catalog, mutate a
// product, serve REST responses) never calls a console/operator API, never
// triggers an import, never resolves a task, and never advances the
// journey. See internal/planes/sync/sim for the implementation.
//
// The `catalog`, `checkout`, and `order` subcommand groups are the one
// exception: they play the *agent* role a real UCP shopping agent plays,
// driving discovery/catalog/checkout create-update-complete against a live
// store's serving plane and reading the resulting order back via the
// control plane — the counterpart to every other command here, which plays
// the merchant. This is what actually proves a store is UCP-compliant end
// to end. See ucp/agent.Agent's doc comment.
//
// Usage:
//
//	go run ./cmd/sim serve                                   # start the fake store HTTP server
//	go run ./cmd/sim seed --store <id> --products 20 --messy  # seed a catalog
//	go run ./cmd/sim mutate price --store <id> --sku S --to 24.99
//	go run ./cmd/sim mutate stock --store <id> --sku S --to 0
//	go run ./cmd/sim mutate attr  --store <id> --sku S --break title
//	go run ./cmd/sim fix          --store <id> --sku S --field title
//	go run ./cmd/sim status [--store <id>]
//	go run ./cmd/sim catalog search    --store <id> [--q "widget"] [--limit 10] [--cursor C]
//	go run ./cmd/sim catalog lookup    --store <id> --item S
//	go run ./cmd/sim checkout run      --store <id> --sku S --qty 1  # create+update+complete in one shot
//	go run ./cmd/sim checkout create   --store <id> --sku S --qty 1
//	go run ./cmd/sim checkout update   --store <id> --checkout <id> --email a@b.com --city Springfield ...
//	go run ./cmd/sim checkout complete --store <id> --checkout <id> [--handler <id>]
//	go run ./cmd/sim order track       --store <id> --order <id>
//	go run ./cmd/sim google fetch-feed --url <feed url from the console>   # play Google's feed fetcher
//
// Configured entirely by environment variables — the same ones the sync and
// control plane processes use, so all four processes (control, sync,
// sim serve, sim <command>) share one Mongo, one Redis, and one Vault
// (secrets) — the same services a self-hosted deployment runs:
//
//	MONGO_CONNECTION_STRING   (default mongodb://admin:password@localhost:27017/?directConnection=true)
//	REDIS_ADDRESS             (default localhost:6379)
//	REDIS_PASSWORD            (default "")
//	VAULT_ADDR                (default http://localhost:8200)
//	VAULT_TOKEN               (default the docker-compose dev token)
//	SYNC_PLANE_BASE_URL       (default http://localhost:8082/v1 — where /connector/signals lives)
//	SIM_STORE_BASE_URL        (default http://localhost:4106 — must match control plane's SIM_STORE_BASE_URL)
//	SERVING_BASE_URL          (default http://localhost:8083/ucp — used by `sim catalog` and `sim checkout`)
//	CONTROL_PLANE_BASE_URL    (default http://localhost:8081/v1 — used by `sim order track`)
package main

import (
	"context"
	"fmt"
	"github.com/sarvexlab/sarvex/internal/pkg/devinfra"
	"log/slog"
	"os"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongoopts "go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
	"github.com/sarvexlab/sarvex/internal/pkg/simstore"
	ucpagent "github.com/sarvexlab/sarvex/internal/pkg/ucp/agent"
	"github.com/sarvexlab/sarvex/internal/planes/sync/connector"
	"github.com/sarvexlab/sarvex/internal/planes/sync/sim"
)

// deps bundles the shared connections every subcommand needs. Built fresh
// per invocation from environment variables — this is a runtime tool, not a
// long-lived service, so there is no config file.
type deps struct {
	stores          sim.StoreRepository
	cli             *sim.CLI
	simStoreBaseURL string
	log             *slog.Logger

	// agent drives the checkout/order subcommands (sim checkout/order) —
	// the only piece of cmd/sim that plays the *agent* role against
	// Sarvex's own serving + control planes, rather than the merchant
	// backend the rest of this tool simulates.
	agent *ucpagent.Agent

	// connectorRepo, secretMgmt, and signer back `sim serve`'s profile
	// doorway (GET /.well-known/ucp) — the same instances CLI uses to sign
	// mutation signals, so they share one Redis sequence counter.
	connectorRepo connector.ConnectorRepository
	secretMgmt    secretmanagement.SecretManagementService
	signer        *sim.Signer
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func newDeps(ctx context.Context) (*deps, error) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mongoURI := envOr("MONGO_CONNECTION_STRING", "mongodb://admin:password@localhost:27017/?directConnection=true")
	mongoClient, err := mongo.Connect(mongoopts.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, fmt.Errorf("connect mongo: %w", err)
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

	syncBaseURL := envOr("SYNC_PLANE_BASE_URL", "http://localhost:8082/v1")
	simStoreBaseURL := envOr("SIM_STORE_BASE_URL", simstore.DefaultBaseURL)

	signer := sim.NewSigner(syncBaseURL, redisClient)
	cli := sim.NewCLI(stores, connectorRepo, secretMgmt, signer, log)

	servingBaseURL := envOr("SERVING_BASE_URL", "http://localhost:8083/ucp")
	controlBaseURL := envOr("CONTROL_PLANE_BASE_URL", "http://localhost:8081/v1")
	// The agent needs a store -> merchant lookup for order tracking only;
	// cmd/sim already holds the connector repository that answers it.
	agent := ucpagent.NewAgent(servingBaseURL, controlBaseURL,
		ucpagent.MerchantResolverFunc(func(ctx context.Context, storeID string) (string, error) {
			model, err := connectorRepo.GetByStoreID(ctx, storeID)
			if err != nil {
				return "", fmt.Errorf("no connector paired for store %s yet — pair it from the console first: %w", storeID, err)
			}
			return model.MerchantID, nil
		}))

	return &deps{
		stores:          stores,
		cli:             cli,
		simStoreBaseURL: simStoreBaseURL,
		log:             log,
		agent:           agent,
		connectorRepo:   connectorRepo,
		secretMgmt:      secretMgmt,
		signer:          signer,
	}, nil
}

func main() {
	root := &cobra.Command{
		Use:   "sim",
		Short: "Runtime tool: a persistent, controllable simulated store + plugin for local dev. Not for CI or go test.",
	}
	root.AddCommand(newServeCmd())
	root.AddCommand(newSeedCmd())
	root.AddCommand(newMutateCmd())
	root.AddCommand(newPickupCmd())
	root.AddCommand(newFixCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newCatalogCmd())
	root.AddCommand(newCheckoutCmd())
	root.AddCommand(newOrderCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "sim:", err)
		os.Exit(1)
	}
}
