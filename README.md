# Sarvex

[![License: Apache 2.0](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26-00ADD8.svg)](go.mod)
[![CI](https://github.com/sarvexlab/sarvex/actions/workflows/go-test.yml/badge.svg?branch=main)](https://github.com/sarvexlab/sarvex/actions/workflows/go-test.yml)

**Sarvex is an open-source gateway that makes an online store transactable by AI shopping agents over the [Universal Commerce Protocol (UCP)](https://github.com/Universal-Commerce-Protocol/ucp).**

<!-- TODO(launch): replace with the final asciinema recording (see docs/demo/README.md) -->
> _Demo recording coming soon — see [`docs/demo`](docs/demo) to run it yourself._

A merchant on WooCommerce or Magento installs a thin plugin. Sarvex then hosts the store's UCP profile and checkout endpoints, keeps its catalog agent-ready (mapping, scoring, remediation tasks), and monitors the integration — so the merchant never has to understand the protocol.

## What it does

- **Hosts the UCP surface for a store** — `/.well-known/ucp`, catalog search/lookup, and checkout sessions, conforming to the published UCP specification (two spec generations are served today, side by side).
- **Keeps the catalog agent-ready** — pulls the catalog from the store, maps raw attributes onto canonical fields, scores readiness against a versioned rubric, and raises tasks for what the merchant must fix.
- **Brokers checkout safely** — stock, shipping, tax and order placement go to the merchant's backend live, through a connector. Sarvex never sees raw payment credentials: the checkout schema has no field that could hold one.
- **Verifies and monitors** — a per-store activation lifecycle (`draft → connector_linked → profile_published → verified → active`), synthetic endpoint probes, conformance runs, and a console for merchants and operators.
- **Is extensible by registration, not by editing** — new UCP spec generations, commerce platforms and agent surfaces each plug in behind a registry. See the `docs/HOWTO-add-*.md` guides.

Supported commerce backends: **WooCommerce** and **Magento 2**. (The Shopify connector exists but is frozen: it is kept compiling, not extended.)

## Architecture

One Go binary runs three isolated planes, selected with `--service` (or all in one process with `--service=all`):

| Plane | Role | Talks to |
|---|---|---|
| **serving** (`internal/planes/serving`) | Always-on, agent-facing: profile, catalog reads, checkout. Reads only the Redis serving cache — except checkout, which calls the merchant backend live through the connector. | Redis, sync-plane connector |
| **sync** (`internal/planes/sync`) | Event-driven: connector signals, ingestion → mapping → scoring → catalog store, order intents, surface artifacts. The one writer of catalog/serving data. | MongoDB, Redis, event bus, merchant store APIs |
| **control** (`internal/planes/control`) | Interactive: the console REST API + WebSocket, accounts, stores, tasks, journey, validation, monitoring, billing. | MongoDB, event bus |

Planes never import each other; they communicate over the event bus or by read-only access to each other's MongoDB collections. Tenancy is fenced at the query level (`merchant → store → connector`). The full picture lives in [`sarvex-v1-architecture.md`](sarvex-v1-architecture.md), cross-plane flows in [`sarvex-v1-sequence.md`](sarvex-v1-sequence.md), and the HTTP/event contracts in [`sarvex-v1-control-plane.yaml`](sarvex-v1-control-plane.yaml) and [`sarvex-v1-syncing-plane-async.yaml`](sarvex-v1-syncing-plane-async.yaml).

```
plugin/ (WooCommerce, Magento)   thin signal sender + /.well-known/ucp doorway
        │ signals (HMAC)                        ▲ profile
        ▼                                       │
   sync plane ──events──▶ control plane ◀── console (web/console)
        │  writes                               
        ▼                                       
  Redis serving cache ──▶ serving plane ◀── AI shopping agents (UCP)
```

## Repository layout

```
main.go, cmd/root.go the single server binary; --service selects the plane
cmd/sarvex/       CLI: onboard merchants, play the shopping-agent role, run conformance
cmd/sim/             persistent fake WooCommerce store for local dev
cmd/                 also gateway-cli, scaffold, verify-routes (tooling)
internal/planes/     serving, sync, control
internal/spec/       one self-contained module per UCP spec generation
internal/platform/   one module per commerce backend (woocommerce, magento, frozen/*)
internal/pkg/        shared machinery: event bus, Vault secrets/keys, signing, negotiation, catalogue
web/console/         Vue 3 + TypeScript management console
plugin/              WooCommerce (PHP) and Magento (PHP) plugins
deployments/         Helm/GitOps for Kubernetes (production), docker-compose for local dev
docs/                ADRs, how-to guides, test and security plans
```

## Quick start (local)

Requirements: Go 1.26+, Docker (MongoDB, Redis, NATS JetStream and Vault run in containers — the same services production uses), Node.js (current LTS) for the console.

```bash
make infra-up      # just the backing services: Mongo, Redis, NATS JetStream, Vault
make dev-up        # the backing services, a fake WooCommerce store, and the three planes
make dev-status
make dev-token     # mint a dev bearer token for curl
make dev-down

make console       # console in mock mode (no backend needed) on http://localhost:5173
make console-real  # console against the local control plane (run make dev-up first)
```

Onboard a fictitious merchant end to end and have an agent buy from it:

```bash
go run ./cmd/sarvex onboard merchant "Acme Widgets"
go run ./cmd/sarvex agent checkout run --store <id> --sku <sku>
```

See [`cmd/sarvex/README.md`](cmd/sarvex/README.md).

## Plugins

| Platform | Path | Guide |
|---|---|---|
| WooCommerce | [`plugin/woocommerce`](plugin/woocommerce) | [README](plugin/woocommerce/README.md) |
| Magento 2 | [`plugin/magento`](plugin/magento) | [README](plugin/magento/README.md) |

The plugins contain no business logic. They signal *that* something changed (ids and a sequence number, signed with an HMAC) and serve Sarvex's profile bytes from the merchant's own domain at `/.well-known/ucp`. Pairing derives the shared HMAC secret.

> The plugins are Apache-2.0 like the rest of the project. The WordPress.org plugin directory only accepts GPL-compatible licences, so the WooCommerce plugin is installed from this repository, not from that directory.

## Testing

```bash
make test-unit              # fast, no external services
make test-scenarios         # seven cross-plane platform flows
make test-integration       # needs MongoDB + Redis (docker run commands in `make help`)
make test-e2e-journey       # WooCommerce → agent end to end
make test-conformance       # external UCP conformance suite
cd web/console && npm run test:run && npm run typecheck
```

See [`TESTING.md`](TESTING.md) for details.

## Configuration and deployment

- Authentication is delegated to an external OIDC provider (Kinde); Sarvex only verifies tokens. Connector and plugin traffic uses HMAC instead.
- `configs/` holds per-service defaults; secrets come from the environment or Vault — never commit them.
- Production is self-hosted Kubernetes: Helm charts and GitOps manifests in [`deployments/selfhost`](deployments/selfhost), backed by MongoDB, Redis, NATS JetStream and Vault. Local development runs the same services via [`deployments/docker-compose.yaml`](deployments/docker-compose.yaml) — see [`deployments/README.md`](deployments/README.md).

The hostnames in configuration and docs (`sarvex.example.com`) are placeholders; substitute your own.

## Documentation

Browse everything below — plus merchant guides and the generated API reference — as a site: `make docs` (live) or `make docs-build` (static, `web/docs/dist`). See [`web/docs/README.md`](web/docs/README.md).

| | |
|---|---|
| [Architecture](sarvex-v1-architecture.md) · [Sequences](sarvex-v1-sequence.md) | How the planes fit together and who triggers what |
| [Tenancy patterns](docs/TENANCY-PATTERNS.md) · [ADRs](docs/adr) | Isolation rules and recorded decisions |
| [Add a spec version](docs/HOWTO-add-spec-version.md) · [platform](docs/HOWTO-add-platform.md) · [surface](docs/HOWTO-add-surface.md) | Extending Sarvex |
| [Idempotency](docs/IDEMPOTENCY-DESIGN.md) · [SLO alerting](docs/SLO-ALERTING.md) · [Security test plan](docs/SECURITY-TEST-PLAN.md) | Operations and security |

## Contributing

Contributions are welcome. Read [`CONTRIBUTING.md`](CONTRIBUTING.md) first — in particular the plane-boundary and tenant-isolation rules, which reviews enforce strictly — and our [Code of Conduct](CODE_OF_CONDUCT.md).

## Security

Please do not open public issues for vulnerabilities. See [`SECURITY.md`](SECURITY.md) for how to report them privately.

## Project status and governance

Sarvex is pre-1.0: interfaces and wire formats may change, and the WooCommerce/Magento pairing protocol was renamed from its pre-release form. See [`CHANGELOG.md`](CHANGELOG.md), [`GOVERNANCE.md`](GOVERNANCE.md) and [`MAINTAINERS.md`](MAINTAINERS.md).

## Licence

Apache License 2.0 — see [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE). Third-party components and their licences are listed in [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md). Source files carry an `SPDX-License-Identifier: Apache-2.0` header (`scripts/license-headers.py --check`).

"Sarvex" and the Sarvex logo are marks of Sarvex Labs; the licence does not grant trademark rights. UCP is an open specification developed independently of this project; Google, WooCommerce, Magento, Kinde and other names are the property of their owners and are used only to describe compatibility.
