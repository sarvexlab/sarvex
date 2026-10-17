# Security Test Plan — Website, Console, and API

> Sections about the marketing `website` describe a separate repository that is not part of this release; they are kept for context.

Status: draft, pending review. Covers this repository (backend planes, console, WooCommerce/Magento plugin). This is **item 1** of two asks — a test plan and pentest case catalog. SAST/DAST tooling recommendations are in §5; nothing here has been executed yet, no scans have been run, and no findings are claimed. Treat every "expected" behavior below as a hypothesis to verify, not a confirmed fact.

Grounded in a read-only sweep of the current tree (`internal/planes/*`, `internal/pkg/middlewares`, `internal/planes/control/auth`, `web/console`, `plugin/{woocommerce,magento}`, `website/server/api`, `.github/workflows`) done 2026-09-26. File:line references below point at real code, not assumed shapes.

---

## 0. Scope & rules of engagement

**In scope:**
- `website` — Nuxt/Cloudflare Worker marketing site, its two Nitro server routes (`server/api/contact.post.ts`, `server/api/subscribe.post.ts`), and the static/prerendered surface.
- `web/console` (`@sarvex/console`) — the Vue 3 SPA, its Kinde PKCE auth flow, and its typed API client.
- `internal/planes/control/api` — the console/operator REST API (Kinde bearer + HMAC connector auth, per `sarvex-v1-control-plane.yaml`).
- `internal/planes/control/realtime` — the operator WebSocket surface.
- `internal/planes/sync` — `/connector/*` HMAC-authenticated ingest, catalog import, order-intent, sweep.
- `internal/planes/serving` — the public, unauthenticated `/.well-known/ucp`, catalog, negotiation, and checkout endpoints (checkout is the one path that calls a merchant backend live, per architecture §A10.2).
- `plugin/woocommerce` and `plugin/magento` — treated as attacker-adjacent code: it runs on merchant infrastructure Sarvex doesn't control.

**Out of scope for this pass** (per `CLAUDE.md` §0/§6 — flag, don't build): anything on the V1 out-of-scope list, the frozen Shopify/BigCommerce/Commercetools connectors (test only enough to confirm they're inert, not their business logic), infrastructure the repo doesn't declare (Kinde's own hosted login UI, Stripe's hosted pages, cluster and Cloudflare account access outside `deployments/selfhost`).

**Rules of engagement:**
- All dynamic/DAST testing runs against a dedicated staging/sandbox deployment with test Stripe keys, a sandboxed Kinde tenant, and simulator merchant stores (`cmd/sim`, `cmd/ucp-sim`-equivalent) — **never** against production, and never against a real merchant's live WooCommerce/Magento install. Checkout is the one endpoint that calls a merchant backend live; that merchant backend must be the simulator, not a real store.
- No destructive testing (data deletion, DoS payloads) without a dedicated disposable environment.
- Credential/secret material (Kinde client secrets, HMAC pairing codes, Vault tokens and Transit keys, Stripe keys) is never exercised against real values in test scripts or commit history.
- Findings are triaged by the invariants in `CLAUDE.md` §2 first — a violation there is a stop-and-report condition regardless of exploitability.

---

## 1. Attack surface map

| Surface | AuthN | Reachability | Key files |
|---|---|---|---|
| Website pages (static/prerendered) | none | public internet | `website/pages`, `website/content` |
| Website `/api/contact`, `/api/subscribe` | Turnstile captcha (contact only) + bearer token to downstream (server-side) | public internet | `website/server/api/*.post.ts` |
| Console SPA | Kinde PKCE (browser) | public internet (static SPA) | `web/console/src/auth` |
| Control-plane REST (`/api/*`) | Kinde JWT bearer (or dev sim resolver) | public internet, behind CORS allow-list to one origin | `internal/planes/control/api`, `auth/bearer.go` |
| Control-plane WebSocket | same bearer, presumably re-checked at upgrade | public internet | `internal/planes/control/realtime` |
| Sync-plane `/connector/*` | HMAC (`X-Sarvex-Signature/-Timestamp/-Sequence/-Credential`) | public internet (merchant plugin calls in) | `auth/hmac.go`, `sync/connector` |
| Serving-plane `/.well-known/ucp`, catalog, negotiation | **none by design** (public agent surface) | public internet | `internal/planes/serving` |
| Serving-plane checkout | none inbound, but calls merchant backend live | public internet in, live outbound call out | `checkout_service.go` |
| WooCommerce plugin `ProfileProxy.php` | none (proxies public profile) | merchant's own domain | `plugin/woocommerce/src/ProfileProxy.php` |
| WooCommerce plugin `SignalSender.php` | HMAC outbound to Sarvex | merchant's own domain → Sarvex | `plugin/woocommerce/src/SignalSender.php` |
| Magento plugin (`Controller/Wellknown`, `Observer/*`) | same pattern, Magento module | merchant's own domain | `plugin/magento/*` |

---

## 2. Threat model — tie every test to an invariant

`CLAUDE.md` §2 lists eight non-negotiable invariants; the ones with real attack-surface implications:

1. **Plane isolation** — a control-plane bug that lets an attacker reach sync/serving internals directly (not via bus/Mongo read) is a structural break, not just a bug. Test: fuzz for any HTTP route that isn't in `sarvex-v1-control-plane.yaml` but resolves anyway (route confusion, path traversal into another plane's router if they ever share a process/port).
2. **Discovery/read serves only the cache; checkout is the one live-call exception** — test that no serving-plane read path can be coerced into a live store call (e.g. via a crafted `store_id` or cache-miss fallback), and that checkout's live call path can't be reached from any other route.
3. **One writer per store** — not directly attacker-testable, but a tenant-isolation bypass that lets one store's connector write another store's catalog would violate it; covered by tenancy tests below.
4. **Two-level tenant isolation, resolved server-side only** — the single highest-value target. `merchant_id`/`store_id` must never be trusted from URL path/query/body; they must come from the verified JWT claim, the HMAC credential, or operator scope. This is a full IDOR test class (§4).
5. **Kinde is the only auth issuer** — `NewSimBearerResolver` (`auth/bearer.go:89`) trusts **unsigned** tokens (`header.payload.anysig`, no signature check) and is wired in whenever `KINDE_ISSUER_URL` is unset (`control/api/entry.go:858-859`). This is intentional for local dev, but its blast radius if it ever activates in a real deployment is total auth bypass. Test: confirm every non-local deployment target in `deployments/` sets `KINDE_ISSUER_URL`, and add a startup guard/CI check that refuses to boot the sim resolver when a production-shaped env var (e.g. `ENV=production`) is set.
6. **Idempotency at every external edge** — checkout mutations, order recording, and connector signal ingestion all claim idempotency; test replay behavior explicitly (§4).
7. **Spec version is data** — lower security relevance, skip for pentest, keep for correctness testing.
8. **Payment-credential boundary is structural** — the checkout schema must have no field able to hold a raw credential; Sarvex only ever sees `handler_id` + opaque references. Test: attempt to smuggle a PAN/CVV-shaped value into any checkout/order field and confirm the schema/validator rejects it, and grep the schema for any string field wide enough to hold one.

---

## 3. Test plan by target

### 3.1 Website
- **Input handling**: `contact.post.ts` and `subscribe.post.ts` do their own validation (email regex, length caps) before any downstream call — test bypasses (unicode homoglyphs in email, `storeUrl`/`message` length-boundary values, header injection via `email`/`storeUrl` reflected into the Resend `text` body — confirm no CRLF injection into the outbound email).
- **SSRF via `storeUrl`**: the field is free text forwarded into an email body only today — confirm it's never later passed to a server-side fetch (`$fetch`) without validation if that code path grows.
- **Captcha bypass**: replay a used Turnstile token, omit `remoteip`, or race two submissions with the same token.
- **Secrets in client bundle**: confirm `turnstileSecretKey`/`resendApiKey`/`contactApiToken` never leak into the client-side runtime config (Nuxt `public` vs private runtimeConfig split) — grep the built `.output/public` bundle for the literal secret strings.
- **Headers**: verify CSP, `X-Frame-Options`/`frame-ancestors`, `Strict-Transport-Security`, `X-Content-Type-Options` are set (Nitro/Cloudflare config) — none were found in `nuxt.config.ts` during recon; likely a gap (§9).
- **Rate limiting / abuse**: neither `/api/contact` nor `/api/subscribe` show rate limiting in the handler — confirm Cloudflare-level rate limiting exists, or add it; otherwise both are open to volumetric abuse (email-bombing the sales inbox, exhausting Resend quota) and to spamming the newsletter backend, which the code forwards to `contactApiUrl` unauthenticated-by-source.
- **Dependency/build supply chain**: `pnpm-lock.yaml` integrity, `postinstall: nuxt prepare` and the `docs` subsite's own `npm install` (two separate lockfiles/ecosystems in one repo — confirm both get scanned).

### 3.2 Console (Vue SPA)
- **Token handling**: where does Kinde PKCE store the access/refresh token (memory vs `localStorage`/`sessionStorage`)? `localStorage` is XSS-exfiltratable; confirm `@kinde-oss/kinde-auth-pkce-js`'s default and whether the app overrides it.
- **XSS surface**: any `v-html` usage, any merchant-controlled data (catalog titles/descriptions pulled from a real WooCommerce store) rendered into the console — a malicious merchant catalog is attacker-controlled input reaching an authenticated console session (stored XSS via product data is a real, high-value path given the ingestion pipeline). Grep `web/console/src` for `v-html`, `innerHTML`, `dangerouslySetInnerHTML`-equivalents.
- **Client-side authorization is not authorization**: confirm every view/route guard is backed by a real server-side check — the console's role/merchant checks are UX only; the actual gate must be in `internal/planes/control/api` middleware. Test by calling the API directly (bypassing the SPA) with a valid-but-wrong-scope token.
- **CORS**: `WithCORS` (`middlewares/withCors.go`) allow-lists exactly one `dashboardUri` via exact-match after trimming trailing slash — test with scheme confusion (`http` vs `https`), subdomain variants, and trailing-path variants to confirm the exact-match really is exact and not a prefix/suffix match.
- **Mock mode leakage**: `src/api/mocks.ts` exists for `make console` (mock, no backend) — confirm build tooling can never accidentally ship the mock backend (with any seeded fake data/tokens) in a production build.
- **Dependency/SCA**: `npm audit`/`osv-scanner` against `web/console/package.json` (axios, vue, kinde SDK, wrangler) — console CI currently runs build+tests only, no SCA (§5).

### 3.3 Control-plane API (`internal/planes/control/api`)
- **AuthN correctness** (`auth/bearer.go`): algorithm confusion (force `alg: none` or HS256-with-public-key-as-secret against the JWKS-based verifier), expired/not-yet-valid token handling, audience/issuer mismatch, missing `merchant_id`/`role` claim handling (already errors — confirm it errors *closed*, not open), token replay across environments (does a staging-issued token verify against production's issuer/audience?).
- **Tenant isolation / IDOR** — the core test class, per `docs/TENANCY-PATTERNS.md` (ADR-2, query-level fencing mandatory): for every endpoint under `api/{account,store,connector,catalog,task,checklist,journey,validation,monitoring,billing,operator,facts,domain}.go`, authenticate as merchant A and request/mutate a resource ID (`store_id`, `connector_id`, order/checkout id, task id) belonging to merchant B. Expected: 404/403, never data. Specifically target `payment_config.go`, `ledger.go`, `checkout_sessions_test.go`'s underlying handler, and `insights.go` — anywhere a lookup is "by ID" rather than "by ID scoped to my merchant" is a finding.
- **Operator scope escalation**: `withAdmin.go`/operator role — confirm a merchant-role token can never reach operator-only routes (`operator/` service, `EnterAsMerchant` in the console implies an operator-impersonation feature — test that impersonation itself is scoped/audited and can't be self-granted).
- **HMAC connector auth** (`auth/hmac.go`): replay beyond the sequence check — the function checks `X-Sarvex-Sequence` against a Redis dedup set but the `X-Sarvex-Timestamp` header is read and folded into the signed message, yet nowhere in the read code does it appear to be checked for staleness (a signature computed once with a fixed, far-future or far-past timestamp would still verify if the sequence hasn't been seen). Confirm whether a timestamp-window check exists elsewhere in the call chain; if not, this is a real replay-window widening finding — test by replaying a captured request with a new/unused sequence number and the original timestamp far outside any reasonable clock-skew window. Also test: Redis unavailable → does dedup check fail closed (`return error`, confirmed in code — good) or does the "mark sequence as seen" write failure silently allow a later replay (confirmed: write failure is logged but not fatal — a second replay in the same request lifetime before the key is set is a narrow race worth confirming isn't exploitable).
- **HMAC credential/connector confusion**: `<store_id>:<connector_id>` credential format — test with a `connector_id` that exists but belongs to a different `store_id` than claimed (code checks this — confirm the check can't be bypassed by encoding tricks in the credential string, e.g. extra colons, since `SplitN(credential, ":", 2)` takes only the first split).
- **Rate limiting**: `WithRateLimiting` (`internal/planes/serving/middleware.go`) is in-memory, per-process, keyed by `RealIP()` — test (a) multi-instance bypass (each pod gets its own bucket), (b) `RealIP()` spoofing via `X-Forwarded-For` if the proxy/LB config trusts client-supplied headers, (c) whether login/token-verification-heavy routes have tighter limits than the blanket 50rps/10burst.
- **Body limits / request validation**: `withBodyLimit.go`, `withRequestValidation.go` — oversized payload DoS, malformed JSON handling, schema-validation bypass (extra/unknown fields — mass assignment into domain structs).
- **Injection**: Mongo query construction anywhere user input reaches a filter directly (NoSQL injection via operator injection if any handler builds filters from raw request maps rather than typed structs) — grep for `bson.M{` built from request bodies.
- **Payment-credential boundary**: attempt to POST a full PAN/CVV-shaped string into any checkout/payment field across `payment_config.go` and the serving-plane checkout schema; confirm rejection and confirm no logging path (`reqlog.go`, `withRequestLogger.go`) would capture it if it were ever sent (log scrubbing test).
- **WebSocket (`realtime/`)**: confirm the upgrade handshake re-validates the bearer token (not just "was authenticated once, trust the socket forever"), and that messages on the socket are still tenant-scoped per §2.4.

### 3.4 Sync-plane connector ingest
- Same HMAC test class as 3.3, but this is the plugin-facing edge reachable from any merchant's WordPress/Magento install — a compromised or malicious merchant is a *legitimate, authenticated* actor here, so also test:
- **Malicious catalog content**: a merchant pushes a product with a script-payload title/description/image-URL. Trace it through `ingestion.go` → `mapping.go` → `catalog_mongo.go` → serving cache → console rendering. Stored XSS via merchant-controlled catalog data reaching the console is the most realistic high-severity finding in this system.
- **SSRF via connector pull**: the "thin events, fat pull" model means Sarvex calls back into the merchant's store API (`GET /connector/profile`-style pulls) using a URL derived from store registration data — test whether that URL can be pointed at internal/metadata addresses (`169.254.169.254`, `localhost`, internal service DNS) at store-registration time.
- **Sweep/job server DoS** (`sweep_handler.go`, `job_server.go`): a merchant that signals at high frequency or with oversized payloads — confirm backpressure/rate limiting per-connector, not just global.
- **Catalog import** (`catalog_import.go`): zip-bomb/oversized-feed handling if imports accept bulk file uploads.

### 3.5 Serving-plane public API
- **No inbound auth by design** — confirm nothing here silently trusts a header value as identity (there should be none to trust).
- **Cache key tenancy**: confirm every Redis cache key (`cache.go`, `cache_redis.go`) is namespaced by `store_id` such that no request for store A can ever read store B's cached profile/catalog (a cache-key collision here is a direct cross-tenant data leak, more severe than a typical API IDOR since it's unauthenticated).
- **Checkout idempotency** (`checkout_idempotency_test.go` exists — confirm real coverage, not just a stub): replay a checkout mutation with the same idempotency key and confirm no double-charge/double-order; replay with same key but different body and confirm rejection, not silent overwrite.
- **Session store** (`session_redis.go`, `session_memory.go`): session fixation, session ID entropy, expiry enforcement, and whether `session_memory.go` (presumably a fallback/dev store) could ever be selected in a multi-instance production deployment (a per-instance memory session store behind a load balancer breaks session continuity and may fail open).
- **Abandonment sweep** (`abandonment_sweep.go`): confirm it can't be triggered/timed by an external actor to cancel another party's in-flight checkout.
- **Webhook endpoint** (`webhook.go`): if this receives inbound callbacks (Stripe or connector), confirm signature verification is mandatory and fails closed.

### 3.6 Merchant plugin (WooCommerce/Magento)
- **`ProfileProxy.php`**: must serve `/.well-known/ucp` with Sarvex's bytes verbatim, `200` only, never a redirect (per `CLAUDE.md` §4) — test cache-poisoning of the "last-known-good" local cache (can an attacker who compromises the merchant's WP admin, or a MITM before TLS, seed a bad cached profile that persists past a legitimate fix?), and confirm it holds no private keys (grep plugin storage/options tables for anything secret-shaped).
- **`SignalSender.php` / HMAC.php**: pairing-code storage on the merchant's own DB (WordPress options table / Magento config) — is it encrypted at rest, or plaintext in a table an SQLi elsewhere on the WP site could read? Confirm signal payloads truly carry ids only (per §4) — grep for any field that could carry PII/payment data.
- **`PairingManager.php`**: the pairing/registration flow is the bootstrap trust anchor — test whether the pairing code can be brute-forced, replayed, or re-used after rotation, and whether re-pairing requires re-proving control of the store.
- **WordPress-specific**: nonce/CSRF protection on any admin-facing action, capability checks (`current_user_can`) on plugin admin pages, no raw `$wpdb->query` string concatenation (SQLi), output escaping in the plugin's own admin UI.
- **Magento-specific**: ACL (`etc/acl.xml`) correctly restricts the plugin's admin routes; `Observer/*` classes don't trust event data as already-validated.

---

## 4. Concrete pentest case catalog

Each case: precondition → action → expected secure result. IDs group by target.

| ID | Target | Case |
|---|---|---|
| WEB-01 | Website | Submit `/api/contact` with a captcha token reused from a prior successful submission → expect rejection |
| WEB-02 | Website | Submit `/api/contact` with `email` containing `\r\nBcc: attacker@evil.com` → expect no header injection into the outbound Resend email |
| WEB-03 | Website | Submit `/api/subscribe` 1000x/min from one IP → expect throttling (Cloudflare or app-level) |
| WEB-04 | Website | Fetch the built client bundle, grep for `resendApiKey`/`turnstileSecretKey`/`contactApiToken` literal values → expect no match |
| WEB-05 | Website | Request any page over plain HTTP or with a wrong `Host` → expect HSTS redirect / rejection, not reflection |
| CON-01 | Console | Inspect browser storage post-login for raw JWT/refresh token in `localStorage` → assess XSS blast radius |
| CON-02 | Console | Search `web/console/src` for `v-html` sinks fed by catalog/product data → expect none, or expect DOMPurify-equivalent sanitization |
| CON-03 | Console | Call a control-plane endpoint directly (curl, valid token, store the console UI never lets you reach) for a resource outside your merchant → expect 403/404 |
| CON-04 | Console | Send a CORS preflight from `https://evil.example` and from `https://console.sarvex.example.com.evil.com` → expect rejection by `WithCORS`'s exact-match check |
| API-01 | Control API | Token for merchant A, request `/api/stores/{storeB_id}` (and equivalent for connector/task/checklist/journey/insights/ledger/billing/payment_config) → expect 403/404 on every one, not just some |
| API-02 | Control API | Forged JWT with `alg: none` or HS256-signed-with-JWKS-public-key against the RS256 verifier → expect rejection |
| API-03 | Control API | Valid merchant-role token against an operator-only route → expect 403 |
| API-04 | Control API | Confirm `NewSimBearerResolver` cannot activate when the deployment is not local dev (check every `deployments/` env for `KINDE_ISSUER_URL`) |
| API-05 | Control API | Replay a captured HMAC-signed connector request with a new sequence number but the original (old) timestamp → expect rejection if a timestamp-freshness check exists; if it doesn't, this is a finding — recommend adding one |
| API-06 | Control API | HMAC credential string `storeA:connX:extra` (extra colon) → expect the `SplitN` parsing can't be abused to attribute the request to a different store than intended |
| API-07 | Control API | Flood one endpoint from many source IPs (simulating a botnet) against the in-memory per-IP rate limiter → expect a documented mitigation (WAF/LB-level) beyond the app-level limiter, since the app-level one is per-process |
| API-08 | Control API | Oversized JSON body (>configured limit) and deeply nested JSON → expect clean rejection, no resource exhaustion |
| API-09 | Control API | POST a 16-digit numeric string into any checkout/payment-adjacent field → expect schema rejection; grep logs after the attempt for the literal value → expect absent |
| API-10 | Control API (WebSocket) | Open a WebSocket with a valid token for merchant A, then attempt to subscribe to/receive merchant B's channel/events → expect rejection |
| SYNC-01 | Sync ingest | Signal a product with title `<img src=x onerror=alert(1)>` through the full ingestion→mapping→catalog pipeline, view it in the console → expect encoded output, not execution |
| SYNC-02 | Sync ingest | Register a store whose backend URL resolves to `169.254.169.254` or `127.0.0.1` → expect the connector pull step to refuse (SSRF guard) |
| SYNC-03 | Sync ingest | Send HMAC-signed signals at high frequency from one connector → expect per-connector backpressure, not just global rate limiting |
| SYNC-04 | Sync ingest | Send a signal with a `store_id` in the payload that doesn't match the HMAC credential's `store_id` → expect rejection |
| SRV-01 | Serving plane | Request store A's cached profile/catalog using any manipulated identifier that could collide with store B's cache key → expect no cross-tenant data returned |
| SRV-02 | Serving plane | Submit the same checkout idempotency key twice with different line items → expect rejection of the second, not silent overwrite or double order |
| SRV-03 | Serving plane | Submit the same checkout idempotency key twice concurrently (race) → expect exactly one order created |
| SRV-04 | Serving plane | Hit the checkout endpoint (which calls the merchant backend live) at high volume → expect the merchant-backend call path has its own timeout/circuit-breaker (`resilience` package) so a flood can't be used to DoS the merchant's store through Sarvex |
| SRV-05 | Serving plane | Inspect `webhook.go` — send an unsigned/wrongly-signed inbound webhook → expect rejection, not best-effort processing |
| PLG-01 | WooCommerce plugin | MITM or compromise the "last-known-good" cache used by `ProfileProxy.php` → assess whether a stale/poisoned profile can persist after the upstream is fixed, and for how long |
| PLG-02 | WooCommerce plugin | Inspect WordPress options table / DB for the HMAC pairing secret → expect it's stored no more readably than other WP secrets, ideally encrypted |
| PLG-03 | WooCommerce plugin | Attempt to re-run the pairing flow without proving control of the store (e.g. replay an old pairing code) → expect rejection |
| PLG-04 | Magento plugin | Access `Controller/Wellknown` and `Observer/*` as an unauthenticated/low-privilege user → expect ACL (`etc/acl.xml`) enforcement matches WooCommerce's equivalent guarantees |

This table is a starting catalog, not exhaustive — expand it once the §1 sweep on `deployments/` (env-per-environment config) and the actual `sarvex-v1-control-plane.yaml` endpoint list are cross-referenced endpoint-by-endpoint (there are more endpoints than are listed individually above; API-01 should be run against literally every merchant-scoped GET/PATCH/DELETE, not just the sample named).

---

## 5. SAST / DAST / SCA — what applies, and what's already there vs. missing

**Status: implemented in this PR, unless noted "still open" below.**

- `.golangci.yml` runs `gosec`, `bodyclose`, `noctx`, `sqlclosecheck`, `rowserrcheck`, `depguard` (architectural import-boundary enforcement — this is the automated check for the plane-isolation invariant) as part of `go-lint.yml`, but scoped to `--new-from-rev` (only new lines in a PR diff) — the existing backlog is not gated. Unchanged; still open (the backlog needs a separate paydown pass before removing the scoping).
- **`.github/workflows/security.yml`** (new): `govulncheck`, `gosec` (SARIF), CodeQL(go) with **`.github/codeql/codeql-config.yml`** (new, scoped to `internal`/`cmd`/`main.go`), Bearer, and **`.semgrep/tenancy.yml`** (new custom rules, see below) — all whole-tree, not diff-scoped, plus `gitleaks` for secret scanning. Semgrep runs `continue-on-error: true` for now (new custom rule set, first findings need triage before it blocks merges — same pattern `go-test.yml` already uses for its own new conformance job).
- **`.semgrep/tenancy.yml`** (new) mechanically enforces two of the invariants a generic SAST ruleset can't reach:
  - `merchant-id-from-url-param`: flags `merchant_id` sourced from `c.Param`/`c.QueryParam`/`c.FormValue` outside `operator.go` (the one sanctioned "authorized operator scope" exception, gated by `requireOperator` in `handler.go`). **Deliberately does not flag `store_id`-from-URL** — that's the normal, blessed REST addressing pattern per `docs/TENANCY-PATTERNS.md` (resource ID in the path, `merchant_id` from the principal fences the query) — an earlier draft of this rule flagged it and produced 59 false positives against exactly that pattern before being narrowed.
  - `checkout-credential-bearing-field`: flags a raw card-number/CVV/PAN-shaped field in Sarvex's own checkout/payment-config schema. **First run found 3 matches** (`CVC string` in `internal/pkg/ucp/types/{v20260408,v20260825}/payment.go`) — investigated rather than auto-fixed, per the stop-and-report instinct these invariants demand: those packages pin the external ucp.dev wire spec (CLAUDE.md §3) and must model the full `CardCredential` shape to represent what a handler's raw-card path can carry on the wire; the invariant is actually enforced one layer up, in `internal/planes/serving/api/checkout_contract.go`'s `OpaquePaymentCredential`, whose `UnmarshalJSON` deliberately extracts only `handler_id`+`token` and silently drops any `CardCredential`-shaped payload. Confirmed via `grep` (no persistence, logging, or Mongo write touches `.Card`/`.CVC` anywhere) — **no violation**, and the rule is now scoped away from `internal/pkg/ucp/types/**` to avoid re-flagging this legitimate spec mirror. Both rules validated against the full tree: 0 findings post-fix.
  - Still open: a `depguard`-style rule for plane-import isolation isn't needed as a *new* rule — `depguard` in `.golangci.yml` already covers it (see comment there); a Semgrep rule for it would be redundant.
- **`console-ci.yml`**: added an `sca` job (`npm audit --audit-level=high`), `continue-on-error: true` until the first backlog is triaged.
- **`dependabot.yml`** (sarvex.cp): added `npm` for `web/console` and `github-actions`, alongside the existing `gomod`.
- **website repo**: added `.github/dependabot.yml` (npm for both the root `pnpm` tree and the separately-installed `docs/` subsite), `.github/workflows/security.yml` (`pnpm audit` + `npm audit` for `docs/` + `gitleaks`), and baseline response headers in `nuxt.config.ts` (`X-Content-Type-Options`, `X-Frame-Options`, `Content-Security-Policy: frame-ancestors 'none'`, `Referrer-Policy`, `Permissions-Policy`, HSTS) addressing WEB-05.

**Still open (not done in this pass — needs its own follow-up):**
| Layer | Tool | Why it's separate |
|---|---|---|
| PHP (woocommerce, magento) | PHPStan/Psalm + `roave/security-advisories`/`composer audit` | WooCommerce plugin has no `composer.json` (Magento does) — needs its dependency model confirmed first (open question in §7), and neither repo's CI currently touches PHP at all, so this is new pipeline plumbing, not an addition to an existing one |
| IaC | `kubeconform`/`checkov` against `deployments/selfhost` (Helm charts, Cloudflare Terraform) | Not run in CI yet |
| DAST | OWASP ZAP baseline against staging console+control-API; manual/Burp testing for the §4 IDOR/tenancy cases | Needs the staging environment question in §7 answered first — no environment to point a scanner at yet |
| Full CSP (`script-src`/`style-src`/etc.) on the website | — | The `frame-ancestors` directive shipped here is safe to add blind (it only restricts being framed); a full CSP risks breaking Turnstile's script+iframe, Nuxt's inline hydration/JSON-LD, or a Cloudflare-injected analytics beacon, and needs verifying against a live preview deploy rather than guessed |
| Container/deploy image scanning (`trivy`) | — | Didn't confirm whether `deploy.yml` builds images in this pass |

Applicability judgment for "if applicable" in the ask: **yes, broadly applicable**, and the bulk of it is now wired up — the codebase is a JWT/HMAC-authenticated multi-tenant Go backend plus a Vue SPA plus PHP plugins reachable from merchant infrastructure; every layer benefits from at least SAST+SCA. The tenancy/IDOR class in §4 remains the one place tooling has limited reach — the two Semgrep rules above catch the mechanical version of it, but the actual cross-merchant IDOR cases (API-01) still need human-driven authenticated testing, not generic tooling.

---

## 6. Sequencing

1. Agree this plan and the case catalog (this document).
2. Wire up SAST/SCA/secret-scanning (§5) — fast, low-risk, catches low-hanging fruit before manual effort.
3. Triage SAST/SCA output; fix or explicitly accept each finding.
4. Run the manual/DAST pentest case catalog (§4) against staging, prioritizing API-01/API-04/API-05 (tenant isolation, sim-resolver gating, HMAC replay) and SYNC-01 (stored XSS via catalog) as highest-severity-if-true.
5. Write up findings with severity, reproduction steps, and fix recommendation; re-test after fixes.

## 7. Open questions to confirm before executing

- Is there a staging environment with test Stripe keys and a sandboxed Kinde tenant available for dynamic testing, or does one need to be stood up first?
- Does `X-Sarvex-Timestamp` get a freshness check anywhere outside `hmac.go` (worth confirming before treating API-05 as a live finding rather than a documentation gap)?
- What's the WooCommerce plugin's dependency/build model (no `composer.json` was found next to `plugin/woocommerce`, unlike Magento) — confirms which PHP SCA tool applies.
- Who is the intended executor of the manual/DAST phase — internal team, or does this need external pentest engagement sign-off/authorization paperwork given it touches a live checkout path (even against simulators)?
