# How to add a UCP spec version

A UCP spec generation (e.g. `2026-08-25`) is one self-contained module, `internal/spec/vYYYYMMDD`, registered by one line in `internal/spec/all`. Adding one is new code plus that line: no established generation, platform or plane changes (sarvex-v1-architecture.md, `docs/plans/version-platform-modularity.md`).

## 1. Scaffold it

```sh
make new-spec-version V=2026-12-01
```

This copies the registry's latest generation into `internal/spec/v20261201`. It rewrites every spelling of the old name (`2026-08-25`, `v20260825`, `V20260825`) in contents and file names, starts a fresh `DISCREPANCIES.md` and adds the generation to `internal/spec/all/all.go`. The copy builds and passes the boundary check (`internal/archtest`). Until you change it, it serves exactly what its template serves.

## 2. Make it say what the spec says

| Package | What it is | What to change |
|---|---|---|
| `internal/spec/vX` (root) | The generation's schema types, plus `Release` in `release.go` | Bring the types in line with the published JSON schemas. |
| `extensions/` | Its extensions' wire types | Add, remove or reshape extensions. |
| `wire` | Its profile document form (e.g. `keys` vs `signing_keys`) | The document's shape. It converts to and from `internal/pkg/ucp/wire`, the generation-neutral profile. |
| `profile` | Its `generation.Generation` | Reads and writes that document. |
| `codec` | Its `wireadapt.Codec` | Translates the canonical model (`internal/pkg/commerce/model`) to and from this generation's checkout, cart, order and catalog (`codec/catalog.go`), and stamps its `ucp` envelope. |
| `release` | Its `spec.Version` | `Serves()` lists every capability and extension Sarvex renders in this generation, and nothing else. Negotiation and the profile advertise only what is listed. |

Rules:

- **Everything version-specific lives in the module.** Callers go through the registry (`spec.Registry`: `Latest`, `Baseline`, `Serving(capability)`, `Servable`, `Codecs`, `Generations`) and never `switch` on a version. If shared code seems to need an `if version == …`, the difference belongs in this codec.
- **The canonical model is not this generation's shape.** If the generation can say something the canonical model cannot, translate it in the codec. Widening the canonical model is a separate decision, because connectors persist it into merchant orders.
- **Record every gap** between the published schema and its prose, and every deliberate departure, in `DISCREPANCIES.md`.
- A capability the generation cannot support yet is left out of `Serves()`. Then the profile and negotiation do not advertise it there (sarvex-v1-architecture.md).

## 3. Goldens and tests

```sh
UPDATE_GOLDENS=1 go test ./internal/planes/...
git status --short '**/testdata/golden'
```

Expect two kinds of change, and nothing else:

- **New files:** the new generation's directory under each golden tree.
- **Additions to the published profile goldens** (`internal/planes/serving/testdata/golden/profile/*.json`): each capability gains an entry at the new version, because the profile advertises every generation that serves it.

Any other change to an existing golden means the new generation changed an existing one's output. That includes an edited or removed entry in the profile goldens. It is a bug in the new module, so do not accept it.

Then:

```sh
make test-unit
make test-scenarios
cd web/console && npm run test:run
```

These checks cover the new generation automatically:

- `TestEveryGenerationIsComplete` checks that it has an id, a codec, a profile and a non-empty `Serves`.
- `TestCatalog_EveryGeneration` covers its catalog reads.
- `TestCapabilityMatrix` runs every platform × servable generation.
- The canary tests (`TestCanaryGenerationChangesNoExistingOutput`, `TestCapabilityMatrix_CanariesChangeNothing`) prove that registering a generation changes nothing else.

## 4. The PR

Label the PR `spec-change/vYYYYMMDD`. golden-guard requires it, because the PR adds goldens.

If the generation becomes the one to author the catalogue at, or changes `Baseline`, call that out in the PR description. Both are registry facts (first and last registered), not constants.
