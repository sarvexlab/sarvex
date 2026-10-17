# Changelog

All notable changes are recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); the project is pre-1.0 and does not yet follow semantic versioning strictly.

## [Unreleased]

## [0.1.0] - 2026-10-17

First public pre-release. Pre-1.0: interfaces and wire formats may change between minor versions.

### Capabilities

- **UCP hosting** — per-store `/.well-known/ucp` profile, catalog search/lookup and checkout sessions, served by the serving plane from a Redis cache (checkout calls the merchant backend live through the connector).
- **Two UCP spec generations** served side by side (`2026-04-08` and `2026-08-25`), each a self-contained module behind a registry.
- **Catalog readiness** — ingestion, attribute mapping onto canonical fields, scoring against a versioned rubric, and remediation tasks for the merchant.
- **Activation lifecycle** — `draft → connector_linked → profile_published → verified → active`, with synthetic endpoint probes and monitoring.
- **Conformance** — in-process and external UCP conformance runs against a live stack.
- **Commerce platforms** — WooCommerce and Magento 2 (plugins in `plugin/`, connectors in `internal/platform/`). The Shopify connector is frozen: kept compiling, not extended.
- **Console** — Vue 3 management console for merchants and operators; docs site in `web/docs`.
- **Deployment** — self-hosted Kubernetes (Helm, Argo CD, Cloudflare Terraform) backed by MongoDB, Redis, NATS JetStream and Vault; docker-compose for local development.

### Changes since the pre-release builds
#### Changed (breaking)

- The project is released as **Sarvex** under the Apache License 2.0 (Go module `github.com/sarvexlab/sarvex`, headers `X-Sarvex-*`, env vars `SARVEX_*`, WordPress options `sarvex_ucp_*`, PHP namespace `Sarvex\UCP`, `@sarvex/console`). Pre-release installs must be upgraded and re-paired together with the server.
- **Production hosting is self-hosted Kubernetes only** (`deployments/selfhost`: Helm, Argo CD, Cloudflare Terraform). The runtime now depends only on MongoDB, Redis, **NATS JetStream** (event bus) and **Vault** (Transit signing keys, KV v2 connector credentials). Configuration changes: `EVENT_BUS_MODE`/`EventBusMode` and `SECRET_MANAGEMENT_MODE` are gone (the backends are fixed), as are `GCP_PROJECT_ID`, `GCP_EMULATOR_HOST`, `GCP_KMS_*` and the Cloud Run, CDN and storage-bucket settings. Set `NATS_URL`, `VAULT_ADDR`, `VAULT_TOKEN` (empty in the cluster, where the Vault Agent Injector supplies one) and `VAULT_TRANSIT_KEY_NAME`.
- **Local development, tests and CI run those same services** from `deployments/docker-compose.yaml` (`make infra-up`; `make dev-up` also starts the planes). Nothing is emulated and there is no separate "dev" event bus. Local Vault is file-backed and provisions itself on every start.
- The container image is built on Alpine (the charts start the binary through a shell) and published to `ghcr.io/sarvexlab/sarvex` by `.github/workflows/image.yml`.

#### Removed

- Google Cloud hosting: `deployments/terraform`, `deployments/cloud-deploy`, `deployments/cloudbuild-infra.yaml`, `.cloudbuild`, the deploy and Terraform workflows, `internal/pkg/gcpkms`, `internal/pkg/gcpsecrets`, the Pub/Sub event bus, the Redis Streams event bus, and the floci-gcp emulator.
- The Google Merchant Center (`google_ucp`) agent surface: its control- and sync-plane modules, supplemental feed generation, the `feed_freshness` health probe, Merchant Center price-drift comparison, the `gmc` platform/connector values, GMC-only catalogue attributes (`brand`, `mpn`, `gender`, `age_group`, `condition`) and the console flow. The generic surface framework and the `direct_agent` surface remain.
- The one-time journey → activation migration command (`cmd/migrate-activation`).
- Bundled third-party agent skills, SQL seeds from an unrelated product, an unreferenced policy bundle, and the unused `contracts/user` package with its unlicensed `onqlave.core` dependency.

#### Fixed

- NATS JetStream event bus: the Watermill NATS backend names a stream after the raw topic, and NATS rejects dots in stream names, so every dotted topic (all of ours) failed to subscribe. Streams are now created with sanitized names.
- Test agent (`internal/pkg/ucp/agent`): `UpdateCheckout` sent the destination in a flat shape the serving plane ignores and never chose a shipping option, so checkouts could not complete against stores that require one (`checkout.complete` conformance check). It now sends the ucp.dev `methods[]` shape and selects the first offered option.
- The `make dev-up` next-steps skipped merchant creation, which billing gating now requires.
- WooCommerce plugin: the class autoloader joined the source directory and file name without a separator, so no plugin class could load. Its unit tests now run without a WordPress install.

#### Added

- `LICENSE` (Apache-2.0), `NOTICE`, `THIRD_PARTY_NOTICES.md`, SPDX headers on first-party sources, and `scripts/license-headers.py` / `scripts/check-licenses.py` checks.
- README, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, `GOVERNANCE.md`, `MAINTAINERS.md`, issue and pull request templates.
