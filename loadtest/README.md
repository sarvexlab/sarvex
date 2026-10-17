# Load testing (k6)

k6 load tests for the control plane and serving plane, against a local
`make dev-up` stack. See "Scope and constraints" below for what the scripts do and do not cover.

## Prerequisites

- [k6](https://k6.io/docs/get-started/installation/) installed and on `PATH`.
- The local dev stack running: `make dev-up` (Mongo + Redis + NATS + Vault +
  `cmd/sim serve` + control/sync/serving planes — see `scripts/dev/up.sh`).
- `jq` installed (used by the seed script to parse API responses).

Everything here targets `--env=local` deployments only. RFC 9421
request/response signing on the serving plane is disabled whenever `Env !=
""` (see `internal/planes/serving/api/dependencies.go`), which is every
`make dev-up` run — these scripts don't sign requests and won't work
against a prod-shaped deployment without that added first.

## Quick start

```sh
make dev-up
make loadtest-seed                    # provisions stores + catalog + pairing
make loadtest-serving                  # runs all serving-plane scenarios
make loadtest-control                  # runs all control-plane scenarios

# Or run one scenario at a time:
make loadtest-serving ARGS=smoke
make loadtest-control ARGS=console_read
```

`loadtest-seed` is safe to re-run but not idempotent — it always adds
`LOADTEST_STORES` *more* stores rather than reusing existing ones. Tune it:

```sh
LOADTEST_STORES=10 LOADTEST_PRODUCTS_PER_STORE=100 make loadtest-seed
```

It also seeds one extra "large" store (`LOADTEST_LARGE_STORE_PRODUCTS`,
default 5000 products, set to `0` to skip) used by
`serving/catalog_read.js` to observe how `catalog/search`'s cost scales with
catalog size — that endpoint is an unindexed `LRANGE` + in-Go scan over the
store's full product list per request (`internal/planes/serving/
cache_redis.go`), not something `limit`/`cursor` bounds.

## Layout

```
loadtest/
  control/       k6 scenarios against the control plane (:8081)
  serving/       k6 scenarios against the serving plane (:8083)
  lib/           shared JS helpers: auth.js, fixtures.js, config.js, manifest.js
  .manifest.json seed.sh's output (gitignored) — store_id/merchant_id/token per store
scripts/loadtest/
  seed.sh          provisions fixtures, writes .manifest.json
  run-control.sh   thin k6-run wrapper (what `make loadtest-control` calls)
  run-serving.sh   same, for serving
```

## What each scenario covers

**Control plane** (critical-path analysis in the PR description / commit
history — see `internal/planes/control/api/handler.go:SetupRoutes` for the
ground truth route list):

| Script | Covers |
|---|---|
| `smoke.js` | Correctness gate: account/stores/journey/tasks/readiness/products, 2 VUs/30s |
| `console_read.js` | Staged load (10→100 VUs) across the same Tier-1 read endpoints, plus health. The control plane has **no rate limiter** — this characterizes that unthrottled read cost. |
| `catalog_import.js` | The control→sync **fan-out bridge**: `resync`, `catalog/audits`, `remediations/apply` — none do the work themselves, they publish a command the sync plane consumes (`control.catalog.import`, `command.audit_requested`, `command.remediations_applied`). Deliberately rate-capped (2/s, 1/s, 0.5/s), not ramped to failure — an uncapped run here is a "can the console DoS the sync plane" test. |
| `websocket_journey.js` | Opens N `journey/stream` WebSocket connections and triggers `task.updated` broadcasts via `POST /tasks/:id/resolve`, to measure Hub fan-out. **Single control-plane instance only** — see caveat in the file header. |

**Serving plane** (`internal/planes/serving/api/*.go`):

| Script | Covers |
|---|---|
| `smoke.js` | Correctness gate: `.well-known/ucp`, `catalog/search`, `catalog/:itemId` — confirms the cache is actually populated before running load. |
| `catalog_read.js` | `baseline_mixed` (traffic spread across all seeded stores) + `single_merchant_ceiling` (all load on one store, to find the real rate-limit onset — see the bug note below). |
| `checkout_flow.js` | The money path: create → update → complete, live round-trips to the merchant backend (`cmd/sim serve` in dev) on every call. Request bodies mirror `internal/planes/sync/sim.Agent` exactly. Kept under ~4 minutes total so the 5-minute cache-freshness guard doesn't trip from test runtime alone (see file header for what a longer soak run needs). |
| `checkout_negative.js` | Correctness: idempotent replay, idempotency-key conflict (409), 404s on unknown sessions, and the double-`complete` outcome (see below). |

## A real bug found while building this

`internal/planes/serving/middleware.go`'s `WithRateLimiting` builds its
lookup key from `c.Path()`:

```go
endpoint := fmt.Sprintf("%s %s", c.Request().Method, c.Path())
```

But the rate map's keys are unprefixed (`"POST /checkout-sessions"`, `"GET
/catalog/search"`, ...), while Echo's `c.Path()` returns the **full
registered route pattern including the group prefix**
(`servingGroupName = "/ucp/:platform/:storeID"`) — e.g. `"POST
/ucp/:platform/:storeID/checkout-sessions"`. Every lookup misses the map and
falls through to the 100 req/s default fallback, so **checkout mutation
endpoints are not actually limited to the documented 50 req/s** — they get
100 like everything else. `catalog_read.js`'s `single_merchant_ceiling`
scenario is designed to make this observable (reads are nominally 100
anyway, so it won't be visible there — it would show up running the same
technique against `checkout_flow.js`'s create/update/complete endpoints).


## Scope and constraints

- **Execution** — everything here runs through the Makefile against a local
  `dev-up` stack.
- **Load numbers** — stages/thresholds are defaults set in each file's
  `options`.
- **RFC 9421 signing** — the scripts do not sign requests, so they only work against
  `--env=X` (non-prod) deployments where signing is disabled.
- **Checkout soak runs** — `checkout_flow.js` is capped under the
  5-minute freshness window; a longer run uses a concurrent keepalive
  (e.g. running `catalog_import.js`'s `resync` scenario alongside it, which
  triggers a cache rebuild and refreshes the watermark).
- **Double-`complete`** — `checkout_negative.js` logs the outcome instead of
  asserting one; `CheckoutService.Complete` does not enforce the request
  body's `idempotency_key` field (unlike `Create`).
