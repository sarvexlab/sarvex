# Sarvex docs site

The documentation site, built with [Blume](https://useblume.dev) (the same
framework as the website's docs). It is **built and viewed locally** — nothing
here is deployed from this repository.

```bash
make docs           # live-reload dev server (http://localhost:4321)
make docs-build     # static HTML into web/docs/dist
make docs-preview   # serve the built site
make docs-check     # Blume doctor + lint the OpenAPI and AsyncAPI contracts
```

Requires Node >= 22.12.

## What is where

| Section | Source |
| --- | --- |
| Getting Started, Using the Dashboard, Integration Guides | Hand-written MDX in `content/` (merchant-facing; a copy of the website's docs, kept independently). |
| API Reference → Control-plane API, Events | **Generated at build** from `sarvex-v1-control-plane.yaml` and `sarvex-v1-syncing-plane-async.yaml` at the repo root (`openapi` / `asyncapi` in `blume.config.ts`). Never copied. |
| API Reference → Conventions, Authentication, Connector ingest, UCP endpoints | Hand-written MDX in `content/api/`. |
| Developers, Contributing | Hand-written index pages plus pages **synced from the repo's own Markdown** (`docs/`, ADRs, `sarvex-v1-architecture.md`, plugin READMEs, `TESTING.md`, `CONTRIBUTING.md`, …) by `scripts/sync-docs.mjs`. |

`scripts/sync-docs.mjs` runs before every dev/build. The synced `.md` pages are
gitignored — edit the source file, not the copy. To publish another repo
Markdown file, add a row to the `pages` list in the script (and to the folder's
`meta.ts` if you want to order it).

The sidebar is driven by `meta.ts` files per folder; the header tabs are in
`navigation.tabs` in `blume.config.ts`. Theme tokens are in `theme.css`.
