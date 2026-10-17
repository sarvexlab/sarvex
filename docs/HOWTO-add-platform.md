# How to add a backend platform

A backend platform (WooCommerce, Magento, …) is one self-contained module, registered by one entry in `platform.All` (`internal/platform/platform.go`). The module is `internal/platform/<name>` plus its merchant-side plugin in `plugin/<name>`. No established platform, spec version or plane changes (sarvex-v1-architecture.md, `docs/plans/version-platform-modularity.md`).

V1's scope is WooCommerce and Magento (sarvex-v1-architecture.md). A connector beyond those is a stop-and-report condition, so get scope agreed before you start.

## 1. Scaffold it

```sh
make new-platform P=shopware D=Shopware
```

`P` is the registry name: lowercase letters only. It must not contain, or be contained in, another platform's name, frozen ones included. The boundary check finds platform code by that name. `D` is the display name; it defaults to `P` capitalised.

This copies the registry's default platform into `internal/platform/<name>`: the connector and merchant config, `validator/` and `sim/`. It rewrites every spelling of the old name: lowercase becomes `P`, uppercase becomes `P` in uppercase, anything else becomes `D`. It also adds the module to `platform.All` and creates `plugin/<name>/README.md`.

The copy builds and passes the boundary check, but **it still speaks the template's store API**.

## 2. Make it talk to the platform

| Path | What it is |
|---|---|
| `internal/platform/<name>` | The connector: `client.go` (the store API), `mapper.go`/`types.go` (store shapes ↔ the canonical model, `internal/pkg/commerce/model`), `config.go` (merchant config + its JSON schema), `factory.go` (`PlatformName`, `Factory`). Platform-only features, such as a platform's own payment handler, live here too. |
| `validator/` | Checks a merchant's credentials against the store. |
| `sim/` | The simulated store: the platform's REST surface over `internal/pkg/simstore`, served by the dev/test simulator under `/stores/{store_id}/`. |
| `plugin/<name>` | The merchant-side plugin. It has two roles only, signalling and the `/.well-known/ucp` doorway, and no business logic (sarvex-v1-architecture.md). |

Rules:

- **Connectors produce the canonical model and never a spec version's types.** Version differences belong in the version codecs.
- **Nothing outside the module names the platform.** Composition roots call `platform.Register*`, the control plane checks `platform.Supported`, and tools default to `platform.Default()`. Add no `switch platform` to shared code.
- **Every capability works on every platform** (sarvex-v1-architecture.md). If the platform cannot support one, stop and report. Do not ship it advertised.

## 3. The contract and the console

These tests fail until each item is done:

1. **`sarvex-v1-control-plane.yaml`** — add `<name>` to the `platform` enum, and a `store_credentials_<name>` schema beside the others (plus its `oneOf` entry where the store credentials are accepted). Checked by `TestControlPlaneContractListsEveryPlatform`. The contract is authoritative (sarvex-v1-architecture.md).
2. **The console** — `cd web/console && npm run gen:api`, then add the platform to `STORE_PLATFORMS` in `src/platforms.ts` and its label to `settings.store` in `src/locales/en.ts`. Checked by `npm run typecheck`.
3. **The plugin** — `plugin/<name>` must exist (the scaffold creates it). Checked by `TestPluginsMatchRegisteredPlatforms`.
4. **The simulated store** — its routes must not overlap another platform's. Checked by `TestSimSurfacesDoNotOverlap`, which fails on a fresh scaffold until `sim/` (and the connector's client) speak the new platform's API.

## 4. Goldens and tests

```sh
UPDATE_GOLDENS=1 go test ./internal/planes/...
git status --short '**/testdata/golden'
```

Only **new** files may appear (the new platform's directory under `internal/planes/sync/sim/testdata/golden`). A modified existing golden means the new platform changed another platform's output, so do not accept it.

Then:

```sh
make test-unit
make test-scenarios
cd web/console && npm run typecheck && npm run test:run
```

These checks cover the new platform automatically:

- `TestCapabilityMatrix` runs it against every servable generation.
- `TestCapabilityMatrixCoversEverySupportedPlatform` and `TestEveryPlatformIsComplete` check that its module is whole.
- `TestCapabilityMatrix_CanariesChangeNothing` proves that registering a platform changes nothing else.

## 5. The PR

Label the PR `platform-change/<name>`. golden-guard requires it, because the PR adds goldens.
