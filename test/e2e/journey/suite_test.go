// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package journey_test

import (
	"context"
	"fmt"
	"github.com/sarvexlab/sarvex/internal/pkg/devinfra"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongoopts "go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/pkg/commandbus"
	"github.com/sarvexlab/sarvex/internal/pkg/concurrency"
	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"

	// Registers eventbus's NATS JetStream backend, which this suite's
	// shared event bus uses.
	_ "github.com/sarvexlab/sarvex/internal/pkg/eventbus/natsjetstream"
	"github.com/sarvexlab/sarvex/internal/pkg/jwkmanagement"
	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
	"github.com/sarvexlab/sarvex/internal/pkg/serving"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	validatorregistry "github.com/sarvexlab/sarvex/internal/pkg/ucp/validator/registry"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
	"github.com/sarvexlab/sarvex/internal/planes/control/account"
	"github.com/sarvexlab/sarvex/internal/planes/control/api"
	"github.com/sarvexlab/sarvex/internal/planes/control/auth"
	"github.com/sarvexlab/sarvex/internal/planes/control/billing"
	"github.com/sarvexlab/sarvex/internal/planes/control/catalog"
	"github.com/sarvexlab/sarvex/internal/planes/control/connector"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/journey"
	"github.com/sarvexlab/sarvex/internal/planes/control/operator"
	"github.com/sarvexlab/sarvex/internal/planes/control/store"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
	"github.com/sarvexlab/sarvex/internal/planes/control/validation"
	syncsvc "github.com/sarvexlab/sarvex/internal/planes/sync"
	syncapi "github.com/sarvexlab/sarvex/internal/planes/sync/api"
	syncauth "github.com/sarvexlab/sarvex/internal/planes/sync/auth"
	syncconnector "github.com/sarvexlab/sarvex/internal/planes/sync/connector"
	"github.com/sarvexlab/sarvex/internal/platform"
)

// e2eEnv holds the complete cross-plane test harness.
// activeBillingStub is billing.StubService with Get overridden to report an
// active subscription, so requireActiveBilling doesn't block the mutating
// routes this suite exercises (Stripe isn't available here).
type activeBillingStub struct {
	billing.StubService
}

func (activeBillingStub) Get(_ context.Context, _ domain.MerchantID) (domain.Billing, error) {
	return domain.Billing{Status: domain.BillingActive}, nil
}

type e2eEnv struct {
	// HTTP servers: the control plane (console/operator API, pairing) and
	// the sync plane (the plugin's /connector/* routes).
	controlServer *httptest.Server
	syncServer    *httptest.Server

	// Infrastructure
	mongoClient *mongo.Client
	db          *mongo.Database
	redisClient *redis.Client
	eventBus    eventbus.EventBus

	// Sync plane (runs as in-process background workers)
	syncSvc syncsvc.Service

	// Repositories for direct test seeding
	connectorRepo connector.ConnectorRepository
	storeRepo     store.StoreRepository
	accountRepo   account.AccountListRepository
	memberRepo    account.MemberRepository
	secretMgmt    secretmanagement.SecretManagementService
}

// testBearerResolver maps test tokens to principals.
// Operator token does not carry a merchant_id (operators act cross-tenant).
func testBearerResolver(token string) (domain.Principal, error) {
	switch token {
	case "operator-token":
		return domain.Principal{
			UserID: "operator-1",
			Email:  "operator@sarvex.example.com",
			Role:   domain.RoleOperator,
		}, nil
	case "owner-token":
		return domain.Principal{
			UserID:     "owner-1",
			Email:      "owner@merchant.example.com",
			Role:       domain.RoleOwner,
			MerchantID: "e2e-merchant",
		}, nil
	default:
		// Dynamic tokens of the form "merchant:<merchant_id>" allow any merchant.
		var merchantID string
		if n, _ := fmt.Sscanf(token, "merchant:%s", &merchantID); n == 1 {
			return domain.Principal{
				UserID:     "owner-dynamic",
				Email:      "owner@dynamic.example.com",
				Role:       domain.RoleOwner,
				MerchantID: domain.MerchantID(merchantID),
			}, nil
		}
		return domain.Principal{}, fmt.Errorf("unknown e2e token: %s", token)
	}
}

var env *e2eEnv

func TestMain(m *testing.M) {
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		fmt.Fprintln(os.Stderr, "MONGO_URI not set; skipping e2e tests")
		os.Exit(0)
	}
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	var err error
	env, err = newE2EEnv(mongoURI, redisAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e env setup failed: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	env.close()
	os.Exit(code)
}

func newE2EEnv(mongoURI, redisAddr string) (*e2eEnv, error) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// ── MongoDB ─────────────────────────────────────────────────────────────
	mongoClient, err := mongo.Connect(mongoopts.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	if err := mongoClient.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("mongo ping: %w", err)
	}
	dbName := fmt.Sprintf("e2e_journey_%d", time.Now().UnixNano())
	db := mongoClient.Database(dbName)

	// ── Redis (HMAC dedup) ──────────────────────────────────
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	// ── Shared event bus (NATS JetStream; the topic prefix isolates this run) ─
	runID := time.Now().UnixNano()
	bus, err := eventbus.New(eventbus.Config{
		ConsumerGroup: fmt.Sprintf("e2e_consumers_%d", runID),
		TopicPrefix:   fmt.Sprintf("e2e_%d.", runID),
		NATS:          eventbus.NATSConfig{URL: devinfra.NATSURL()},
	})
	if err != nil {
		return nil, fmt.Errorf("event bus: %w", err)
	}

	// ── Secret management (against the local Vault) ──────────────────
	secretMgmt, err := secretmanagement.New(ctx, devinfra.SecretConfig())
	if err != nil {
		return nil, fmt.Errorf("secret management: %w", err)
	}

	// ── JWK service (Redis-backed) ────────────────────────────────────────────
	jwkSvc, err := jwkmanagement.NewFromConfig(devinfra.JWKConfig(redisAddr), log)
	if err != nil {
		return nil, fmt.Errorf("jwk service: %w", err)
	}

	// ── Control plane (mirrors internal/planes/control/api/entry.go) ────────
	connectorRepo, err := connector.NewMongoConnectorRepository(db, true)
	if err != nil {
		return nil, fmt.Errorf("connector repo: %w", err)
	}
	storeRepo, err := store.NewMongoStoreRepository(db, true)
	if err != nil {
		return nil, fmt.Errorf("store repo: %w", err)
	}
	accountRepo, err := account.NewMongoAccountRepository(db)
	if err != nil {
		return nil, fmt.Errorf("account repo: %w", err)
	}
	memberRepo, err := account.NewMongoMemberRepository(db)
	if err != nil {
		return nil, fmt.Errorf("member repo: %w", err)
	}
	taskRepo, err := task.NewMongoTaskRepository(db)
	if err != nil {
		return nil, fmt.Errorf("task repo: %w", err)
	}
	journeyRepo, err := journey.NewMongoJourneyRepository(db)
	if err != nil {
		return nil, fmt.Errorf("journey repo: %w", err)
	}
	manualStepRepo, err := operator.NewMongoManualStepRepository(db)
	if err != nil {
		return nil, fmt.Errorf("manual step repo: %w", err)
	}
	auditLogRepo, err := operator.NewMongoAuditLogRepository(db)
	if err != nil {
		return nil, fmt.Errorf("audit log repo: %w", err)
	}
	boardRepo, err := operator.NewMongoBoardRepository(db)
	if err != nil {
		return nil, fmt.Errorf("board repo: %w", err)
	}
	presenceRepo, err := operator.NewMongoPresenceRepository(db)
	if err != nil {
		return nil, fmt.Errorf("presence repo: %w", err)
	}
	consistencyRepo, err := operator.NewMongoConsistencyRepository(db)
	if err != nil {
		return nil, fmt.Errorf("consistency repo: %w", err)
	}
	healthRepo, err := operator.NewMongoHealthRepository(db)
	if err != nil {
		return nil, fmt.Errorf("health repo: %w", err)
	}
	readinessRepo, err := catalog.NewMongoReadinessRepository(db)
	if err != nil {
		return nil, fmt.Errorf("readiness repo: %w", err)
	}
	productRepo, err := catalog.NewMongoProductRepository(db)
	if err != nil {
		return nil, fmt.Errorf("product repo: %w", err)
	}
	remediationRepo, err := catalog.NewMongoRemediationRepository(db)
	if err != nil {
		return nil, fmt.Errorf("remediation repo: %w", err)
	}
	writeBackRepo, err := catalog.NewMongoWriteBackRepository(db)
	if err != nil {
		return nil, fmt.Errorf("write-back repo: %w", err)
	}
	auditJobRepo, err := catalog.NewMongoAuditJobRepository(db)
	if err != nil {
		return nil, fmt.Errorf("audit job repo: %w", err)
	}

	servingCache := serving.NewRedisCache(redisAddr, "", 0, 5*time.Minute)
	profileSource := serving.NewCacheProfileSource(servingCache)

	catalogSvc := catalog.NewCatalogService(readinessRepo, productRepo, remediationRepo, writeBackRepo, auditJobRepo,
		commandbus.NewEventBusCommandEmitter(bus))
	taskSvc := task.NewTaskService(taskRepo, catalogSvc, bus, log)
	accountSvc := account.NewAccountService(accountRepo, memberRepo, nil, unlimitedSeats{}, noTrial{})
	storeSvc := store.NewStoreService(storeRepo, jwkSvc, log)
	validatorReg := validatorregistry.New(log)
	platform.RegisterValidators(validatorReg)
	connectorSvc := connector.NewConnectorService(connectorRepo, bus, profileSource, secretMgmt, validatorReg, log)
	journeySvc := journey.NewJourneyService(journeyRepo, taskSvc, bus, log)
	operatorSvc := operator.NewOperatorService(
		manualStepRepo, auditLogRepo, boardRepo, taskSvc, journeySvc,
		servingCache, presenceRepo, operator.NewMongoOrderReader(db), consistencyRepo, healthRepo,
		storeRepo, bus, log,
	)

	const dedupTTL = 10 * time.Minute
	h := api.New(api.Dependencies{
		Logger:        log,
		Account:       accountSvc,
		Store:         storeSvc,
		Connector:     connectorSvc,
		Catalog:       catalogSvc,
		Task:          taskSvc,
		Journey:       journeySvc,
		Billing:       activeBillingStub{},
		Operator:      operatorSvc,
		Validation:    validation.StubService{},
		ResolveBearer: testBearerResolver,
		ResolveHMAC:   auth.NewHMACResolver(log, connectorSvc, redisClient, secretMgmt, dedupTTL),
	})
	e := echo.New()
	if err := h.SetupRoutes("/v1", e.Group("/v1")); err != nil {
		return nil, fmt.Errorf("control routes: %w", err)
	}
	controlSrv := httptest.NewServer(e)

	// ── Sync plane (mirrors internal/planes/sync/api/entry.go) ──────────────
	// The plugin's connector routes (/connector/signals, /connector/profile)
	// are the sync plane's.
	syncConnectorRepo, err := syncconnector.NewMongoConnectorRepository(db)
	if err != nil {
		return nil, fmt.Errorf("sync connector repo: %w", err)
	}
	syncConnectorSvc := syncconnector.NewConnectorService(syncConnectorRepo, bus, profileSource, secretMgmt, log)
	sh := syncapi.New(syncapi.Dependencies{
		Logger:         log,
		Connector:      syncConnectorSvc,
		EventPublisher: bus,
		ResolveHMAC:    syncauth.NewHMACResolver(log, syncConnectorSvc, redisClient, secretMgmt, dedupTTL),
	})
	se := echo.New()
	if err := sh.SetupRoutes("/v1", se.Group("/v1")); err != nil {
		return nil, fmt.Errorf("sync routes: %w", err)
	}
	syncSrv := httptest.NewServer(se)

	provider := func(merchantID, storeID string) (ucp.StoreProvider, error) {
		_, platformName, err := syncConnectorSvc.GetStoreCredentials(context.Background(), storeID)
		if err != nil {
			return nil, fmt.Errorf("resolve platform for store %s: %w", storeID, err)
		}
		return storeIdentity{merchantID: merchantID, storeID: storeID, platform: platformName}, nil
	}
	reg := base.New(base.CredentialsFunc(func(ctx context.Context, storeID string) (map[string]any, error) {
		creds, _, err := syncConnectorSvc.GetStoreCredentials(ctx, storeID)
		return creds, err
	}), log)
	platform.RegisterConnectors(reg)

	remediations, err := syncsvc.NewMongoRemediationStore(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("remediation store: %w", err)
	}
	syncService := syncsvc.NewSyncService(db, bus, reg, provider, remediations,
		concurrency.NewRedisLease(redisClient, "sync:sweep:"),
		asynq.RedisClientOpt{Addr: redisAddr}, redisClient, log)
	if err := syncService.Start(ctx); err != nil {
		return nil, fmt.Errorf("start sync service: %w", err)
	}

	return &e2eEnv{
		controlServer: controlSrv,
		syncServer:    syncSrv,
		mongoClient:   mongoClient,
		db:            db,
		redisClient:   redisClient,
		eventBus:      bus,
		syncSvc:       syncService,
		connectorRepo: connectorRepo,
		storeRepo:     storeRepo,
		accountRepo:   accountRepo,
		memberRepo:    memberRepo,
		secretMgmt:    secretMgmt,
	}, nil
}

// unlimitedSeats lets the operator invite members without a billing plan.
type unlimitedSeats struct{}

func (unlimitedSeats) Get(context.Context, domain.MerchantID) (domain.Billing, error) {
	return domain.Billing{Status: domain.BillingActive}, nil
}

// noTrial skips provisioning a Stripe trial for a new merchant (Stripe is not
// available here).
type noTrial struct{}

func (noTrial) ProvisionTrial(context.Context, domain.MerchantID, string) error { return nil }

// storeIdentity is the registry identity the sync plane resolves a store to:
// its merchant, id and platform (the only Configuration fields the connector
// registry reads).
type storeIdentity struct {
	merchantID, storeID string
	platform            domain.Platform
}

func (s storeIdentity) Configuration() ucp.Configuration {
	return ucp.Configuration{MerchantID: s.merchantID, StoreID: s.storeID, Platform: s.platform}
}
func (s storeIdentity) StoreID() string                                   { return s.storeID }
func (s storeIdentity) Version() string                                   { return "" }
func (s storeIdentity) Capabilities() map[string][]wire.Capability        { return nil }
func (s storeIdentity) Services() map[string][]wire.Service               { return nil }
func (s storeIdentity) PaymentHandlers() map[string][]wire.PaymentHandler { return nil }
func (s storeIdentity) SigningKeys() []wire.JWK                           { return nil }
func (s storeIdentity) StorefrontURL() string                             { return "" }

func (e *e2eEnv) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = e.syncSvc.Stop(ctx)
	e.controlServer.Close()
	e.syncServer.Close()
	_ = e.eventBus.Close()
	_ = e.db.Drop(context.Background())
	_ = e.mongoClient.Disconnect(context.Background())
	_ = e.redisClient.Close()
}

func (e *e2eEnv) controlURL(path string) string {
	return e.controlServer.URL + path
}

// syncURL is the sync plane's — the plugin's connector routes.
func (e *e2eEnv) syncURL(path string) string {
	return e.syncServer.URL + path
}
