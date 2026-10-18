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

	"github.com/sarvexlab/sarvex/internal/platform"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/hibiken/asynq"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongoopts "go.mongodb.org/mongo-driver/v2/mongo/options"

	"go.opentelemetry.io/otel"

	"github.com/sarvexlab/sarvex/internal/pkg/common"
	"github.com/sarvexlab/sarvex/internal/pkg/concurrency"
	pkgdomain "github.com/sarvexlab/sarvex/internal/pkg/domain"
	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/middlewares"
	"github.com/sarvexlab/sarvex/internal/pkg/otelboot"
	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
	"github.com/sarvexlab/sarvex/internal/pkg/serving"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
	"github.com/sarvexlab/sarvex/internal/pkg/utils"
	"github.com/sarvexlab/sarvex/internal/planes/sync"
	"github.com/sarvexlab/sarvex/internal/planes/sync/auth"
	"github.com/sarvexlab/sarvex/internal/planes/sync/connector"
	"github.com/sarvexlab/sarvex/internal/planes/sync/sim"
	"github.com/sarvexlab/sarvex/internal/planes/sync/webbaseline"
)

type syncPlaneEntry struct {
	configPath string
}

const controlGroupName = "/v1"

// NewSyncPlaneEntry returns a ServiceEntry that runs the V1 sync plane.
func NewSyncPlaneEntry(path string) common.ServiceEntry {
	return &syncPlaneEntry{configPath: path}
}

func (s *syncPlaneEntry) Init(tls *bool, port *int, env *string) error {
	config, err := utils.LoadConfigurations(s.configPath)
	if err != nil {
		return err
	}
	config.Env = utils.StringValue(env)

	otelShutdown := otelboot.Setup(context.Background(), "sync")
	defer func() { _ = otelShutdown(context.Background()) }()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: toSlogLevel(config.LogLevel),
	}))

	e := echo.New()
	e.Use(middlewares.WithServerHeader())
	//e.Use(middlewares.WithErrorHandler())
	e.Use(middlewares.WithCorrelationID())
	// Wire OpenTelemetry tracing and metrics (F-043)
	// Uses no-op providers by default; operations can configure exporters via OTEL_* env vars
	e.Use(middlewares.WithOTELTracing(otel.GetTracerProvider(), "sync"))
	e.Use(middlewares.WithOTELMetrics(otel.GetMeterProvider(), "sync"))
	e.Use(middleware.Secure())
	e.Use(middleware.Recover())
	e.Use(middleware.Decompress())
	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		Skipper: func(c echo.Context) bool {
			return strings.Contains(c.Path(), "metrics")
		},
	}))
	e.Use(middlewares.WithCORS(config.DashboardAddress))

	group := e.Group(controlGroupName)

	// ── 1. MongoDB Initialization ──────────────────────────────────────────
	// MongoDB connection with explicit timeouts (15s for sync plane).
	// Sync plane is event-driven and writes catalog data; use longer timeouts (15s)
	// than serving plane to allow time for bulk operations.
	mongoOpts := mongoopts.Client().
		ApplyURI(config.MongoConnectionString).
		SetConnectTimeout(15 * time.Second).
		SetTimeout(15 * time.Second)
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

	// ── 2. Event Bus Initialization (NATS JetStream)
	busConfig := eventbus.Config{
		ConsumerGroup: "sync_plane_consumer_group",
		Logger:        newEventBusLogger(log),
		NATS:          eventbus.NATSConfig{URL: config.NATSURL},
	}

	eventBus, err := eventbus.New(busConfig)
	if err != nil {
		log.Error("failed to create event bus", "error", err)
		return err
	}
	defer eventBus.Close()

	// ── 3. Registry & Connector Factories ───────────────────────────────────
	// Connector repository
	connectorRepo, err := connector.NewMongoConnectorRepository(db)
	if err != nil {
		log.Error("failed to create connector repository", "error", err)
		return fmt.Errorf("connector repository: %w", err)
	}

	// Secret management: decrypts store credentials for live connector calls
	// (write-back, and the store-API pull methods). The sync plane never
	// writes creds, so validators are nil.
	secretMgmt, err := secretmanagement.New(context.Background(), secretmanagement.Config{
		VaultAddr:  config.VaultAddr,
		VaultToken: config.VaultToken,
	})
	if err != nil {
		log.Error("failed to initialize secret management", "error", err)
		return err
	}

	// Serving cache (read-only here) backs the plugin profile doorway: the
	// connector service serves the last-known-good rendered profile from the
	// same Redis snapshot the serving plane builds and reads.
	servingCache := serving.NewRedisCache(
		config.RedisAddress,
		config.RedisPassword,
		config.RedisDatabase,
		5*time.Minute,
	)

	cacheProfileService := serving.NewCacheProfileSource(servingCache)

	connectorSvc := connector.NewConnectorService(connectorRepo, eventBus, cacheProfileService, secretMgmt, log)

	// provider resolves the base.Registry identity (MerchantID/StoreID/Platform
	// — the only Configuration() fields registry.Get/the connector factories
	// read) from the connector service directly, not from the serving cache.
	// A cache-backed provider would return (nil, nil) on a cache miss (by
	// design — see CacheProfileSource's doc comment, for the profile
	// doorway's own graceful-degradation need) and a store's very first
	// catalog import runs before any cache snapshot has ever been built for
	// it, so that nil would otherwise reach base.Registry.Get and panic.
	provider := func(merchantID, storeID string) (ucp.StoreProvider, error) {
		_, platformName, err := connectorSvc.GetStoreCredentials(context.Background(), storeID)
		if err != nil {
			return nil, fmt.Errorf("resolve platform for store %s: %w", storeID, err)
		}
		return &connectorRegistryIdentity{merchantID: merchantID, storeID: storeID, platform: platformName}, nil
	}

	// Credentials provider for the registry's store-API pull methods
	// (FetchProduct, FetchCatalog, Ping, ...): resolved fresh on every call
	// via the connector service, never cached here, so a credential rotation
	// at pairing takes effect on the very next pull.
	storeCreds := base.CredentialsFunc(func(ctx context.Context, storeID string) (map[string]any, error) {
		creds, _, err := connectorSvc.GetStoreCredentials(ctx, storeID)
		return creds, err
	})
	reg := base.New(storeCreds, log)
	platform.RegisterConnectors(reg)

	// ── 4. Redis — shared by the sweep overlap-guard lease and the asynq job queue
	// Sync plane uses Redis for dedup leases and async job queue; use moderate timeouts (5s dial, 3s read/write).
	redisClient := redis.NewClient(&redis.Options{
		Addr:         config.RedisAddress,
		Password:     config.RedisPassword,
		DB:           config.RedisDatabase,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		log.Error("failed to connect to redis", "error", err)
		return err
	}
	sweepLease := concurrency.NewRedisLease(redisClient, "sync:sweep:")

	asynqRedisOpt := asynq.RedisClientOpt{
		Addr:     config.RedisAddress,
		Password: config.RedisPassword,
		DB:       config.RedisDatabase,
	}

	// ── 5. Instantiate Sync Service ─────────────────────────────────────────
	// The sync plane owns the remediation records its score stage creates and
	// its apply-remediations handler marks applied.
	remediations, err := sync.NewMongoRemediationStore(context.Background(), db)
	if err != nil {
		log.Error("failed to initialise remediation store", "error", err)
		return err
	}
	syncSvc := sync.NewSyncService(db, eventBus, reg, provider, remediations, sweepLease, asynqRedisOpt, redisClient, log)

	dedupTTL := 10 * time.Minute

	if config.KindeIssuerURL == "" {
		log.Warn("KINDE_ISSUER_URL unset — starting sync-plane plugin simulator (dev only)")

		// Start the in-process WooCommerce plugin simulator so heartbeat and
		// signal paths work without a real plugin install. Pairing completion
		// is simulated separately by the control plane's own plugin simulator;
		// this one discovers newly-paired connectors by polling connectorSvc.
		baseURL := fmt.Sprintf("http://localhost:%d%s", *port, controlGroupName)
		pluginSim := sim.New(baseURL, connectorSvc, redisClient, log)
		go pluginSim.Run(context.Background())
	}

	h := New(Dependencies{
		Logger:         log,
		Connector:      connectorSvc,
		EventPublisher: eventBus,
		ResolveHMAC:    auth.NewHMACResolver(log, connectorSvc, redisClient, secretMgmt, dedupTTL),
	})

	if err := h.SetupRoutes(controlGroupName, group); err != nil {
		log.Error("failed to setup control plane routes", "error", err)
		return err
	}

	// ── Health Probes ──────────────────────────────────────────────────
	e.GET("/healthz", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "live"})
	})
	e.GET("/readyz", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ready"})
	})

	go func() {
		log.Info("starting sync plane service", "port", *port)
		if err := e.Start(fmt.Sprintf(":%d", *port)); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "error", err)
		}
	}()

	// Start background workers
	startupCtx, startupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer startupCancel()
	if err := syncSvc.Start(startupCtx); err != nil {
		log.Error("failed to start sync service", "error", err)
		return err
	}

	// ── 5b. Web baseline daily sync worker ──────────────────────────────────
	webBaselineStop, err := webbaseline.StartWorker(context.Background(), webbaseline.WorkerDeps{
		DB:  db,
		Log: log,
	})
	if err != nil {
		log.Error("failed to start web baseline sync worker", "error", err)
		return err
	}
	defer webBaselineStop()

	// ── 5c. Surface artifacts ───────────────────────────────────────────────
	// Generates each enrolled surface's artifact into the serving cache and
	// records what is observed about it.
	stopSurfaceArtifacts, err := startSurfaceArtifacts(context.Background(), config, db, redisClient, eventBus, log)
	if err != nil {
		log.Error("failed to start surface artifact runner", "error", err)
		return err
	}
	defer stopSurfaceArtifacts()

	// ── 6. Graceful Shutdown Block ──────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Info("shutting down sync plane service gracefully")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := syncSvc.Stop(shutdownCtx); err != nil {
		log.Error("failed to stop sync service cleanly", "error", err)
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

// connectorRegistryIdentity is a minimal ucp.StoreProvider carrying only the
// connector-registry identity fields (MerchantID, StoreID, Platform) that
// base.Registry.Get and the connector factories actually read via
// Configuration(). The other StoreProvider methods (Capabilities, Services,
// PaymentHandlers, SigningKeys, StorefrontURL, Version) belong to profile
// rendering/negotiation — a separate concern the connector-construction path
// never touches — so they return zero values here.
type connectorRegistryIdentity struct {
	merchantID string
	storeID    string
	platform   pkgdomain.CommerceSystemType
}

func (s *connectorRegistryIdentity) Configuration() ucp.Configuration {
	return ucp.Configuration{MerchantID: s.merchantID, StoreID: s.storeID, Platform: s.platform}
}
func (s *connectorRegistryIdentity) StoreID() string                                   { return s.storeID }
func (s *connectorRegistryIdentity) Version() string                                   { return "" }
func (s *connectorRegistryIdentity) Capabilities() map[string][]wire.Capability        { return nil }
func (s *connectorRegistryIdentity) Services() map[string][]wire.Service               { return nil }
func (s *connectorRegistryIdentity) PaymentHandlers() map[string][]wire.PaymentHandler { return nil }
func (s *connectorRegistryIdentity) SigningKeys() []wire.JWK                           { return nil }
func (s *connectorRegistryIdentity) StorefrontURL() string                             { return "" }

var _ ucp.StoreProvider = (*connectorRegistryIdentity)(nil)
