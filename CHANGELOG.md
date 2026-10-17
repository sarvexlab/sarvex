# Changelog

All notable changes are recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); the project is pre-1.0 and does not yet follow semantic versioning strictly.

## [Unreleased]

### Changed (breaking)

- The project is renamed back to **Sarvex** (from Conductor); every identifier below that was renamed to `conductor`/`Conductor`/`CONDUCTOR` is now `sarvex`/`Sarvex`/`SARVEX` again (headers `X-Sarvex-*`, WordPress options `sarvex_ucp_*`, PHP namespaces `Sarvex\UCP`, env vars `SARVEX_*`, `@sarvex/console`, Go module `github.com/sarvexlab/sarvex`, `cmd/sarvex`, Helm charts and file names). Existing installs must be upgraded and re-paired together with the server.
- The project is now **Conductor** (formerly Sarvex) and is released under the Apache License 2.0.
- Wire and configuration identifiers were renamed. Existing plugin installs must be upgraded and re-paired together with the server:
  - HTTP headers `X-Sarvex-*` → `X-Conductor-*`
  - WordPress options `sarvex_ucp_*` → `conductor_ucp_*`; PHP namespaces `Sarvex\UCP` / `Sarvex\Ucp` → `Conductor\UCP` / `Conductor\Ucp`
  - Environment variables `SARVEX_*` → `CONDUCTOR_*`; npm package `@sarvex/console` → `@conductor/console`; Composer package `sarvex/module-ucp-gateway` → `conductor/module-ucp-gateway`
  - The task owner `sarvex` is now `conductor`, and the former `google` owner is `external`
  - The checkout surface value `google_ucp_checkout` is now `ucp_checkout`
- Production hostnames in configuration and docs are placeholders (`conductor.example.com`).

- **Production hosting is self-hosted Kubernetes only** (`deployments/selfhost`: Helm, Argo CD, Cloudflare Terraform). The runtime now depends only on MongoDB, Redis, **NATS JetStream** (event bus) and **Vault** (Transit signing keys, KV v2 connector credentials). Configuration changes: `EVENT_BUS_MODE`/`EventBusMode` and `SECRET_MANAGEMENT_MODE` are gone (the backends are fixed), as are `GCP_PROJECT_ID`, `GCP_EMULATOR_HOST`, `GCP_KMS_*` and the Cloud Run, CDN and storage-bucket settings. Set `NATS_URL`, `VAULT_ADDR`, `VAULT_TOKEN` (empty in the cluster, where the Vault Agent Injector supplies one) and `VAULT_TRANSIT_KEY_NAME`.
- **Local development, tests and CI run those same services** from `deployments/docker-compose.yaml` (`make infra-up`; `make dev-up` also starts the planes). Nothing is emulated and there is no separate "dev" event bus. Local Vault is file-backed and provisions itself on every start.
- The container image is built on Alpine (the charts start the binary through a shell) and published to `ghcr.io/conductorlabs/conductor.cp` by `.github/workflows/image.yml`.

### Removed

- Google Cloud hosting: `deployments/terraform`, `deployments/cloud-deploy`, `deployments/cloudbuild-infra.yaml`, `.cloudbuild`, the deploy and Terraform workflows, `internal/pkg/gcpkms`, `internal/pkg/gcpsecrets`, the Pub/Sub event bus, the Redis Streams event bus, and the floci-gcp emulator.
- The Google Merchant Center (`google_ucp`) agent surface: its control- and sync-plane modules, supplemental feed generation, the `feed_freshness` health probe, Merchant Center price-drift comparison, the `gmc` platform/connector values, GMC-only catalogue attributes (`brand`, `mpn`, `gender`, `age_group`, `condition`) and the console flow. The generic surface framework and the `direct_agent` surface remain.
- The one-time journey → activation migration command (`cmd/migrate-activation`).
- Bundled third-party agent skills, SQL seeds from an unrelated product, an unreferenced policy bundle, and the unused `contracts/user` package with its unlicensed `onqlave.core` dependency.

### Fixed

- NATS JetStream event bus: the Watermill NATS backend names a stream after the raw topic, and NATS rejects dots in stream names, so every dotted topic (all of ours) failed to subscribe. Streams are now created with sanitized names.
- Test agent (`internal/pkg/ucp/agent`): `UpdateCheckout` sent the destination in a flat shape the serving plane ignores and never chose a shipping option, so checkouts could not complete against stores that require one (`checkout.complete` conformance check). It now sends the ucp.dev `methods[]` shape and selects the first offered option.
- The `make dev-up` next-steps skipped merchant creation, which billing gating now requires.
- WooCommerce plugin: the class autoloader joined the source directory and file name without a separator, so no plugin class could load. Its unit tests now run without a WordPress install.

### Added

- `LICENSE` (Apache-2.0), `NOTICE`, `THIRD_PARTY_NOTICES.md`, SPDX headers on first-party sources, and `scripts/license-headers.py` / `scripts/check-licenses.py` checks.
- README, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, `GOVERNANCE.md`, `MAINTAINERS.md`, issue and pull request templates.
