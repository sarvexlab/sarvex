# Sync-plane signal de-dup window persistence via Redis

The sync-plane's de-duplication window for inbound connector signals persists in Redis rather than as in-process memory. This ensures that duplicate re-ingestions after a sync-plane restart do not cause redundant catalog processing.

The signal ingestion pipeline maintains a de-dup window (`storeGate`) to prevent duplicate catalog operations when signals arrive multiple times. Previously this window existed only in process memory and reset on every sync-plane restart. While the reconciliation sweeper serves as a safety net for genuinely missed signals, it does not protect against duplicates re-ingested after a restart boundary — the sweeper pulls missing data, not replayed signals.

Backing the de-dup window with Redis reuses an existing pattern already present in the codebase (the `dedup:%s` key-prefix convention used elsewhere for similar de-duplication concerns). This provides persistence across restarts without introducing new infrastructure or patterns.

## Considered options

**Leave as in-process memory**: Rely on the reconciliation sweeper as the safety net. A duplicate re-ingestion after a restart-boundary redelivery is a redundant catalog upsert (idempotent by construction), so the risk is bounded by how often sync-plane restarts coincide with in-flight redeliveries — plausibly rare and low-consequence when it happens. Zero engineering cost.

**Use application-level timeout or timestamp checks**: Rather than storing dedup state, validate incoming signals against recent catalog modification timestamps to reject near-duplicates. Simpler than Redis but requires coordinating timestamps across signal metadata and catalog records.

## Consequences

- The `storeGate` de-dup map in sync-plane signal ingestion is backed by Redis keys with the `dedup:signal:<store_id>:<sequence>` pattern.
- De-dup entries expire after a configured TTL (suggested: 24 hours, aligned with typical signal batch windows); expired entries are garbage-collected by Redis.
- Sync-plane restarts no longer reset the de-dup state — the window persists across deployments.
- This adds a Redis dependency to the signal ingestion hot path; latency should be monitored to ensure it remains acceptable. A missing Redis connection should not block signal processing (fall back to in-process de-dup with a warning).
- The existing `dedup:%s` pattern documentation should be updated to include signal de-duplication as a use case.
