# `sarvex`

Drives Sarvex from the outside: onboards merchants through the control
plane's own API, and plays the UCP shopping-agent role against the stores
that result.

Those are two halves of one thing. A store is only really onboarded if an
agent can transact against it, and an agent has nothing to transact against
without a store — so `conform` does both, in that order.

## Onboarding

```bash
sarvex onboard merchant "Acme Widgets"
```

That is the whole interface. From the name, the CLI derives the account's
business facts, the store's display name and business profile, its
storefront URL, its support contacts — then creates the merchant, creates
the store, configures it, seeds a catalog, pairs a connector, runs the
catalog audit, resolves the remediation tasks, requests verification, and
waits for both lifecycles to land:

```
activation   draft → connector_linked → profile_published → verified → active
journey      prospect → connected → synced → ready → live
```

Both are waited on. They are independent — activation is the store's own UCP
readiness, journey is the merchant's engagement stage — and neither derives
from the other.

```bash
sarvex onboard merchant "Acme Widgets" --stores 2 --messy
sarvex onboard store "Acme Outlet" --merchant <id>
sarvex onboard status --merchant <id>
```

`--messy` seeds deliberate readiness problems, so the run exercises the
remediation path rather than routing around it.

### Determinism

Fixtures are seeded from a hash of the merchant name, so the same name
always builds the same fixture. Every run prints its seed; passing
`--seed <n>` rebuilds that exact run. Generated emails use RFC 2606's
`example.com` and phone numbers use the reserved `555-01xx` range, so a
fictitious account can never point at a real address or subscriber.

## Agent

Individual capabilities, for poking at a store by hand:

```bash
sarvex agent profile --store <id>
sarvex agent catalog search --store <id> --query lantern
sarvex agent catalog lookup --store <id> --item <sku>
sarvex agent checkout run --store <id> --sku <sku>
sarvex agent order track --store <id> --order <id>
```

These are thin wrappers over `internal/pkg/ucp/agent`, which is also what
`cmd/sim`'s agent-side commands use.

## Conformance

```bash
sarvex conform run                 # against a running stack (make dev-up)
sarvex conform run --in-process    # starts the planes itself
sarvex conform run --store <id>    # skip onboarding, drive an existing store
sarvex conform run -o junit > results.xml
```

Phase 1 onboards a fresh merchant and asserts it reaches active/live. Phase
2 drives the agent capability matrix against the store that produced.

Checked today: profile discovery, payment-handler advertisement, catalog
search, search pagination, catalog lookup, unknown-item lookup, checkout
create, empty-cart rejection, checkout update (including the spec's totals
invariant), checkout complete, idempotent complete replay, unknown-handler
rejection, out-of-stock refusal, and broken-attribute propagation.

Failure paths are checks, not an afterthought. Faults are injected through
the simulated store's own mutate/fix path — the same one `sim mutate` uses —
so anything the suite produces can be reproduced by hand.

### Two rules this suite is built around

**It only asserts against capabilities the serving plane implements.** The
suite this replaces carried 85 scenarios driving `cart.*`, `identity.*`,
`ap2.*` and `mcp.*` steps against endpoints that have never existed. They
could not pass, and their failures said nothing about whether Sarvex worked.
A test enforces the rule against the route table in
`internal/planes/serving/api/handler.go`.

**It cannot pass by skipping.** A run that executes zero checks fails,
however many skips it reports. Both replaced suites reported success while
asserting nothing — one printed `Ran 0 of 95 Specs — Suite skipped`, the
other passed in 40ms because a TCP guard fired. That is worse than failing,
because it keeps reporting success while the thing it guards rots.

## In CI

```bash
make test-conformance
```

runs `go test -tags=conformance ./cmd/sarvex/conformance`, which starts the
planes in-process on OS-assigned ports. It needs MongoDB, Redis, NATS and Vault (`make infra-up`; CI starts the
same compose file) and nothing else running.

Ports are OS-assigned rather than fixed. Fixed ports are how the previous
suite ended up aimed at the sync plane after the port layout changed.

## The official UCP conformance suite

`conform` above is Sarvex's own suite — useful, but not the authority on
protocol conformance. That authority is
[the official UCP conformance suite](https://github.com/Universal-Commerce-Protocol/conformance),
published by the UCP project itself. Rather than duplicate it, Sarvex wraps
it:

```bash
sarvex conform serve --in-process --output-dir /tmp/ucp-fixtures --ready-file /tmp/ucp-fixtures/ready
```

`serve` runs the same onboarding phase `run` does, then — instead of
driving Sarvex's own assertions — writes the fixture files the official
suite reads (`conformance_input-<version>.json` per `--ucp-versions`, one
shared `test_fixtures.json`, both derived from the store's actual live
catalog) and blocks until interrupted, so an external test runner can point
at it.

```bash
make test-conformance-official
```

drives that handshake end to end: `scripts/ucp-conformance/run.sh` clones
the official suite and its `python-sdk` dependency at pinned commits,
`uv sync`s them, starts `sarvex conform serve --in-process` in the
background, and runs `uv run pytest` against it once per UCP release
`internal/pkg/ucp/types` vendors (today: `2026-04-08`, `2026-08-25`). It
needs the same infrastructure `test-conformance` does, plus Python 3.10+
and [`uv`](https://docs.astral.sh/uv/).

Discount codes, known-customer addresses and dynamic fulfillment are left
unconfigured in the generated fixtures: V1 does not implement those
capabilities (CLAUDE.md §0), and the official suite skips the corresponding
tests honestly when a fixture is absent rather than failing — the wrapper does not work around that.

## Configuration

Flags override environment variables, which default to what
`scripts/dev/up.sh` exports — so against a `make dev-up` stack, no flags are
needed.

| Variable | Default |
|---|---|
| `CONTROL_PLANE_BASE_URL` | `http://localhost:8081/v1` |
| `SERVING_BASE_URL` | `http://localhost:8083/ucp` |
| `SYNC_PLANE_BASE_URL` | `http://localhost:8082/v1` |
| `SIM_STORE_BASE_URL` | `http://localhost:4106` |
| `MONGO_CONNECTION_STRING` | `mongodb://admin:password@localhost:27017/?directConnection=true` |
| `REDIS_ADDRESS` | `localhost:6379` |
| `NATS_URL` | `nats://localhost:4222` |
| `VAULT_ADDR` | `http://localhost:8200` |
| `VAULT_TOKEN` | `sarvex-dev-token` |

Authentication is a dev bearer token minted in-process, accepted only when
the control plane runs with `KINDE_ISSUER_URL` unset. This is a development
and CI tool; it cannot authenticate against a real deployment.

## Relationship to `cmd/sim`

`cmd/sim` simulates a *merchant's* store for Sarvex to consume from.
`sarvex` drives *Sarvex's own* surfaces from the outside. They meet at the
simulated store: `sarvex onboard` seeds and mutates it through the same
`sim.CLI` that `sim seed` and `sim mutate` use, so there is exactly one way
a simulated catalog comes into being.
