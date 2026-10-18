// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

//go:build conformance

// Package conformance runs the sarvex conformance suite as a Go test, so CI
// gates on it the same way it gates on the scenario and journey suites.
//
// It starts the planes in this process on OS-assigned ports, onboards a
// fresh merchant, and drives the UCP agent surface against the store that
// results. Nothing needs to be running first beyond the infrastructure
// (Mongo, Redis, NATS, Vault) that CI already provides as services.
//
// Build-tagged so `go test ./...` does not try to run it without that
// infrastructure — the same gate internal/planes/scenarios and
// test/e2e/journey use.
package conformance_test

import (
	"context"
	"fmt"
	"github.com/sarvexlab/sarvex/internal/pkg/devinfra"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/cmd/sarvex/internal/client"
	"github.com/sarvexlab/sarvex/cmd/sarvex/internal/conform"
	"github.com/sarvexlab/sarvex/cmd/sarvex/internal/onboard"
	"github.com/sarvexlab/sarvex/cmd/sarvex/internal/stack"
	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
	ucpagent "github.com/sarvexlab/sarvex/internal/pkg/ucp/agent"
	"github.com/sarvexlab/sarvex/internal/planes/sync/connector"
	"github.com/sarvexlab/sarvex/internal/planes/sync/sim"

	"log/slog"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongoopts "go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TestConformance is the whole suite: onboard, then conform.
//
// It deliberately has no infrastructure skip guard. The build tag already
// means "the caller asked for this and provided the infrastructure", so a
// missing dependency here is a failure, not a reason to report success —
// which is precisely how the two suites this replaces went unnoticed for
// months while asserting nothing.
func TestConformance(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

	// The simulated store is built inside the factory, not before Start:
	// its signer captures SYNC_PLANE_BASE_URL at construction, and that is
	// not correct until Start has reserved ports and exported them. Built
	// too early, every change signal would be posted to the wrong port.
	var suite *suiteDeps
	st, err := stack.Start(ctx, stack.Options{
		ConfigDir: configDir(t),
		SimStoreFactory: func(baseURL string) (http.Handler, error) {
			suite = newDeps(ctx, t)
			return suite.newStoreHandler(baseURL), nil
		},
	})
	if err != nil {
		t.Fatalf("start in-process stack: %v", err)
	}
	defer st.Close()
	if suite == nil {
		t.Fatal("simulated store was never built")
	}
	t.Logf("stack up: control %s serving %s sim %s", st.ControlURL, st.ServingURL, st.SimURL)

	// ── Phase 1: build the world ─────────────────────────────────────────
	operator, err := client.NewOperator(st.ControlURL, 30*time.Second)
	if err != nil {
		t.Fatalf("operator client: %v", err)
	}

	name := fmt.Sprintf("Conformance %d", time.Now().UnixNano())
	result, err := onboard.Run(ctx, operator, onboard.Options{
		Name:            name,
		Stores:          1,
		Platform:        "woocommerce",
		Products:        12,
		SimStoreBaseURL: st.SimURL,
		Timeout:         4 * time.Minute,
		Seeder:          suite.seeder,
		Progress:        testWriter{t},
	})
	if err != nil {
		t.Fatalf("onboarding phase: %v", err)
	}
	if !result.AllLive() {
		for _, s := range result.Stores {
			t.Errorf("store %s reached %s/%s (blocking: %s)", s.StoreID, s.UCPState, s.JourneyState, s.Blocking)
		}
		t.Fatalf("onboarding phase did not reach %s/%s — seed %d rebuilds this fixture",
			onboard.StateActive, onboard.JourneyLive, result.Seed)
	}
	storeID := result.Stores[0].StoreID
	t.Logf("store %s live (seed %d)", storeID, result.Seed)

	// ── Phase 2: drive the protocol ──────────────────────────────────────
	report, err := conform.Run(ctx, ucpagent.NewAgent(st.ServingURL, st.ControlURL, nil), conform.Options{
		StoreID:  storeID,
		Seed:     result.Seed,
		Timeout:  60 * time.Second,
		Fault:    suite.fault,
		Progress: func(res conform.Result) { t.Logf("%-5s %s %s", res.Outcome, res.ID, res.Detail) },
	})
	if err != nil {
		t.Fatalf("conformance run: %v", err)
	}

	if err := conform.WriteHuman(testWriter{t}, report); err != nil {
		t.Errorf("render report: %v", err)
	}
	if err := report.Err(); err != nil {
		t.Fatalf("%v — seed %d rebuilds this fixture", err, report.Seed)
	}
}

// testWriter routes progress output into the test log, so a failing run in
// CI shows what happened without a separate artifact.
type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}

// Seed satisfies onboard.CatalogSeeder; simCLI implements both that and
// conform.Fault over the same sim.CLI.
type simCLI struct{ cli *sim.CLI }

func (s simCLI) Seed(ctx context.Context, storeID string, products int, messy bool) error {
	_, err := s.cli.Seed(ctx, storeID, products, messy)
	return err
}

func (s simCLI) SetStock(ctx context.Context, storeID, sku string, quantity int) error {
	_, err := s.cli.MutateStock(ctx, storeID, sku, quantity)
	return err
}

func (s simCLI) BreakAttribute(ctx context.Context, storeID, sku, field string) error {
	_, err := s.cli.MutateAttr(ctx, storeID, sku, field)
	return err
}

func (s simCLI) Fix(ctx context.Context, storeID, sku, field string) error {
	_, err := s.cli.Fix(ctx, storeID, sku, field)
	return err
}

type suiteDeps struct {
	seeder          simCLI
	fault           simCLI
	newStoreHandler func(baseURL string) http.Handler
}

func newDeps(ctx context.Context, t *testing.T) *suiteDeps {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	mongoURI := envOr("MONGO_URI", envOr("MONGO_CONNECTION_STRING", "mongodb://admin:password@localhost:27017/?directConnection=true"))
	mongoClient, err := mongo.Connect(mongoopts.Client().ApplyURI(mongoURI))
	if err != nil {
		t.Fatalf("connect mongo at %s: %v", mongoURI, err)
	}
	if err := mongoClient.Ping(ctx, nil); err != nil {
		t.Fatalf("ping mongo at %s: %v", mongoURI, err)
	}
	t.Cleanup(func() { _ = mongoClient.Disconnect(context.WithoutCancel(ctx)) })
	db := mongoClient.Database("sarvex")

	redisClient := redis.NewClient(&redis.Options{
		Addr:     envOr("REDIS_ADDR", envOr("REDIS_ADDRESS", "localhost:6379")),
		Password: os.Getenv("REDIS_PASSWORD"),
	})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}
	t.Cleanup(func() { _ = redisClient.Close() })

	secretMgmt, err := secretmanagement.New(ctx, devinfra.SecretConfig())
	if err != nil {
		t.Fatalf("secret management: %v", err)
	}

	connectorRepo, err := connector.NewMongoConnectorRepository(db)
	if err != nil {
		t.Fatalf("connector repository: %v", err)
	}

	stores := sim.NewMongoStoreRepository(db)
	// SYNC_PLANE_BASE_URL is set by stack.Start before this runs — see the
	// SimStoreFactory call site for why the ordering matters.
	signer := sim.NewSigner(envOr("SYNC_PLANE_BASE_URL", "http://localhost:8082/v1"), redisClient)
	cli := simCLI{cli: sim.NewCLI(stores, connectorRepo, secretMgmt, signer, log)}

	return &suiteDeps{
		seeder: cli,
		fault:  cli,
		newStoreHandler: func(baseURL string) http.Handler {
			return sim.NewServer(stores, baseURL, connectorRepo, secretMgmt, signer, log).Handler()
		},
	}
}

// configDir finds the repo's configs/ directory. The test runs from its own
// package directory, so the plane entrypoints' default "./configs" does not
// resolve.
func configDir(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv("SARVEX_CONFIG_DIR"); dir != "" {
		return dir
	}
	// cmd/sarvex/conformance → repo root.
	return "../../../configs"
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
