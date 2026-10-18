// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package api is the HTTP layer for the Sarvex V1 control plane.
//
// All routes follow the contract defined in sarvex-v1-control-plane.yaml.
// merchant_id is never a URL parameter — it is resolved from the authenticated
// principal injected by requireAuth.
package api

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/sarvexlab/sarvex/internal/pkg/email"
	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/utils"
	"github.com/sarvexlab/sarvex/internal/planes/control/account"
	"github.com/sarvexlab/sarvex/internal/planes/control/activation"
	"github.com/sarvexlab/sarvex/internal/planes/control/billing"
	"github.com/sarvexlab/sarvex/internal/planes/control/catalog"
	"github.com/sarvexlab/sarvex/internal/planes/control/channel"
	"github.com/sarvexlab/sarvex/internal/planes/control/checkoutescalation"
	"github.com/sarvexlab/sarvex/internal/planes/control/connector"
	"github.com/sarvexlab/sarvex/internal/planes/control/endpointhealth"
	"github.com/sarvexlab/sarvex/internal/planes/control/facts"
	"github.com/sarvexlab/sarvex/internal/planes/control/insights"
	"github.com/sarvexlab/sarvex/internal/planes/control/journey"
	"github.com/sarvexlab/sarvex/internal/planes/control/ledger"
	"github.com/sarvexlab/sarvex/internal/planes/control/operator"
	"github.com/sarvexlab/sarvex/internal/planes/control/realtime"
	"github.com/sarvexlab/sarvex/internal/planes/control/store"
	"github.com/sarvexlab/sarvex/internal/planes/control/surface"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
	"github.com/sarvexlab/sarvex/internal/planes/control/validation"
)

// Handler is the control-plane HTTP handler interface.
// Create a concrete instance with New and register routes with SetupRoutes.
type Handler interface {
	utils.HandlerSetup
}

// Dependencies holds all service interfaces the handler layer depends on.
// Each field is an interface so tests can inject stubs without a running system.
type Dependencies struct {
	Logger  *slog.Logger
	Account account.Service
	// Locales supplies the country/timezone reference list GET /account
	// attaches as locale_options. Falls back to no locale_options in the
	// response if nil.
	Locales   account.LocaleProvider
	Store     store.Service
	Connector connector.Service
	Catalog   catalog.Service
	Task      task.Service
	Journey   journey.Service
	// Activation owns the store UCP activation lifecycle — the states a
	// store passes through as its own endpoints become agent-transactable.
	// Independent of Surfaces: a store reaches active with none enrolled.
	Activation activation.Service
	// Surfaces owns per-store agent-surface enrollment. Nil is valid — the
	// routes degrade to 501 rather than panicking, same convention as
	// Realtime below.
	Surfaces surface.Service
	// Realtime fans control.journey.updated out to open console WebSocket
	// sessions (GET /stores/{store_id}/journey/stream). Nil is valid — the
	// route degrades to 501 (see streamJourney) rather than panicking, so a
	// deployment that hasn't wired it up loses only the live-push
	// optimization, never the underlying GET /journey read path.
	Realtime *realtime.Hub
	// DashboardOrigin is checked against the WebSocket handshake's Origin
	// header as defense-in-depth alongside bearer auth (same value passed to
	// middlewares.WithCORS for the JSON API). Empty allows any origin — safe
	// here because the stream is bearer-authenticated, not cookie-authenticated,
	// so an unset value fails open on origin only, never on identity.
	DashboardOrigin string
	// Facts checks persisted business facts against the catalogue-defined
	// required set. Consumed by the checklist/journey service to surface
	// missing facts as open items; no HTTP route of its own.
	Facts             facts.Predicate
	Billing           billing.Service
	Operator          operator.Service
	Validation        validation.Service
	PaymentConfigRepo store.PaymentConfigRepository
	PaymentValidator  *store.PaymentValidator
	Profile           *store.ProfileService
	EventPublisher    eventbus.EventBus

	// EndpointHealthStatus/Alerts/History back the endpoint-health surface
	// composed into GET /stores/{id}/health (endpoint_status + active_alerts)
	// plus /health/history and /alerts. Nil is valid — the endpoint_status
	// composition is skipped and /alerts, /health/history degrade to empty
	// results rather than panicking, so a deployment that hasn't wired the
	// probe/evaluator workers yet still serves the pre-existing consistency
	// health surface untouched.
	EndpointHealthStatus  endpointhealth.StatusRepository
	EndpointHealthAlerts  *endpointhealth.AlertService
	EndpointHealthHistory endpointhealth.HistoryRepository
	// EndpointHealthIdentity publishes the endpoint-health probe's own
	// reference profile at GET /_probe/.well-known/ucp — merchants'
	// serving-plane instances fetch it to verify signed probe requests and
	// negotiate against it (see endpointhealth.Identity's doc comment).
	// Nil is valid — the route 404s rather than panicking.
	EndpointHealthIdentity *endpointhealth.Identity

	// CheckoutSessions/CheckoutSessionTimeline back the Checkout Escalation
	// Console read API (GET /checkout-sessions, /{id}, /{id}/timeline,
	// /live) — written by the session-projection worker, a background
	// component of this same control-plane process (see
	// internal/planes/control/checkoutescalation.StartWorker, started from
	// entry.go's Init). Nil is valid — every route degrades to an empty
	// result (list endpoints) or 404 (get endpoints) rather than panicking.
	CheckoutSessions        checkoutescalation.SessionRepository
	CheckoutSessionTimeline checkoutescalation.TimelineRepository

	// Ledger backs the Lost Revenue Ledger read API (GET /ledger/summary,
	// GET /ledger/entries, PATCH /ledger/entries/{entry_id}) — written by
	// the ledger worker, a background component of this same control-plane
	// process (see internal/planes/control/ledger.StartWorker), read-only
	// here and gated at the analytics tier (requireAnalyticsTier). Nil is
	// valid — every route degrades to an empty result or 404 rather than
	// panicking.
	Ledger ledger.Store

	// Channel/WebBaseline back the Channel Dashboard read API (GET
	// /channel/summary, /channel/funnel, /channel/daily,
	// /channel/comparison) — Channel is written by the channel worker, a
	// background component of this same control-plane process (see
	// internal/planes/control/channel.StartWorker); WebBaseline is written
	// by the sync plane's web-baseline-sync worker
	// (internal/planes/sync/webbaseline.StartWorker), a background
	// component of the sync-plane process, and read here directly (a
	// cross-plane-collection read, not a cross-plane import). Both
	// read-only here and gated at the analytics tier (requireAnalyticsTier,
	// reused from the Ledger — R15). Nil is valid, same degrade-gracefully
	// contract as Ledger above.
	Channel     channel.Store
	WebBaseline channel.BaselineReader

	// Insights backs the Agent Behavior Insights read API (GET
	// /insights/capability_gaps, /insights/product_demand,
	// /insights/negotiation) — written by the behavior-insights worker, a
	// background component of this same control-plane process (see
	// internal/planes/control/insights.StartWorker), read-only here and
	// gated at the analytics tier (requireAnalyticsTier, reused from the
	// Ledger — R16). Nil is valid, same degrade-gracefully contract as
	// Ledger/Channel above.
	Insights insights.Store

	// ResolveBearer maps a bearer token to a Principal (real: JWT validation against the IdP).
	ResolveBearer PrincipalResolver
	// ResolveHMAC validates X-Sarvex-Signature and returns a Principal (real: pairing lookup + HMAC verify).
	ResolveHMAC HMACResolver

	// ContactSender delivers POST /contact submissions, From the address
	// configured via CONTACT_SENDER_EMAIL (see entry.go). Nil is valid —
	// the route degrades to 501 rather than panicking, same convention as
	// Realtime/Surfaces above.
	ContactSender *email.Sender
	// ContactNotifyEmail is the designated recipient for contact-form
	// submissions. Empty at construction time is resolved to a default in
	// entry.go, not here, so this field is never itself the source of truth
	// for the default.
	ContactNotifyEmail string
	// ContactApiToken gates POST /contact — see requireContactToken's doc
	// comment. Empty hides the route (404) rather than accepting
	// unauthenticated submissions.
	ContactApiToken string
	// ContactAllowedOrigins is POST /contact's CORS allow-list (the
	// sarvex.example.com marketing website's own origin(s)) — see
	// CONTACT_ALLOWED_ORIGINS. Empty allows no origin (the browser-facing
	// route becomes unreachable cross-origin, not open to any origin).
	ContactAllowedOrigins []string
}

type handler struct {
	log  *slog.Logger
	deps Dependencies
}

var _ Handler = (*handler)(nil)

// New constructs a Handler from the provided dependencies.
func New(deps Dependencies) Handler {
	log := deps.Logger
	if log == nil {
		log = slog.Default()
	}
	return &handler{log: log, deps: deps}
}

// SetupRoutes wires every control-plane route onto the given Echo group.
// The group is typically mounted at /v1.
func (h *handler) SetupRoutes(_ string, g *echo.Group) error {
	auth := requireAuth(h.deps.ResolveBearer, h.deps.Account)
	hmac := requireHMAC(h.deps.ResolveHMAC)
	// billingActive gates merchant-initiated writes once a subscription has
	// lapsed (requireActiveBilling's doc comment has the full rule). Not
	// applied to GETs, to /billing/portal-link (a read-only merchant must
	// still be able to reactivate — plan switching and cancellation live
	// inside the portal itself, see BillingService.CreatePortalLink), to
	// connector HMAC ingest (a different, always-on concern — see
	// architecture Domain 1a), or to /operator/** (operators are never
	// billing-gated).
	//
	// SetupRoutes runs once at boot, so this cache is constructed once and
	// its closure is shared by every request through billingActive below —
	// billingReader.Get() at request time hits Mongo only on a cache miss
	// or expiry, not on every gated call. See DefaultBillingStatusCacheTTL
	// for the staleness tradeoff.
	billingReader := billing.NewCachedPlanReader(h.deps.Billing, billing.DefaultBillingStatusCacheTTL)
	billingActive := requireActiveBilling(billingReader)
	// analyticsTier gates the Lost Revenue Ledger's read routes (R15).
	// Shares billingReader's cache rather than constructing a second one —
	// domain.Billing already carries Plan alongside Status, so one cached
	// read serves both gates.
	analyticsTier := requireAnalyticsTier(billingReader)

	// ── Auth ─────────────────────────────────────────────────────────────
	g.GET("/auth/me", h.getMe, auth)

	// ── Account ──────────────────────────────────────────────────────────
	g.GET("/account", h.getAccount, auth)
	g.PATCH("/account", h.updateAccount, auth, billingActive)
	g.GET("/account/members", h.listMembers, auth)
	g.POST("/account/members", h.inviteTeamMember, auth, requireOwner, billingActive)

	// ── Stores ───────────────────────────────────────────────────────────
	g.GET("/stores", h.listStores, auth)
	g.POST("/stores", h.createStore, auth, billingActive)
	g.GET("/stores/:store_id", h.getStore, auth)
	g.PATCH("/stores/:store_id", h.updateStore, auth, billingActive)
	g.DELETE("/stores/:store_id", h.deleteStore, auth, billingActive)
	g.GET("/stores/:store_id/profile", h.getStoreProfile, auth)
	g.PUT("/stores/:store_id/profile", h.updateStoreProfile, auth, billingActive)

	// ── Payment Configuration ────────────────────────────────────────────
	g.GET("/stores/:store_id/payment-config", h.getPaymentConfig, auth)
	g.PUT("/stores/:store_id/payment-config", h.updatePaymentConfig, auth, billingActive)

	// ── Connectors ───────────────────────────────────────────────────────
	g.GET("/connectors", h.listConnectors, auth)
	g.POST("/stores/:store_id/connectors/pairing", h.beginPairing, auth, billingActive)
	g.POST("/connectors/pairing/complete", h.completePairing, hmac)
	g.GET("/connectors/:connector_id", h.getConnector, auth)
	g.DELETE("/connectors/:connector_id", h.disconnect, auth, billingActive)
	g.POST("/connectors/:connector_id/resync", h.resync, auth, billingActive) // publishes control.catalog.import; see ConnectorService.TriggerResync

	// ── Catalog (per-store) ──────────────────────────────────────────────
	g.GET("/stores/:store_id/catalog/readiness", h.getReadiness, auth)
	g.GET("/stores/:store_id/catalog/products", h.listProducts, auth)
	g.GET("/stores/:store_id/catalog/products/:sku", h.getProduct, auth)
	g.POST("/stores/:store_id/catalog/audits", h.triggerAudit, auth, billingActive)
	g.GET("/stores/:store_id/catalog/remediations", h.listRemediations, auth)
	g.POST("/stores/:store_id/catalog/remediations/apply", h.applyRemediations, auth, billingActive)
	g.POST("/stores/:store_id/catalog/write-backs/:write_back_id/revert", h.revertWriteBack, auth, billingActive)

	// ── Tasks ────────────────────────────────────────────────────────────
	g.GET("/tasks", h.listTasks, auth)
	g.GET("/tasks/:task_id", h.getTask, auth)
	g.POST("/tasks/:task_id/resolve", h.resolveTask, auth, billingActive)

	// ── Store UCP activation ─────────────────────────────────────────────
	// The store's own lifecycle: profile served, capabilities declared,
	// signatures verifying, checkout working. No surface appears here.
	g.GET("/stores/:store_id/activation", h.getActivation, auth)
	g.POST("/stores/:store_id/activation/verify", h.verifyActivation, auth, billingActive)

	// ── Agent surfaces ───────────────────────────────────────────────────
	// Generic over the registry: no route, handler or middleware below
	// names a particular surface. /surfaces is the registry itself and is
	// not store-scoped — which surfaces exist is a property of the
	// deployment, not of a merchant.
	g.GET("/surfaces", h.listSurfaceRegistry, auth)
	g.GET("/stores/:store_id/surfaces", h.listStoreSurfaces, auth)
	g.POST("/stores/:store_id/surfaces/:surface_id/enroll", h.enrollSurface, auth, billingActive)
	g.POST("/stores/:store_id/surfaces/:surface_id/prerequisites/evaluate", h.evaluateSurfacePrerequisites, auth, billingActive)
	g.POST("/stores/:store_id/surfaces/:surface_id/submit", h.submitSurface, auth, billingActive)
	g.PUT("/stores/:store_id/surfaces/:surface_id/config", h.setSurfaceConfig, auth, billingActive)
	g.DELETE("/stores/:store_id/surfaces/:surface_id", h.withdrawSurface, auth, billingActive)

	// ── Journey ──────────────────────────────────────────────────────────
	g.GET("/stores/:store_id/journey", h.getJourney, auth)
	// WebSocket upgrade: token travels as a Sec-WebSocket-Protocol entry, not
	// an Authorization header, so this uses requireAuthWS rather than auth.
	g.GET("/stores/:store_id/journey/stream", h.streamJourney, requireAuthWS(h.deps.ResolveBearer, h.deps.Account))

	// ── Validation (per-store) ───────────────────────────────────────────
	g.GET("/stores/:store_id/validation/status", h.getValidationStatus, auth)
	g.GET("/stores/:store_id/validation/runs", h.listValidationRuns, auth)
	g.POST("/stores/:store_id/validation/runs", h.triggerValidationRun, auth, billingActive)

	// ── UCP Presence (per-store) ─────────────────────────────────────────
	g.GET("/stores/:store_id/presence", h.getPresence, auth)
	g.GET("/stores/:store_id/presence/manifest", h.getManifest, auth)
	g.POST("/stores/:store_id/presence/rebuild", h.rebuildCache, auth, billingActive)

	// ── Orders (per-store) ───────────────────────────────────────────────
	g.GET("/stores/:store_id/orders", h.listOrders, auth)
	g.GET("/stores/:store_id/orders/:order_id", h.getOrder, auth)

	// ── Health (per-store) ───────────────────────────────────────────────
	// No billingActive on any route in this section, nor on
	// /notifications/preferences below — endpoint health and alerting are
	// core safety, available at every billing tier, never an upsell.
	g.GET("/stores/:store_id/health", h.getHealth, auth)
	g.GET("/stores/:store_id/health/consistency", h.listConsistencyFindings, auth)
	g.GET("/stores/:store_id/health/history", h.getHealthHistory, auth)

	// ── Endpoint health alerts ───────────────────────────────────────────
	g.GET("/alerts", h.listAlerts, auth)
	g.PATCH("/alerts/:alert_id", h.patchAlert, auth)

	// ── Checkout Escalation Console ──────────────────────────────────────
	// No billingActive on any route in this section — diagnostic, and the
	// support burden falls on Sarvex otherwise, available at every billing
	// tier (R13). /live is registered before the /:checkout_id routes so
	// its literal "live" segment is unambiguous.
	g.GET("/checkout-sessions/live", h.listLiveCheckoutSessions, auth)
	g.GET("/checkout-sessions", h.listCheckoutSessions, auth)
	g.GET("/checkout-sessions/:checkout_id", h.getCheckoutSession, auth)
	g.GET("/checkout-sessions/:checkout_id/timeline", h.getCheckoutSessionTimeline, auth)

	// ── Lost Revenue Ledger ──────────────────────────────────────────────
	// Gated at the analytics tier (R15) — the ledger worker (a background
	// component of this control-plane process) writes for every tier
	// regardless, only these reads are gated.
	g.GET("/ledger/summary", h.getLedgerSummary, auth, analyticsTier)
	g.GET("/ledger/entries", h.listLedgerEntries, auth, analyticsTier)
	g.PATCH("/ledger/entries/:entry_id", h.updateLedgerEntryStatus, auth, analyticsTier)

	// ── Channel Dashboard ────────────────────────────────────────────────
	// Gated at the analytics tier (R15) — the sessionization/rollup worker
	// (a background component of this control-plane process) and the
	// sync-plane's web-baseline-sync worker write for every tier
	// regardless, only these reads are gated. Reuses the Ledger's
	// analyticsTier gate rather than a distinct one — R15 asks for this
	// explicitly.
	g.GET("/channel/summary", h.getChannelSummary, auth, analyticsTier)
	g.GET("/channel/funnel", h.getChannelFunnel, auth, analyticsTier)
	g.GET("/channel/daily", h.getChannelDaily, auth, analyticsTier)
	g.GET("/channel/comparison", h.getChannelComparison, auth, analyticsTier)

	// ── Agent Behavior Insights ──────────────────────────────────────────
	// Gated at the analytics tier (R16) — the behavior-insights worker (a
	// background component of this control-plane process) writes for every
	// tier regardless, only these reads are gated. Reuses the Ledger's
	// analyticsTier gate, same as Channel Dashboard above. No
	// /insights/attribute_gaps route:
	// R3 (attribute demand) is not implemented — see package insights'
	// doc comment for why there is nothing honest to serve there.
	g.GET("/insights/capability_gaps", h.getCapabilityGaps, auth, analyticsTier)
	g.GET("/insights/product_demand", h.getProductDemand, auth, analyticsTier)
	g.GET("/insights/negotiation", h.getNegotiationInsights, auth, analyticsTier)

	// ── Endpoint health probe identity ───────────────────────────────────
	// No auth — a .well-known-style document, publicly fetchable by design
	// (same as the serving plane's own GET /.well-known/ucp).
	g.GET(endpointhealth.ProbeProfileRoutePath, h.getProbeProfile)

	// ── Billing ──────────────────────────────────────────────────────────
	g.GET("/billing", h.getBilling, auth)
	g.POST("/billing/portal-link", h.billingPortalLink, auth)
	// No auth/hmac middleware — Stripe signs with its own scheme, verified
	// inside the handler (see stripeWebhook's doc comment).
	g.POST("/billing/webhooks/stripe", h.stripeWebhook)

	// ── Notifications ────────────────────────────────────────────────────
	g.GET("/notifications/preferences", h.getNotificationPreferences, auth)
	g.PUT("/notifications/preferences", h.setNotificationPreferences, auth)

	// ── Operator (operator role required) ────────────────────────────────
	g.GET("/operator/board", h.operatorBoard, auth, requireOperator)
	g.POST("/operator/manual-steps", h.captureManualStep, auth, requireOperator)
	g.GET("/operator/manual-steps", h.listManualSteps, auth, requireOperator)
	g.PUT("/operator/manual-steps/:step_id/retag", h.retagManualStep, auth, requireOperator)
	g.POST("/operator/manual-steps/bulk-retag", h.bulkRetagManualSteps, auth, requireOperator)
	g.GET("/operator/timeline-metrics", h.timelineMetrics, auth, requireOperator)
	g.GET("/operator/audit-log", h.auditLog, auth, requireOperator)
	g.POST("/operator/act-on-behalf", h.actOnBehalf, auth, requireOperator)

	// Operator merchant provisioning (architecture §3 Domain 1 bootstrap flow).
	g.GET("/operator/merchants", h.listMerchants, auth, requireOperator)
	g.POST("/operator/merchants", h.createMerchant, auth, requireOperator)
	g.GET("/operator/merchants/:merchant_id/members", h.operatorListMembers, auth, requireOperator)
	g.POST("/operator/merchants/:merchant_id/members", h.operatorInviteMember, auth, requireOperator)
	g.GET("/operator/merchants/:merchant_id/stores", h.operatorListStores, auth, requireOperator)
	g.POST("/operator/merchants/:merchant_id/stores", h.operatorCreateStore, auth, requireOperator)

	// ── Contact ──────────────────────────────────────────────────────────
	// Not requireAuth/billingActive — the caller is the sarvex.example.com marketing
	// website, which has no Sarvex account. contactCORS restricts this one
	// route to h.deps.ContactAllowedOrigins (config-driven — see
	// CONTACT_ALLOWED_ORIGINS), separately from the dashboard-only global
	// CORS policy applied to every other route (middlewares.WithCORS in
	// entry.go). requireContactToken gates on a static shared bearer token
	// instead of a JWT.
	contactCORS := middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: h.deps.ContactAllowedOrigins,
		AllowMethods: []string{http.MethodPost, http.MethodOptions},
		AllowHeaders: []string{echo.HeaderAuthorization, echo.HeaderContentType},
	})
	g.POST("/contact", h.postContact, contactCORS, requireContactToken(h.deps.ContactApiToken))
	// The browser's CORS preflight is an OPTIONS request with no
	// Authorization header — it must resolve to a route on its own so
	// contactCORS (route-scoped, not global like middlewares.WithCORS) gets
	// a chance to run and answer it before token auth is ever reached.
	g.OPTIONS("/contact", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) }, contactCORS)

	return nil
}
