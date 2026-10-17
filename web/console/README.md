# Sarvex Console

Vue 3 + Vite + TypeScript SPA. Built to the spec in this PR description.

## Scripts

```
npm ci              # or npm install
npm run dev            # vite dev server on :5173
npm run build          # type-check + production build
npm run typecheck      # vue-tsc --noEmit
npm run test           # vitest in watch mode
npm run test:run       # vitest one-shot (what CI runs)
```

From the repo root, `make console` does the install + `dev` above (mock
mode). `make console-real` instead points the console at the local dev
stack's real control plane — see "Dual-mode mocks / real backend" below.

## Tests

[Vitest](https://vitest.dev/) + `@vue/test-utils`, jsdom env. Tests live next
to the file they cover (`foo.ts` ↔ `foo.test.ts`) and target the bug-prone
bits: the route-guard gate (both pure-logic + integration), Pinia store
reactivity, the dual-mode adapter routing, auth simulator claims, the
shared visual primitives, and the `ConnectSurface` polling flow that
broke us twice. The intent is to pin past regressions, not chase coverage.

Coverage is measured by `@vitest/coverage-v8`. The config scopes the
report to modules we directly unit-test; the included surface currently
hits ~95% lines, ~90% branches, ~78% functions. Thresholds in
`vitest.config.ts` set the floor — bump them up as new tests land.

```
npm run test:coverage    # run + assert against the floor; HTML in ./coverage
```

CI (`.github/workflows/console-ci.yml`) runs on every PR touching
`web/console/**` or the OpenAPI: regenerates `src/api/schema.d.ts` and
fails if it drifts from `sarvex-v1-control-plane.yaml`, then
`npm run build` (typecheck + vite build) and `npm run test:coverage`
(tests + coverage gate). The HTML report is uploaded as a CI artifact.

## Layout

- `src/styles/tokens.css` — every colour/typography/radius token. Components
  read `var(--…)` only; do not introduce hex outside this file. Adding a theme
  is one more `[data-theme="<name>"]` block.
- `src/stores/theme.ts` — `paper` / `midnight` switch, persisted, honours
  `prefers-color-scheme` on first load.
- `src/router/index.ts` — routes + the `NAV` array driving the left nav.
- `src/components/shell/AppShell.vue` — left nav + topbar (store switcher,
  theme toggle, user menu) wrapping every authed surface.
- `src/views/*Surface.vue` — one per surface from the spec; currently
  placeholders until the API client and shared components land.

## API client

Generated from `../../sarvex-v1-control-plane.yaml` via
[`openapi-typescript`](https://openapi-ts.dev/) into `src/api/schema.d.ts`.
Regenerate after the OpenAPI changes:

```
npm run gen:api
```

The generated file is committed so a clean `npm install && npm run build`
works without first running the generator. CI should run `gen:api` and fail
if the diff is non-empty.

`src/api/client.ts` wraps Axios with a Bearer interceptor that reads the token
from the auth store at send time. `src/api/auth.ts` provides typed wrappers
(currently just `GET /auth/me`).

## Auth

Authentication is external (see sarvex-v1-architecture.md A10.4). The console verifies
nothing itself — it just attaches the IdP's JWT and reads the principal
from `GET /auth/me`. Two providers, env-switched:

| Mode | Set | Behaviour |
|---|---|---|
| **Simulator** (default) | `VITE_AUTH_MODE` unset or `simulator` | Mints a JWT-shaped token locally; `/login` exposes a role picker so the operator flow is reachable without a second real account. Zero config needed. |
| **Kinde** | `VITE_AUTH_MODE=kinde` + `VITE_KINDE_DOMAIN` + `VITE_KINDE_CLIENT_ID` | OAuth 2.0 Authorization Code + PKCE via the Kinde SDK. `signIn()` redirects to the Kinde-hosted page; the SDK exchanges the code for tokens on the return trip. Token refresh handled by the SDK (re-pulled on a 10-minute tick into the cached value our Axios interceptor reads). Role picker hides; the role comes from the user's JWT claims via `/auth/me`. |

- `src/auth/provider.ts` — `AuthProvider` interface (signIn / signOut /
  onToken) + the env switch.
- `src/auth/simulator.ts` — local-dev provider.
- `src/auth/kinde.ts` — real Kinde provider. Lazy-imported so simulator
  builds don't carry the SDK. Uses `createKindeClient` which handles
  the Auth Code + PKCE redirect; the resolved client is cached as a
  singleton.
- `src/stores/auth.ts` — Pinia store: token, principal, status. The
  router guard awaits its first resolution before routing.

When using Kinde: copy the values from your Kinde dashboard
(Settings → Applications → your SPA) into `.env.local`:

```
VITE_AUTH_MODE=kinde
VITE_KINDE_DOMAIN=https://your-tenant.kinde.com
VITE_KINDE_CLIENT_ID=...
VITE_KINDE_AUDIENCE=https://api.sarvex.example.com   # if the backend validates aud
```

Add `http://localhost:5173` (and any deploy origins) under
**Settings → Applications → your SPA → Allowed callback URLs** *and*
**Allowed logout redirect URLs**.

**Passkeys:** enable the **Passkey / WebAuthn** connection in your Kinde
tenant (**Authentication → Passwordless / Passkey**). It appears on
the hosted sign-in page automatically alongside any other configured
methods — no client code change needed. The hosted page is where users
register and use their passkeys; the SDK never sees the WebAuthn
credentials itself.

The backend verifies the Kinde access token via its JWKS endpoint
(`{VITE_KINDE_DOMAIN}/.well-known/jwks`) and reads `merchant_id` / `role`
/ `store_ids` from the custom claims you configure on the API in Kinde.

## Dual-mode mocks / real backend

`src/api/mocks.ts` installs an Axios adapter that routes each request to
either the in-process mock layer or the real backend, per env config. Always
installed — the per-request decision is what matters.

**Two configurations:**

1. **Mock-by-default** (default — `VITE_USE_MOCKS=1`).
   - Every route hits a mock unless its path prefix is in `VITE_REAL_ROUTES`.
   - Use this when the backend is mostly unavailable but you want to
     exercise a few real endpoints — e.g.
     `VITE_REAL_ROUTES=/auth/me,/journey`.

2. **Real-by-default** (`VITE_USE_MOCKS=0`).
   - Every route hits the real backend unless its path prefix is in
     `VITE_MOCK_ROUTES`.
   - Use this when the backend is mostly up but a few endpoints aren't
     ready yet — e.g. `VITE_MOCK_ROUTES=/operator,/tasks`.

Real-backend requests go to `VITE_API_BASE` (default `/api`). For
same-origin dev set `VITE_API_PROXY_TARGET=http://localhost:8081` (the
control plane's port in `scripts/dev/lib.sh`) and Vite will forward `/api` →
`<target>/v1` there.

`make console-real` does exactly this against the local dev stack: run
`make dev-up` first (starts Mongo/Redis, the control plane, and the sync
plane in dev mode, `KINDE_ISSUER_URL` unset), then `make console-real` to
serve the console with `VITE_USE_MOCKS=0` and the proxy pointed at
`:8081`. The console's simulator auth provider (default, no Kinde config
needed) mints unsigned JWT-shaped tokens that the control plane's
dev-mode `SimBearerResolver` accepts as-is — real API calls, no IdP
required.

Set `VITE_API_DEBUG=1` to log every request's resolved mode + path to the
browser console.

The mock layer also falls through to the real backend on any unmocked route
when in mock-by-default, so a gap in fixtures doesn't hard-404 — it just
attempts the real call.

## Deploying (Cloudflare Worker, `console.sarvex.example.com`)

The console is a static SPA, deployed as a Cloudflare Worker (assets-only,
no Worker script) via `wrangler.jsonc`. The Worker itself only serves the
built `dist/` files — it never proxies API calls; the browser talks to the
control plane directly at `VITE_API_BASE`.

**Deploys are driven by Cloudflare Workers Builds (Git integration), not
by anyone running `wrangler` locally or from this repo's CI.** Cloudflare
watches `main` on `sarvexlab/sarvex` and builds/deploys on every
push — nothing here needs a `CLOUDFLARE_API_TOKEN` or `wrangler login`.

### One-time setup in the Cloudflare dashboard

1. **Workers & Pages → Create → Workers → Import a repository** (or, on an
   existing Worker, **Settings → Builds → Connect** — this is the "connect
   to Git" flow you already use for the other CF-deployed app). Pick
   `sarvexlab/sarvex`, branch `main`.
2. **Build configuration:**
   - Root directory: `web/console`
   - Build command: `npm run build`
   - Deploy command: `npx wrangler deploy` (Workers Builds runs this after
     the build step; it just reads `wrangler.jsonc` from the root directory
     above)
3. **Build-time environment variables** (Settings → Variables — set these
   as *build* variables, since they're baked into the bundle at `vite
   build`, not read at request time):
   ```
   VITE_API_BASE=https://api.sarvex.example.com/v1
   VITE_USE_MOCKS=0
   VITE_AUTH_MODE=kinde
   VITE_KINDE_DOMAIN=https://<tenant>.kinde.com
   VITE_KINDE_CLIENT_ID=<id>
   VITE_KINDE_AUDIENCE=https://api.sarvex.example.com
   VITE_KINDE_REDIRECT_URI=https://console.sarvex.example.com
   VITE_KINDE_LOGOUT_URI=https://console.sarvex.example.com
   ```
4. **Custom domain:** the `routes` entry in `wrangler.jsonc` (`pattern:
   console.sarvex.example.com`, `custom_domain: true`) attaches automatically on the
   first successful deploy, provisioning the proxied DNS record itself —
   requires the `sarvex.example.com` zone to already exist on this Cloudflare
   account. If a conflicting `app` DNS record already exists, delete it
   first, or attach manually instead under **Settings → Domains & Routes →
   Add → Custom Domain**.
5. Also add `https://console.sarvex.example.com` to the Kinde application's **Allowed
   callback URLs** and **Allowed logout redirect URLs**.

From then on, every push to `main` that touches `web/console/**` (or
whatever path filter you set on the Worker's build config) triggers a new
build + deploy automatically — same model as the other app already wired
up this way.

`npm run deploy` / `deploy:dry-run` in `package.json` remain as a local
escape hatch (e.g. testing a build against a personal Cloudflare account
before merging) — they are not part of the normal release path.

## Build order

1. ✅ Shell + tokens + theme switch.
2. ✅ Generated API client, Axios + Bearer, `authStore` + `/login` (this step).
3. Shared components (`JourneyBar`, `TaskCard`, `OwnerChip`, `Chip`, `Field`).
4. ConnectSurface + PathSurface end-to-end.
5. StoreSurface + product detail.
6. SettingsSurface + HealthSurface.
7. BoardSurface + operator modals + enter-as-merchant + `OperatorBar`.
8. Deep-link-to-task, empty/loading/error states.
