# Console redesign + friction reduction — audit & plan

Scope: `web/console` (Vue 3 + TypeScript + Vite + Pinia + Vue Router + axios). No backend/contract changes.

## 0. Framework correction (read this first)

The originating brief says "Stay on Vuetify 3. No new UI libraries." **The console has no Vuetify anywhere** — it's a hand-rolled design system (`src/styles/tokens.css` + `src/components/ui/*`), already themeable (`paper`/`midnight`), already with a styleguide route (`/_/styleguide`). Introducing Vuetify now would itself be "a new UI library" and would throw away a working, tested system.

Decision (confirmed with the requester): **keep the existing custom system.** "No new UI libraries" is the binding constraint; "Vuetify 3" is treated as an error in the brief. Phase 1 below evolves the current tokens/components to match the target design language and component list, without adding a dependency.

## 1. Route / flow map (current state)

Router: `src/router/index.ts`. Nav today (`NAV` const): Path, Store, Health, Analytics, Settings — a flat topbar, not a sidebar.

| # | Flow | Route(s) | View(s) | Clicks/fields today | Dead ends / jargon | Missing states |
|---|---|---|---|---|---|---|
| 1 | Sign in | `/login` | `LoginView.vue` | 1 (real SSO) / 2-3 (sim mode: role → merchant → sign in) | none | no `Skeleton`/`ErrorBanner` use — plain text for loading/error/empty (predates those components) |
| 2 | Access guard | n/a (router guard) | `beforeEachGuard` + `needsConnect.ts` | 0 (logic only) | guard can block first render indefinitely if the auth `$subscribe` never resolves (no timeout) | — |
| 3 | Connect (pairing) | `/connect` | `ConnectSurface.vue` + `Connect{Idle,Pairing,Paired}Step.vue` | 3 (generate code → paste in plugin → auto-poll → done), lean | **pairing-generation failure has no visible error** — silently resets to idle + `console.error` only | no error state for `beginPairing` failure |
| 4 | Setup / onboarding path | `/path` | `PathSurface.vue`, `journey/stages/StageLayout.vue`, `JourneyBar.vue` | variable — 5-step `JourneyBar` (Draft/Connected/Published/Verified/Active) + open-ended task feed | "UCP active" appears once, in the final stage headline | has skeleton + `ErrorBanner`+retry — good |
| 5 | Catalog (products) | `/store` (Products tab) | `StoreSurface.vue` → `StoreProductsPanel.vue`, `ProductDrawer.vue` | tab → scorecard → search/table → row → drawer | dimension labels are raw keys (`attribute quality`, `delivery data`) — readable but borderline spec-y | has skeleton + `ErrorBanner`; empty/no-results is hand-rolled, not a shared `EmptyState` |
| 6 | Orders | `/store` (Orders tab) | `StoreOrdersPanel.vue` | 0 (read-only list) | — | **no loading skeleton, no error banner at all** — only view with neither |
| 7 | Health / readiness | `/health` | `HealthSurface.vue` | read-only + Ack/Resolve on alerts | copy already well laundered (explicit doc-comment about avoiding raw probe ids) — model for the rest | skeleton + `ErrorBanner` + two empty states — solid |
| 8 | Going live / validation | `/validation` | `ValidationView.vue` | "Run pre-validation" (1 click) + failures list + history | **heaviest jargon in the app**: title is *"How conformant are you with the UCP spec?"*; renders raw `requirement`, `spec_section`, `target_spec_version`, `run_id` directly, not behind a disclosure | skeleton + `ErrorBanner`; empty only for run history |
| 9 | Analytics | `/analytics` | `AnalyticsSurface.vue` (4 tabs: Checkouts/Lost Revenue/Channel/Insights) | tab click, `?tab=` synced | — | `TierGate.vue` already implements the "locked, plan-gated panel + upgrade CTA" pattern |
| 10 | Account (multi-store) | `/account` | `AccountHomeView.vue` | 1 click (card → `/path`) | — | **no loading skeleton** |
| 11 | Settings (incl. Team, Billing) | `/settings` | `SettingsSurface.vue`, `AccountSettingsPanel.vue`, `StoreSettingsPanel.vue`, `SettingsDrawer.vue` (12 named panels) | 1-2 clicks per edit (open drawer → save), toast confirms | **Domain panel renders `/.well-known/ucp` as a raw field value**, with copy naming "UCP profile" directly | skeleton + `ErrorBanner` — good |
| 12 | Operator / board (agency) | `/board`, `/board/:merchantId/:storeId` | `BoardSurface.vue`, `EnterAsMerchantView.vue` | Kanban + New merchant (2 fields)/Add store modals | — | skeleton per column + `ErrorBanner` + empty state — solid |

Team and billing already exist and are more complete than assumed:
- **Team**: `AccountSettingsPanel.vue` → "Business & team" → `SettingsDrawer` panel `team` — member list with role chips, seat-cap-aware invite form, `member_limit_reached`/`member_already_active` errors surfaced. No remove/role-change UI (invite-only in V1, per its own doc comment).
- **Billing**: Stripe-portal-delegated — no in-app plan editor; "Change plan"/"Update payment"/"View invoices" deep-link to the Stripe portal.
- **Multi-store switcher**: `StoreSwitcher.vue` — static label at 1 store, popover with per-store status at 2+. Already matches the brief's "hidden if only one store" requirement.

Not routed: `/surfaces` (generic agent-surface enrollment) — built, but with only the zero-config `direct_agent` surface there is nothing to enroll in.

## 2. Component gap analysis

| Target component | Status | Notes |
|---|---|---|
| `StatusChip` | **exists** as `ui/Chip.vue` | kinds: ok/warn/danger/quiet/you/us/google — already the semantic status pill; will re-skin colors, not replace |
| `ReadinessScore` | missing — inline in `StoreProductsPanel.vue` (`.big-num` + `.readiness-dims`) | extract |
| `IssueRow` | missing — repeated `.row`+`Chip` markup in `HealthSurface.vue`, `ValidationView.vue`, team rows | extract |
| `EmptyState` | missing — every view hand-rolls `.empty-note` | extract (highest-value win, most repetitive) |
| `PageHeader` | missing — every view hand-rolls `.page-head`/`.eyebrow`/`.page-title.display`/`.page-sub` | extract (very repetitive, easy win) |
| `StepChecklist` | closest is `JourneyBar.vue` (5-node horizontal progress) | adapt/rename for the Home "N of M done" list |
| `CopyField` | **exists**, matches spec | reskin only |
| `SkeletonCard` | **exists** as `ui/Skeleton.vue` (shimmer primitive) | wrap as a card-shaped preset |

## 3. Design tokens — target mapping

Marketing site tokens (`website/assets/css/tokens.css`, dark = `[data-theme='dark']`) vs. console tokens today (`console/src/styles/tokens.css`, two themes: `paper`/`midnight`):

| Site (dark) | Console today (`midnight`) | Target console token |
|---|---|---|
| `--bg:#07070b` | `--bg:#0F1419` | `--bg:#08080a` |
| `--surface:#0e0e14` | `--surface:#151B24` | `--surface:#111114` |
| `--surface-2:#15151d` | `--surface2:#1E2431` | `--surface-2:#18181c` |
| `--border: rgba(255,255,255,.075)` | `--line:#2A3444` | `--border:#1f1f24` |
| `--text:#f2f2f5` | `--ink:#F5F7FA` | `--text:#ededf0` |
| `--text-muted:#a1a1b0` | `--muted:#A8B4C4` | `--text-muted:#9a9aa5` |
| `--text-faint:#858596` | `--faint:#6B7584` | `--text-faint:#71717a` |
| `--accent:#9d9bff` | `--accent:#00D9FF` (cyan — off-brand) | `--accent:#9d9bff` |
| `--pass:#4ade80` | `--ok:#00E676` | `--pass:#4ade80` |
| `--flag:#f5a524` | `--warn:#FFB800` | `--flag:#f5a524` |
| `--fail`/`--danger:#FF4444`→ site has no fail token, uses same spectrum | `--danger:#FF4444` | `--fail:#f87171` |
| n/a | n/a | `--live:` accent — new status, not in console today |
| Fonts: Geist / Geist Mono / Instrument Serif (self-hosted, `/fonts/*.woff2`) | Instrument Sans / Fraunces / Geist Mono | Adopt site's font files + `--font-sans`/`--font-mono`/`--font-serif` names; drop Fraunces/Instrument Sans |
| `--radius:14px`, `--radius-lg:22px` | `--radius:12px`, `--radius-sm:8px`, `--radius-lg:16px` | `--radius:12px --radius-sm:8px` (per brief) |
| `--gap:24px` (site has no explicit gap token — section rhythm only) | `--gap-feed:11px` | `--gap:24px` |

Console keeps **dark as the only/default theme** per the brief ("dark default, identical to the marketing site") — `paper` (light) mode's fate: keep as a secondary theme (existing `ThemeToggle.vue`) since users can already switch, just re-derive its palette from the site's *light* `:root` block instead of retiring it outright (removing a working preference toggle is out of scope for a frontend-only redesign).

Logo lockup: reuse the site's `SarvexMark.vue` pattern (lit-diamond mark) — the console's own `shell/Mark.vue` will be re-implemented to match it exactly, wordmark unchanged ("Sarvex").

## 4. Information architecture change

Current topbar nav (Path/Store/Health/Analytics/Settings) → target sidebar (Home/Setup/Catalog/Orders/Analytics/Settings):

- **Home** = today's `/path` (rename route label only; URL can stay `/path` to avoid touching deep-linked `?task=` bookmarks, or alias `/` → `/path`).
- **Setup** = the `JourneyBar`/task-feed portion of Home, split out as its own sidebar entry *until live*, then folds back into Settings — per brief. Implemented as a computed nav-item visibility rule keyed on `ucp_state`/`activation` stage, not a new route.
- **Catalog** = today's `/store` Products tab, promoted to a full route.
- **Orders** = today's `/store` Orders tab, promoted to a full route (fixes the "no loading/error state" gap at the same time it's rebuilt).
- **Health** (`/health`) and **Validation** (`/validation`) fold into **Home** (readiness score + top blockers) and **Setup** (checklist) — no standalone nav entries, per brief ("do not keep separate pages for them"). Routes stay reachable (deep links, e.g. from task drawers) but drop out of primary nav.
  - **Health is back in the nav until its fold lands** (see §9.4). Dropping the entry before Home carried Health's content left active alerts — and their Acknowledge/Resolve actions — reachable only by typing `/health`; nothing linked there. Remove the nav entry in the same change that moves endpoint status, statements and alerts (with Ack/Resolve) onto Home.
- **Analytics** unchanged (`/analytics`).
- **Settings** unchanged, gains nothing new (Team/Billing already live there).

Top bar: store switcher (already conditional on store count — keep), a new global status pill (Not connected → Setting up → Ready to go live → Live, derived from existing `ucp_state`/journey data, no new endpoint), ⌘K palette (new), user menu (`UserMenu.vue`, keep).

## 5. Onboarding — before/after click count

**Before** (sign-up isn't in-scope of the console today — it starts at `/login`, and platform/store-URL entry doesn't exist as a step; the flow effectively starts at Connect):
1. Login (SSO) — 1 click
2. `/connect`: generate code (1 click) → switch to WooCommerce admin, paste code (outside app) → wait for poll → "Done" (1 click) → lands on `/path`
3. `/path`: no automatic "first check" — merchant must discover Health/Validation exist and manually click "Run pre-validation" on a *different page* (`/validation`) to get a first readiness result.

Rough total inside the app: **~4 clicks + 1 cross-app context switch (plugin paste) + a self-directed hunt for the validation page** before any readiness result appears.

**After (shipped):** Connect → pairing code → paired → Home, same ~3 in-app steps + 1 plugin paste, now with: a visible error state if pairing fails (previously a silent dead end); a sidebar/status pill that orients the merchant immediately; the first conformance check firing automatically the moment pairing completes (no more hunting for `/validation`); and — found while verifying this end-to-end — **a fix for a bug that meant this flow never completed at all against a contract-accurate backend** (§9). The "hunt for the validation page" step is now genuinely gone. The sign-up step (email+URL ahead of Connect) remains out — see §6.

## 6. Investigated and declined — not gaps, architecture conflicts

Both re-checked directly against `sarvex-v1-architecture.md` Part C/D and `sarvex-v1-control-plane.yaml` before writing anything:

- **Self-serve sign-up (email + store URL, platform auto-detect).** Not a missing endpoint — a different product model than the one documented. `POST /contact` is the marketing site's lead-capture form only; `/account` and `/stores` have no self-serve creation method; merchant/store creation exists solely under `POST /operator/merchants` and `POST /operator/merchants/{id}/stores`. Accounts are operator-provisioned by design. Building a self-serve flow would invert that, not fix a hole in it — needs a product decision, not a frontend guess.
- **"Go live" confirm button.** Also not a gap. The architecture doc states `verified → active` is "driven by `endpointhealth`'s probes" — fully automatic, with zero merchant action. There is no activation mutation in the contract because none is meant to exist. Shipped the spirit of this instead: `GoLiveCelebration.vue` watches the activation state the shell already keeps live (via the journey stream) and shows a one-time success state the instant a store actually crosses into `active` — real mechanism, no invented endpoint.

## 7. Remaining friction not fixable frontend-only

- Self-serve sign-up (§6) is the one remaining piece of "sign-up → first result in under 5 minutes" this pass didn't close, because there's no product surface to build it against yet.
- Home/Setup are still one page (`/path`) rather than the two the brief describes (a light dashboard vs. the full task-feed setup view). Splitting them is real, scoped, achievable frontend work — just not done in this pass.
- Full Phase 4 (issue drawers built on `IssueRow`, wired into Health) and Phase 5 (per-screen polish for Catalog/Orders/Analytics/Settings/Team) are started (see §9) but not complete.

## 8. Stubbed / no new backend needed

Nothing in this pass required a new backend field or endpoint. `StoreStatusPill`, `StepChecklist`, `ReadinessScore`, and `GoLiveCelebration` are all derived from data the shell already loads (`activation`, `connectors`, `catalog.readiness`) or already streams live (the journey WebSocket's `onActivationUpdate`).

## 9. Execution status

1. ✅ Design tokens + shared components — tokens realigned to the site's dark palette (site fonts self-hosted); `StatusChip`, `PageHeader`, `EmptyState`, `IssueRow`, `ReadinessScore`, `StepChecklist`, `SkeletonCard` built.
2. ✅ Sidebar IA + top bar — collapsible desktop sidebar, mobile drawer, `StoreStatusPill`, ⌘K `CommandPalette`. Health temporarily restored to the nav (§4, §9.4).
3. ✅ Onboarding — pairing-error state; first check now auto-fires on pairing; go-live celebration on real state transition. Sign-up step declined per §6.
4. ⚠️ Issue drawers — `IssueRow`/`StatusChip` exist; `ValidationView` and its task-drawer twin `PreValidationPanel` now lead with plain-English titles and a real "Technical details" disclosure (not yet using `IssueRow` itself as the row primitive). `HealthSurface` not yet touched, and **not yet folded into Home**: Home shows only the consistency stat, so endpoint status, health statements and active alerts (Ack/Resolve) still live only on `/health`, which stays in the nav until they move.
5. ⚠️ Screen polish — `StoreProductsPanel` now uses the shared `ReadinessScore` ring with plain-English dimension labels. Catalog/Orders/Analytics/Settings/Team otherwise still render their pre-existing layouts.
6. ✅ States & feedback — Connect pairing error, Orders skeleton/error, Account skeleton all fixed.
7. ✅ Copy pass — full sweep of `src/locales/en.ts` for "UCP"/"conformance"/"profile" (not just the two spots found in the original audit): stage headlines including the terminal "is agent-ready" headline, Settings' domain/return/profile/shipping copy, Health's check label+meaning, activation blocking/suspension reasons, `PreValidationPanel`. `StorePresencePanel`'s raw `/.well-known/ucp` manifest viewer was deliberately left alone — it's an inherently technical view (a JSON document viewer), not primary UI, and is no longer in primary nav.
8. ⚠️ **Corrected later**: this pass moved `createConnector()` from `POST /stores/{id}/connectors/pairing` to `POST /connectors` to match the contract, verified only against the mock backend. The contract was the stale side: the control plane, both plugins, scripts and tests all use the store-scoped route, and `POST /connectors` with only `{ platform }` cannot say which of a merchant's stores to pair. Against the real control plane it returned 405. The contract now specifies `POST /stores/{store_id}/connectors/pairing`, and `src/api/connectors.test.ts` pins the URL the console sends. Lesson: a mock that implements the contract proves agreement with the contract, not with the backend — check `make check-routes` too.

Every commit was gated on `npm run typecheck`, `npm run build`, and `npm run test:run` (530 tests) passing, plus repeated Playwright smoke passes — including one full Connect → pairing → paired → Home run against the mock backend, which is what surfaced the bug in §9.8.
