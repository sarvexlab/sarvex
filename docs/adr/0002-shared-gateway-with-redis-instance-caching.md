# Shared/centralized gateway with Redis instance caching

All merchant traffic flows through a single shared gateway fleet. Merchant configuration (`UCPInstance`) is cached in Redis with write-through invalidation rather than fetched from MongoDB on every request.

The `Deployment` aggregate has `cloudRunServiceName`, `cloudRunURL`, and `cdnEndpoint` fields that suggest per-merchant deployments. We are not doing this in Phase 1. A dedicated gateway per merchant is the right eventual model (Phase 3), but for early-stage operations the overhead of N services per N merchants is unjustifiable. The shared fleet is simpler to operate, faster to ship, and sufficient for the initial merchant base.

The gateway resolves a `UCPInstance` on every inbound request via `ucpService.GetByID()`. Without caching this is a MongoDB round-trip in the hot path. Redis with write-through invalidation (the control plane invalidates the cache entry on every config save) gives sub-millisecond reads with bounded staleness. Phase 2 replaces write-through with event-driven cache invalidation when the `InstanceUpdated` event pipeline is built.

## Consequences

- The `cloudRunServiceName`, `cloudRunURL`, and `cdnEndpoint` fields on `Deployment` are always empty in Phase 1. An empty value must not be interpreted as a failed deployment — check `DeploymentStatus` instead.
- Config changes (including trusted platform updates) propagate to the gateway within one Redis TTL cycle. This is intentional — merchants do not need to redeploy to change runtime config.
- Phase 3 migrates to a hybrid model: shared control plane, per-merchant data plane. The `Deployment` fields are forward scaffolding for that migration.
