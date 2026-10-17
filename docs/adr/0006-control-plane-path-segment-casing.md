# Control-plane path-segment casing: hyphen convention

The control-plane HTTP API uses hyphenated path segments (e.g., `checkout-sessions`, `payment-config`) for consistency with the dominant convention across the rest of the control-plane surface. The previous underscore-based `checkout_sessions` endpoint is renamed to `checkout-sessions`.

The control-plane OpenAPI specification (`sarvex-v1-control-plane.yaml`) predominantly uses hyphens in path segments (`stores`, `catalog-products`, `payment-config`, `feed-config`, etc.). The `checkout_sessions` endpoint was the sole underscore outlier, creating inconsistency with the codebase's own stated pattern and causing a spelling collision with the serving-plane's `checkout-sessions` endpoint (which is fixed by the external ucp.dev spec).

Standardizing on hyphens reduces cognitive load, eliminates the naming collision, and aligns the control-plane API with REST conventions (hyphens preferred over underscores in URLs per RFC 3986 guidance and common REST practice).

## Considered options

**Leave as-is and document the exception**: Keep `checkout_sessions` and add a comment explaining the inconsistency. Zero engineering cost but permanently enshrines an outlier with no functional justification.

**Standardize on underscore instead**: Rename all hyphenated paths to use underscores. Also a breaking change but moves away from the majority pattern — no obvious benefit.

## Consequences

- The control-plane endpoint path changes from `POST /checkout_sessions` to `POST /checkout-sessions`.
- This is a breaking API change requiring coordinated updates to:
  - `sarvex-v1-control-plane.yaml` (OpenAPI spec)
  - Go route registrations in `internal/planes/control/api`
  - Generated TypeScript types in `web/console/src/api/schema.d.ts` (regenerated via `npm run gen:api`)
  - Console API calls in `web/console/src/api/` modules
- The change should be deployed with the console frontend to avoid breakage during the transition.
- Any external consumers of the control-plane API must be notified of the path change.
