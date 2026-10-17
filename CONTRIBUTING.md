# Contributing to Sarvex

Thank you for helping. This guide covers how to set up, what we check, and the few architectural rules that reviews enforce strictly.

By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md). Report security problems privately — see [SECURITY.md](SECURITY.md).

## Ways to contribute

- **Bugs**: open an issue with steps to reproduce, expected and actual behaviour, and versions. A failing test is the best bug report.
- **Features**: open an issue first for anything beyond a small change, so we can agree on shape before you invest time. Read the "Scope" notes in [`sarvex-v1-architecture.md`](sarvex-v1-architecture.md).
- **New spec versions, commerce platforms, agent surfaces**: these are designed to be *added*, not edited into. Follow [`docs/HOWTO-add-spec-version.md`](docs/HOWTO-add-spec-version.md), [`docs/HOWTO-add-platform.md`](docs/HOWTO-add-platform.md) and [`docs/HOWTO-add-surface.md`](docs/HOWTO-add-surface.md).
- **Docs, tests, examples**: always welcome. The docs site lives in `web/docs` (`make docs` to preview); the API reference is generated from the contract files, and developer/contributor pages are synced from the Markdown under `docs/` — edit the source file.

## Development setup

You need Go 1.26+, Node.js (current LTS), and Docker for the local stack.

```bash
git clone https://github.com/sarvexlabs/sarvex.cp && cd sarvex.cp
go build ./...
make test-unit                      # no external services
make dev-up                         # full local stack (Mongo, Redis, NATS, Vault, planes)
make console                        # console in mock mode on :5173
cd web/console && npm ci && npm run test:run && npm run typecheck
```

PHP plugins: `plugin/woocommerce` runs with PHPUnit 9 (`composer require --dev phpunit/phpunit:^9.5 && vendor/bin/phpunit`); the Magento module's pairing tests need a Magento install.

## The rules reviews enforce

These come from the architecture ([`CLAUDE.md`](CLAUDE.md) §2 has the full list). Breaking one blocks a merge.

1. **Three isolated planes.** `internal/planes/{serving,sync,control}` never import one another. They communicate over the event bus or by *read-only* access to a collection the other plane owns. `internal/archtest` and `.golangci.yml` enforce this.
2. **One writer per collection.** Every collection has exactly one writing component. Others read.
3. **Tenant isolation is query-level.** Every data-layer query filters by `merchant_id` (account data) or `store_id` (catalog/serving data), resolved server-side from the verified token or connector HMAC — never from a URL or query parameter. Read [`docs/TENANCY-PATTERNS.md`](docs/TENANCY-PATTERNS.md) before touching the data layer.
4. **The serving read path uses only the cache.** Checkout is the one exception, and only through the sync-plane connector.
5. **No credentials in checkout.** Payment data is an opaque reference plus a handler id. Never add a credential-bearing field.
6. **Authentication is external.** Services verify tokens; they never mint them or store passwords.
7. **Idempotency at every external edge.** Retries are the normal case.
8. **Every change works on every supported spec generation and backend platform.** A connector change is made to both `internal/platform/woocommerce` and `internal/platform/magento`; `TestCapabilityMatrix` must cover new capabilities.
9. **Contracts are authoritative.** The control-plane HTTP API is exactly `sarvex-v1-control-plane.yaml`; sync-plane events are exactly `sarvex-v1-syncing-plane-async.yaml`. Change the contract and the code together, and regenerate the console types (`cd web/console && npm run gen:api`).
10. **Bugs you find get fixed with a regression test**, in the same change.

## Before you open a pull request

```bash
gofmt -l . | grep -v node_modules     # must print nothing
go vet ./...
make test-unit
python3 scripts/license-headers.py --check
python3 scripts/check-licenses.py      # when you touched dependencies
cd web/console && npm run typecheck && npm run test:run     # when you touched the console
```

Golden files under `**/testdata/golden` freeze existing (platform, spec-version) output. Regenerate with `UPDATE_GOLDENS=1 go test ./...` only when the change is intended, and label the PR `spec-change/<version>` or `platform-change/<platform>` (see `.github/workflows/golden-guard.yml`).

## Style

- Follow the surrounding code's idioms, naming and comment density. Go is formatted with `gofmt`; linting is `golangci-lint` (`.golangci.yml`).
- New Go, TypeScript, Vue and PHP source files start with the SPDX header; `python3 scripts/license-headers.py` adds it.
- Keep PHP plugins thin: no scoring, mapping or protocol interpretation in PHP.
- Commit messages: a short imperative subject, then why the change is needed.

## Licensing of contributions

Sarvex is licensed under the [Apache License 2.0](LICENSE). Unless you state otherwise, contributions you submit are licensed under the same terms (Apache-2.0 §5). Do not copy code from sources whose licence is incompatible, and do not add dependencies under GPL/AGPL/SSPL/BUSL; `scripts/check-licenses.py` will fail on them.

## Review and merge

Maintainers (see [MAINTAINERS.md](MAINTAINERS.md)) review pull requests; CI must be green. Small, focused changes merge faster than large ones. See [GOVERNANCE.md](GOVERNANCE.md) for how decisions are made.
