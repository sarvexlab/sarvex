// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package ucp

// Note: sync.product.changed, sync.price.changed, sync.inventory.changed,
// sync.catalog.scored, and control.catalog.sweep are deliberately absent
// here: ingestion -> mapping/scoring and the sweep scheduler -> sweep
// handler are same-process handoffs inside the sync-plane binary, not
// events crossing a service boundary, so they're direct calls rather than
// bus topics (see mappingPipeline's doc comment in
// internal/planes/sync/ingestion.go and sweepRunner's in
// internal/planes/sync/sweep_scheduler.go).
const (
	EventConnectorResyncRequested       = "control.connector.resync_requested"
	EventStoreProfileUpdated            = "control.store_profile_updated"
	EventPaymentConfigUpdated           = "control.payment_config_updated"
	EventJourneySignal                  = "control.journey.signal"
	EventJourneyUpdated                 = "control.journey.updated"
	EventTaskUpdated                    = "control.task.updated"
	EventOrderIntent                    = "serving.checkout.order_intent"
	EventOrderCreated                   = "sync.order.created"
	EventLogRequested                   = "serving.log.requested"
	EventSnapshotSwapped                = "serving.snapshot.swapped"
	EventRebuildRequested               = "serving.rebuild.requested"
	EventCatalogImportProgress          = "sync.catalog.import_progress"
	EventSyncTaskRaised                 = "sync.task.raised"
	EventCatalogProductsUpdated         = "sync.catalog.products_updated"
	EventCatalogProductsReady           = "sync.catalog.products_ready"
	EventReconciliationDiscrepancyFound = "sync.reconciliation.discrepancy_found"
	EventConnectorSignalReceived        = "sync.connector.signal_received"
	EventCatalogImport                  = "control.catalog.import"
	EventFeedRegenerateRequested        = "control.feed.regenerate_requested"

	// Store UCP activation and agent-surface enrollment. The two are
	// separate topics because they are separate lifecycles: activation is
	// a fact about the store's own endpoints, enrollment is a fact about
	// its relationship with one distribution surface. Consumers that care
	// about "is this store transactable" subscribe to the first and are
	// unaffected by every surface event; consumers that gate on a
	// particular surface (feed generation, feed-freshness health)
	// subscribe to the second.
	EventStoreActivationStateChanged = "control.store.activation_state_changed"
	EventStoreSurfaceStateChanged    = "control.store.surface_state_changed"

	// EventSurfaceArtifactFetched: the serving plane served a surface's
	// artifact to a fetcher. Carries only the artifact's public token — the
	// serving plane resolves no tenant for it; the sync plane, which owns
	// the artifact record, maps the token back.
	EventSurfaceArtifactFetched = "serving.surface_artifact.fetched"
	// EventSurfaceArtifactUpdated: the sync plane generated or observed
	// something new about a surface artifact. The control plane re-evaluates
	// that surface's enrollment on it.
	EventSurfaceArtifactUpdated = "sync.surface_artifact.updated"

	// EventTelemetryRecorded carries per-request outcome (signature/negotiation
	// result, status, latency) for every serving-plane agent request. It is a
	// distinct, richer event from EventLogRequested ("serving.log.requested")
	// rather than a rename of it — EventLogRequested predates this and its
	// consumers (if any) are left untouched; this event exists specifically
	// for the endpoint-health evaluator's rolling-window computation.
	EventTelemetryRecorded = "serving.request.telemetry_recorded"

	// EventCatalogQueryRecorded carries what an agent asked the discovery
	// read path for (CatalogService.Search/Lookup): matched product ids,
	// query kind, surface — deliberately never the raw free-text query
	// string (sarvex-v1-architecture.md privacy invariant). Added to close the emission gap
	// documented in the Agent Behavior Insights audit: prior to this event,
	// nothing recorded what a catalog request was actually for, only that a
	// request happened (EventTelemetryRecorded) or was logged
	// (EventLogRequested).
	EventCatalogQueryRecorded = "serving.catalog.query_recorded"

	// Endpoint-health events. Producer/consumer is the new cross-cutting
	// "ops" concern (probe worker, evaluator worker) introduced for
	// synthetic + real-traffic endpoint health — it doesn't belong to one
	// plane, hence the "ops." prefix rather than control./serving./sync.
	EventHealthProbeCompleted    = "ops.health.probe_completed"
	EventHealthThresholdBreached = "ops.health.threshold_breached"
	EventHealthRecovered         = "ops.health.recovered"

	// Checkout lifecycle events. Emitted by the serving-plane checkout
	// service (checkout_session_service) alongside its existing
	// EventOrderIntent emission, at the same fire-and-forget/best-effort
	// call sites — these never gate or alter the checkout response, they
	// only give the session-projection worker (control plane) something to
	// consume. "Completed" has no event of its own: EventOrderIntent
	// already fires exactly once at that transition and carries everything
	// the projection needs.
	EventCheckoutSessionInitiated = "serving.checkout.session_initiated"
	EventCheckoutCartUpdated      = "serving.checkout.cart_updated"
	EventCheckoutPaymentSubmitted = "serving.checkout.payment_submitted"
	EventCheckoutEscalated        = "serving.checkout.escalated"
	EventCheckoutFailed           = "serving.checkout.failed"

	// Versioned checkout lifecycle event contract (event_version "1.0",
	// internal/planes/serving/lifecycle_contract.go). A separate, generic,
	// envelope-versioned contract alongside the session-initiated/
	// cart_updated/payment_submitted events above — those remain exactly as
	// they are (still consumed by the session-projection worker); this
	// contract is additive. EventCheckoutEscalated and EventCheckoutFailed
	// are shared by both: the same topic carries an enriched, additively
	// versioned payload (new fields only — no existing field removed or
	// repurposed) so the session-projection worker keeps parsing correctly.
	EventCheckoutInitiated = "serving.checkout.initiated"
	EventCheckoutUpdated   = "serving.checkout.updated"
	EventCheckoutCompleted = "serving.checkout.completed"
	EventCheckoutAbandoned = "serving.checkout.abandoned"
)
