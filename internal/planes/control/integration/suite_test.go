// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

//go:build integration

// Package integration contains end-to-end HTTP tests for the control plane.
//
// Each test hits the real Echo handler wired to real MongoDB repositories and a
// real Redis client. IdP JWT verification and Stripe are replaced by
// lightweight test doubles so the suite runs with only MongoDB and Redis
// available (see docker-compose.yaml).
//
// Run with:
//
//	MONGO_URI="mongodb://admin:password@localhost:27017/?directConnection=true" REDIS_ADDR=localhost:6379 \
//	  go test -tags=integration ./internal/planes/control/integration/ -v
package integration

import (
	"context"
	"errors"
	"fmt"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/devinfra"
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

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/commandbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/jwkmanagement"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/secretmanagement"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/account"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/api"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/auth"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/billing"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/catalog"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/connector"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/journey"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/operator"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/store"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/task"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/validation"
	"github.com/sarvexlabs/sarvex.cp/internal/testtenancy"
)

// Fixed tokens resolved by testBearerResolver — never valid JWTs in production.
const (
	tokenMerchantA      = "merchant-a-token"
	tokenMerchantAStaff = "merchant-a-staff-token"
	tokenMerchantB      = "merchant-b-token"
	tokenOperator       = "operator-token"
)

// Fixed merchant IDs used throughout the suite.
const (
	merchantAID = domain.MerchantID("merch-a")
	merchantBID = domain.MerchantID("merch-b")
)

// testBearerResolver replaces IdP JWT verification during integration tests.
// Three fixed tokens map to known principals; a testtenancy-minted token
// (see internal/testtenancy) resolves dynamically to one of the N tenants a
// tenant_isolation_test.go matrix seeds; any other token is rejected.
func testBearerResolver(token string) (domain.Principal, error) {
	if i, ok := testtenancy.TenantFromToken(token); ok {
		merchantID := domain.MerchantID(testtenancy.MerchantID(i))
		return domain.Principal{
			UserID:     fmt.Sprintf("tenancy-test-user-%d", i),
			Email:      fmt.Sprintf("tenancy-test-%d@example.com", i),
			Role:       domain.RoleOwner,
			MerchantID: merchantID,
		}, nil
	}
	switch token {
	case tokenMerchantA:
		return domain.Principal{
			UserID:     "user-a",
			Email:      "merchant-a@example.com",
			Role:       domain.RoleOwner,
			MerchantID: merchantAID,
		}, nil
	case tokenMerchantAStaff:
		return domain.Principal{
			UserID:     "user-a-staff",
			Email:      "staff-a@example.com",
			Role:       domain.RoleStaff,
			MerchantID: merchantAID,
		}, nil
	case tokenMerchantB:
		return domain.Principal{
			UserID:     "user-b",
			Email:      "merchant-b@example.com",
			Role:       domain.RoleOwner,
			MerchantID: merchantBID,
		}, nil
	case tokenOperator:
		return domain.Principal{
			UserID: "operator-1",
			Email:  "operator@sarvex.example.com",
			Role:   domain.RoleOperator,
		}, nil
	default:
		return domain.Principal{}, errors.New("unknown test token")
	}
}

// testSeatLimiter is a fixed-cap account.SeatLimiter for integration tests —
// standing in for the real billing-plan seat cap without a Stripe subscription.
type testSeatLimiter struct {
	cap int
}

func (l testSeatLimiter) Get(_ context.Context, _ domain.MerchantID) (domain.Billing, error) {
	return domain.Billing{Usage: domain.BillingUsage{MemberCap: l.cap}}, nil
}

// testTrialProvisioner is an in-memory account.TrialProvisioner for
// integration tests — standing in for the real Stripe-backed
// billing.BillingService.ProvisionTrial, so account creation can be
// exercised without a real Stripe account.
type testTrialProvisioner struct {
	mu    sync.Mutex
	calls map[domain.MerchantID]string // merchant_id -> owner email
}

func newTestTrialProvisioner() *testTrialProvisioner {
	return &testTrialProvisioner{calls: make(map[domain.MerchantID]string)}
}

func (p *testTrialProvisioner) ProvisionTrial(_ context.Context, id domain.MerchantID, email string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls[id] = email
	return nil
}

// activeBillingStub is billing.StubService with Get overridden to report an
// active subscription, so requireActiveBilling doesn't block the mutating
// routes this suite exercises (Stripe isn't available here).
type activeBillingStub struct {
	billing.StubService
}

func (activeBillingStub) Get(_ context.Context, _ domain.MerchantID) (domain.Billing, error) {
	return domain.Billing{Status: domain.BillingActive}, nil
}

// testEnv holds all shared state for the integration test suite.
type testEnv struct {
	server      *httptest.Server
	mongoClient *mongo.Client
	db          *mongo.Database
	redisClient *redis.Client

	// Repositories exposed for direct seeding in tests.
	connectorRepo connector.ConnectorRepository
	storeRepo     store.StoreRepository
	accountRepo   account.AccountRepository
	memberRepo    account.MemberRepository
	taskRepo      task.TaskRepository

	// trialProvisioner records every account.Create -> ProvisionTrial call,
	// so tests can assert a merchant never gets created without one.
	trialProvisioner *testTrialProvisioner
}

// env is the single shared test environment, initialised by TestMain.
var env *testEnv

func TestMain(m *testing.M) {
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://admin:password@localhost:27017/?directConnection=true"
	}
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	var err error
	env, err = newTestEnv(mongoURI, redisAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration test env setup failed: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	env.close()
	os.Exit(code)
}

func newTestEnv(mongoURI, redisAddr string) (*testEnv, error) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// ── MongoDB ────────────────────────────────────────────────────────────
	mongoClient, err := mongo.Connect(mongoopts.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	if err := mongoClient.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("mongo ping: %w", err)
	}
	dbName := fmt.Sprintf("control_inttest_%d", time.Now().UnixNano())
	db := mongoClient.Database(dbName)

	// ── Redis ──────────────────────────────────────────────────────────────
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	// ── Repositories ───────────────────────────────────────────────────────
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

	// ── Services ───────────────────────────────────────────────────────────
	commandEmitter := commandbus.NewEventBusCommandEmitter(eventbus.NoopEventBus{})
	catalogSvc := catalog.NewCatalogService(
		readinessRepo, productRepo, remediationRepo, writeBackRepo, auditJobRepo,
		commandEmitter,
	)
	taskSvc := task.NewTaskService(taskRepo, catalogSvc, eventbus.NoopEventBus{}, log)
	jwkSvc, err := jwkmanagement.NewFromConfig(devinfra.JWKConfig(redisAddr), log)
	if err != nil {
		return nil, fmt.Errorf("jwk service: %w", err)
	}
	// Integration tests never complete pairing with real credentials, so a
	// secret-management instance against the local Vault is
	// sufficient.
	secretMgmt, err := secretmanagement.New(context.Background(), devinfra.SecretConfig())
	if err != nil {
		return nil, fmt.Errorf("secret management: %w", err)
	}
	connectorSvc := connector.NewConnectorService(
		connectorRepo,
		eventbus.NoopEventBus{},
		&ucp.StubProfileSource{},
		secretMgmt,
		nil, // validators: only needed at pairing-complete, not exercised here
		log,
	)
	storeSvc := store.NewStoreService(storeRepo, jwkSvc, log)

	// The repositories the operator service's health/presence/orders reads
	// need. Wired for real against the same test database as everything else
	// rather than passed as nil, so an authenticated test against those
	// routes exercises the real read path instead of hitting a nil.
	presenceRepo, err := operator.NewMongoPresenceRepository(db)
	if err != nil {
		return nil, fmt.Errorf("presence repository: %w", err)
	}
	orderReader := operator.NewMongoOrderReader(db)
	consistencyRepo, err := operator.NewMongoConsistencyRepository(db)
	if err != nil {
		return nil, fmt.Errorf("consistency repository: %w", err)
	}
	healthRepo, err := operator.NewMongoHealthRepository(db)
	if err != nil {
		return nil, fmt.Errorf("health repository: %w", err)
	}

	journeySvc := journey.NewJourneyService(journeyRepo, taskSvc, eventbus.NoopEventBus{}, log)

	// servingCache is nil: it is a Redis-backed reader owned by the serving
	// plane, and constructing one here would add a control→serving import
	// that ADR-0004 forbids. The operator service treats it as optional —
	// sync-freshness is omitted from health, and the presence reads return
	// errServingCacheUnavailable rather than panicking.
	operatorSvc := operator.NewOperatorService(
		manualStepRepo, auditLogRepo, boardRepo, taskSvc, journeySvc,
		nil, presenceRepo, orderReader, consistencyRepo, healthRepo,
		storeRepo, eventbus.NoopEventBus{}, log,
	)

	// Billing and validation use stubs — Stripe and the validation test harness
	// are external services not available during integration tests.
	// billingSvc reports an active subscription (rather than
	// billing.StubService's ErrNotImplemented) so requireActiveBilling
	// doesn't 403 every mutating route this suite exercises.
	billingSvc := activeBillingStub{}
	validationSvc := validation.StubService{}

	// The self-serve team-invite seat cap is billing-plan data (see
	// domain.BillingUsage.MemberCap); testSeatLimiter fixes it at a small value
	// so cap-reached behavior is exercisable without a real Stripe subscription.
	trialProvisioner := newTestTrialProvisioner()
	accountSvc := account.NewAccountService(accountRepo, memberRepo, nil, testSeatLimiter{cap: 2}, trialProvisioner)

	// ── HTTP handler ───────────────────────────────────────────────────────
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

	return &testEnv{
		server:           httptest.NewServer(e),
		mongoClient:      mongoClient,
		db:               db,
		redisClient:      redisClient,
		connectorRepo:    connectorRepo,
		storeRepo:        storeRepo,
		accountRepo:      accountRepo,
		memberRepo:       memberRepo,
		taskRepo:         taskRepo,
		trialProvisioner: trialProvisioner,
	}, nil
}

func (e *testEnv) close() {
	e.server.Close()
	_ = e.db.Drop(context.Background())
	_ = e.mongoClient.Disconnect(context.Background())
	_ = e.redisClient.Close()
}

// url returns the full URL for a given API path on the test server.
func (e *testEnv) url(path string) string {
	return e.server.URL + path
}
