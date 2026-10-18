// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/sarvexlab/sarvex/internal/pkg/common"
	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/middlewares"
	"github.com/sarvexlab/sarvex/internal/pkg/otelboot"
	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
	servingpkg "github.com/sarvexlab/sarvex/internal/pkg/serving"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue"
	"github.com/sarvexlab/sarvex/internal/pkg/utils"
	"github.com/sarvexlab/sarvex/internal/planes/serving"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongoopts "go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.opentelemetry.io/otel"
)

const servingGroupName = "/ucp/:platform/:storeID"

type servingPlaneEntry struct {
	configPath string
}

// NewServingPlaneEntry returns a ServiceEntry that runs the V1 serving plane.
func NewServingPlaneEntry(path string) common.ServiceEntry {
	return &servingPlaneEntry{configPath: path}
}

func (s *servingPlaneEntry) Init(tls *bool, port *int, env *string) error {
	config, err := utils.LoadConfigurations(s.configPath)
	if err != nil {
		return err
	}
	config.Env = utils.StringValue(env)

	otelShutdown := otelboot.Setup(context.Background(), "serving")
	defer func() { _ = otelShutdown(context.Background()) }()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: toSlogLevel(config.LogLevel),
	}))

	// ── Redis Cache ────────────────────────────────────────────────
	redisCache := serving.NewRedisCache(
		config.RedisAddress,
		config.RedisPassword,
		config.RedisDatabase,
		// No expiry: SwapSnapshot's atomic delete+rewrite (cache_redis.go)
		// already keeps entries current on every real rebuild, which only
		// fires on catalog.products_ready — not on a fixed schedule. A
		// wall-clock TTL here previously evicted entries between rebuilds
		// for any store whose catalog didn't change within the window,
		// turning every subsequent read into a spurious not-found instead
		// of the designed staleness path (Watermark.IsStale, checked by
		// e.g. CheckoutComplete's freshness guard in handler.go).
		0,
	)
	servingCache := redisCache

	// ── Event Bus (NATS JetStream) ──
	busConfig := eventbus.Config{
		ConsumerGroup: "serving_consumer_group",
		Logger:        newEventBusLogger(log),
		NATS:          eventbus.NATSConfig{URL: config.NATSURL},
	}
	eventPublisher, err := eventbus.New(busConfig)
	if err != nil {
		log.Error("failed to create event bus", "error", err)
		return err
	}
	defer eventPublisher.Close()

	// ── MongoDB Connection ────────────────────────────────────────────
	// Connect to MongoDB for reading store profiles and products.
	// Serving plane is read-heavy and serves external agents; use shorter timeouts (5s)
	// to prioritize agent responsiveness over internal consistency.
	mongoOpts := mongoopts.Client().
		ApplyURI(config.MongoConnectionString).
		SetConnectTimeout(5 * time.Second).
		SetTimeout(5 * time.Second)
	mongoClient, err := mongo.Connect(mongoOpts)
	if err != nil {
		log.Error("failed to connect to mongodb", "error", err)
		return err
	}
	defer func() {
		if err := mongoClient.Disconnect(context.Background()); err != nil {
			log.Error("failed to disconnect from mongodb", "error", err)
		}
	}()

	db := mongoClient.Database("sarvex")

	// Secret management decrypts the store credentials the checkout path needs
	// for its live merchant calls. Connected before the dependencies because
	// the connector registry is built with them — see checkoutOnlyCredentials.
	secretMgmt, err := secretmanagement.New(context.Background(), secretmanagement.Config{
		VaultAddr:  config.VaultAddr,
		VaultToken: config.VaultToken,
	})
	if err != nil {
		log.Error("failed to initialize secret management", "error", err)
		return err
	}

	// ── Dependencies ───────────────────────────────────────────────
	deps, err := FromConfig(config, eventPublisher, log, servingCache, db, secretMgmt, redisCache.Client())
	if err != nil {
		log.Error("failed to build serving dependencies", "error", err)
		return err
	}

	// ── Catalogue & Cache Rebuilder ────────────────────────────────
	// Load the catalogue for spec-compliant profile rendering.
	cat, err := catalogue.LoadCatalogue(context.Background(), log)
	if err != nil {
		log.Error("failed to load catalogue", "error", err)
		return err
	}

	// Create catalog source backed by MongoDB.
	// Capability definitions come from the authoritative catalogue.
	// Reads products from sync_catalog (sync plane authority).
	// Reads store profiles from control_store_profiles (control plane authority).
	catalogSource := serving.NewMongoCatalogSource(db, cat, deps.JWKService, log)

	// Create the cache rebuilder that orchestrates snapshot generation and swap.
	cacheRebuilder := serving.NewCacheRebuilder(
		redisCache, // Implements CacheWriter interface
		catalogSource,
		cat,
		config.ServingOrigin(),
		eventPublisher,
		log,
	)

	// ── Background Service ─────────────────────────────────────────
	// Initialize and start the serving plane event handler service.
	// This listens to sync plane events (catalog.products_ready) and orchestrates
	// full cache rebuild with profile rendering.
	servingSvc := serving.NewServingService(
		cacheRebuilder,
		redisCache,
		catalogSource,
		eventPublisher,
		log,
	)

	startupCtx, startupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer startupCancel()
	if err := servingSvc.Start(startupCtx); err != nil {
		log.Error("failed to start serving background service", "error", err)
		return err
	}

	// ── Echo Server ────────────────────────────────────────────────
	e := echo.New()
	e.Use(middlewares.WithServerHeader())
	//e.Use(middlewares.WithErrorHandler())
	e.Use(middlewares.WithCorrelationID())
	// Wire OpenTelemetry tracing and metrics (F-043)
	// Uses no-op providers by default; operations can configure exporters via OTEL_* env vars
	e.Use(middlewares.WithOTELTracing(otel.GetTracerProvider(), "serving"))
	e.Use(middlewares.WithOTELMetrics(otel.GetMeterProvider(), "serving"))
	e.Use(middleware.Secure())
	e.Use(middleware.Recover())
	e.Use(middleware.Decompress())
	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		Skipper: func(c echo.Context) bool {
			return strings.Contains(c.Path(), "metrics")
		},
	}))

	group := e.Group(servingGroupName)

	// ── Session Store ──────────────────────────────────────────────
	// Use Redis for distributed session storage (horizontal scaling).
	sessionStore := serving.NewRedisSessionStore(
		redisCache.Client(),
		30*time.Minute, // Session TTL
	)

	deps.CartStore = serving.NewRedisCartStore(redisCache.Client(), 24*time.Hour)

	// ── Handler Setup ──────────────────────────────────────────────
	handler := NewServingHandler(config, deps, log, sessionStore)
	if err := handler.SetupRoutes(servingGroupName, group); err != nil {
		log.Error("failed to setup serving routes", "error", err)
		return err
	}

	// ── Surface artifacts ──────────────────────────────────────────────
	// Public, token-addressed, cache-only (see serving.ArtifactHandler). Not
	// in the UCP group: its fetcher is not a UCP agent.
	artifactHandler := serving.NewArtifactHandler(
		servingpkg.NewRedisArtifacts(redisCache.Client()), eventPublisher, deps.RateLimiter, log)
	e.GET(serving.ArtifactRoute, artifactHandler.Get)

	// ── Health Probes ──────────────────────────────────────────────────
	e.GET("/healthz", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "live"})
	})
	e.GET("/readyz", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ready"})
	})

	// ── Checkout Abandonment Sweep (R4) ─────────────────────────────
	abandonmentSweeper := serving.NewAbandonmentSweeper(sessionStore, eventPublisher, serving.DefaultAbandonmentSweepConfig(), log)
	sweepCtx, cancelSweep := context.WithCancel(context.Background())
	go abandonmentSweeper.Run(sweepCtx)

	// ── Graceful Shutdown ──────────────────────────────────────────
	go func() {
		log.Info("starting serving plane server", "port", *port)
		if err := e.Start(fmt.Sprintf(":%d", *port)); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cancelSweep()

	// Stop the background service
	if err := servingSvc.Stop(ctx); err != nil {
		log.Error("failed to stop serving background service", "error", err)
	}

	if err := e.Shutdown(ctx); err != nil {
		log.Error("shutdown error", "error", err)
		return err
	}
	return nil
}

func toSlogLevel(level string) slog.Level {
	switch level {
	case "DEBUG":
		return slog.LevelDebug
	case "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func newEventBusLogger(log *slog.Logger) eventbus.Logger {
	return &eventBusLoggerAdapter{log: log}
}

type eventBusLoggerAdapter struct {
	log *slog.Logger
}

func (a *eventBusLoggerAdapter) Info(msg string, fields watermill.LogFields) {
	args := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	a.log.Info(msg, args...)
}

func (a *eventBusLoggerAdapter) Error(msg string, err error, fields watermill.LogFields) {
	args := make([]any, 0, len(fields)*2+2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	args = append(args, "error", err)
	a.log.Error(msg, args...)
}

func (a *eventBusLoggerAdapter) Debug(msg string, fields watermill.LogFields) {
	args := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	a.log.Debug(msg, args...)
}

func (a *eventBusLoggerAdapter) Trace(msg string, fields watermill.LogFields) {
	args := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	a.log.Debug(msg, args...)
}

func (a *eventBusLoggerAdapter) With(fields watermill.LogFields) watermill.LoggerAdapter {
	args := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	return &eventBusLoggerAdapter{log: a.log.With(args...)}
}
