# Sarvex V1 — Sequence diagrams (who triggers what, across planes)

Companion to `sarvex-v1-architecture.md`, the control-plane OpenAPI (`sarvex-v1-control-plane.yaml`), and the sync-plane AsyncAPI (`sarvex-v1-syncing-plane-async.yaml`). Each diagram traces one flow a[...]

Participants are grouped by plane (see `sarvex-v1-architecture.md` Part A):

- **External** — AI shopping agents (UCP), the merchant's WooCommerce store + Sarvex PHP plugin, the merchant/operator humans, the Vue console.
- **Serving plane** (`internal/planes/serving`) — `/.well-known/ucp`, catalog reads, checkout sessions, negotiation, profile renderer, serving cache.
- **Sync plane** (`internal/planes/sync`) — `/connector/*` HMAC API, ingestion, mapping/scoring, catalog import, sweep, surface artifacts, order-intent, connector registry.
- **Control plane** (`internal/planes/control`) — console REST API + WebSocket, pairing lifecycle, the journey concierge funnel + the store's own `activation` lifecycle (the actual "alive" truth), t[...]

**Conventions:** solid arrow = request/call; dashed arrow = return/async event. Bus topic names are the literal constants from `internal/pkg/ucp/event_types.go` / `command_types.go` — that file is t[...]

> **Per-store note:** every flow is scoped to one **store** (`domain.StoreID`). A merchant may own several stores (`merchant → store → connector`); each has its own connector, profile, serving-cac[...]

---

## 1. Onboarding → first readiness score

**Trigger:** an operator (or, in Phase B, the merchant) creates a merchant + store in the console and pairs the plugin. **Reaction chain:** pairing completes → the control plane automatically fires [...]

```mermaid
sequenceDiagram
    autonumber
    actor Op as Operator/Merchant
    participant UI as Console (web/console)
    participant CS as ConnectorService (control/connector)
    participant Plugin as Plugin (merchant origin)
    participant Bus as Event bus (NATS JetStream)
    participant Imp as CatalogImportHandler (sync)
    participant Reg as Connector registry (sync)
    participant Store as WooCommerce backend
    participant Cat as Catalog store (sync, Mongo)
    participant JSM as Journey state machine (control)
    participant Tasks as Task service (control)

    Op->>UI: create merchant + store (NewMerchantModal / AddStoreModal)
    UI->>CS: POST /operator/merchants, POST .../stores, POST /stores/{id}/connectors/pairing
    CS-->>UI: connector_id + one-time pairing_code
    Op->>Plugin: install plugin, paste pairing_code
    Plugin->>CS: POST /connectors/pairing/complete (code, site facts, store creds)
    CS->>CS: verify code+expiry, encrypt store creds (Vault), mark healthy
    Note over CS: single writer of the connector/pairing record
    CS-)Bus: control.journey.signal (connector created)
    CS-)Bus: control.catalog.import (reason=initial) — automatic
    Bus-)Imp: control.catalog.import
    Imp->>Imp: persist ImportJob, run import (async)
    Imp->>Reg: registry.Get(provider(merchantID, storeID))
    Reg->>Store: FetchCatalog (paginated pull)
    Store-->>Reg: products / pricing / inventory
    loop per batch
        Imp->>Cat: UpsertProduct
        Imp->>Imp: resolve → rubric → score (ReadinessResult)
        Imp->>Cat: write ReadinessResult (overall, dims, issues)
        Imp-)Bus: sync.catalog.import_progress
    end
    Imp-)Bus: sync.task.raised (per actionable issue)
    Bus-)Tasks: sync.task.raised
    Imp-)Bus: control.journey.signal (first sync complete, blocks_checkout count)
    Bus-)JSM: control.journey.signal
    JSM->>JSM: evaluate predicate (zero blocks_checkout?)
    Note over Op,Tasks: Merchant now sees a score + a task feed
```

A console "resync" button (`POST /connectors/{id}/resync`) publishes `control.connector.resync_requested` → `control.catalog.import` (reason=resync), handled by the identical `CatalogImportHandler` [...]

**Teammate invites (not diagrammed — single control-plane hop):** once an owner exists, `Console → POST /account/members {email} → AccountService.InviteTeamMember` (checks the seat cap from bill[...]

---

## 2. Catalog change heartbeat (steady state)

**Trigger:** something changes in the merchant's store (price, stock, product). **Reaction chain:** thin signal → fat pull → re-score → cache rebuild + tasks + journey re-eval. Price/stock take [...]

```mermaid
sequenceDiagram
    autonumber
    participant Store as WooCommerce backend
    participant Plugin as Plugin (merchant origin)
    participant SyncAPI as Sync connector API (HMAC verify)
    participant Bus as Event bus
    participant Ingest as Ingestion (sync)
    participant Reg as Connector registry (sync)
    participant Map as Mapping/scoring (sync, direct call)
    participant Cat as Catalog store (sync)
    participant Reb as Rebuilder (sync→serving)
    participant Cache as Serving cache (serving, Redis)
    participant Tasks as Task service (control)
    participant JSM as Journey state machine (control)

    Store->>Plugin: hook fires (price/stock/product changed)
    Plugin->>SyncAPI: POST /connector/signals (type, id, seq, HMAC) — hard timeout
    Note over Plugin: dropped signal? sweeper catches it later
    SyncAPI->>SyncAPI: verify HMAC, look up connector, record LastSignalAt
    SyncAPI-)Bus: sync.connector.signal_received
    Note over SyncAPI,Ingest: the one bus hop kept *inside* the sync plane — decouples HTTP from per-store-ordered ingestion
    Bus-)Ingest: sync.connector.signal_received
    Ingest->>Ingest: de-dup / per-store order
    Ingest->>Reg: FetchProduct / FetchInventory / FetchPricing
    Reg->>Store: pull authoritative entity
    Store-->>Reg: current entity
    Ingest->>Cat: upsert entity
    Ingest->>Map: OnProductChanged/OnPriceChanged/OnInventoryChanged (direct call)
    Map->>Map: ontology map → rubric → score + issues (price/stock = priority lane)
    Map->>Cat: write updated ReadinessResult
    Map-)Bus: serving.rebuild.requested
    Bus-)Reb: serving.rebuild.requested
    Reb->>Cat: build product snapshot
    Reb-)Bus: sync.catalog.products_ready
    Bus-)Cache: sync.catalog.products_ready
    Cache->>Cache: atomic snapshot swap (serving.snapshot.swapped)
    Map-)Bus: sync.task.raised / control.journey.signal
    Bus-)Tasks: sync.task.raised
    Bus-)JSM: control.journey.signal (blocks_checkout count) → re-evaluate predicate
```

**Reconciliation sweep (safety net, scheduled):** `SweepHandler` periodically full-pulls via the connector registry, diffs against the catalog store, and emits `sync.reconciliation.discrepancy_found` [...]

---

## 3. Profile discovery & per-request negotiation

**Trigger:** an agent discovers the merchant and calls a UCP operation. **Reaction chain:** agent fetches the profile from the merchant origin (served by the plugin from Sarvex's bytes), then calls Sa[...] 

```mermaid
sequenceDiagram
    autonumber
    participant Agent as AI agent
    participant Plugin as Plugin (/.well-known/ucp)
    participant SyncAPI as Sync connector API
    participant Edge as Serving edge (serving/api)
    participant Nego as Negotiation (ucpnego)
    participant Cache as Serving cache (Redis)
    participant Sig as ucpsig (Vault Transit-backed keys)

    Agent->>Plugin: GET /.well-known/ucp (merchant origin)
    alt Sarvex reachable
        Plugin->>SyncAPI: GET /connector/profile (conditional/ETag)
        SyncAPI-->>Plugin: profile bytes (signed by Sarvex)
        Plugin->>Plugin: cache last-known-good
    else Sarvex briefly unreachable
        Plugin-->>Agent: serve last-known-good (never 30X/5xx/vanish)
    end
    Plugin-->>Agent: 200 profile (services.endpoint → Sarvex)
    Note over Agent,Plugin: profile MUST be 200, no redirect, Cache-Control public max-age ≥ 60s

    Agent->>Edge: UCP API call (UCP-Agent header w/ agent profile URL)
    Edge->>Sig: verify inbound signature (RFC 9421, kid from Signature-Input)
    Edge->>Nego: resolve agent profile → intersect with store profile
    Nego->>Cache: read store's rendered profile (cache-only)
    Nego->>Nego: version-select + prune extensions to fixpoint
    Nego-->>Edge: negotiated capability set
    Edge->>Cache: read requested resource (catalog/browse)
    Edge->>Sig: sign response
    Edge-->>Agent: 200 signed response (ucp.capabilities scoped to op type)
```

Discovery/negotiation failures follow the spec's two-bucket model: transport errors (profile unreachable/malformed) are 4xx; a valid-but-empty intersection is a `200` business outcome with a `continue[...] 

---

## 4. Checkout session (the money path)

**Trigger:** an agent starts a purchase. **Reaction chain:** create/update/complete against the serving plane; unlike discovery, checkout calls the **merchant backend live** (via the sync connector) f[...]

```mermaid
sequenceDiagram
    autonumber
    participant Agent as AI agent
    participant Edge as Checkout service (serving/api)
    participant Sess as Session store (Redis)
    participant Reg as Connector registry (sync)
    participant Store as WooCommerce backend
    participant Bus as Event bus
    participant OI as OrderIntentHandler (sync)
    participant Cat as Catalog/Order store (sync)

    Agent->>Edge: POST /checkout-sessions (line items, Idempotency-Key)
    Edge->>Reg: FetchInventory / FetchPricing (live)
    Reg->>Store: stock + pricing for this cart
    Store-->>Reg: current values
    Edge->>Sess: persist session (status incomplete)
    Edge-->>Agent: 201 session + recoverable messages

    Agent->>Edge: PUT /checkout-sessions/{id} (buyer + fulfillment address)
    Note over Edge: full-replacement semantics
    Edge->>Reg: FetchShippingOptions(address) (live) → recompute tax/totals
    Reg->>Store: shipping rates
    Store-->>Reg: options + costs
    Edge->>Sess: replace session
    Edge-->>Agent: 200 session (available handlers resolved per cart)

    Agent->>Edge: POST /checkout-sessions/{id}/complete (opaque credential + handler_id, Idempotency-Key)
    Note over Edge: schema has NO field able to store a raw credential
    Edge->>Reg: create order via connector (live, in-band)
    Reg->>Store: create order
    Store-->>Reg: store order ref
    Edge->>Sess: mark completed
    Edge-)Bus: serving.checkout.order_intent (already carries order_id/status)
    Edge-->>Agent: 200 status=completed + order {id, permalink_url}
    Bus-)OI: serving.checkout.order_intent
    OI->>Cat: record order locally (idempotent)
    OI-)Bus: sync.order.created
```

---

## 5. Task raising & notification

**Trigger:** any component detects something actionable, or a journey transition prescribes a required step. **Reaction chain:** `sync.task.raised` → task service creates the task → console shows [...]

```mermaid
sequenceDiagram
    autonumber
    participant Src as Detector (scoring/sweep/monitoring/validation)
    participant Bus as Event bus
    participant Tasks as Task service (control)
    participant JSM as Journey (control)
    participant Store as Task store (Mongo)
    participant UI as Console TaskFeed

    Src-)Bus: sync.task.raised (detected task)
    Bus-)Tasks: sync.task.raised
    Tasks->>Store: create task owner you/us/external
    JSM->>JSM: on journey transition, reconcile prescribed tasks (catalog import/readiness milestones, pre-validation delegate)
    JSM->>Tasks: create prescribed tasks (same writer)
    UI->>Tasks: GET /tasks (poll / WebSocket journey stream)
    Tasks-->>UI: task list
```

Both detected and prescribed tasks live on the **one spine** (`control/task`), differ only by owner, and are created by the single task-service writer.

---

## 6. Monitoring loop (post go-live)

**Trigger:** scheduled. **Reaction chain:** semantic probes hit the serving plane *as an agent would*; consistency runs on change; findings route to tasks/health.

```mermaid
sequenceDiagram
    autonumber
    participant Sched as Monitoring loop (control/monitoring)
    participant Edge as Serving plane (agent path)
    participant Cons as Consistency source (control)
    participant Bus as Event bus
    participant Tasks as Task service (control)
    participant Health as Health surface (console)

    Sched->>Edge: semantic probe (full request/response, not health-check)
    Edge-->>Sched: signed response
    Sched->>Sched: validate spec-conformance + store-consistency
    Bus-)Cons: sync.reconciliation.discrepancy_found
    Cons->>Cons: discrepancy subscriber → finding
    Sched-)Bus: sync.task.raised (on failure)
    Bus-)Tasks: sync.task.raised
    Health->>Tasks: GET /stores/{id}/health, .../health/consistency
```

The probe path being identical to the agent path is the point — it can't pass while real traffic fails.

---

## 7. Agent-surface enrollment and artifacts

Who triggers what for a surface that generates an artifact. The flow names no
surface: `<surface>` stands for any registered surface whose sync half generates
an artifact (the bundled `direct_agent` surface generates none, so this flow is
what a new surface plugs into — see `docs/HOWTO-add-surface.md`).

```mermaid
sequenceDiagram
    autonumber
    participant UI as Console (/surfaces)
    participant C as Control: surface service
    participant B as Event bus
    participant S as Sync: surface runner
    participant R as Serving cache (Redis)
    participant G as Fetcher (the surface's partner)

    UI->>C: POST /stores/{id}/surfaces/#123;surface#125;/enroll
    Note over C: store must be UCP active, state becomes prerequisites_pending
    C->>B: control.store.surface_state_changed
    C->>B: request_audit (rescore with the surface's attributes)
    B->>S: surface_state_changed
    S->>S: read control_store_surfaces (enrolled?) + catalog
    S->>R: PutArtifact(token, artifact bytes)
    S->>S: write sync_surface_artifacts (token, url, counts)
    S->>B: sync.surface_artifact.updated
    B->>C: surface_artifact_updated
    C->>C: re-evaluate prerequisites (artifact now exists)
    UI->>C: PUT .../config (surface-specific settings)
    UI->>C: POST .../submit (merchant did the partner's steps) → verifying
    G->>R: GET /artifacts/{token} (serving plane, cache only)
    R-->>G: artifact
    Note over R: serving plane publishes serving.surface_artifact.fetched (token only)
    B->>S: surface_artifact_fetched
    S->>S: first_fetched_at (once), emit surface_artifact.updated
    B->>C: surface_artifact_updated
    C->>C: verification signal true → enrolled
```

Catalog changes re-run the generator (debounced); an hourly safety net backs up
missed events; withdrawal unpublishes the artifact and deletes its record.

---

## 8. Validation & conformance

**Trigger:** the journey's `ready` stage delegates a "run pre-validation" milestone task (`journey/stage_work.go`), or an operator/merchant triggers a run directly. **Reaction chain:** the validation [...]

```mermaid
sequenceDiagram
    autonumber
    actor Op as Operator/Merchant
    participant UI as Console (ValidationView / Path)
    participant Val as Validation orchestrator (control)
    participant Edge as Serving plane
    participant Rec as Conformance record (Mongo)
    participant Tasks as Task service (control)

    Op->>UI: POST /stores/{id}/validation/runs
    UI->>Val: start run
    Val->>Edge: exercise UCP operations (same path as agents)
    Edge-->>Val: signed responses
    Val->>Val: check against reconciled rules + pinned spec version
    Val->>Rec: write conformance record (what/when/spec version/result)
    Val->>Tasks: retire "run pre-validation" milestone, raise/retire the failure task
    UI->>Val: GET /stores/{id}/validation/status, .../runs
```

The conformance record is the audit trail for "this store is compliant as of date X," pinned to the definitions-catalogue / spec version in force.

> **How a store actually reaches `live`:** not through this flow. `internal/planes/control/activation` independently derives the store's real lifecycle (`draft → connector_linked → profile_publish[...]

*Reading these before implementing any cross-plane flow: the bus is the only cross-plane channel, the serving plane's only inbound dependency is the atomic snapshot swap, and the one-writer-per-store [...]
