# `cmd`

Executables. The server itself is the repository-root `main.go` and `cmd/root.go`: one binary, with `--service` selecting the serving, sync or control plane. Everything else here is tooling.

| Directory | What it is |
|---|---|
| `sarvex/` | CLI that onboards merchants through the control plane's API and plays the shopping-agent role against the resulting stores. Also runs conformance. See [`sarvex/README.md`](sarvex/README.md). |
| `sim/` | Persistent fake WooCommerce store for local development (`make dev-up` starts it). |
| `scaffold/` | Adds a UCP spec generation or a commerce platform as a new module (`make new-spec-version`, `make new-platform`). See the `docs/HOWTO-add-*.md` guides. |
| `verify-routes/` | Checks that every control-plane route is documented in the OpenAPI spec (`make check-routes`). |

Keep `main` packages thin: logic that more than one command needs belongs under `internal/`.
