# CLAUDE.md — Sarvex

Guide for AI coding agents (and a concise rule sheet for human contributors) working on this repository. Humans: start with `CONTRIBUTING.md`. Read it fully before any task. If anything you are asked to do conflicts with this file, **stop and report** rather than proceeding.

This file is **derived from the current implementation**, not from design sessions. The code is the ground truth — when this file and the code disagree, the code wins and this file is the bug (fix it).

---

## 0. What Sarvex is (and is not)

Sarvex is an open-source (Apache-2.0) gateway that makes a WooCommerce or Magento merchant **transactable by AI shopping agents over the Universal Commerce Protocol (UCP)**: Sarvex hosts the merchant's UCP profile and checkout endpoints, keeps their catalog agent-ready, and monitors the integration. The merchant never has to understand the protocol.

**The current scope is exactly this. It is not an analytics/governance platform.** Out of scope for now: analytics as a product, cross-merchant benchmarking, governance workflows, LLM enrichment/remediation, an omnichannel UI, partner-marketplace agent surfaces beyond the bundled `direct_agent`, an Ontology API, or connectors beyond WooCommerce/Magento. If a task asks for these, stop and report (§6) — maintainers can widen the scope through an ADR (see `GOVERNANCE.md`).

**Modelled-for, built-minimally — build to the model, not past it:** (1) the data/tenancy model supports **multiple stores per merchant** (`merchant → store → connector`); the console onboards one store per merchant today, but no code may assume one-store-per-merchant. (2) the definitions catalogue represents the **full UCP capability surface**; Sarvex implements the checkout-first subset but represents the whole.

### Source-of-truth documents (read before building)
- `sarvex-v1-architecture.md` — **the authority.** Backend architecture (Part A: planes, services, endpoints, invariants), frontend architecture (Part B: the Vue console), and **Risks** (Part C) and **Dependencies** (Part D). It documents only what is implemented.
- `sarvex-v1-sequence.md` — cross-plane interaction sequences (who triggers what) for onboarding, catalog heartbeat, discovery/negotiation, checkout, tasks/notification, monitoring, surface artifacts, and validation. Read this before implementing any flow that spans planes.
- `sarvex-v1-control-plane.yaml` — control-plane + connector-ingest HTTP contract (OpenAPI 3.0.3). The console generates its TypeScript types from this file.
- `sarvex-v1-syncing-plane-async.yaml` — sync-plane event contract (AsyncAPI 2.6.0).
- `internal/pkg/ucp/event_types.go` / `command_types.go` — **the single source of truth for bus topic names.**
- `docs/TENANCY-PATTERNS.md` — **required reading for all data-layer work.** Query-level tenant fencing is mandatory (ADR-2); handler-level checks are defense-in-depth only. Violating this pattern is a stop-and-report condition.

---

## 1. First, before writing any code: verify the ground

Do a read-only sweep before building on any believed-existing asset. Confirm presence and test state of the package you intend to touch, and report exists-as-code / code-plus-tests / passing. The architecture doc is a snapshot of the implementation — verify it against the tree.

---

## 2. Architecture you must preserve (non-negotiable invariants)

From `sarvex-v1-architecture.md` §A10. Violating one is a stop-and-report condition.

1. **Three planes, isolated.** `internal/planes/{serving,sync,control}` — a plane must **never import another** (`internal/planes/doc.go`). Cross-plane communication is **the event bus only** (NATS JetStream for asynchronous events) **or direct MongoDB collection reads** (read-only, by collection name, without importing plane-specific code). Example: sync plane reads `control_stores`, `control_connectors` and `control_store_surfaces` (which surfaces a store is on — to scope attributes and scoring rules to it, and to decide whom to generate a surface artifact for); control plane reads `sync_surface_artifacts` (the artifact a surface's sync half generated and what was observed about it); control plane reads `sync_feed_status`, `sync_web_baseline_daily` and `sync_catalog_remediations` (remediation records are sync-owned: its score stage creates them, its apply-remediations handler marks them applied; control only reads them and sends the apply command). All such reads are **read-only** — a collection has exactly one writer (the owning plane) and zero or more read-only cross-plane consumers. This pattern avoids code coupling while allowing planes to access each other's reference data without going through the bus.
2. **Discovery/read serves only the cache.** The serving plane's profile + catalog paths read only the Redis serving cache — no live store calls, no control-plane calls. **Checkout is the one spec-mandated exception:** it calls the merchant backend live (stock/shipping/tax/order) — and only through the sync-plane connector, never elsewhere.
3. **One writer per store.** Each store's catalog/serving data has exactly one writing component; everyone else consumes events.
4. **Two-level tenant isolation.** Account data by `merchant_id`, catalog/serving data by `store_id`; each store belongs to one merchant. `merchant_id`/`store_id` are resolved **server-side** (Kinde bearer claim / connector HMAC / authorized operator scope) — **never URL path/query params**. The only cross-tenant stores are the ontology + definitions catalogue.
5. **Authentication is external (Kinde).** Kinde issues the JWT (OAuth 2.0 auth-code + PKCE); Sarvex services only **verify** (issuer, audience, JWKS signature, expiry) and read identity + scope from claims. Do **not** build login/logout/password/session endpoints, store passwords, or mint/revoke tokens. Connector/plugin calls use HMAC, not the JWT.
6. **Idempotency at every external edge.** Inbound signals de-dup by sequence; checkout mutations require idempotency keys; order recording is idempotent. Retries are the normal case.
7. **Spec version is data.** Scoring rubric and the definitions catalogue are versioned loadable content (`internal/pkg/ucp/catalogue`); conformance records pin the version.
8. **The payment-credential boundary is structural.** The checkout schema has no field able to hold a raw credential. Sarvex sees opaque references + `handler_id` only. Never add a credential-bearing field.

---

## 3. The contracts are authoritative — generate from them, do not redesign them

- The control-plane HTTP surface is **exactly** `sarvex-v1-control-plane.yaml`. Do not invent endpoints or rename fields. If the contract is insufficient, stop and report.
- The sync-plane event surface is **exactly** `sarvex-v1-syncing-plane-async.yaml`.
- **The serving-plane UCP endpoints (`/.well-known/ucp` + the UCP API + checkout) are NOT in these specs by design** — they conform to the external **ucp.dev** spec (types pinned at `internal/spec/v20260408`, deltas tracked in `DISCREPANCIES.md`). Implement against the reconciled ucp.dev spec, not against Sarvex's own OpenAPI.
- **Wire conventions:** snake_case fields; list envelope `{ "data": [...], "page": { "cursor", "has_more" } }`; error envelope `{ "error": { "code", "message", "details" } }`; money as integer minor units + `currency`; timestamps RFC 3339 UTC.

---

## 4. The plugin has two roles — keep it thin

`plugin/woocommerce` (PHP). No business logic in PHP — no scoring, mapping, or UCP interpretation.

- **Role 1 — signalling (outbound).** *Thin events, fat pull.* `SignalSender.php` signals *that* something changed (type, id, seq, HMAC) to `POST /connector/signals`; Sarvex pulls the full entity. Async, hard timeout, local retry; a dropped signal is caught by the sweeper. Signals carry ids only — never payment credentials or PII.
- **Role 2 — profile doorway (inbound).** `ProfileProxy.php` serves **only** `/.well-known/ucp` from the merchant's domain, returning Sarvex's bytes with `200` (never a redirect), fetched from `GET /connector/profile` with last-known-good cache. It holds no private keys and authors nothing. It does **not** proxy the UCP API — the profile's `services.endpoint` points directly at Sarvex.

Anything that smells like logic belongs server-side.

---

## 5. Where things live (current tree)

**Backend (single Go binary, `--service` selects the plane; `cmd/root.go`):**
- `internal/planes/serving` — always-on, agent-facing: `/.well-known/ucp`, catalog reads, checkout sessions, negotiation, profile renderer, session store. The serving cache itself (types, `ServingCache`, the Redis client, `CacheProfileSource`) is in `internal/pkg/serving`, because the sync plane (its one writer) and the control plane (the profile doorway) construct it too.
- `internal/planes/sync` — event-driven: `/connector/*` (HMAC), ingestion → mapping → scoring → catalog store (Mongo), catalog import, sweep, `surface` (the sync half of an agent surface: artifact generators), order-intent, `connector` registry (store-API clients).
- `internal/planes/control` — interactive: console/operator REST API (`api/`) + WebSocket (`realtime/`) over services `account`, `activation`, `auth`, `store`, `connector`, `catalog`, `task`, `journey`, `validation`, `monitoring`, `endpointhealth`, `billing`, `operator`, `facts`, `domain`, `engagement`, `integration`, `checkoutescalation`, `surface` (the registry-driven agent-surface enrollment; one module per surface under `surface/<name>`, registered in `surface/all`), plus the bus-fed read models `channel`, `insights`, and `ledger` (and `sim`, the in-process plugin simulator for local dev).
- `internal/spec/vYYYYMMDD` — one self-contained module per UCP spec generation (today `v20260408`, `v20260825`): its schema types (+ `extensions/`, `DISCREPANCIES.md`), `wire` (its profile document form), `profile` (its `generation.Generation`: reads and writes that document) `codec` (its `wireadapt.Codec`: checkout/cart/order translation, the response envelope, and catalog search/lookup) and `release` (its `spec.Version`: id, what it serves, codec, profile). The spec registry is `internal/spec` (`Latest`, `Baseline`, `Serving(capability)`, `Servable`, `Codecs`, `Generations`), assembled in `internal/spec/all` — one line per generation; nothing but `internal/spec/all` imports a version module. `internal/spec/spectest` is a test-only canary generation.
- `internal/pkg/ucp` — shared protocol machinery: `types` (the `Version` type), `wire` (the generation-neutral profile constructs), `wireadapt` (the codec interface and the codec view of the spec registry), `generation` (the profile-document interface and its view), `ucpsig` (RFC 9421 signing), `ucpnego` (negotiation), `catalogue` (definitions + scoring rubric), `connectors/base` (the platform-neutral connector interface + registry), `validator` (+ `registry`), `paymenthandler`, `resilience`, `event_types.go`/`command_types.go`.
- `internal/platform` — the platform registry (`platform.All`), and one self-contained module per backend platform: `internal/platform/<name>` (connector + merchant config), `<name>/validator` (credential validator), `<name>/sim` (simulated store surface); frozen connectors under `internal/platform/frozen/*`. See `docs/plans/version-platform-modularity.md`.
- `internal/pkg/commerce/model` — the canonical commerce model (checkout, cart, order, fulfillment, messages, payment, consent, discounts, catalog query/product/page): Sarvex-owned types the connectors produce and the money path works in. It belongs to no spec version — each version's `wireadapt` codec renders it (and stamps that version's `ucp` envelope); nothing outside a version's own packages reaches these shapes through `internal/spec/vYYYYMMDD`.
- `internal/pkg/*` — `eventbus` (NATS JetStream via Watermill), `commandbus`, `devinfra` (local/test addresses of the backing services), `secretmanagement` (Vault KV v2), `jwkmanagement`, `keypair`, `email`, `middlewares`, `simstore` (the platform-neutral simulated store model), `golden` (golden-file test helper).
- `internal/archtest` — repository-wide boundary check: any spec version or platform named outside its own module or a registration root, any surface id or surface module named outside its own module, the shared id package (`internal/pkg/ucp/surfaces`) or a surface registration root, and any plane-to-plane import, fails. There is no allowlist.

**Frontend (`web/console`, `@sarvex/console`):** Vue 3 + TypeScript + Vite + Pinia + Vue Router + axios. Views in `src/views` (surfaces: Connect/Path/Store/Health/Analytics/Validation/Account/Settings/Board/CheckoutSessionDetail/EnterAsMerchant/Login). Pinia stores in `src/stores` (one per domain). API layer in `src/api` (typed modules + `client.ts` axios + `mocks.ts` mock backend + `schema.d.ts` generated from the OpenAPI). Auth in `src/auth` (Kinde PKCE or a local simulator, selected by `VITE_AUTH_MODE`). Components in `src/components/{ui,shell,journey,task,connect,store,operator,settings,analytics,channel,checkout,insights,ledger}`.

**Infra:** MongoDB (primary), Redis (serving cache + sessions), NATS JetStream (bus), Vault (Transit for signing keys + KV v2 for creds/handler config), Stripe (billing), Kinde (IdP). Production is self-hosted Kubernetes (`deployments/selfhost`: Helm + Argo CD + Cloudflare Terraform); local, tests and CI run the *same* services via `deployments/docker-compose.yaml` (`make infra-up`) — nothing is emulated.

---

## 6. Stop-and-report conditions (do not work around)

Stop and report when a task would:
- Build anything on the out-of-scope list (§0): analytics/benchmarking as product, governance/approval workflows, LLM enrichment/remediation, multi-store management UI, omnichannel, Ontology API, the public free-audit tool, or connectors beyond WooCommerce/Magento.
- Violate any §2 invariant (cross-tenant query, **second writer on any collection** — collections have exactly one writer, read-only cross-plane readers are OK per §2.1 pattern, serving-plane read-path dependency on the store/control plane, `merchant_id`/`store_id` in a URL, a credential-bearing checkout field, a plane importing another plane, minting auth tokens).
- Require diverging from `sarvex-v1-control-plane.yaml` / `sarvex-v1-syncing-plane-async.yaml`.
- Extend the frozen non-target connectors (Shopify, BigCommerce, Commercetools) with features — they exist in CI/scenarios so they don't rot; add nothing.
- Depend on an asset the §1 sweep has not confirmed exists and passes, or that the architecture doc describes without first confirming it in the tree.

---

## 7. Build / test / run commands (from the Makefile)

- **Local stack:** `make infra-up` (Mongo, Redis, NATS JetStream, Vault, OTel collector); `make dev-up` (that + `cmd/sim serve` + control + sync + serving planes), `make dev-down`, `make dev-status`. `make dev-token` mints a dev bearer for curl.
- **Console:** `make console` (Vite :5173, mock mode — no backend), `make console-real` (proxies `/api` → control plane :8081; run `make dev-up` first).
- **Tests:** `make test-unit`; `make test-scenarios` (7 platform flows); `make test-integration` (control/sync/serving — need `make infra-up`); `make test-e2e-journey` (WooCommerce→agent E2E); `make test-conformance` (external UCP conformance); `make test` runs unit + E2E + integration.
- **Adding a spec version / platform:** `make new-spec-version V=YYYY-MM-DD`, `make new-platform P=name [D=Display]` scaffold the module and its registration line; then follow `docs/HOWTO-add-spec-version.md` / `docs/HOWTO-add-platform.md`.
- **Frontend tests:** `cd web/console && npm run test:run` (Vitest); `npm run typecheck` (vue-tsc); `npm run gen:api` regenerates `schema.d.ts` from the OpenAPI.

Always run the relevant tests before claiming a change is done, and report failures with their output.

---

## 8. Working style in this repo

- Go 1.26; follow the surrounding code's idioms, naming, and comment density. Format with `gofmt`, lint with `golangci-lint` (`.golangci.yml`), and add the SPDX header (`python3 scripts/license-headers.py`) to new source files.
- Keep changes on the branch you were assigned; commit with clear messages; open a draft PR after pushing if none is open.
- **No bug is "pre-existing".** Any bug you find while working — in code you touched or not — gets fixed in the same task, with a regression test, to the ucp.dev spec where the spec applies. Do not defer, label, or report-and-leave a bug. If a fix would violate a §2 invariant or §6 stop condition, stop and report that conflict instead; otherwise fix it.
- **Every change works across all spec versions and all backend platforms.** A capability, extension, fix or behaviour change is not done until it holds on **every active spec generation** (the spec registry's `Servable()` — today 2026-04-08 and 2026-08-25) **and on every supported backend platform** (WooCommerce and Magento; the frozen Shopify/BigCommerce/Commercetools connectors are exempt, §6). Concretely: a change to a connector is made to both `internal/platform/woocommerce` and `internal/platform/magento` (plus the simulated store surface each is tested against, `internal/platform/<name>/sim`); a change to the canonical model or a response is checked through `wireadapt` in every servable generation; and `TestCapabilityMatrix` (`internal/planes/sync/sim/capability_matrix_test.go`) gains the new capability/extension so both axes stay covered — adding a platform or a generation extends that matrix automatically. If one platform or version genuinely cannot support something, stop and report it (and make the profile/checkout not advertise it there) instead of shipping it for one and leaving the other behind.
- **Surfaces are added, not edited into.** An agent surface (`direct_agent` is the bundled one) is a control half (`internal/planes/control/surface/<name>`: descriptor, prerequisite checks, artifact/verification adapters), an optional sync half (`internal/planes/sync/surface/<name>`: an artifact `Generator`), and a console folder (`web/console/src/surfaces/<id>`: its copy), each registered by one line (`surface/all` on each plane; the console picks its folder up by glob). Its id lives once, in `internal/pkg/ucp/surfaces`. Do not add `if surfaceID == …` anywhere else — if behaviour differs, it belongs in that surface's modules or its descriptor. `docs/HOWTO-add-surface.md`.
- **Versions and platforms are added, not edited into.** Each spec generation and each backend platform is one self-contained unit behind a registry, so a new one is new code plus one registration line and never a change to an established one. Versions: one module per generation (`internal/spec/vYYYYMMDD`: schema, `wire`, `profile`, `codec`, `release`), registered by one line in `internal/spec/all`; callers go through the spec registry and never `switch` on a version — which generations serve a capability is `Serving(capability)` (from each module's `Serves`), and a default generation is the registry's `Baseline()`/`Latest()`, never a constant. Platforms: one module per platform (`internal/platform/<name>`: connector, merchant config + schema, validator, simulated store, plugin pointer), listed once in `internal/platform` (`platform.All`); the planes' composition roots call `platform.Register*`, the control plane validates with `platform.Supported`, tools default to `platform.Default()`, and nothing else names a platform (a platform-only feature such as a platform's own payment handler lives in that platform's module). Adding a platform also adds it to `sarvex-v1-control-plane.yaml`'s platform enum and credentials schemas, and adds `plugin/<name>` — `internal/platform`'s tests fail until all three agree. Do not add `if version == …` / `switch platform` branches in shared code — if behaviour differs, it belongs in that version's codec or that platform's connector. `TestEveryGenerationIsComplete` and `TestCapabilityMatrixCoversEverySupportedPlatform` fail when a version or platform is incomplete, and the canary tests (`TestCanaryGenerationChangesNoExistingOutput`, `TestCapabilityMatrix_CanariesChangeNothing`) prove a new one changes no existing output; the goldens (`**/testdata/golden`, `UPDATE_GOLDENS=1` to regenerate) freeze each existing (platform, version) pair's output, and changing one needs a `spec-change/<version>` or `platform-change/<platform>` PR label (`.github/workflows/golden-guard.yml`).
- The plane boundary and the one-writer rule are the two things most worth protecting in review — if a change blurs either, flag it rather than shipping it.
