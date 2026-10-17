# Plane boundary enforcement via depguard

The three-plane isolation invariant is mechanically enforced via `depguard` linter with explicit per-plane deny rules. Planes must not import packages from other planes; cross-plane communication flows only through the event bus.

The plane-isolation invariant is stated throughout the codebase (sarvex-v1-architecture.md, architecture, plane doc.go files) but was previously unenforced at the toolchain level. Over the course of initial development, multiple cross-plane imports accumulated despite the stated rule, creating a structural risk: as the codebase grows, such violations become harder to detect in review alone.

`depguard` is already present in the Go linter toolchain (`.golangci.yml`) but was disabled. Enabling it with explicit per-plane rules provides mechanical enforcement at lint time, which runs in CI before merge.

## Considered options

**Custom `go test`-based import-graph check**: A bespoke tool that walks `go list -deps` output per package and asserts no forbidden edges. More flexible than `depguard` (can express narrower rules like "only read-only cache clients may cross"), but requires writing and maintaining custom tooling that duplicates what `depguard` already does for the package-level case.

**Formalize read-only cross-plane access as an exception**: Acknowledge that certain read-only cache-access patterns have been deliberately engineered and codify them as an explicit exception. Weaker mechanically (the line between read and write is easy to blur in review without tooling that distinguishes them), but more honest about the current state if such reads are genuinely necessary.

## Consequences

- `depguard` is enabled in `.golangci.yml` with the following deny rules:
  - `internal/planes/serving` cannot import `internal/planes/sync` or `internal/planes/control`
  - `internal/planes/sync` cannot import `internal/planes/control` or `internal/planes/serving`
  - `internal/planes/control` cannot import `internal/planes/sync` or `internal/planes/serving`
  - No `internal/planes/**` packages can be imported by `internal/pkg/**`
- Any cross-plane cache-client interfaces remain in `internal/pkg` (shared, read-only contracts that all planes may use).
- `golangci-lint` failures on import violations block PR merge. Existing violations must be resolved before the check is enabled.
