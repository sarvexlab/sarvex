# Plan — one module per spec version, one module per e-commerce platform

Status: **approved — Phases 0–4 landed** (decisions in §7, open items in §8)
Scope: backend (`internal/`), simulator, sarvex-v1-architecture.md. Frontend and PHP plugins are untouched except where noted.

## 1. Goal

1. Each UCP spec generation served by the serving plane has its **own implementation and its own contract** (types, wire envelope, request/response rendering for every resource, profile rendering, conformance fixtures).
2. Each backend platform (WooCommerce, Magento, …) has its **own connector and implementation** (client, mapper, config + schema, credential validator, simulator surface, fixtures).
3. The only shared code is code that is **neutral to both version and platform**: the canonical commerce model, the money path (session store, idempotency, order handoff), negotiation mechanics, caches, planes.
4. **Adding a version or a platform cannot regress an existing one.** This is enforced by tests and import rules, not by review discipline.

Adding version N+1 = one new directory + one registration line. Adding platform P = one new directory + one registration line. Neither touches an existing version's or platform's directory, and CI proves that the existing ones' output didn't change.

## 2. Where we are today (sweep of the current tree)

The repo already moved partway: there are registries (`platforms.All`, `wireadapt.Registry`, `generation.Registry`, `types.Servable`) and two guard tests (`TestServableMatchesCodecs`, `TestCapabilityMatrixCoversEverySupportedPlatform`). The remaining problems are where version- or platform-specific code still lives in shared code, and where "the canonical model" is really one version's wire types.

### 2.1 Versions

| # | Finding | Where | Why it matters |
|---|---------|-------|----------------|
| V1 | **The canonical model *is* the 2026-04-08 wire types.** `connectors/base` aliases `types/v20260408` (`CheckoutStatus = v20260408.CheckoutStatus`, `CheckoutResponse.UCP v20260408.CheckoutResponseMetadata`, …). | `internal/pkg/ucp/connectors/base/response_types.go`, `request_types.go`, `escalation.go` | 2026-04-08 can never be changed, fixed or retired without touching every connector and the money path. Anything a newer generation can express but 04-08 can't (e.g. 08-25's consent map) needs side channels — see V4. |
| V2 | **The 2026-04-08 codec is the identity function**; 04-08 is implicitly "the model", not a version. | `wireadapt/v20260408/v20260408.go` | There is no 04-08 contract to test in isolation; a canonical-model change silently changes the 04-08 wire output. |
| V3 | **Catalog rendering branches on version inside the shared handler.** `negotiatedVersionIsV20260825(c)` string-compares `"2026-08-25"`; the 08-25 catalog contract is a file in `serving/api`, not in a version module; the 04-08 one is the unnamed default. | `serving/api/handler.go:193,215,242`, `catalog_contract.go`, `catalog_contract_v20260825.go` | A new version means editing `handler.go` — the shared handler every version runs through. |
| V4 | **Version special-cases in the HTTP edge.** `bind` fast-paths 04-08; `respond` fetches `consentWire` only when `v != V20260408`; `RequestExtras.BuyerConsent` + `CheckoutService.ConsentWire` persist the 08-25 wire shape because the canonical model can't hold it. | `serving/api/version.go:37,45,66` | Every new version with a richer shape adds another extras field and another branch. |
| V5 | **Hard-coded default/fallback version** in four places. | `serving/api/version.go:37`, `serving/webhook.go:146`, `profile_renderer.go:329` (`catalogueSpecVersion`), `control/endpointhealth/identity.go:66` | Retiring or re-baselining 04-08 is a scavenger hunt. |
| V6 | **Profile rendering branches on version.** `if ucpVersion == V20260408 { SigningKeys } else { Keys }`. | `serving/profile_renderer.go:93` | Profile shape is a per-version contract living in shared code. |
| V7 | **Connectors stamp a spec-version envelope.** `checkoutResponseMeta()` etc. build `ucp.version` + capability map at `ducp.UCPVersion_20240408` (misnamed constant). Near-identical copies in each connector. | `connectors/{woocommerce,magento,shopify}/profile.go`, `internal/pkg/ucp/profile.go:6` | Platform code knows about spec versions — the two axes are entangled. |
| V8 | **A version is scattered across five places** with four registries: `types/vX` (schema), `wire/vX` (envelope — except 04-08, whose envelope is `catalogue/v1/wire` and is imported by ~30 shared files as *the* envelope), `generation/vX` (profile decode), `wireadapt/vX` (checkout/cart/order), `serving/api/catalog_contract_vX.go`; plus `types.registry`, `types.Servable`, `types.CheckoutServable`. | `internal/pkg/ucp/{types,wire,generation,wireadapt,catalogue/v1/wire}` | No single unit to add, test, or delete. |
| V9 | Stale docs: `serving/api/dependencies.go:106-111` and `negotiation/service.go:106` still say checkout is pinned to 04-08; `types.CheckoutServable == Servable` today. | — | Fix in passing (sarvex-v1-architecture.md: no pre-existing bugs). |

### 2.2 Platforms

| # | Finding | Where | Why it matters |
|---|---------|-------|----------------|
| P1 | **Three platform enums.** `pkg/domain.CommerceSystemType`, `control/domain.Platform`, `sync/domain.Platform`. | `internal/pkg/domain/types.go`, `planes/{control,sync}/domain/connector.go` | Adding a platform means editing each. |
| P2 | **Control plane hard-codes the allow-list** `PlatformWooCommerce \|\| PlatformMagento`; activation defaults to WooCommerce. | `control/api/store.go:232`, `control/api/connector.go:96`, `control/activation/activation_service.go:199` | A new platform is rejected at the API until someone edits the control plane. |
| P3 | **All platforms' configs live in one shared package**, with a second, hand-maintained platform list (`configs.NewSchemaRegistry`, which also lists frozen Shopify). | `internal/pkg/ucp/configs/*` | Platform config is not owned by the platform; two lists can drift. |
| P4 | **Connectors emit spec-version types** (see V7) and duplicate the same envelope/fulfillment-option helpers. | `connectors/*/profile.go`, `extension.go` | Connector must change when a version is added. |
| P5 | **The simulator is one package for all platforms** (`sim/http.go`, `sim/http_magento.go`, `sim/store.go`); the capability matrix lists platforms by hand (the coverage test catches omissions, but the test file must still be edited). | `internal/planes/sync/sim/` | Adding a platform edits a shared file. |

What's already right and should be kept: `platforms.All()` as the single registration point; composition roots calling `platforms.Register*`; connector/validator factory pairs; `wireadapt.Registry` with panic-on-duplicate; negotiation being registry-driven; the capability matrix idea.

## 3. Target architecture

Per decision 2 (§7), the two axes are new top-level folders under `internal/`
(siblings of `planes/` and `pkg/`; they stay under `internal/` so Go keeps them
private to this module):

```
internal/pkg/commerce/                  # version- AND platform-neutral (shared)
  model/                                # Sarvex-owned canonical model: Checkout, Cart, Order,
                                        #   Product, Fulfillment, Discount, Consent, Messages…
                                        #   Superset of what any served version can express.
  connector/                            # Connector interface + registry (today connectors/base),
                                        #   speaks only model/*. No spec-version import.

internal/spec/                          # version axis
  spec.go                               # Version interface + Registry (replaces the four registries)
  conformance/                          # shared contract-test harness run against every Version
  v20260408/                            # ONE self-contained module per generation
    schema/        (today types/v20260408 + extensions)
    wire/          (today catalogue/v1/wire — envelope)
    codec.go       checkout/cart/order/catalog encode+decode  model <-> wire
    profile.go     profile rendering + DecodeProfile (today generation/v20260408)
    capabilities.go which capabilities/extensions this version serves
    testdata/golden/…   frozen request/response fixtures
    DISCREPANCIES.md
  v20260825/  (same shape)
  all/all.go                            # the ONE registration line per version

internal/platform/                      # platform axis — as built in Phase 1
  platform.go                           # descriptor + registry: the ONE registration line per platform
  woocommerce/                          # package woocommerce: connector, client, mapper + MerchantConfig/schema
    validator/                          # credential validator
    sim/                                # simulated store surface (simstore.Surface) + sim credentials
  magento/   (same shape)
  frozen/shopify/                       # frozen: compiles and keeps its tests, not registered
```

(As built, the connector *is* the `<name>` package rather than a `connector/`
sub-package, so it reads `woocommerce.NewConnector`; the registry is the root
`internal/platform` package rather than a separate `all/`. A per-platform
`capabilities.go` arrives with Phase 2's canonical model.)

### 3.1 Canonical core + per-version module (decided: option A)

The requirement is "separate implementation & contracts for each version on the serving side". Two ways to meet it:

- **(A, recommended) Canonical core + per-version module.** The money path (sessions, idempotency, connector calls, order handoff) is written once against a Sarvex-owned `commerce/model`. Each version module owns its *entire* contract: its schema types, its envelope, its encode/decode of every resource, its profile shape, its golden fixtures. Handlers never know which version they're serving. This keeps §D R1/R3 of the architecture doc (don't fork the money path) and still gives each version a separate, independently-tested implementation.
- **(B) Fork handlers per version.** Each version has its own handler set end-to-end. Maximum isolation, but duplicates the money path (idempotency, escalation, order recording) per version — every money-path bug fix must be made N times, which is exactly the kind of cross-version regression requirement 4 wants to prevent. Not recommended.

**Decided: (A).**

### 3.2 Key interfaces (sketch)

```go
// spec.Version — everything the serving plane needs from one generation.
type Version interface {
    ID() types.Version
    Serves() CapabilitySet                    // replaces Servable / CheckoutServable
    DecodeRequest(r Resource, body []byte) (model.Request, error)   // typed, not []byte→[]byte
    EncodeResponse(r Resource, v model.Resource) ([]byte, error)
    RenderProfile(p model.Profile) ([]byte, error)                  // signing_keys vs keys lives here
    DecodeProfile(raw json.RawMessage) (Profile, error)
}

// platform.Platform — everything the planes need from one backend.
type Platform struct {
    Name         PlatformName          // single enum, owned here
    Connector    connector.Factory
    Validator    validator.Factory
    ConfigSchema json.RawMessage
    Capabilities CapabilitySet         // intersected with Version.Serves() when advertising
    Sim          sim.Surface           // test/dev only
    Plugin       string                // repo path of the merchant-side plugin, e.g. "plugin/woocommerce"
}
```

A test asserts every `plugin/<name>` directory belongs to a registered platform and vice versa, so the PHP side is covered by the same "one place per platform" rule.

Shared code then has exactly one way to reach a version (`spec.Registry.Get(negotiated)`) and one way to reach a platform (`platform.Registry.Get(store.Platform)`). Defaults come from the registry (`Registry.Baseline()` = oldest servable), never from a constant.

The advertised capability set for a store = `Version.Serves() ∩ Platform.Capabilities`, which is what sarvex-v1-architecture.md ("make the profile/checkout not advertise it there").

## 4. How requirement 4 (no regressions) is enforced

1. **Per-version golden fixtures.** Every version module carries frozen request→response fixtures for every resource × every platform (generated from the simulator). A PR that changes any existing version's golden files fails unless the PR carries an explicit `spec-change/<version>` label (decision 3). Adding v2027 adds new goldens only.
   - Exception by design: the published `/.well-known/ucp` profile is **one document spanning every generation** (it renders at `types.Latest()` and lists each capability's versions), so adding a version *does* change it. Its golden (`profile/<platform>.json`) accepts any `spec-change/*` label, so that change is reviewed rather than silent.
2. **Per-platform golden fixtures.** Same, for connector output in canonical form (`model.*` JSON) per scenario.
3. **Import-boundary lint** (`depguard` in `.golangci.yml` + a Go test that walks `go list -deps`, so it runs in `make test-unit` too):
   - `spec/vX` may import `commerce/model`, `spec`, its own sub-packages — never another `spec/vY`, never `platform/*`.
   - `platform/X` may import `commerce/*`, `platform` — never `spec/*`, never another `platform/Y`.
   - Everything else may import `spec` and `platform` (registries) and `commerce/*`, but never a concrete `spec/vX` or `platform/X` (except the two `all/` packages and composition roots).
   - No string literal or identifier naming a known version or platform outside those directories (AST test).
4. **Registry-driven conformance matrix.** `spec/conformance` runs the full contract suite for `spec.All() × platform.All()`; adding either extends the matrix automatically (generalises today's `TestCapabilityMatrix`).
5. **Canary registrations.** A test-only `v99991231` version and `fakeplatform` platform are registered in a test; the test asserts every existing golden still passes byte-for-byte. This is the executable proof that "add = new code + one line" holds.
6. **Canonical model evolution rule:** `commerce/model` changes are additive-only (new optional fields). Enforced by the goldens: an existing version's encoder ignores fields it doesn't know, so its output can't change.

## 5. Phased delivery (each phase is its own PR, green on its own)

**Phase 0 — Safety net first (no production code moves).** ✅ Done
- `internal/pkg/golden` — shared golden helper (`UPDATE_GOLDENS=1` regenerates; timestamps and run-specific URLs/ids masked).
- Goldens for today's behaviour:
  - `internal/planes/sync/sim/testdata/golden/<platform>/<version>/` — checkout create/complete/pickup and cart create, rendered per generation, from `TestCapabilityMatrix`;
  - `…/<platform>/canonical/` — the connector's own output (catalog lookup, checkout, cart, order get);
  - `internal/planes/serving/api/testdata/golden/<version>|not-negotiated/` — catalog search and lookup;
  - `internal/planes/serving/testdata/golden/profile/<platform>.json` — the published profile.
- `internal/archtest` — `TestVersionAndPlatformBoundaries`, a ratchet over imports, string literals and identifiers. The 140 current violations are in `testdata/allowlist.txt`; a new one fails, and so does a fixed one until the list is shrunk (`ARCHTEST_UPDATE=1`), so the list can only shrink. Runs in `make test-unit`.
- `.github/workflows/golden-guard.yml` — fails a PR that changes an existing golden without `spec-change/<version>` / `platform-change/<platform>`, or grows the allowlist without `boundary-exception`.
- Fixed V9 stale comments.

**Phase 1 — Platform axis.** ✅ Done
- Each platform is one module under `internal/platform/<name>` (connector + `MerchantConfig`/schema, `validator/`, `sim/`); frozen Shopify under `internal/platform/frozen/shopify`. The registry `internal/platform` carries name, connector, validator, config schema, plugin path and simulated store (P3, P5, decision 4).
- One platform name type (`pkg/domain.CommerceSystemType`); the planes' `Platform` types are aliases of it and their duplicated constants are gone; each platform owns its `PlatformName` (P1).
- Control plane store-create / pairing checks use `platform.Supported`; activation verification no longer defaults to WooCommerce (P2).
- `configs.NewSchemaRegistry` (a second platform list) removed; the definitions catalogue's backend compositions come from the registry — **fixed Magento missing from them** (and so from the gateway-cli wizard).
- Simulator: the neutral store model is `internal/pkg/simstore`; each platform's REST surface and simulated credentials live in its `sim/`; the sync simulator registers every platform's surface from the registry; `TestCapabilityMatrix` iterates the registry (P5). **Fixed:** the control plane's dev plugin simulator paired *every* store with WooCommerce credentials — it now uses the store's own platform.
- Tests keep the registry in step with `plugin/` and with `sarvex-v1-control-plane.yaml` (platform enum + `store_credentials_<name>` schema).
- `internal/archtest` follows the new layout and gained the plane-isolation rule (§2.1). The allowlist went 140 → 105 lines with nothing added.
- Also fixed: the sync simulator's `integration`-tagged tests did not compile (a test fake went with the archived integration).
- Every golden is byte-identical across the phase.

Adding a platform now touches: `internal/platform/<name>/` (new), one entry in `platform.All`, `plugin/<name>` (new), and the contract's platform enum + credentials schema — the last is the authoritative contract (sarvex-v1-architecture.md), and a test fails until it agrees. P4 (connectors emitting spec-version types) is Phase 2's.

**Phase 2 — Introduce the Sarvex-owned canonical model.** ✅ Done
- `internal/pkg/commerce/model` is the canonical model: Sarvex's own copy of the shapes the money path uses (checkout parts, cart, order, fulfillment, messages, payment, buyer consent, discounts), taken from 2026-04-08 by reachability. It imports no spec version; nor do `connectors/base` or any platform module.
- The response envelope left the connectors (V7/P4): the canonical responses carry none, and each `wireadapt` codec stamps its own generation's envelope (`wireadapt.WithEnvelope`, per-resource capabilities in `Resource.Capabilities`). **Fixed:** 2026-08-25 responses declared `version: 2026-04-08`, and Magento's checkout envelope omitted `buyer_consent`. (23 goldens changed, only in their `ucp` block — verified by script; labels `platform-change/woocommerce` + `platform-change/magento`.)
- The model's JSON is still byte-identical to 2026-04-08's, so that codec only adds the envelope; `TestCanonicalModelIsThe20260408WireForm` pins it.
- `model.NewFulfillmentOption` replaces a constructor copied in each connector.
- **Moved to Phase 3:** growing the model beyond 2026-04-08 (e.g. 2026-08-25's rich consent, which would delete the `RequestExtras`/`ConsentWire` side channel, V4). It changes the canonical JSON, so it needs 2026-04-08's real codec first — a Phase 3 item.

**Phase 3 — One module per version.** ✅ Done
- Each generation is one module, `internal/spec/vYYYYMMDD` (moved with `git mv`, no logic edits): its schema types + `extensions/` + `DISCREPANCIES.md` (from `types/vX`), `wire` (its profile document — from `catalogue/v1/wire` and `wire/v20260825`), `profile` (its `generation.Generation` — from `generation/vX`) and `codec` (its `wireadapt.Codec` — from `wireadapt/vX`). The boundary test owns a version by its `internal/spec/vX` directory; `make test-unit` runs `./internal/spec/...`.
- **Profile (V6).** The profile constructs are the same in every served generation, so they live once in `internal/pkg/ucp/wire`; each `spec/vX/wire` holds only its document form (`signing_keys` vs `keys`) and converts. `generation.Generation` reads a profile into the neutral form and writes one in its own (`DecodeProfile`/`EncodeProfile`); `generation/all` registers every generation. The renderer's key placement, the ucpnego profile handler and the endpoint-health reference profile all write through the declared generation. **Fixed:** request-signature verification rejected every 2026-08-25 agent — `ucpsig`'s resolver (and the shared profile decode) read keys only from `signing_keys`; and the probe's reference profile declared 2026-08-25 but was published with `signing_keys`.
- **Catalog (V3).** Each codec owns its catalog contract (`codec/catalog.go`): it reads its own search_request into a neutral `model.CatalogQuery` and renders `model.CatalogPage`/`model.CatalogProduct` as its own Product. `catalog_contract_v20260825.go` and `negotiatedVersionIsV20260825` are gone, and a 2026-08-25 search is read against 2026-08-25's schema (it was bound against 2026-04-08's).
- **Serving edge (V4, V5).** `bind` always goes through the negotiated codec (no 2026-04-08 fast path); the original consent is a lookup the codec calls only if its generation needs it (no `v != V20260408` branch); never-negotiated requests and unversioned order webhooks use `wireadapt.Registry.Baseline()`; the renderer reads the catalogue's authored generation from `Catalogue.SpecVersion`; the probe and `sarvex conform serve --ucp-versions` derive their versions from the registries. The serving API package names no version.
- The allowlist went 68 → 32 lines with nothing added; every golden is byte-identical across the phase.

What remains of the version axis moves to Phase 4:
- **The definitions catalogue is typed in 2026-04-08's schema** (`catalogue/v1` — its `Service`/`Capability`/`PaymentHandler`), and through it the published profile's `ucp` block (`serving.UCPBlockCache`), the renderer, the cache rebuilder and the control plane's store profile. Moving them to `ucp/wire`'s neutral types changes the published profile's bytes (key order, omitted-empty members) and the Redis snapshot format, so it is its own reviewed step (`spec-change/*` label on the profile goldens). Almost all of the 32 remaining allowlist lines are this.
- **Rich consent in the canonical model** (deleting `RequestExtras`/`ConsentWire`, V4's last side channel, and giving 2026-04-08 a real translation, V2). The canonical consent is also what connectors persist into the merchant's order (WooCommerce order meta, Magento order comment), so widening it changes stored merchant data. **Decided: not built.** The canonical consent stays yes/no flags, so merchant orders keep exactly what they store today, and 2026-08-25's richer consent keeps travelling through the `RequestExtras`/`ConsentWire` side channel, which its codec alone reads (no version branch in shared code).
- Enforce mode for the version axis follows once the catalogue item lands.

**Phase 4 — Collapse the registries.** ✅ Done
- **One registry.** `types.registry`/`Servable`/`CheckoutServable`, `wireadapt/all` and `generation/all` collapse into `internal/spec` (`spec.Registry`), assembled in `internal/spec/all` — one line per generation. Each module contributes one `spec.Version` from its `release` package: its id (the module's `Release` constant), what it serves (`Serves`), its codec and its profile generation. `Latest`, `Baseline`, `Serving(capability)`, `Servable`, and the codec and generation views all derive from that. Negotiation's servable sets are the generations serving the catalog and checkout; the renderer advertises each capability at the generations serving it; operator, endpointhealth, conform and ucpnego read the same registry. `types` keeps only the `Version` type.
- **The catalogue and profile rendering on neutral types** (moved from Phase 3): `catalogue/v1`, `serving.UCPBlockCache`, the renderer, rebuilder, store-profile repositories and payment validator use `ucp/wire`. Only enabled capability ids are persisted, so storage is unchanged; the published profile is semantically identical. **Fixed:** the store profile negotiation reads dropped every capability's `extends` and every service's `transport`, so an agent could negotiate an extension (fulfillment, discount, buyer consent) without its parent checkout — UCP intersection step 3 prunes by the business capability's `extends`; and `wire.Capability` wrote `"extends": null` for a root capability (`omitzero` now).
- **No leaks left; enforce mode.** Shop Pay (a Shopify-only payment handler, unreachable since Shopify is frozen and unserved) moved into the frozen Shopify module; the catalogue is authored at the registry's `Baseline`; CLI defaults and the agent's decorative path segment come from `platform.Default()`/`platform.Names()`; the ucpnego examples speak the registry's `Baseline`. `TestVersionAndPlatformBoundaries` now fails on any violation — the allowlist (140 lines at Phase 0) is gone, and golden-guard no longer watches one.
- **Canaries (§4.5).** `internal/spec/spectest` is a test-only generation (`9999-12-31`); `TestCanaryGenerationChangesNoExistingOutput` checks every existing generation's resources, catalog reads and profile document byte-for-byte with it registered, and `TestCapabilityMatrix_CanariesChangeNothing` runs every existing platform × generation of the capability matrix — with the canary generation registered and a canary platform's simulated store served beside the real ones (`sim.Server.AddSurface`) — against the existing goldens.

**Phase 5 — Docs + scaffolding.** ✅ Done
- sarvex-v1-architecture.md `sarvex-v1-architecture.md` describe the new layout (updated as each phase landed).
- `docs/HOWTO-add-spec-version.md` and `docs/HOWTO-add-platform.md` walk through adding each.
- `make new-spec-version V=YYYY-MM-DD` / `make new-platform P=name [D=Display]` (`cmd/scaffold`, `internal/scaffold`) copy the registry's latest generation / default platform into a new module with every spelling of its name rewritten, and add the registration line (a platform also gets `plugin/<name>`). Both templates come from the registries, so the tool names no version or platform. A fresh scaffold builds and passes the boundary check; what is left fails a named test — the contract's platform enum and credentials schema, the console's platform labels (typecheck), the new module's goldens, and for a platform `TestSimSurfacesDoNotOverlap` until its simulated store speaks its own API.
- **Fixed:** the simulator registered every platform's simulated store on one mux, so two platforms with a route in common (a fresh scaffold, or two real platforms whose APIs share a path) panicked the whole simulator. Each surface now has its own mux, the first claiming a path serves it, and `TestSimSurfacesDoNotOverlap` keeps the registered ones apart. Two tests pinned today's generations (the operator presence's supported versions, and the profile renderer's top-level version as `2026-08-25`), so adding any generation failed them; they read the registry now. The console named WooCommerce and Magento in three places; one table (`web/console/src/platforms.ts`), typed over the contract's enum, replaces them.

Suggested order: 0 → 1 → 2 → 3 → 4 → 5. Phase 1 is independent of 2–4 and can run in parallel after Phase 0.

## 6. Risks and how each is handled

| Risk | Mitigation |
|------|------------|
| A large `git mv` breaks import paths in many files | Moves happen in dedicated PRs with no logic edits; `gopls`/`gofmt -r` rewrites; goldens must be byte-identical. |
| Canonical-model rewrite changes money-path behaviour | Aliases first (Phase 2 step 1), then per-resource replacement, each gated by goldens + `make test-integration` + `make test-e2e-journey`. |
| Contract drift vs `sarvex-v1-control-plane.yaml` | The only control-plane-visible change is the platform enum source; the enum values stay the same strings. No endpoint or field changes. If the OpenAPI enum must become open-ended, stop and report (sarvex-v1-architecture.md). |
| Plane-isolation invariant | New packages live under `internal/pkg`, `internal/spec` or `internal/platform`, never under `internal/planes`; Phase 1 extends the boundary test to assert no plane imports another. |
| Frozen connectors | Moved under `platform/frozen/` without feature changes; they keep compiling and keep their CI scenarios, but are not in `platform/all`. |

## 7. Decisions

1. **Option A** — canonical core + per-version module (§3.1).
2. **New top-level folders**: `internal/spec/vYYYYMMDD` and `internal/platform/<name>` (§3).
3. **PR labels** gate golden changes: `spec-change/<version>`, `platform-change/<platform>` (`boundary-exception` gated allowlist growth until the allowlist was removed in Phase 4) (§4, `golden-guard.yml`).
4. **Plugins are part of the platform module**: the platform descriptor points at `plugin/<name>`, and a test keeps plugins and registered platforms in step (§3.2).

## 8. Open items found along the way

The plane-isolation rule added in Phase 1 found existing cross-plane imports (sarvex-v1-architecture.md A10.1). Two were fixed in Phase 1 (sync used serving's cache types and control's error envelope); the rest are resolved below.

1. ✅ **`control/api/entry.go` and `sync/api/entry.go` imported `internal/planes/serving`** for `NewRedisCache` / `NewCacheProfileSource`. Fixed by moving the serving cache (read interface, snapshot types, Redis client, profile source) into `internal/pkg/serving`, with aliases left in the serving plane. The move also exposed two different `ErrNotFound` sentinels — the Redis cache returned one, the control plane's operator service checked the other — so a store with no snapshot yet was reported as an error; there is now one.
2. ✅ **`sync/service.go` imported `internal/planes/control/catalog`** to insert remediation records into `control_catalog_remediations`. **Decided: the sync plane owns remediation records.** The collection is now `sync_catalog_remediations`, written only by sync: its score stage creates one record per remediable issue and retires it when the issue resolves, and its apply-remediations handler (the existing `global.apply.remediation` command) applies each approved record's fix to that record's product, writes it back to the store, and marks the record applied. The control plane reads the collection by name, read-only; its `Insert`/`MarkApplied` are gone. A contract test (`internal/archtest`) holds sync's record shape and control's read model together. Fixed along the way: nothing created records in production (the writer was wired `nil`), so the console's remediation list was always empty; nothing ever marked a record applied, so the same fix could be applied repeatedly; sync wrote a `trust` value outside the contract's `high|low` enum; and the apply handler looked records up as rubric ids and wrote the fix into every product of the store. No data migration is needed: `control_catalog_remediations` never had a production writer. ✅ Reverting a write-back now undoes it: the control plane sends `global.revert.remediation`, and the sync plane restores each field's prior value (recorded when the fix was applied) on the product and in the store, reopens the records and re-scores. (It used to only flag the write-back.)
3. ✅ The canary registrations (§4.5) landed with Phase 4.
4. ✅ (repaired separately, PR #323: rebuilt on the planes' current composition and added to CI's integration job) **The e2e journey suite (`test/e2e/journey`, `make test-e2e-journey`) did not compile**, and has not since well before this plan: it builds the control and sync planes by hand against constructors that have since changed (`task.NewTaskService`, `store.NewStoreService`, `connector.NewConnectorService`, `operator.NewOperatorService`, `auth.NewHMACResolver`, `base.New`, `syncsvc.NewSyncService`, …) and imports connector packages from before Phase 1. CI does not run it, which is how it rotted. Repairing it means rebuilding its composition from the planes' current composition roots (and adding it to CI); it is tracked here rather than folded into a refactor PR.
