// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build scenarios

package scenarios_test

import (
	"context"
	"fmt"
	"github.com/sarvexlab/sarvex/internal/pkg/devinfra"
	"log/slog"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongoopts "go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlab/sarvex/internal/pkg/commandbus"
	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/jwkmanagement"
	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
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
	syncapi "github.com/sarvexlab/sarvex/internal/planes/sync/api"
	syncauth "github.com/sarvexlab/sarvex/internal/planes/sync/auth"
	syncconnector "github.com/sarvexlab/sarvex/internal/planes/sync/connector"
	"github.com/sarvexlab/sarvex/internal/testtenancy"
)

// activeBillingStub is billing.StubService with Get overridden to report an
// active subscription, so requireActiveBilling doesn't block the mutating
// routes these scenarios exercise (Stripe isn't available here).
type activeBillingStub struct {
	billing.StubService
}

func (activeBillingStub) Get(_ context.Context, _ domain.MerchantID) (domain.Billing, error) {
	return domain.Billing{Status: domain.BillingActive}, nil
}

// scenarioEnv holds all infrastructure for scenario tests.
type scenarioEnv struct {
	server      *httptest.Server
	mongoClient *mongo.Client
	db          *mongo.Database
	redisClient *redis.Client
	eventBus    *eventCapture

	// secretMgmt is the same Vault-backed secret-management instance both planes' HMAC
	// resolvers decrypt through. Seeding a connector has to store its secret
	// the way pairing does — as an EncryptedConfig reference — or the resolver
	// cannot verify a signature against it.
	secretMgmt secretmanagement.SecretManagementService

	// Repositories
	connectorRepo connector.ConnectorRepository
	storeRepo     store.StoreRepository
	accountRepo   account.AccountRepository
	memberRepo    account.MemberRepository
	taskRepo      task.TaskRepository
}

// eventCapture captures all events published to the event bus during tests.
type eventCapture struct {
	mu     sync.Mutex
	events []eventbus.DomainEvent
}

func (ec *eventCapture) Publish(ctx context.Context, event eventbus.DomainEvent) error {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.events = append(ec.events, event)
	return nil
}

func (ec *eventCapture) Events() []eventbus.DomainEvent {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	result := make([]eventbus.DomainEvent, len(ec.events))
	copy(result, ec.events)
	return result
}

func (ec *eventCapture) Clear() {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.events = nil
}

func (ec *eventCapture) FindEvent(eventType string, merchantID domain.MerchantID) eventbus.DomainEvent {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	for _, e := range ec.events {
		if e.EventType() == eventType && domain.MerchantID(e.MerchantID()) == merchantID {
			return e
		}
	}
	return nil
}

func (ec *eventCapture) Close() error {
	return nil
}

func (ec *eventCapture) Subscribe(ctx context.Context, eventType string, handler eventbus.EventHandler) (eventbus.Subscription, error) {
	return nil, nil
}

func (ec *eventCapture) Unsubscribe(ctx context.Context, sub eventbus.Subscription) error {
	return nil
}

// testBearerResolver maps test tokens to principals. A testtenancy-minted
// token (see internal/testtenancy) resolves dynamically to one of the N
// tenants a tenant_isolation_matrix_test.go run seeds.
func testBearerResolver(token string) (domain.Principal, error) {
	if i, ok := testtenancy.TenantFromToken(token); ok {
		return domain.Principal{
			UserID:     fmt.Sprintf("tenancy-test-user-%d", i),
			Email:      fmt.Sprintf("tenancy-test-%d@example.com", i),
			Role:       domain.RoleOwner,
			MerchantID: domain.MerchantID(testtenancy.MerchantID(i)),
		}, nil
	}
	switch token {
	case "merchant-a-token":
		return domain.Principal{
			UserID:     "user-a",
			Email:      "merchant-a@example.com",
			Role:       domain.RoleOwner,
			MerchantID: "merch-a",
		}, nil
	case "merchant-b-token":
		return domain.Principal{
			UserID:     "user-b",
			Email:      "merchant-b@example.com",
			Role:       domain.RoleOwner,
			MerchantID: "merch-b",
		}, nil
	case "operator-token":
		return domain.Principal{
			UserID: "operator-1",
			Email:  "operator@sarvex.example.com",
			Role:   domain.RoleOperator,
		}, nil
	default:
		return domain.Principal{}, fmt.Errorf("unknown token: %s", token)
	}
}

var env *scenarioEnv

func TestMain(m *testing.M) {
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		fmt.Fprintln(os.Stderr, "MONGO_URI not set; skipping scenario tests")
		os.Exit(0)
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	var err error
	env, err = newScenarioEnv(mongoURI, redisAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scenario env setup failed: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	env.close()
	os.Exit(code)
}

func newScenarioEnv(mongoURI, redisAddr string) (*scenarioEnv, error) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// MongoDB
	mongoClient, err := mongo.Connect(mongoopts.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	if err := mongoClient.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("mongo ping: %w", err)
	}
	dbName := fmt.Sprintf("control_scenario_%d", time.Now().UnixNano())
	db := mongoClient.Database(dbName)

	// Redis
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	// Repositories
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
		return nil, fmt.Errorf("writeback repo: %w", err)
	}
	auditJobRepo, err := catalog.NewMongoAuditJobRepository(db)
	if err != nil {
		return nil, fmt.Errorf("audit job repo: %w", err)
	}

	// Event bus capture for cross-plane tracking
	eventCapture := &eventCapture{}

	// Services. catalogSvc is built before taskSvc because the task service
	// takes it as its ReadinessLookup — the same order entry.go wires them in.
	commandEmitter := commandbus.NewEventBusCommandEmitter(eventCapture)
	catalogSvc := catalog.NewCatalogService(
		readinessRepo, productRepo, remediationRepo, writeBackRepo, auditJobRepo,
		commandEmitter,
	)
	taskSvc := task.NewTaskService(taskRepo, catalogSvc, eventCapture, log)
	accountSvc := account.NewAccountService(accountRepo, memberRepo, nil, nil, nil) // seatLimiter, trialProvisioner unused: InviteTeamMember/Create not exercised in these scenarios
	jwkSvc, err := jwkmanagement.NewFromConfig(devinfra.JWKConfig(redisAddr), log)
	if err != nil {
		return nil, fmt.Errorf("jwk service: %w", err)
	}
	secretMgmt, err := secretmanagement.New(context.Background(), devinfra.SecretConfig())
	if err != nil {
		return nil, fmt.Errorf("secret management: %w", err)
	}
	// Key provisioning moved out of the connector service; validators are
	// only needed at pairing-complete, which these scenarios don't exercise.
	connectorSvc := connector.NewConnectorService(
		connectorRepo,
		eventCapture,
		&ucp.StubProfileSource{},
		secretMgmt,
		nil,
		log,
	)
	storeSvc := store.NewStoreService(storeRepo, jwkSvc, log)

	// Repositories behind the operator service's health/presence/orders
	// reads, against the same test database as everything else.
	presenceRepo, err := operator.NewMongoPresenceRepository(db)
	if err != nil {
		return nil, fmt.Errorf("presence repo: %w", err)
	}
	orderReader := operator.NewMongoOrderReader(db)
	consistencyRepo, err := operator.NewMongoConsistencyRepository(db)
	if err != nil {
		return nil, fmt.Errorf("consistency repo: %w", err)
	}
	healthRepo, err := operator.NewMongoHealthRepository(db)
	if err != nil {
		return nil, fmt.Errorf("health repo: %w", err)
	}

	journeySvc := journey.NewJourneyService(journeyRepo, taskSvc, eventCapture, log)

	// servingCache is nil — it is a Redis-backed reader owned by the serving
	// plane, and constructing one here would add a control→serving import
	// that ADR-0004 forbids. The operator service treats it as optional.
	operatorSvc := operator.NewOperatorService(
		manualStepRepo,
		auditLogRepo,
		boardRepo,
		taskSvc,
		journeySvc,
		nil,
		presenceRepo,
		orderReader,
		consistencyRepo,
		healthRepo,
		storeRepo,
		eventCapture,
		log,
	)

	// Stubs for external services. billingSvc reports an active subscription
	// (rather than billing.StubService's ErrNotImplemented) so
	// requireActiveBilling doesn't 403 every mutating route these scenarios
	// exercise — Stripe itself isn't available in this test environment.
	billingSvc := activeBillingStub{}
	validationSvc := validation.StubService{}

	// HTTP handler
	const dedupTTL = 10 * time.Minute
	h := api.New(api.Dependencies{
		Logger:        log,
		Account:       accountSvc,
		Store:         storeSvc,
		Connector:     connectorSvc,
		Catalog:       catalogSvc,
		Task:          taskSvc,
		Journey:       journeySvc,
		Billing:       billingSvc,
		Operator:      operatorSvc,
		Validation:    validationSvc,
		ResolveBearer: testBearerResolver,
		ResolveHMAC:   auth.NewHMACResolver(log, connectorSvc, redisClient, secretMgmt, dedupTTL),
	})

	e := echo.New()
	g := e.Group("/v1")
	if err := h.SetupRoutes("/v1", g); err != nil {
		return nil, fmt.Errorf("setup routes: %w", err)
	}

	// The sync plane's connector-ingest edge, mounted on the same group.
	//
	// These are cross-plane E2E flows, and the plugin's three routes
	// (/connector/{signals,heartbeat,profile}) moved to the sync plane when the
	// planes were split — the scenarios were never updated, so every scenario
	// that starts with the plugin signalling was hitting a control-plane 404.
	//
	// Mounting both handlers behind one group is what deployment does: the
	// paths are bundled into the control-plane spec precisely so a single
	// public boundary reaches both planes, with requests routed by path prefix
	// (see sarvex-v1-control-plane.yaml, "Connector Ingest"). The two route
	// sets are disjoint, so neither handler shadows the other. This is a test
	// harness, not a plane, so composing both is not a plane-isolation
	// violation — nothing here is imported by either plane.
	syncConnectorRepo, err := syncconnector.NewMongoConnectorRepository(db)
	if err != nil {
		return nil, fmt.Errorf("sync connector repository: %w", err)
	}
	syncConnectorSvc := syncconnector.NewConnectorService(
		syncConnectorRepo, eventCapture, &ucp.StubProfileSource{}, secretMgmt, log,
	)
	syncHandler := syncapi.New(syncapi.Dependencies{
		Logger:         log,
		Connector:      syncConnectorSvc,
		EventPublisher: eventCapture,
		ResolveHMAC:    syncauth.NewHMACResolver(log, syncConnectorSvc, redisClient, secretMgmt, dedupTTL),
	})
	if err := syncHandler.SetupRoutes("/v1", g); err != nil {
		return nil, fmt.Errorf("setup sync routes: %w", err)
	}

	return &scenarioEnv{
		server:        httptest.NewServer(e),
		mongoClient:   mongoClient,
		db:            db,
		redisClient:   redisClient,
		eventBus:      eventCapture,
		secretMgmt:    secretMgmt,
		connectorRepo: connectorRepo,
		storeRepo:     storeRepo,
		accountRepo:   accountRepo,
		memberRepo:    memberRepo,
		taskRepo:      taskRepo,
	}, nil
}

func (e *scenarioEnv) close() {
	e.server.Close()
	_ = e.db.Drop(context.Background())
	_ = e.mongoClient.Disconnect(context.Background())
	_ = e.redisClient.Close()
}

func (e *scenarioEnv) url(path string) string {
	return e.server.URL + path
}
