# Control-plane API: Go code as source of truth with generated OpenAPI

The Go implementation of the control-plane HTTP surface is the authoritative source of truth. The `sarvex-v1-control-plane.yaml` OpenAPI specification is generated from Go route registrations and handler-level annotations rather than hand-maintained. This eliminates drift between code and spec.

The architecture document states that "where the code and this document disagree, the code wins and this document is the bug." However, the control-plane API contract (`sarvex-v1-control-plane.yaml`) was previously treated as hand-authored, creating a second source of truth. Audit finding F-038 revealed that the YAML contained internal inconsistencies and diverged from the code in six endpoint groups, proving that hand-maintenance of a parallel spec is error-prone.

Reversing the direction of derivation—generating the YAML from the code—makes code the single source of truth for the control-plane surface, consistent with the architecture principle. The frontend and external consumers derive their knowledge from the generated spec, which is always correct by construction.

## Considered options

**Treat the YAML as authoritative with CI enforcement**: Require every code change to update the spec in the same PR, enforced by a CI check (like the frontend's existing drift check). Enforces contract-first discipline but requires fixing current drift before the check can be enabled.

**Keep both hand-authored and stop silently when they disagree**: Treat disagreement as a stop-and-report condition per CLAUDE.md §3, but rely on humans to catch the disagreement early. This is what the rule already says to do; the gap is lack of automation to catch violations before merge.

## Consequences

- Control-plane route handlers and response types are annotated with metadata (handler path, method, request/response shapes, status codes). This follows the pattern of tools like `swaggo` (Swagger + Go comments).
- A build-time code generator (or CI step) walks these annotations and produces `sarvex-v1-control-plane.yaml`. The schema is regenerated as part of the build.
- `sarvex-v1-control-plane.yaml` becomes a **generated artifact**, not a hand-edited source file. It is checked into version control (for visibility in diffs and CI logs) but human edits to it are discouraged — changes must be made to the Go annotations.
- The frontend's `npm run gen:api` step continues to regenerate `schema.d.ts` from the YAML, which is now always in sync with the code.
- Developers making API changes update the Go code and its annotations in one place; the spec is automatically correct.
- The generated YAML must be reproducible and deterministic (consistent field ordering, no timestamps) so that diffs show only intentional changes.
- Current hand-authored YAML discrepancies must be resolved and brought into sync with the code before the generator is turned on.
