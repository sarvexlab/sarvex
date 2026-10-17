// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package api is the HTTP layer for the Sarvex V1 control plane.
//
// All routes follow the contract defined in sarvex-v1-control-plane.yaml.
// merchant_id is never a URL parameter — it is resolved from the authenticated
// principal injected by requireAuth.
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
	"github.com/samber/lo"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongoopts "go.mongodb.org/mongo-driver/v2/mongo/options"

	"go.opentelemetry.io/otel"

	"github.com/sarvexlab/sarvex/internal/pkg/commandbus"
	"github.com/sarvexlab/sarvex/internal/pkg/common"
	pkgdomain "github.com/sarvexlab/sarvex/internal/pkg/domain"
	"github.com/sarvexlab/sarvex/internal/pkg/email"
	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/jwkmanagement"
	"github.com/sarvexlab/sarvex/internal/pkg/middlewares"
	"github.com/sarvexlab/sarvex/internal/pkg/otelboot"
	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
	"github.com/sarvexlab/sarvex/internal/pkg/serving"
	"github.com/sarvexlab/sarvex/internal/pkg/simstore"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	validatorregistry "github.com/sarvexlab/sarvex/internal/pkg/ucp/validator/registry"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
	"github.com/sarvexlab/sarvex/internal/pkg/utils"
	"github.com/sarvexlab/sarvex/internal/planes/control/account"
	"github.com/sarvexlab/sarvex/internal/planes/control/activation"
	"github.com/sarvexlab/sarvex/internal/planes/control/auth"
	"github.com/sarvexlab/sarvex/internal/planes/control/billing"
	"github.com/sarvexlab/sarvex/internal/planes/control/catalog"
	"github.com/sarvexlab/sarvex/internal/planes/control/channel"
	"github.com/sarvexlab/sarvex/internal/planes/control/checkoutescalation"
	"github.com/sarvexlab/sarvex/internal/planes/control/connector"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/endpointhealth"
	"github.com/sarvexlab/sarvex/internal/planes/control/facts"
	"github.com/sarvexlab/sarvex/internal/planes/control/insights"
	"github.com/sarvexlab/sarvex/internal/planes/control/journey"
	"github.com/sarvexlab/sarvex/internal/planes/control/ledger"
	"github.com/sarvexlab/sarvex/internal/planes/control/monitoring"
	"github.com/sarvexlab/sarvex/internal/planes/control/operator"
	"github.com/sarvexlab/sarvex/internal/planes/control/realtime"
	"github.com/sarvexlab/sarvex/internal/planes/control/sim"
	"github.com/sarvexlab/sarvex/internal/planes/control/store"
	"github.com/sarvexlab/sarvex/internal/planes/control/surface"
	surfaceall "github.com/sarvexlab/sarvex/internal/planes/control/surface/all"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
	"github.com/sarvexlab/sarvex/internal/planes/control/validation"
)

const controlGroupName = "/v1"

type controlPlaneEntry struct {
	configPath string
}

// NewControlPlaneEntry returns a ServiceEntry that runs the Sarvex V1 control plane.
func NewControlPlaneEntry(path string) common.ServiceEntry {
	return &controlPlaneEntry{configPath: path}
}

func (s *controlPlaneEntry) Init(_ *bool, port *int, env *string) error {
	config, log, err := s.initConfigAndLogger(env)
	if err != nil {
		return err
	}

	otelShutdown := otelboot.Setup(context.Background(), "control")
	defer func() { _ = otelShutdown(context.Background()) }()

	e, group := s.initEchoServerWithMiddlewares(config.DashboardAddress)

	mongoClient, db, redisClient, eventBus, dedupTTL, err := s.initDatabaseConnections(config, log)
	if err != nil {
		return err
	}
	defer mongoClient.Disconnect(context.Background())
	defer redisClient.Close()
	defer eventBus.Close()

	// Activation subscriber needs its own event bus with a different consumer group
	// to avoid conflicts with the journey subscriber on the same topic (control.journey.signal).
	// Each gets its own subscription, so both can receive independently.
	activationEventBus, err := s.initActivationEventBus(config, log)
	if err != nil {
		return err
	}
	defer activationEventBus.Close()

	// The journey subscriber needs the same isolation on the other side: it
	// also subscribes to control.store.activation_state_changed, and so does
	// the surface subscriber (surfaceSubscriber below), both still on the
	// shared eventBus's default consumer group. Two subscribers competing
	// for one group means each event goes to only one of them — Redis
	// the bus distributes a consumer group's messages across
	// its members instead of fanning out to each, so the journey subscriber
	// silently misses roughly half the activation transitions it needs.
	// That is what let a store sit at journey "ready" forever despite its
	// UCP profile genuinely reaching "active": the one control.store.
	// activation_state_changed event that would have advanced it went to
	// surfaceSubscriber's handler instead. Its own bus removes it from that
	// competition entirely, mirroring activationEventBus/realtimeEventBus above.
	journeyEventBus, err := s.initJourneyEventBus(config, log)
	if err != nil {
		return err
	}
	defer func() { _ = journeyEventBus.Close() }()

	// ── Repository Initialization ──────────────────────────────────────────

	// Account repositories
	accountRepo, err := account.NewMongoAccountRepository(db)
	if err != nil {
		log.Error("failed to create account repository", "error", err)
		return err
	}
	memberRepo, err := account.NewMongoMemberRepository(db)
	if err != nil {
		log.Error("failed to create member repository", "error", err)
		return err
	}

	// Connector repository
	connectorRepo, err := connector.NewMongoConnectorRepository(db, true)
	if err != nil {
		log.Error("failed to create connector repository", "error", err)
		return err
	}

	// Catalog repositories
	readinessRepo, err := catalog.NewMongoReadinessRepository(db)
	if err != nil {
		log.Error("failed to create readiness repository", "error", err)
		return err
	}
	productRepo, err := catalog.NewMongoProductRepository(db)
	if err != nil {
		log.Error("failed to create product repository", "error", err)
		return err
	}
	remediationRepo, err := catalog.NewMongoRemediationRepository(db)
	if err != nil {
		log.Error("failed to create remediation repository", "error", err)
		return err
	}
	writeBackRepo, err := catalog.NewMongoWriteBackRepository(db)
	if err != nil {
		log.Error("failed to create writeback repository", "error", err)
		return err
	}
	auditJobRepo, err := catalog.NewMongoAuditJobRepository(db)
	if err != nil {
		log.Error("failed to create audit job repository", "error", err)
		return err
	}

	// Task repository
	taskRepo, err := task.NewMongoTaskRepository(db)
	if err != nil {
		log.Error("failed to create task repository", "error", err)
		return err
	}

	// Journey repository
	journeyRepo, err := journey.NewMongoJourneyRepository(db)
	if err != nil {
		log.Error("failed to create journey repository", "error", err)
		return err
	}

	// Billing repositories
	billingRepo, err := billing.NewMongoBillingRepository(db)
	if err != nil {
		log.Error("failed to create billing repository", "error", err)
		return err
	}
	notificationPrefsRepo, err := billing.NewMongoNotificationPrefsRepository(db)
	if err != nil {
		log.Error("failed to create notification preferences repository", "error", err)
		return err
	}
	webhookEventRepo, err := billing.NewMongoWebhookEventRepository(db)
	if err != nil {
		log.Error("failed to create billing webhook event repository", "error", err)
		return err
	}

	// Operator repositories
	manualStepRepo, err := operator.NewMongoManualStepRepository(db)
	if err != nil {
		log.Error("failed to create manual step repository", "error", err)
		return err
	}
	auditLogRepo, err := operator.NewMongoAuditLogRepository(db)
	if err != nil {
		log.Error("failed to create audit log repository", "error", err)
		return err
	}
	boardRepo, err := operator.NewMongoBoardRepository(db)
	if err != nil {
		log.Error("failed to create board repository", "error", err)
		return err
	}
	presenceRepo, err := operator.NewMongoPresenceRepository(db)
	if err != nil {
		log.Error("failed to create presence repository", "error", err)
		return err
	}
	orderReader := operator.NewMongoOrderReader(db)
	consistencyRepo, err := operator.NewMongoConsistencyRepository(db)
	if err != nil {
		log.Error("failed to create consistency findings repository", "error", err)
		return err
	}
	healthRepo, err := operator.NewMongoHealthRepository(db)
	if err != nil {
		log.Error("failed to create health repository", "error", err)
		return err
	}

	// ── Service Initialization ─────────────────────────────────────────────

	// Catalog service with EventBus-backed CommandEmitter (Phase 5 - handlers wired in sync plane)
	commandEmitter := commandbus.NewEventBusCommandEmitter(eventBus)
	catalogSvc := catalog.NewCatalogService(
		readinessRepo, productRepo, remediationRepo, writeBackRepo, auditJobRepo,
		commandEmitter,
	)

	// Task service (depends on catalog service for readiness-task drill-in;
	// bus publishes control.task.updated for the console-realtime hub)
	taskSvc := task.NewTaskService(taskRepo, catalogSvc, eventBus, log)

	localeProvider := account.NewStaticLocaleProvider()

	// Serving cache (read-only here) backs the plugin profile doorway: the
	// connector service serves the last-known-good rendered profile from the
	// same Redis snapshot the serving plane builds and reads.
	servingCache := serving.NewRedisCache(
		config.RedisAddress,
		config.RedisPassword,
		config.RedisDatabase,
		5*time.Minute,
	)

	// JWK management — single source of truth for store signing keys. The
	// connector service provisions a store's keypair here at pairing completion;
	// the serving plane reads the same active keypair to sign and to publish the
	// profile. Cross-plane sharing requires a durable backend (ModeRedis); see
	// jwkmanagement.NewFromConfig.
	jwkService, err := jwkmanagement.NewFromConfig(jwkmanagement.Config{
		Mode:                jwkmanagement.Mode(config.JWKManagementMode),
		RedisAddr:           config.RedisAddress,
		RedisPassword:       config.RedisPassword,
		RedisDB:             config.RedisDatabase,
		VaultAddr:           config.VaultAddr,
		VaultToken:          config.VaultToken,
		VaultTransitKeyName: config.VaultTransitKeyName,
		RequireEncryption:   config.Env == "production",
	}, log)
	if err != nil {
		log.Error("failed to initialize jwk management", "error", err)
		return err
	}

	// Secret management — stores/retrieves per-store API credentials
	// provisioned at pairing in Vault KV v2.
	secretMgmt, err := secretmanagement.New(context.Background(), secretmanagement.Config{
		VaultAddr:  config.VaultAddr,
		VaultToken: config.VaultToken,
	})
	if err != nil {
		log.Error("failed to initialize secret management", "error", err)
		return err
	}

	// Email — member invites, task notifications, and general control-plane
	// notifications. ModeMock (default) logs instead of sending, for local/test;
	// ModeResend sends via the Resend API in production.
	emailClient, err := email.NewClient(email.Config{
		Mode:         email.Mode(config.EmailMode),
		ResendAPIKey: config.ResendApiKey,
	})
	if err != nil {
		log.Error("failed to initialize email client", "error", err)
		return err
	}
	emailSender := email.NewSender(emailClient, config.SenderEmailAddress, config.SenderTitle)

	// Contact — POST /contact (sarvex.example.com marketing website ingest) sends as
	// CONTACT_SENDER_EMAIL rather than the general SenderEmailAddress
	// configured above, so it gets its own Sender over the same transport
	// client. contactNotifyEmail/contactSenderEmail/contactAllowedOrigins
	// each default when their env var is unset, rather than hardcoding the
	// default into the config struct's zero value.
	contactSenderEmail := config.ContactSenderEmail
	if contactSenderEmail == "" {
		contactSenderEmail = "hello@sarvex.example.com"
	}
	contactSender := email.NewSender(emailClient, contactSenderEmail, "Sarvex")
	contactNotifyEmail := config.ContactNotifyEmail
	if contactNotifyEmail == "" {
		contactNotifyEmail = "hello@sarvex.example.com"
	}
	contactAllowedOrigins := strings.Split(config.ContactAllowedOrigins, ",")
	if config.ContactAllowedOrigins == "" {
		contactAllowedOrigins = []string{"https://sarvex.example.com", "https://www.sarvex.example.com"}
	}

	// Validator registry — validates platform credentials at pairing time before
	// they are encrypted and persisted.
	validatorReg := validatorregistry.New(log)
	platform.RegisterValidators(validatorReg)

	// Connector service (wired with real event bus; profile source reads the
	// serving cache to feed GET /connector/profile).
	connectorSvc := connector.NewConnectorService(
		connectorRepo,
		eventBus, // Events flow to sync plane for resync and signal ingestion
		serving.NewCacheProfileSource(servingCache),
		secretMgmt,
		validatorReg,
		log,
	)

	// Journey service (depends on task service for prescribed-task
	// reconciliation on stage transitions — see
	// journey.JourneyService.reconcileStageWork)
	journeySvc := journey.NewJourneyService(journeyRepo, taskSvc, eventBus, log)

	journeySubscriber := journey.NewSubscriber(journeySvc, connectorSvc, journeyEventBus, log)
	err = journeySubscriber.Start(context.Background())
	if err != nil {
		log.Error("failed to start journey subscription", "error", err)
		return err
	}

	// Console-realtime hub: fans control.journey.updated and
	// control.task.updated out to open GET /stores/{store_id}/journey/stream
	// WebSocket connections (journeySvc backs the task.updated relevance
	// check — only stores with an active journey get task pushes). The
	// eventbus subscription (not just the in-process Hub) is what makes this
	// correct under horizontal scaling — see realtime.Subscriber's doc comment.
	// Create separate event bus with dedicated consumer group so realtime gets
	// a copy of all events (fan-out) instead of competing with other subscribers.
	realtimeEventBus, err := s.initRealtimeEventBus(config, log)
	if err != nil {
		log.Error("failed to initialize realtime event bus", "error", err)
		return err
	}
	defer realtimeEventBus.Close()

	realtimeHub := realtime.NewHub()
	realtimeSubscriber := realtime.NewSubscriber(realtimeHub, journeySvc, realtimeEventBus, log)
	if err := realtimeSubscriber.Start(context.Background()); err != nil {
		log.Error("failed to start realtime journey subscription", "error", err)
		return err
	}

	// Billing service (Stripe client + config). ModeMock (default) fabricates
	// hosted URLs instead of calling Stripe, for local/test; ModeStripe
	// creates real Checkout/Portal sessions in production.
	// Stripe client with explicit HTTP timeout (10s). Control plane makes synchronous
	// calls to Stripe for portal/checkout operations, so tight timeouts are appropriate.
	stripeClient, err := billing.NewClientWithHTTPClient(billing.ClientConfig{
		Mode:            billing.Mode(config.BillingMode),
		StripeSecretKey: config.StripeSecretKey,
	}, &http.Client{Timeout: 10 * time.Second})
	if err != nil {
		log.Error("failed to initialize billing client", "error", err)
		return err
	}
	billingCfg := billing.Config{
		PlanPriceIDs: map[domain.Plan]string{
			domain.PlanSelfServe: config.StripePriceIDSelfServe,
			domain.PlanConcierge: config.StripePriceIDConcierge,
			domain.PlanScale:     config.StripePriceIDScale,
		},
		PortalReturnURL:       config.StripePortalReturnURL,
		WebhookSecret:         config.StripeWebhookSecret,
		TrialDays:             config.BillingTrialDays,
		PortalConfigurationID: config.StripePortalConfigurationID,
	}
	billingSvc := billing.NewBillingService(billingRepo, notificationPrefsRepo, webhookEventRepo, stripeClient, billingCfg)

	// Reconcile the Billing Portal Configuration (which plans are
	// self-serve switch targets, cancellation behavior) to what's declared
	// in billing.Config every boot. Non-fatal: on failure, portal sessions
	// still work, just unscoped (Stripe's account-default configuration),
	// same as before this feature existed — logged loudly since it silently
	// degrades "no downgrade to self_serve" back to Stripe's defaults.
	if configID, err := billingSvc.EnsurePortalConfiguration(context.Background()); err != nil {
		log.Error("failed to ensure stripe billing portal configuration — portal sessions will use the Stripe account default instead", "error", err)
	} else if configID != config.StripePortalConfigurationID {
		log.Warn("stripe billing portal configuration created — persist this as STRIPE_PORTAL_CONFIGURATION_ID to avoid recreating it on next boot", "configuration_id", configID)
	}

	// Account service (billingSvc doubles as both the SeatLimiter for self-serve
	// team invites — the member cap is billing-plan data, see domain.BillingUsage —
	// and the TrialProvisioner that gives every new merchant an entry-level trial
	// subscription at creation).
	accountSvc := account.NewAccountService(accountRepo, memberRepo, account.NewEmailMailer(emailSender), billingSvc, billingSvc)

	// A merchant's plan tier is effectively static and gets read once per
	// prescribed task raised — including every monitoring probe pass, every
	// few minutes, per store — so validation and monitoring share one
	// TTL-cached reader instead of each hitting Mongo through billingSvc
	// directly. See CachedPlanReader's doc comment for the staleness tradeoff.
	planReader := billing.NewCachedPlanReader(billingSvc, billing.DefaultPlanCacheTTL)

	presenceSubscriber := operator.NewPresenceSubscriber(presenceRepo, eventBus, log)
	if err := presenceSubscriber.Start(context.Background()); err != nil {
		log.Error("failed to start presence subscription", "error", err)
		return err
	}

	// Validation service (HTTPTestHarness)
	validationStore := validation.NewMemoryConformanceStore()

	// Determine spec version from config or use default
	specVersion := config.SpecVersion
	if specVersion == "" {
		specVersion = "1.0"
	}

	testHarness := validation.NewHTTPTestHarness(config.ValidationTestHarnessBaseURL, log)

	// Load catalogue for validation (pinned spec versions for audit trail)
	cat, err := catalogue.LoadCatalogue(context.Background(), log)
	if err != nil {
		log.Error("failed to load catalogue for validation", "error", err)
		// Non-fatal: validation will continue without catalogue
		cat = nil
	}

	validationOrch := validation.NewOrchestrator(
		testHarness,
		validationStore,
		validationPlatformResolver{repo: connectorRepo}, // resolves storeID -> platform for the /ucp/:platform/:storeID serving route
		eventBus, // Wired for journey.signal emission
		cat,
		log,
		specVersion,
	)
	validationOrch.WithTaskService(taskSvc)   // Task emission on validation failures
	validationOrch.WithPlanReader(planReader) // Pre-validation milestone owner follows the merchant's plan
	validationSvc := validation.NewOrchestratorService(validationOrch)

	// Wire the "run pre-validation" milestone task onto the journey state
	// machine now that the orchestrator exists (constructed after journeySvc
	// — see journey.JourneyService.WithValidationMilestoneRaiser's doc comment).
	journeySvc.WithValidationMilestoneRaiser(validationOrch)

	// Monitoring loop (semantic probes, consistency checks, sweeper) — fully
	// wired further down, once the connector registry/provider and health
	// repository dependencies it needs are constructed.
	findingRouter := monitoring.NewTaskServiceRouter(taskSvc, log).WithPlanReader(planReader)

	// Cross-plane leg of sync.reconciliation.discrepancy_found: the sync
	// plane's reconciliation sweeper finds a discrepancy and corrects its own
	// catalog mirror directly (same-process); this subscriber is what
	// surfaces that discrepancy as a task here in the control plane.
	discrepancySubscriber := monitoring.NewDiscrepancySubscriber(findingRouter, consistencyRepo, eventBus, log)
	if err := discrepancySubscriber.Start(context.Background()); err != nil {
		log.Error("failed to start reconciliation discrepancy subscription", "error", err)
		return err
	}

	// Cross-plane leg of sync.task.raised: mapping/scoring workers (and the
	// reconciliation sweeper) find a blocking readiness issue and publish
	// this event; this subscriber is what turns it into a control-plane
	// Task so it shows up for the merchant/operator.
	taskRaisedSubscriber := task.NewSubscriber(taskSvc, eventBus, log)
	if err := taskRaisedSubscriber.Start(context.Background()); err != nil {
		log.Error("failed to start task.raised subscription", "error", err)
		return err
	}

	// Store service — CRUD for store records owned by a merchant.
	storeRepo, err := store.NewMongoStoreRepository(db, true)
	if err != nil {
		log.Error("failed to create store repository", "error", err)
		return err
	}
	storeSvc := store.NewStoreService(storeRepo, jwkService, log)

	// Operator service (wired with TaskService for board task counts, JourneyService
	// for engagement stage context, servingCache/presenceRepo for the presence/manifest
	// views, orderReader for the orders view, consistencyRepo for the health/consistency
	// views, storeRepo for verifying store ownership before GetManifest reads the
	// store-only-keyed serving cache, eventBus for RebuildCache)
	operatorSvc := operator.NewOperatorService(manualStepRepo, auditLogRepo, boardRepo, taskSvc, journeySvc, servingCache, presenceRepo, orderReader, consistencyRepo, healthRepo, storeRepo, eventBus, log)

	// Store profile service — control plane is the single writer for per-store
	// business profile data (legal entity, support contact, storefront domain,
	// fulfillment regions, shipping zones) read by the serving/sync planes.
	profileRepo, err := store.NewMongoProfileRepository(db, cat, true)
	if err != nil {
		log.Error("failed to create profile repository", "error", err)
		return err
	}
	profileSvc := store.NewProfileService(profileRepo)

	// Monitoring loop, continued: connector registry/provider (mirrors
	// internal/planes/sync/api/entry.go's identical pattern — each plane
	// wires its own copy since they are separately deployable) so the
	// consistency source can pull live store price/stock, a health
	// statement repository for the semantic_endpoint/profile_edge kinds,
	// and a store-scoped storefront-URL provider for the profile_edge probe.
	monitoringProvider := func(merchantID, storeID string) (ucp.StoreProvider, error) {
		_, platformName, err := connectorSvc.GetStoreCredentials(context.Background(), storeID)
		if err != nil {
			return nil, fmt.Errorf("resolve platform for store %s: %w", storeID, err)
		}
		return &monitoringRegistryIdentity{merchantID: merchantID, storeID: storeID, platform: platformName}, nil
	}
	monitoringStoreCreds := base.CredentialsFunc(func(ctx context.Context, storeID string) (map[string]any, error) {
		creds, _, err := connectorSvc.GetStoreCredentials(ctx, storeID)
		return creds, err
	})
	monitoringRegistry := base.New(monitoringStoreCreds, log)
	platform.RegisterConnectors(monitoringRegistry)

	consistencySource := monitoring.NewConnectorConsistencySource(storeSvc, monitoringProvider, monitoringRegistry, servingCache, productRepo)
	semanticProbe := monitoring.NewSemanticProbe(config.ServingOrigin(), log, monitoring.NewProfileStoreURLProvider(profileSvc))
	consistencyEngine := monitoring.NewConsistencyEngine(consistencySource, log)
	reconciliationSweeper := monitoring.NewReconciliationSweeper(consistencySource, log)
	monitoringLoop := monitoring.NewMonitoringLoop(semanticProbe, consistencyEngine, reconciliationSweeper, findingRouter, consistencyRepo, healthRepo, log)

	// Scheduler runs the loop's probe/sweep passes across every store on
	// their configured intervals — the one process-wide background job that
	// actually populates GET .../health's semantic_endpoint/profile_edge
	// statements and keeps .../health/consistency findings fresh without an
	// event to drive them (sync.catalog.products_ready only signals "a
	// store's snapshot was rebuilt", not which SKUs changed, so there is no
	// granular event to check consistency on; the scheduled sweep is the
	// mechanism, not a gap).
	// WithKeyEnsurer makes signing-key provisioning self-healing: it ran once
	// at pairing and never again, so a store that lost its keys published an
	// unverifiable profile forever (see Scheduler.reconcileKeys).
	monitoringScheduler := monitoring.NewScheduler(monitoringLoop, monitoringStoreLister{repo: storeRepo}, log).
		WithKeyEnsurer(storeSvc)
	go monitoringScheduler.Run(context.Background())

	// Business-facts completeness predicate — checks persisted merchant/store
	// business facts against the catalogue-defined required set. Read-only;
	// exposed for the checklist/journey service to consume as a prescribed-task
	// input (missing fact = an open item), not wired to any HTTP route.
	factsPredicate := facts.New(accountSvc, profileSvc, cat)

	// Payment config repository — per-store payment handler configuration
	paymentConfigRepo, err := store.NewMongoPaymentConfigRepository(db)
	if err != nil {
		log.Error("failed to create payment config repository", "error", err)
		return err
	}

	// Payment validator — validates handler config against catalogue
	paymentValidator := store.NewPaymentValidator(cat, log)

	// Endpoint-health read/write API surface: the HTTP routes below only
	// read status/history and acknowledge/resolve alerts — the probe and
	// evaluator background workers (started further down, see
	// endpointhealth.StartProbeWorker/StartEvaluatorWorker) are the sole
	// writers of status and the only place new alerts are opened (see
	// internal/planes/control/endpointhealth). Alert transitions made via
	// the console (merchant acknowledging/resolving) use a log-only
	// notifier rather than the workers' email notifier — a
	// merchant-initiated action doesn't need to email the merchant about
	// their own action.
	endpointHealthStatusRepo, err := endpointhealth.NewMongoStatusRepository(db)
	if err != nil {
		log.Error("failed to create endpoint health status repository", "error", err)
		return err
	}
	endpointHealthAlertRepo, err := endpointhealth.NewMongoAlertRepository(db)
	if err != nil {
		log.Error("failed to create endpoint health alert repository", "error", err)
		return err
	}
	endpointHealthAlerts := endpointhealth.NewAlertService(endpointHealthAlertRepo, endpointhealth.NewLoggingAlertNotifier(log), log)
	endpointHealthHistoryRepo, err := endpointhealth.NewMongoHistoryRepository(db)
	if err != nil {
		log.Error("failed to create endpoint health history repository", "error", err)
		return err
	}
	// The probe identity is loaded here so this process can both serve
	// GET /_probe/.well-known/ucp and pass the same *Identity into the
	// probe worker below (endpointhealth.StartProbeWorker) — one persisted
	// Mongo document, one in-memory load.
	endpointHealthIdentity, err := endpointhealth.LoadOrCreateIdentity(context.Background(), db, config.ControlBaseURL+controlGroupName+endpointhealth.ProbeProfileRoutePath)
	if err != nil {
		log.Error("failed to load endpoint health probe identity", "error", err)
		return err
	}

	// ── Store UCP activation + agent surfaces ────────────────────────────
	//
	// Two independent lifecycles. Activation is the store's own — its
	// endpoints being served, declared, signed, verified and monitored —
	// and knows nothing about any surface. Surface enrollment is per-store,
	// zero-to-many, and entirely registry-driven.
	//
	// The only dependency between them runs one way and lives on the
	// surface side: surfaceSvc reads activation to enforce the active-store
	// gate. Nothing in the activation service knows surfaces exist (see
	// activation/surface_isolation_test.go, which enforces that).
	activationRepo, err := activation.NewMongoRepository(db)
	if err != nil {
		log.Error("failed to create activation repository", "error", err)
		return err
	}
	// The verifier is the endpoint-health probe runner with no feed reader
	// and no surface reader — which is the definition, not a limitation:
	// it then runs exactly the four store-level probes against the store's
	// own endpoints, with no external party involved. That is what makes
	// `verified` reachable with zero surfaces enrolled.
	activationSvc := activation.NewActivationService(
		activationRepo,
		storeSvc,
		newProbeVerifier(config.ServingOrigin(), endpointHealthIdentity),
		eventBus,
		log,
	)

	// Signing keys are provisioned on the profile_published transition, so the
	// state that means "keys provisioned" records the key it published.
	activationSvc.WithKeyEnsurer(storeSvc)

	// Wire activation verification into validation orchestrator: when validation
	// passes, the orchestrator will automatically trigger activation verification
	// to advance from profile_published → verified → active.
	validationOrch.WithActivationService(activationSvc)

	// Wire the Ready -> Live self-heal check (see
	// journey.JourneyService.WithActivationStateReader's doc comment) —
	// another post-construction setter because activationSvc, like
	// validationOrch above, is built after journeySvc.
	journeySvc.WithActivationStateReader(activationSvc)

	surfaceRepo, err := surface.NewMongoRepository(db)
	if err != nil {
		log.Error("failed to create surface repository", "error", err)
		return err
	}

	// The registry is the whole of what surfaces exist. Adding one is a
	// descriptor plus its adapters registered here — no route, state
	// machine, or console change anywhere. surfaceall.Registrations is the one
	// place a surface is listed (direct_agent, …); see
	// docs/HOWTO-add-surface.md.
	surfaceRegistry, err := surface.NewRegistry(surfaceall.Registrations(db)...)
	if err != nil {
		log.Error("failed to build surface registry", "error", err)
		return err
	}

	surfaceSvc := surface.NewSurfaceService(
		surfaceRegistry,
		surfaceRepo,
		activationSvc,
		connectorReaderAdapter{connectors: connectorSvc},
		eventBus,
		log,
	)

	// Activation advances from evidence already on the bus — a connector
	// paired, a catalog scored, a health probe passing. No plane emits
	// anything new for it (see activation.Subscriber).
	// Uses its own event bus (with separate consumer group) to avoid conflicts
	// with journey subscriber on the same topics.
	activationSubscriber := activation.NewSubscriber(activationSvc, activationEventBus, log)
	if err := activationSubscriber.Start(context.Background()); err != nil {
		log.Error("failed to start activation subscription", "error", err)
		return err
	}

	// direct_agent auto-enrolls when a store reaches active, so "active
	// with no distribution partner" renders as an explicit card rather than
	// an empty page.
	surfaceSubscriber := surface.NewSubscriber(surfaceSvc, eventBus, log)
	if err := surfaceSubscriber.Start(context.Background()); err != nil {
		log.Error("failed to start surface activation subscription", "error", err)
		return err
	}

	// Checkout Escalation Console read API. These repositories are also
	// passed to the session-projection worker below
	// (checkoutescalation.StartWorker) — the sole writer of both
	// collections, run as a background component of this process (see
	// internal/planes/control/checkoutescalation's package doc).
	checkoutSessionRepo, err := checkoutescalation.NewMongoSessionRepository(db)
	if err != nil {
		log.Error("failed to create checkout session repository", "error", err)
		return err
	}
	checkoutSessionTimelineRepo, err := checkoutescalation.NewMongoTimelineRepository(db)
	if err != nil {
		log.Error("failed to create checkout session timeline repository", "error", err)
		return err
	}

	// Channel Dashboard read API. channelStore is also passed to the
	// channel worker below (channel.StartWorker) — the sole writer of
	// agent_sessions/channel_daily, run as a background component of this
	// process. sync_web_baseline_daily is written by the sync plane's
	// web-baseline-sync worker (internal/planes/sync/webbaseline, a
	// background component of the sync-plane process) and read directly
	// here (a sync-plane-owned collection): the read-only cross-plane
	// collection-read pattern.
	channelStore, err := channel.NewMongoStore(mongoClient, db)
	if err != nil {
		log.Error("failed to create channel store", "error", err)
		return err
	}
	webBaselineReader := channel.NewMongoBaselineReader(db)

	// Agent Behavior Insights read API: this process only reads —
	// the behavior-insights worker is the sole writer of
	// control_product_demand_daily, control_negotiation_daily,
	// control_payment_handler_mismatch_daily, and control_capability_gaps_monthly
	// (see internal/planes/control/insights).
	insightsStore, err := insights.NewMongoStore(db)
	if err != nil {
		log.Error("failed to create insights store", "error", err)
		return err
	}

	// ── Background Workers ─────────────────────────────────────────────────
	// Every background worker for this plane starts here, in-process,
	// reusing the Mongo/Redis connections and repositories already
	// constructed above. Each still owns its own EventBus connection on its
	// historical ConsumerGroup so that horizontally-scaled control-plane
	// replicas keep distributing its events as competing consumers, exactly
	// as when each ran as a separate --service process.
	asynqOpt := asynq.RedisClientOpt{
		Addr:     config.RedisAddress,
		Password: config.RedisPassword,
		DB:       config.RedisDatabase,
	}

	probeStop, err := endpointhealth.StartProbeWorker(context.Background(), endpointhealth.ProbeWorkerDeps{
		Config:    config,
		Log:       log,
		DB:        db,
		Identity:  endpointHealthIdentity,
		StoreRepo: storeRepo,
		Redis:     redisClient,
		AsynqOpt:  asynqOpt,
	})
	if err != nil {
		log.Error("failed to start endpoint health probe worker", "error", err)
		return err
	}
	evaluatorStop, err := endpointhealth.StartEvaluatorWorker(context.Background(), endpointhealth.EvaluatorWorkerDeps{
		Config:                config,
		Log:                   log,
		StatusRepo:            endpointHealthStatusRepo,
		AlertRepo:             endpointHealthAlertRepo,
		NotificationPrefsRepo: notificationPrefsRepo,
		StoreRepo:             storeRepo,
		Redis:                 redisClient,
		AsynqOpt:              asynqOpt,
	})
	if err != nil {
		log.Error("failed to start endpoint health evaluator worker", "error", err)
		return err
	}

	sessionProjectionStop, err := checkoutescalation.StartWorker(context.Background(), checkoutescalation.WorkerDeps{
		SessionRepo:  checkoutSessionRepo,
		TimelineRepo: checkoutSessionTimelineRepo,
		Log:          log,
		Config:       config,
	})
	if err != nil {
		log.Error("failed to start checkout session projection worker", "error", err)
		return err
	}

	ledgerStore, ledgerStop, err := ledger.StartWorker(context.Background(), ledger.WorkerDeps{
		DB:     db,
		Log:    log,
		Config: config,
	})
	if err != nil {
		log.Error("failed to start ledger worker", "error", err)
		return err
	}

	channelStop, err := channel.StartWorker(context.Background(), channel.WorkerDeps{
		Store:  channelStore,
		Log:    log,
		Config: config,
	})
	if err != nil {
		log.Error("failed to start channel worker", "error", err)
		return err
	}

	insightsStop, err := insights.StartWorker(context.Background(), insights.WorkerDeps{
		Store:  insightsStore,
		Log:    log,
		Config: config,
	})
	if err != nil {
		log.Error("failed to start behavior insights worker", "error", err)
		return err
	}

	var bearerResolver auth.PrincipalResolver
	if config.KindeIssuerURL == "" {
		log.Warn("KINDE_ISSUER_URL unset — using simulator bearer resolver (dev only)")
		bearerResolver = auth.NewSimBearerResolver()

		// Start the in-process WooCommerce plugin simulator so the pairing
		// flow works without a real plugin install. Heartbeats and signals
		// are simulated separately by the sync plane's own plugin simulator.
		// The simulator calls real HTTP endpoints on localhost.
		baseURL := fmt.Sprintf("http://localhost:%d%s", *port, controlGroupName)
		simStoreBaseURL := config.SimStoreBaseURL
		if simStoreBaseURL == "" {
			simStoreBaseURL = simstore.DefaultBaseURL
		}
		pluginSim := sim.New(baseURL, simStoreBaseURL, redisClient, log)
		connectorSvc.SetDevPairingHook(pluginSim.PairingHook())
		go pluginSim.Run(context.Background())
	} else {
		kindeConfig := auth.KindeConfig{
			IssuerURL: config.KindeIssuerURL,
			Audience:  config.KindeAudience,
		}
		bearerResolver = auth.NewBearerResolver(log, kindeConfig)
	}

	h := New(Dependencies{
		Logger:                  log,
		Store:                   storeSvc,
		Account:                 accountSvc,
		Locales:                 localeProvider,
		Connector:               connectorSvc,
		Catalog:                 catalogSvc,
		Task:                    taskSvc,
		Journey:                 journeySvc,
		Activation:              activationSvc,
		Surfaces:                surfaceSvc,
		Facts:                   factsPredicate,
		Billing:                 billingSvc,
		Operator:                operatorSvc,
		Validation:              validationSvc,
		Profile:                 profileSvc,
		PaymentConfigRepo:       paymentConfigRepo,
		PaymentValidator:        paymentValidator,
		Ledger:                  ledgerStore,
		EndpointHealthStatus:    endpointHealthStatusRepo,
		EndpointHealthAlerts:    endpointHealthAlerts,
		EndpointHealthHistory:   endpointHealthHistoryRepo,
		EndpointHealthIdentity:  endpointHealthIdentity,
		CheckoutSessions:        checkoutSessionRepo,
		CheckoutSessionTimeline: checkoutSessionTimelineRepo,
		Channel:                 channelStore,
		WebBaseline:             webBaselineReader,
		Insights:                insightsStore,
		EventPublisher:          eventBus,
		Realtime:                realtimeHub,
		DashboardOrigin:         config.DashboardAddress,
		ResolveBearer:           bearerResolver,
		ResolveHMAC:             auth.NewHMACResolver(log, connectorSvc, redisClient, secretMgmt, dedupTTL),
		ContactSender:           contactSender,
		ContactNotifyEmail:      contactNotifyEmail,
		ContactApiToken:         config.ContactApiToken,
		ContactAllowedOrigins:   contactAllowedOrigins,
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

	return s.startServerAndWaitForShutdown(e, port, log, journeySubscriber, activationSubscriber, realtimeSubscriber, discrepancySubscriber, taskRaisedSubscriber, probeStop, evaluatorStop, sessionProjectionStop, ledgerStop, channelStop, insightsStop)
}

// ── Helper Methods for Init Refactoring ────────────────────────────────────

// initConfigAndLogger loads configuration and creates a structured logger.
func (s *controlPlaneEntry) initConfigAndLogger(env *string) (*utils.Configurations, *slog.Logger, error) {
	config, err := utils.LoadConfigurations(s.configPath)
	if err != nil {
		return nil, nil, err
	}
	config.Env = utils.StringValue(env)

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slogLevel(config.LogLevel),
	}))
	return config, log, nil
}

// initEchoServerWithMiddlewares creates and configures the Echo server with all required middleware.
func (s *controlPlaneEntry) initEchoServerWithMiddlewares(dashboardAddress string) (*echo.Echo, *echo.Group) {
	e := echo.New()
	e.Use(middlewares.WithServerHeader())
	//e.Use(middlewares.WithErrorHandler())
	e.Use(middlewares.WithCorrelationID())
	// Wire OpenTelemetry tracing and metrics (F-043)
	// Uses no-op providers by default; operations can configure exporters via OTEL_* env vars
	e.Use(middlewares.WithOTELTracing(otel.GetTracerProvider(), "control"))
	e.Use(middlewares.WithOTELMetrics(otel.GetMeterProvider(), "control"))
	e.Use(middleware.Secure())
	e.Use(middleware.Recover())
	e.Use(middleware.Decompress())
	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		Skipper: func(c echo.Context) bool {
			// The gzip middleware wraps http.ResponseWriter in a type that
			// doesn't implement http.Hijacker, which the WebSocket upgrade
			// on /journey/stream requires — skip it there or every upgrade
			// attempt fails with "response does not implement http.Hijacker".
			return strings.Contains(c.Path(), "metrics") || strings.HasSuffix(c.Path(), "/stream")
		},
	}))
	e.Use(middlewares.WithCORS(dashboardAddress))

	group := e.Group(controlGroupName)
	return e, group
}

// initDatabaseConnections establishes connections to MongoDB, Redis, and EventBus.
// Timeouts are calibrated for control plane (bounded latency to ensure operator responsiveness).
func (s *controlPlaneEntry) initDatabaseConnections(config *utils.Configurations, log *slog.Logger) (*mongo.Client, *mongo.Database, *redis.Client, eventbus.EventBus, time.Duration, error) {
	// MongoDB connection with explicit connect and operation timeouts (10s).
	// Control plane handles interactive operator requests, so we use tighter timeouts.
	// SetConnectTimeout: time to establish initial connection.
	// SetTimeout: time for individual operations (queries, inserts, etc).
	mongoOpts := mongoopts.Client().
		ApplyURI(config.MongoConnectionString).
		SetConnectTimeout(10 * time.Second).
		SetTimeout(10 * time.Second)
	mongoClient, err := mongo.Connect(mongoOpts)
	if err != nil {
		log.Error("failed to connect to mongodb", "error", err)
		return nil, nil, nil, nil, 0, err
	}

	db := mongoClient.Database("sarvex")

	// Redis connection (for sequence dedup, session store, etc.) with explicit timeouts.
	// Control plane uses Redis for session/dedup data; use moderate timeouts (5s dial, 3s read/write).
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
		return nil, nil, nil, nil, 0, err
	}

	// EventBus connection (for cross-plane command emission and event subscription).
	// NATS JetStream, in every environment (NATS_URL).
	busConfig := eventbus.Config{
		Logger: newEventBusLogger(log),
		NATS:   eventbus.NATSConfig{URL: config.NATSURL},
	}
	eventBus, err := eventbus.New(busConfig)
	if err != nil {
		log.Error("failed to initialize event bus", "error", err)
		return nil, nil, nil, nil, 0, err
	}

	// Dedup TTL for HMAC sequence replay prevention (10 minutes)
	// TODO(G2): Make this configurable via config.HMACDedupTTL once added to Configurations
	dedupTTL := 10 * time.Minute

	return mongoClient, db, redisClient, eventBus, dedupTTL, nil
}

// initActivationEventBus creates a separate EventBus instance with a distinct
// consumer group for the activation subscriber. This prevents subscription
// conflicts when multiple subscribers listen to the same topic
// (control.journey.signal) — each gets its own subscription with a different
// consumer group name. Uses the same backend.
func (s *controlPlaneEntry) initActivationEventBus(config *utils.Configurations, log *slog.Logger) (eventbus.EventBus, error) {
	busConfig := eventbus.Config{
		ConsumerGroup: "activation",
		Logger:        newEventBusLogger(log),
		NATS:          eventbus.NATSConfig{URL: config.NATSURL},
	}
	eventBus, err := eventbus.New(busConfig)
	if err != nil {
		log.Error("failed to initialize activation event bus", "error", err)
		return nil, err
	}
	return eventBus, nil
}

// initJourneyEventBus creates a separate EventBus instance with a distinct
// consumer group for the journey subscriber. Without this it shares the
// default group with the surface subscriber on control.store.
// activation_state_changed, and a consumer group splits a topic's messages
// across its members rather than fanning out a copy to each — see the call
// site's comment for the onboarding stall this caused. Uses the same backend.
func (s *controlPlaneEntry) initJourneyEventBus(config *utils.Configurations, log *slog.Logger) (eventbus.EventBus, error) {
	busConfig := eventbus.Config{
		ConsumerGroup: "journey",
		Logger:        newEventBusLogger(log),
		NATS:          eventbus.NATSConfig{URL: config.NATSURL},
	}
	eventBus, err := eventbus.New(busConfig)
	if err != nil {
		log.Error("failed to initialize journey event bus", "error", err)
		return nil, err
	}
	return eventBus, nil
}

// initRealtimeEventBus creates a separate EventBus instance with a distinct
// consumer group for the realtime WebSocket subscriber. This ensures the
// realtime subscriber receives a copy of all events (fan-out) instead of
// competing with other subscribers that use the default consumer group.
// Uses the same backend.
func (s *controlPlaneEntry) initRealtimeEventBus(config *utils.Configurations, log *slog.Logger) (eventbus.EventBus, error) {
	busConfig := eventbus.Config{
		ConsumerGroup: "realtime_websocket",
		Logger:        newEventBusLogger(log),
		NATS:          eventbus.NATSConfig{URL: config.NATSURL},
	}
	eventBus, err := eventbus.New(busConfig)
	if err != nil {
		log.Error("failed to initialize realtime event bus", "error", err)
		return nil, err
	}
	return eventBus, nil
}

// startServerAndWaitForShutdown starts the HTTP server and waits for shutdown signals.
func (s *controlPlaneEntry) startServerAndWaitForShutdown(
	e *echo.Echo,
	port *int,
	log *slog.Logger,
	journeySubscriber *journey.Subscriber,
	activationSubscriber *activation.Subscriber,
	realtimeSubscriber *realtime.Subscriber,
	discrepancySubscriber *monitoring.DiscrepancySubscriber,
	taskRaisedSubscriber *task.Subscriber,
	probeStop, evaluatorStop, sessionProjectionStop, ledgerStop, channelStop, insightsStop func(context.Context) error,
) error {
	go func() {
		log.Info("starting control plane server", "port", *port)
		if err := e.Start(fmt.Sprintf(":%d", *port)); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer journeySubscriber.Stop(ctx)
	defer activationSubscriber.Stop(ctx)
	defer realtimeSubscriber.Stop(ctx)
	defer discrepancySubscriber.Stop(ctx)
	defer taskRaisedSubscriber.Stop(ctx)
	defer func() { _ = probeStop(ctx) }()
	defer func() { _ = evaluatorStop(ctx) }()
	defer func() { _ = sessionProjectionStop(ctx) }()
	defer func() { _ = ledgerStop(ctx) }()
	defer func() { _ = channelStop(ctx) }()
	defer func() { _ = insightsStop(ctx) }()
	defer cancel()
	if err := e.Shutdown(ctx); err != nil {
		log.Error("shutdown error", "error", err)
		return err
	}
	return nil
}

func slogLevel(level string) slog.Level {
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

// newEventBusLogger creates an adapter from slog.Logger to eventbus.Logger.
func newEventBusLogger(log *slog.Logger) eventbus.Logger {
	return &eventBusLoggerAdapter{log: log}
}

// eventBusLoggerAdapter adapts slog.Logger to watermill.LoggerAdapter interface.
type eventBusLoggerAdapter struct {
	log *slog.Logger
}

// levelEnabled reports whether a would actually emit at level — checked
// before building the fields->args slice in every method below, since that
// build interface-boxes every field value and runs on every event-bus
// publish/subscribe/ack callback regardless of whether the record would be
// discarded (watermill's LoggerAdapter interface gives no context, so the
// check uses context.Background(), matching the level-only checks most
// slog.Handler implementations perform).
func (a *eventBusLoggerAdapter) levelEnabled(level slog.Level) bool {
	return a.log.Enabled(context.Background(), level)
}

func (a *eventBusLoggerAdapter) Info(msg string, fields watermill.LogFields) {
	if !a.levelEnabled(slog.LevelInfo) {
		return
	}
	args := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	a.log.Info(msg, args...)
}

func (a *eventBusLoggerAdapter) Error(msg string, err error, fields watermill.LogFields) {
	if !a.levelEnabled(slog.LevelError) {
		return
	}
	args := make([]any, 0, len(fields)*2+2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	args = append(args, "error", err)
	a.log.Error(msg, args...)
}

func (a *eventBusLoggerAdapter) Debug(msg string, fields watermill.LogFields) {
	if !a.levelEnabled(slog.LevelDebug) {
		return
	}
	args := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	a.log.Debug(msg, args...)
}

func (a *eventBusLoggerAdapter) Trace(msg string, fields watermill.LogFields) {
	// Trace emits via Debug (slog has no Trace level), so it's gated by the
	// same level.
	if !a.levelEnabled(slog.LevelDebug) {
		return
	}
	args := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	a.log.Debug(msg, args...)
}

func (a *eventBusLoggerAdapter) With(fields watermill.LogFields) watermill.LoggerAdapter {
	return a
}

// monitoringRegistryIdentity is a minimal ucp.StoreProvider carrying only the
// registry-lookup identity (platform/store/merchant), mirroring
// internal/planes/sync/api/entry.go's connectorRegistryIdentity — each plane
// wires its own copy of this glue since they are separately deployable
// services sharing only the base.Connector/registry library packages.
type monitoringRegistryIdentity struct {
	merchantID, storeID string
	platform            pkgdomain.CommerceSystemType
}

func (s *monitoringRegistryIdentity) Configuration() ucp.Configuration {
	return ucp.Configuration{MerchantID: s.merchantID, StoreID: s.storeID, Platform: s.platform}
}
func (s *monitoringRegistryIdentity) StoreID() string                                   { return s.storeID }
func (s *monitoringRegistryIdentity) Version() string                                   { return "" }
func (s *monitoringRegistryIdentity) Capabilities() map[string][]wire.Capability        { return nil }
func (s *monitoringRegistryIdentity) Services() map[string][]wire.Service               { return nil }
func (s *monitoringRegistryIdentity) PaymentHandlers() map[string][]wire.PaymentHandler { return nil }
func (s *monitoringRegistryIdentity) SigningKeys() []wire.JWK                           { return nil }
func (s *monitoringRegistryIdentity) StorefrontURL() string                             { return "" }

// monitoringStoreLister adapts store.StoreRepository.ListAll to
// monitoring.StoreLister for the scheduler.
type monitoringStoreLister struct {
	repo store.StoreRepository
}

func (l monitoringStoreLister) ListAllStores(ctx context.Context, afterID string, limit int) ([]monitoring.StoreRef, bool, error) {
	models, hasMore, err := l.repo.ListAll(ctx, afterID, limit)
	if err != nil {
		return nil, false, err
	}
	refs := lo.Map(models, func(m *store.StoreModel, _ int) monitoring.StoreRef {
		return monitoring.StoreRef{MerchantID: m.MerchantID, StoreID: m.ID, Platform: string(m.Platform)}
	})
	return refs, hasMore, nil
}

// validationPlatformResolver adapts connector.ConnectorRepository's
// single-store lookup to validation.PlatformResolver. Unlike
// monitoringStoreLister above (a system-wide, cross-tenant scheduler read),
// storeID here is already authorized by the inbound request, so this stays
// a scoped, single-store lookup.
type validationPlatformResolver struct {
	repo connector.ConnectorRepository
}

func (r validationPlatformResolver) GetPlatform(ctx context.Context, storeID domain.StoreID) (domain.Platform, error) {
	model, err := r.repo.GetByStoreID(ctx, string(storeID))
	if err != nil {
		return "", fmt.Errorf("get connector for store %s: %w", storeID, err)
	}
	return model.Platform, nil
}
