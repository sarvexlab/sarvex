# Entity ID generation: scheme selection by entity type

Different kinds of entities use different ID-generation schemes. The scheme is selected based on the entity's role and lifecycle: compound deterministic keys for tenant-scoped singletons, prefixed ULIDs for append-only records, prefixed nanoid for user-facing short IDs, and bare UUIDs for general-purpose resource IDs.

The codebase supports four coexisting, internally-consistent ID schemes. Each emerged to serve a specific purpose: deterministic keys ensure singular identity for aggregates that are recreated on the same tenant boundary, ULIDs provide natural ordering for audit trails, nanoid provides short user-facing identifiers, and UUID provides standard randomness for generic resources. Rather than unifying to a single scheme (which would lose useful semantic signals), the schemes are codified as a selection rule.

## Scheme selection matrix

| Entity type | Scheme | Example | Rationale |
|---|---|---|---|
| Tenant-scoped singletons & aggregates | Compound deterministic key | `readiness:store_12345:v2`, `checklist:store_12345`, `channel:store_12345:2026-08` | Identity is naturally determined by tenant + version/time; recreating the entity must preserve its identity; determinism allows idempotent upserts. |
| Audit-log / append-only records | Prefixed ULID | `alert_01ARZ3NDEKTSV4RRFFQ69G5FAV`, `ledger_01ARZ3NDEKTSV4RRFFQ70H5FAW` | ULID provides microsecond-precision ordering; prefix clarifies entity type in logs/traces; append-only semantics guarantee no recreations. |
| User-facing, short IDs | Prefixed nanoid | `merchant_V1StGXR8_Z5j` | Compact, URL-safe, occasionally displayed to users; prefix disambiguates type. Merchants/store IDs fall here. |
| General-purpose resource IDs | Bare UUID | `550e8400-e29b-41d4-a716-446655440000` | Standard, widely understood, sufficient randomness for resources with no special identity constraints. Stores, sessions, jobs, validation runs. |

## Considered options

**Standardize on one scheme going forward**: Use a single scheme (e.g., ULID everywhere for sortability, or UUID everywhere for simplicity) for all new entities, migrating nothing existing. Simpler decision-making for new features but loses the semantic signal that a prefixed ID currently carries about entity type and lifecycle.

## Consequences

- New entities must select their ID scheme based on the matrix above. This is a lint/review checklist item.
- Code review should flag ID choices that don't fit the matrix (e.g., a singleton using bare UUID, or an audit record using nanoid).
- No migration of existing IDs is required; the four schemes coexist and are internally sound.
- The matrix should be updated if a new entity class emerges that doesn't fit the four categories.
