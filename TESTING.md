# Sarvex V1 Backend — Testing Guide

This document describes how to run the comprehensive test suite covering all three planes and 7 key flows.

## Quick Start

```bash
# Run all tests (unit + integration + E2E)
make test

# Run only unit tests
make test-unit

# Run only end-to-end tests
make test-e2e

# Run only integration tests
make test-integration
```

## Test Suites

### 1. Unit Tests
**What:** Fast, isolated tests of individual modules (no external dependencies)

**Command:**
```bash
make test-unit
```

**Coverage:**
- ~174 test files across all packages
- Auth, caching, event bus, connectors, UCP negotiation/signature
- Account, store, connector, catalog, task, journey, operator, validation services

**Time:** ~30 seconds

---

### 2. Scenario Tests (E2E Cross-Plane)
**What:** End-to-end tests of all 7 critical flows using event capture system (no external services)

**Command:**
```bash
make test-scenarios
```

**Flows Covered:**
1. **Onboarding** — Pairing flow, first readiness score, tenant isolation (2 tests)
2. **Catalog Heartbeat** — Signal→score→cache rebuild (3 tests)
3. **Profile Discovery** — /.well-known/ucp serving with HMAC (3 tests)
4. **Per-Request Negotiation** — Capability intersection, extension pruning (5 tests)
5. **Checkout Session** — Create/update, payment transparency, order creation, async sync (6 tests)
6. **Remediation** — Apply/revert rules, audit trail, graceful degradation (5 tests)
7. **Monitoring Loop** — Semantic probes, consistency checks, alerts (5 tests)

**Total: 29 tests**  
**Time:** ~0.7 seconds  
**Build Tag:** `-tags=scenarios`

---

### 3. Integration Tests — Control Plane
**What:** HTTP-layer tests of control plane endpoints with real MongoDB

**Command:**
```bash
make test-integration-control
```

**Prerequisites:**
- MongoDB running (see MongoDB Setup below)

**Coverage:**
- Account management (4 tests)
- Authentication (60+ tests: bearer tokens, HMAC, operator roles)
- Catalog operations (5 tests)
- Connector lifecycle (15 tests: pairing, plugin endpoints, HMAC replay detection)
- Operator endpoints (5 tests)
- Store management (10 tests)
- Task operations (6 tests)

**Total: 127 tests**  
**Time:** ~0.8 seconds  
**Build Tag:** `-tags=integration`  
**Requires:** `MONGO_URI=mongodb://admin:password@localhost:27017`

---

### 4. Integration Tests — Sync Plane
**What:** Event processing tests of sync plane catalog storage with real MongoDB

**Command:**
```bash
make test-integration-sync
```

**Prerequisites:**
- MongoDB running (see MongoDB Setup below)

**Coverage:**
- Product CRUD operations (6 tests)
- Tenant isolation (2 tests)
- Idempotency & dedup (5 tests)
- Batch updates & concurrency (2 tests)

**Total: 13 tests**  
**Time:** ~0.7 seconds  
**Build Tag:** `-tags=integration`  
**Requires:** `MONGO_URI=mongodb://admin:password@localhost:27017`

---

### 5. All Integration Tests
**What:** Control plane + sync plane together

**Command:**
```bash
make test-integration
```

**Total: 140 tests**  
**Time:** ~1.6 seconds

---

### 6. All E2E Tests (Scenarios + Integration)
**What:** All end-to-end tests without unit tests

**Command:**
```bash
make test-e2e
```

**Total: 169 tests**  
**Time:** ~2.3 seconds

---

### 7. All Tests (Complete Suite)
**What:** Unit + scenario + integration tests

**Command:**
```bash
make test
```

**Total: 350+ tests**  
**Time:** ~35 seconds

---

### 8. UCP Conformance Tests
**What:** Drives a merchant from nothing to a live UCP store, then exercises every
capability the serving plane implements — plus its documented failure paths — as a
UCP shopping agent would. See `cmd/sarvex/README.md`.

**Command:**
```bash
make test-conformance
```

**Prerequisites:** MongoDB and Redis (same as the integration suites). The planes are
started in-process on OS-assigned ports, so nothing else needs to be running.

To drive an already-running stack instead (`make dev-up`):
```bash
go run ./cmd/sarvex conform run --target local
```

**Note:** This runs in CI. It is *not* the external ucp.dev spec-validation suite —
that lives outside this repo.

---

### 9. Multi-Tenant Isolation Tests
**What:** Negative tests proving tenant isolation holds — cross-tenant read, cross-tenant
event consumption, and ucpnego/negotiation-cache key collision — across a configurable
tenant/store matrix, not just a single pair. Config lives in `internal/testtenancy`
(deterministic merchant/store ids and fake bearer tokens, shared by every suite below).

**Scale (env-configurable, defaults shown):**
```bash
TENANCY_TEST_TENANTS=5              # min 5
TENANCY_TEST_STORES_PER_TENANT=3    # min 3 per merchant
```
Override either to run a larger matrix locally or in CI without a code change.

**Where the tests live (no separate command — each runs as part of its suite above):**
- `internal/testtenancy` — the config/matrix package itself (`make test-unit`)
- `internal/pkg/ucp/negotiation/cache_isolation_test.go` — ucpnego composite
  cache-key collision, including a forced store_id collision across two merchants
  (`make test-unit`)
- `internal/pkg/eventbus/tenant_isolation_test.go` — cross-tenant event consumption
  against the real in-process event bus (`make test-unit`)
- `internal/planes/control/integration/tenant_isolation_test.go`,
  `internal/planes/sync/integration/tenant_isolation_test.go`,
  `internal/planes/serving/integration/tenant_isolation_test.go` — cross-tenant read
  matrix per plane (`make test-integration`)
- `internal/planes/scenarios/tenant_isolation_matrix_test.go` — end-to-end HTTP
  matrix over onboarding/pairing (`make test-scenarios`)

---

## MongoDB Setup

### Option 1: Docker (Recommended)

Start MongoDB with default credentials:
```bash
docker run -d --name mongodb -p 27017:27017 \
  -e MONGO_INITDB_ROOT_USERNAME=admin \
  -e MONGO_INITDB_ROOT_PASSWORD=password \
  mongo:latest
```

### Option 2: Custom URI

```bash
MONGO_URI=mongodb://user:pass@host:27017 make test-integration
```

### Option 3: Docker Compose

```bash
cd deployments
docker-compose up -d mongodb
```

Then in another terminal:
```bash
make test-integration
```

### Cleanup

```bash
docker stop mongodb
docker rm mongodb
```

---

## Test Coverage Summary

| Layer | Test Type | Count | Time | Prereqs |
|-------|-----------|-------|------|---------|
| **Unit** | Package-level | ~174 | 30s | None |
| **Sync Plane** | Integration | 13 | 0.7s | MongoDB |
| **Control Plane** | Integration | 127 | 0.8s | MongoDB |
| **All Planes** | Scenario (E2E) | 29 | 0.7s | None |
| **Total** | | **350+** | **35s** | MongoDB (for integration) |

---

## Continuous Integration

In CI/CD pipelines:

```bash
# Fast feedback (no external deps)
make test-unit
make test-scenarios

# Full validation (with MongoDB)
docker-compose -f deployments/docker-compose.yaml up -d mongodb
make test-integration
make test-integration-control
make test-integration-sync

# Cleanup
docker-compose -f deployments/docker-compose.yaml down
```

---

## Debugging Tests

### Run a single test

```bash
# Unit test
go test ./internal/planes/control/api -run TestAuth_BearerEnforcement -v

# Scenario test
go test -tags=scenarios ./internal/planes/scenarios -run TestScenario_CheckoutSession_CreateAndUpdate -v

# Integration test
MONGO_URI=mongodb://admin:password@localhost:27017 go test -tags=integration ./internal/planes/control/integration -run TestCatalog_InsertRetrieveProduct -v
```

### Verbose output

All Make test targets accept `-v` flag. Add to Makefile target:
```bash
make test-unit -- -v
```

### Keep MongoDB between test runs

Don't stop MongoDB between tests. The integration tests create/drop their own test databases.

---

## Test Status

**Current Status:** ✅ All tests passing

- Unit tests: ✅ 174+ passing
- Scenario tests: ✅ 29/29 passing (all 7 flows)
- Control-plane integration: ✅ 127/127 passing
- Sync-plane integration: ✅ 13/13 passing

**Ready for:** Sandbox testing (external UCP validation + real merchant stores)

---

## Architecture Verified by Tests

✅ **Three-plane isolation** — Serving, Sync, Control planes verified independently and in cross-plane scenarios  
✅ **Tenant isolation** — merchant_id and store_id scoping enforced at every layer  
✅ **Idempotency** — Dedup, sequence tracking, exactly-once semantics all verified  
✅ **Graceful degradation** — Service failures handled without halting processing  
✅ **Authentication & authorization** — Bearer tokens, HMAC, operator roles all tested  
✅ **Data persistence** — Real MongoDB storage tested end-to-end  

---

## Known Limitations

- Scenario tests use in-memory event capture; real NATS/Mongo/Vault behavior is validated separately in the integration and conformance tests
- Integration tests create isolated test databases; never affects production
- Sync-plane tests focus on MongoDB storage; event handler implementation tested via scenarios

---

## For More Information

- Architecture: `sarvex-v1-backend-architecture.md`
- Sequences: `sarvex-v1-sequence-diagrams.md`
- Build order: `CLAUDE.md` §5
- Phase status: `.claude/projects/*/memory/phase_*.md`
